package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/terminal"

	"github.com/wei-dong/tako/internal/hostlib"
	"github.com/wei-dong/tako/internal/i18n"
	"github.com/wei-dong/tako/internal/importer/sshconfig"
	"github.com/wei-dong/tako/internal/inputbus"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/monitor"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/session"
	"github.com/wei-dong/tako/internal/settings"
	"github.com/wei-dong/tako/internal/store"
	"github.com/wei-dong/tako/internal/transfer"
	tssh "github.com/wei-dong/tako/internal/transport/ssh"
	tui "github.com/wei-dong/tako/internal/ui"
	"github.com/wei-dong/tako/internal/workspace"
)

// SaveDelay is how long the session waits after a change before writing
// session.json.
const SaveDelay = time.Second

// maxParallelDials bounds simultaneous connection attempts, so restoring many
// blocks does not open every connection in the same instant.
const maxParallelDials = 4

// Options configure [New]. Only Dirs is required.
type Options struct {
	Dirs store.Dirs
	// Keychain stores secrets; nil uses the system keychain.
	Keychain secrets.Store
	// Locale is the system locale for the default language.
	Locale  string
	Version string
	// SSHConfig is the file the import reads; "" means ~/.ssh/config.
	SSHConfig string
	// Home expands "~" in imported paths; "" means the user's home.
	Home string
	// SaveDelay overrides [SaveDelay] (tests).
	SaveDelay time.Duration
	// Backoff overrides the reconnect schedule (tests).
	Backoff []time.Duration
	// ResizeDelay overrides how long a terminal size must stay still before
	// the shell is told (tests).
	ResizeDelay time.Duration
	// AgentConn overrides the SSH agent connection (tests).
	AgentConn func() (net.Conn, error)
	// PickFile is the native file chooser.
	PickFile func(title string) (string, error)
	// Files overrides the file endpoints of the transfer sheet and queue
	// (tests); nil reaches hosts over SSH.
	Files transfer.Provider
	// OnQuit is called when the window asks to quit.
	OnQuit func()
}

// App is the running application.
type App struct {
	UI       *tui.App
	Model    *workspace.Model
	Library  *hostlib.Library
	Secrets  *secrets.Layered
	Settings *settings.Settings
	Prompts  *tui.Prompts
	// Monitor measures the hosts that have a block; Transfers moves files.
	Monitor   *monitor.Hub
	Transfers *transfer.Queue

	fleet   *fleet
	dirs    store.Dirs
	opts    Options
	lock    *store.Lock
	cancel  context.CancelFunc
	saver   *store.Saver
	doc     store.Doc
	snap    atomic.Pointer[workspace.State]
	wake    atomic.Pointer[func()]
	closeFn func()
	closed  atomic.Bool
	// restored is set once Restore ran: before that the saved session must
	// not be overwritten with an empty one.
	restored bool
}

