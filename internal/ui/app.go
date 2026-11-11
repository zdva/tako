// Package ui is Tako's window, built with MyGo's native UI toolkit. It holds
// the view code and its state, and no business logic: hosts, sessions and
// layout come from the packages it is given in [Deps].
package ui

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/hostlib"
	"github.com/wei-dong/tako/internal/i18n"
	"github.com/wei-dong/tako/internal/layout"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/monitor"
	"github.com/wei-dong/tako/internal/scheme"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/settings"
	"github.com/wei-dong/tako/internal/theme"
	"github.com/wei-dong/tako/internal/transfer"
	"github.com/wei-dong/tako/internal/workspace"
)

// TestResult is what a successful "test connection" learned.
type TestResult struct {
	Latency time.Duration
	// OS is the detected system, empty when detection failed.
	OS     model.OSKind
	OSName string
}

// Connector tries a host without opening a tab.
type Connector interface {
	Test(ctx context.Context, h model.Host) (TestResult, error)
}

// Deps are the services the window uses.
type Deps struct {
	Model     *workspace.Model
	Library   *hostlib.Library
	Secrets   *secrets.Layered
	Connector Connector
	Prompts   *Prompts
	Settings  *settings.Settings
	// ImportSSHConfig reads ~/.ssh/config; the strings are its warnings.
	ImportSSHConfig func() ([]model.Host, []string, error)
	// Locale is the system locale ("zh_CN.UTF-8"), for the default language.
	Locale string
	// Wake redraws the window from any goroutine.
	Wake func()
	// PickFile shows a native file chooser (nil in tests); it blocks.
	PickFile func(title string) (string, error)
	// PickSave shows a native "save as" chooser (nil in tests); it blocks.
	PickSave func(title, name string) (string, error)
	// ImportHosts reads a host file or folder of another terminal (Xshell,
	// SecureCRT, Termius) or a Tako export; ExportHosts writes the library
	// without secrets; ImportColors reads an iTerm2 color preset.
	ImportHosts  func(path string) (ImportSummary, error)
	ExportHosts  func(path string) error
	ImportColors func(path string) (scheme.Scheme, error)
	// DefaultExport is the path the export field starts with.
	DefaultExport string
	// Paths are the folders shown on the Data page.
	Paths struct{ Config, Logs string }
	// Diagnostics returns the text "Copy diagnostics" puts on the clipboard.
	Diagnostics func() string
	// CrashNotice describes the crash log of the previous run, or "".
	CrashNotice func() string
	// CheckUpdates opens the update window; UpdateAvailable is the newer
	// version found by a check, or "".
	CheckUpdates    func()
	UpdateAvailable func() string
	// OnQuit asks the app to quit.
	OnQuit func()
	// Version is shown in the settings sheet.
	Version string

	// Monitor measures the hosts that have a block; nil turns monitoring off.
	Monitor *monitor.Hub
	// Transfers runs file transfers and Files lists directories for the
	// transfer sheet; SetDirect switches direct mode on or off.
	Transfers *transfer.Queue
	Files     transfer.Provider
	SetDirect func(on bool)
}

// App is the state of the window.
type App struct {
	d Deps

	tr *i18n.Bundle
	tk theme.Tokens

	inboxMu sync.Mutex
	inbox   []func()

	sidebarCollapsed bool
	filter           string
	collapsedGroups  map[string]bool

	sheet    hostSheet
	palette  paletteState
	settings settingsState
	// focusTab asks the next frame to give a tab's terminal the keyboard.
	focusTab *workspace.Tab
	// confirmDelete is the host awaiting delete confirmation.
	confirmDelete *model.Host
	deleteErr     string
	pendingToast  string
	// sshConfigHosts caches the hosts ~/.ssh/config would import, for the
	// empty state's card.
	sshConfigHosts  []model.Host
	sshConfigLoaded bool
	pendingClip     string

	// Main area state: the divider being dragged, the viewport of the last
	// frame (a change of it makes the blocks follow at once instead of
	// gliding), the minimap, the rename dialog and the terminal search.
	div          dividerDrag
	lastViewport layout.Size
	mm           minimapState
	rename       renameState
	find         findState
	mon          monitorUI
	xfer         transferUI
	// jobState is what OnJob last saw, so a progress tick wakes the window
	// only while the sheet shows it.
	jobMu    sync.Mutex
	jobState map[int]transfer.State
	xferOpen atomic.Bool
	// heldMods are modifiers the pointer events do not carry: tests set
	// them, since the Tester's pointer events have none.
	heldMods ui.Modifiers
	look     termLook
	// loc formats numbers and dates as the system region does.
	loc i18n.Locale
}

