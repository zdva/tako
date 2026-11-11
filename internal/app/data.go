package app

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wei-dong/tako/internal/diag"
	"github.com/wei-dong/tako/internal/importer/hostfile"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/scheme"
	tui "github.com/wei-dong/tako/internal/ui"
)

// importHosts reads a file or folder of hosts (Xshell, SecureCRT, Termius or
// a Tako export) into the library. Passwords in the source are never read.
func (a *App) importHosts(path string) (tui.ImportSummary, error) {
	opts := hostfile.Options{}
	if u, err := user.Current(); err == nil {
		opts.DefaultUser = u.Username
	}
	res, err := hostfile.Load(expandHome(path, a.opts.Home), opts)
	sum := tui.ImportSummary{Format: string(res.Format), Warnings: res.Warnings, Passwords: res.Passwords}
	if err != nil {
		return sum, fmt.Errorf("importing hosts: %w", err)
	}
	added, skipped, err := a.Library.AddMany(res.Hosts)
	sum.Added, sum.Skipped = len(added), skipped
	for _, t := range res.Tags {
		if t.Color != "" && a.Library.TagColor(t.Name) == "" {
			a.Library.SetTagColor(t.Name, t.Color)
		}
	}
	if err != nil {
		return sum, fmt.Errorf("importing hosts: %w", err)
	}
	a.doWake()
	return sum, nil
}

// exportHosts writes the library to path without any secret or reference to
// one, readable by the owner only.
func (a *App) exportHosts(path string) error {
	data, err := a.Library.Export()
	if err != nil {
		return fmt.Errorf("exporting hosts: %w", err)
	}
	path = expandHome(path, a.opts.Home)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// importColors reads an iTerm2 color preset.
func (a *App) importColors(path string) (scheme.Scheme, error) {
	path = expandHome(path, a.opts.Home)
	f, err := os.Open(path)
	if err != nil {
		return scheme.Scheme{}, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()
	buf := make([]byte, scheme.MaxITermSize+1)
	n, _ := f.Read(buf)
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	s, err := scheme.ParseITerm(buf[:n], name)
	if err != nil {
		return scheme.Scheme{}, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}
	return s, nil
}

func expandHome(path, home string) string {
	path = strings.TrimSpace(path)
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	if home == "" {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			return path
		}
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// diagnostics builds the text "Copy diagnostics" puts on the clipboard.
func (a *App) diagnostics() string {
	v := a.Settings.Values()
	var names []string
	for _, h := range a.Library.Hosts() {
		names = append(names, h.Name, h.Addr, h.User)
	}
	home, _ := os.UserHomeDir()
	if u, err := user.Current(); err == nil {
		names = append(names, u.Username)
	}
	in := diag.Info{
		Version: a.opts.Version, Locale: a.opts.Locale, Language: string(v.ResolveLanguage(a.opts.Locale)),
		Appearance: string(v.ResolveAppearance()), Display: displayServer(),
		Hosts: a.Library.Len(), Blocks: len(a.Model.Blocks()), Started: a.started,
		Names: names, Home: home,
	}
	var report string
	if a.opts.Crashes != nil {
		if rs := a.opts.Crashes.Reports(); len(rs) > 0 {
			report, _ = diag.ReadReport(rs[0], 6000)
		}
	}
	return diag.Build(in, diag.Tail(filepath.Join(a.dirs.Logs(), "tako.log"), 60, 64<<10), report)
}

func displayServer() string {
	switch {
	case runtime.GOOS != "linux":
		return runtime.GOOS
	case os.Getenv("WAYLAND_DISPLAY") != "":
		return "wayland"
	case os.Getenv("DISPLAY") != "":
		return "x11"
	}
	return "none"
}

// crashNotice describes the crash report of the previous run, if any.
func (a *App) crashNotice() string {
	if a.opts.Crashes == nil || a.opts.Crashes.Previous == "" {
		return ""
	}
	return filepath.Base(a.opts.Crashes.Previous) + " (" + diag.Stamp(a.opts.Crashes.Previous).Format(time.DateTime) + ")"
}

// withDefaults returns h with the default keepalive of the settings filled in,
// for hosts that do not set their own.
func (a *App) withDefaults(h model.Host) model.Host {
	if h.KeepAlive <= 0 {
		h.KeepAlive = a.Settings.Values().Connection.KeepAlive()
	}
	return h
}

// lookup finds a host by ID with the settings defaults applied; the
// connection code uses it instead of the library directly.
func (a *App) lookup(id model.ID) (model.Host, bool) {
	h, ok := a.Library.Get(id)
	return a.withDefaults(h), ok
}