// New opens the stores and builds the workspace. Call [App.Restore] once the
// UI is ready, and [App.Close] on exit.
func New(parent context.Context, o Options) (*App, error) {
	if err := o.Dirs.Ensure(); err != nil {
		return nil, fmt.Errorf("preparing data directories: %w", err)
	}
	lock, err := store.Acquire(o.Dirs.Config)
	if err != nil {
		return nil, fmt.Errorf("another Tako instance is using %s: %w", o.Dirs.Config, err)
	}
	ctx, cancel := context.WithCancel(parent)
	a := &App{dirs: o.Dirs, opts: o, lock: lock, cancel: cancel}
	ok := false
	defer func() {
		if !ok {
			a.Close()
		}
	}()

	a.closeFn, err = setupLogging(o.Dirs)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{o.Dirs.Hosts(), o.Dirs.Session()} {
		if err := store.RotateBackups(p); err != nil {
			slog.Warn("rotating backups failed", "file", filepath.Base(p), "err", err)
		}
	}

	keychain := o.Keychain
	if keychain == nil {
		keychain = secrets.NewKeychain()
	}
	a.Secrets = secrets.NewLayered(keychain)
	if a.Library, err = hostlib.Open(
		store.Doc{Path: o.Dirs.Hosts(), Version: hostlib.Version}, a.Secrets,
		func(err error) { slog.Error("saving hosts failed", "err", err) },
	); err != nil {
		return nil, fmt.Errorf("opening the host library: %w", err)
	}
	if a.Settings, err = settings.Open(filepath.Join(o.Dirs.Config, "settings.json")); err != nil {
		return nil, fmt.Errorf("opening settings: %w", err)
	}
	a.Prompts = tui.NewPrompts()
	a.Prompts.SetWake(a.doWake)

	dialer := &tssh.Dialer{
		KnownHosts: tssh.NewKnownHosts(o.Dirs.KnownHosts()),
		Secrets:    a.Secrets,
		Prompter:   a.Prompts,
		Lookup:     a.Library.Get,
		Remembered: a.remembered,
		AgentConn:  o.AgentConn,
	}
	limit := make(chan struct{}, maxParallelDials)

	a.doc = store.Doc{Path: o.Dirs.Session(), Version: workspace.StateVersion}
	delay := o.SaveDelay
	if delay == 0 {
		delay = SaveDelay
	}
	a.saver = store.NewSaver(delay, a.writeSession, func(err error) { slog.Error("saving session failed", "err", err) })

	bus := inputbus.New(ctx)
	a.Model = workspace.New(workspace.Deps{
		Ctx: ctx, Bus: bus,
		NewSession: func(h model.Host) *session.Session {
			return session.New(session.Options{
				Host: h, Dialer: dialer, Limit: limit, Backoff: o.Backoff, ResizeDelay: o.ResizeDelay,
				StartDir: h.StartDir, StartCmd: h.StartCmd, Notice: a.notice,
			})
		},
		Host:    a.Library.Get,
		Theme:   func() *terminal.Theme { return a.UI.Tokens().Terminal() },
		Post:    func(fn func()) { a.UI.Post(fn) },
		Changed: a.doWake,
		Save:    a.requestSave,
	})
	connector := &Connector{Dialer: &tssh.Dialer{
		KnownHosts: dialer.KnownHosts, Secrets: a.Secrets, Prompter: a.Prompts,
		Lookup: a.Library.Get, AgentConn: o.AgentConn,
	}}
	a.fleet = newFleet(&tssh.Dialer{
		KnownHosts: dialer.KnownHosts, Secrets: a.Secrets, Prompter: a.Prompts,
		Lookup: a.Library.Get, Remembered: a.remembered, AgentConn: o.AgentConn,
	}, a.Library.Get, o.AgentConn)
	prefs := a.Settings.Values()
	a.Monitor = monitor.NewHub(monitor.HubOptions{
		Interval: time.Duration(prefs.Monitor.Interval()) * time.Second,
		OnChange: func(model.ID) { a.doWake() },
	})
	var files transfer.Provider = a.fleet
	if o.Files != nil {
		files = o.Files
	}
	topts := transfer.Options{Provider: files, Concurrency: prefs.Transfer.Parallel()}
	if prefs.Transfer.AllowDirect {
		topts.Direct = &transfer.Direct{Env: a.fleet}
	}
	a.Transfers = transfer.New(topts)
	a.UI = tui.New(tui.Deps{
		Monitor: a.Monitor, Transfers: a.Transfers, Files: files,
		SetDirect: a.setDirect,
		Model:     a.Model, Library: a.Library, Secrets: a.Secrets, Connector: connector,
		Prompts: a.Prompts, Settings: a.Settings, Locale: o.Locale, Wake: a.doWake,
		PickFile: o.PickFile, OnQuit: o.OnQuit, Version: o.Version,
		ImportSSHConfig: a.importSSHConfig,
	})
	a.Transfers.Subscribe(a.UI.OnJob)
	ok = true
	return a, nil
}

func (a *App) doWake() {
	if f := a.wake.Load(); f != nil {
		(*f)()
	}
}

// SetWake sets the function that redraws the window from any goroutine.
func (a *App) SetWake(fn func()) { a.wake.Store(&fn) }