// New returns the window's state.
func New(d Deps) *App {
	a := &App{d: d, collapsedGroups: map[string]bool{}}
	a.sheet.init(a)
	a.mm.menuIdx, a.mm.hover = -1, -1
	v := d.Settings.Values()
	a.tr = i18n.Must(v.ResolveLanguage(d.Locale))
	a.loc = i18n.ParseLocale(d.Locale)
	return a
}

// Post runs fn on the UI goroutine in the next frame. It is safe from any
// goroutine, and is what session and terminal callbacks use.
func (a *App) Post(fn func()) {
	a.inboxMu.Lock()
	a.inbox = append(a.inbox, fn)
	a.inboxMu.Unlock()
	if a.d.Wake != nil {
		a.d.Wake()
	}
}

func (a *App) drain() {
	a.inboxMu.Lock()
	fns := a.inbox
	a.inbox = nil
	a.inboxMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// SetPaths tells the Data page where the settings and the logs are.
func (a *App) SetPaths(config, logs string) { a.d.Paths.Config, a.d.Paths.Logs = config, logs }

// Tokens returns the palette in use.
func (a *App) Tokens() theme.Tokens { return a.tk }

// T translates a key.
func (a *App) T(key string, args ...any) string { return a.tr.T(key, args...) }

// syncPrefs applies the saved language and appearance.
func (a *App) syncPrefs(c *ui.Context) {
	v := a.d.Settings.Values()
	if lang := v.ResolveLanguage(a.d.Locale); lang != a.tr.Language() {
		a.tr = i18n.Must(lang)
	}
	dark := c.Theme().Dark // the window's own theme follows the desktop
	switch v.ResolveAppearance() {
	case settings.AppearanceDark:
		dark = true
	case settings.AppearanceLight:
		dark = false
	case settings.AppearanceSystem:
	}
	tk := theme.Light()
	if dark {
		tk = theme.Dark()
	}
	if sc, ok := scheme.Find(v.Terminal.Scheme, v.Terminal.Custom); ok {
		tk = tk.WithScheme(sc)
	}
	// Terminals keep their own copy of the colors and the font, so a change
	// is pushed to them once, not drawn from the tokens every frame.
	look := termLook{dark: dark, scheme: v.Terminal.Scheme, font: v.Terminal.Font(),
		copyOnSelect: v.Terminal.CopyOnSelect, contrast: v.Terminal.KeepContrast}
	if len(v.Terminal.Custom) > 0 && tk.Scheme != nil {
		look.custom = tk.Scheme.Background + tk.Scheme.Foreground + tk.Scheme.Palette[1]
	}
	a.tk = tk
	if look != a.look {
		a.look = look
		if a.d.Model != nil {
			a.d.Model.RestyleTerminals()
		}
	}
}

// termLook is what the terminals were last styled with.
type termLook struct {
	dark         bool
	scheme       string
	custom       string
	font         int
	copyOnSelect bool
	contrast     bool
}

// Build is the window's view function.
func (a *App) Build(c *ui.Context) {
	a.drain()
	a.syncPrefs(c)
	a.syncMonitors()
	ui.Themed(c, a.tk.UI()).Fill().Background(a.tk.Bg).Children(func() {
		a.shortcuts(c)
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if !a.sidebarCollapsed {
				a.sidebar(c)
			}
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.toolbar(c)
				ui.Column(c).Key("stage").Grow(1).MinHeight(0).Children(func() {
					a.main(c)
					if a.mon.host != "" {
						a.monitorPage(c)
					}
				})
				a.statusBar(c)
			})
		})
		a.sheet.build(c, a)
		a.paletteView(c)
		a.settingsView(c)
		a.transferSheet(c)
		a.promptView(c)
		a.deleteView(c)
		a.renameView(c)
	})
}
