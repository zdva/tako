// Package ui is Tako's window, built with MyGo's native UI toolkit. It holds
// the view code and its state, and no business logic: hosts, sessions and
// layout come from the packages it is given in [Deps].
package ui

import (
	"context"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/hostlib"
	"github.com/wei-dong/tako/internal/i18n"
	"github.com/wei-dong/tako/internal/layout"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/settings"
	"github.com/wei-dong/tako/internal/theme"
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
	// OnQuit asks the app to quit.
	OnQuit func()
	// Version is shown in the settings sheet.
	Version string
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
	// heldMods are modifiers the pointer events do not carry: tests set
	// them, since the Tester's pointer events have none.
	heldMods ui.Modifiers
}

// New returns the window's state.
func New(d Deps) *App {
	a := &App{d: d, collapsedGroups: map[string]bool{}}
	a.sheet.init(a)
	a.mm.menuIdx, a.mm.hover = -1, -1
	v := d.Settings.Values()
	a.tr = i18n.Must(v.ResolveLanguage(d.Locale))
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
	if dark {
		a.tk = theme.Dark()
	} else {
		a.tk = theme.Light()
	}
}

// Build is the window's view function.
func (a *App) Build(c *ui.Context) {
	a.drain()
	a.syncPrefs(c)
	ui.Themed(c, a.tk.UI()).Fill().Background(a.tk.Bg).Children(func() {
		a.shortcuts(c)
		ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
			if !a.sidebarCollapsed {
				a.sidebar(c)
			}
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				a.toolbar(c)
				a.main(c)
				a.statusBar(c)
			})
		})
		a.sheet.build(c, a)
		a.paletteView(c)
		a.settingsView(c)
		a.promptView(c)
		a.deleteView(c)
		a.renameView(c)
	})
}