// Restore reopens the blocks and tabs saved by the previous run.
func (a *App) Restore() {
	a.restored = true
	var st workspace.State
	switch err := a.doc.Load(&st); {
	case err == nil:
		n := a.Model.Restore(st)
		slog.Info("session restored", "blocks", n)
	case errors.Is(err, store.ErrNotExist):
	default:
		slog.Warn("session file unreadable, starting empty", "err", err)
	}
}

// requestSave runs on the UI goroutine after a change: it takes the snapshot
// there and the saver writes it later from its own goroutine.
func (a *App) requestSave() {
	if a.closed.Load() {
		return
	}
	st := a.Model.Snapshot()
	a.snap.Store(&st)
	a.saver.Request()
}

func (a *App) writeSession() error {
	st := a.snap.Load()
	if st == nil {
		return nil
	}
	if err := a.doc.Save(*st); err != nil {
		return fmt.Errorf("writing session: %w", err)
	}
	return nil
}

// remembered stores the keychain reference the user just chose in the host.
func (a *App) remembered(h model.Host, idx int, ref string) {
	cur, ok := a.Library.Get(h.ID)
	if !ok || idx >= len(cur.Auth) || cur.Auth[idx].SecretRef == ref {
		return
	}
	cur.Auth[idx].SecretRef = ref
	if err := a.Library.Update(cur); err != nil {
		slog.Warn("recording the saved secret failed", "host", h.Name, "err", err)
	}
}

func (a *App) importSSHConfig() ([]model.Host, []string, error) {
	path := a.opts.SSHConfig
	home := a.opts.Home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if path == "" {
		path = filepath.Join(home, ".ssh", "config")
	}
	hosts, warns, err := sshconfig.ParseFile(path, sshconfig.Options{Home: home})
	if err != nil {
		return nil, nil, fmt.Errorf("importing %s: %w", path, err)
	}
	out := make([]string, len(warns))
	for i, w := range warns {
		out[i] = w.String()
	}
	return hosts, out, nil
}

// Close saves the session and stops everything. Call it on the UI goroutine:
// it takes the final snapshot of the workspace. It is safe to call twice.
func (a *App) Close() {
	if a.closed.Load() {
		return
	}
	if a.restored && a.Model != nil {
		st := a.Model.Snapshot()
		a.snap.Store(&st)
		a.saver.Request()
	}
	a.closed.Store(true)
	if a.Prompts != nil {
		a.Prompts.CancelAll()
	}
	if a.saver != nil {
		a.saver.Flush()
		a.saver.Close()
	}
	if a.Model != nil {
		a.Model.Close()
	}
	a.cancel()
	if a.Transfers != nil {
		a.Transfers.Close()
	}
	if a.Monitor != nil {
		a.Monitor.Close()
	}
	if a.fleet != nil {
		a.fleet.close()
	}
	if a.Library != nil {
		a.Library.Close()
	}
	if a.closeFn != nil {
		a.closeFn()
	}
	if a.lock != nil {
		if err := a.lock.Release(); err != nil {
			slog.Warn("releasing the lock failed", "err", err)
		}
	}
}

// notice is the line printed into a pane when its connection drops or comes
// back.
func (a *App) notice(ev session.Event, at time.Time) []byte {
	zh := a.Settings.Values().ResolveLanguage(a.opts.Locale) == i18n.Chinese
	var text string
	switch {
	case ev == session.EventLost && zh:
		text = "连接中断 " + at.Format("15:04:05")
	case ev == session.EventLost:
		text = "connection lost " + at.Format("15:04:05")
	case zh:
		text = "已重新连接 " + at.Format("15:04:05")
	default:
		text = "reconnected " + at.Format("15:04:05")
	}
	return []byte("\r\n\x1b[2m── " + text + " ──\x1b[0m\r\n")
}

// setDirect turns direct server-to-server transfers on or off for jobs added
// from now on.
func (a *App) setDirect(on bool) {
	a.Transfers.SetDirect(a.directRunner(on))
}

func (a *App) directRunner(on bool) transfer.DirectRunner {
	if !on {
		return nil
	}
	return &transfer.Direct{Env: a.fleet}
}
