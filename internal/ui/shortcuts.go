package ui

import (
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

// goos is the system the shortcuts are written for; tests change it to check
// both conventions.
var goos = runtime.GOOS

// actID names something a shortcut, a palette command or a menu item can do.
type actID int

// The actions that have a shortcut (DESIGN.md §4.6).
const (
	actPalette actID = iota + 1
	actSidebar
	actAddHost
	actNewTab
	actCloseTab
	actCloseBlock
	actGotoTab
	actFocus
	actSyncBlock
	actSyncAll
	actMonitor
	actTransfer
	actMinimap
	actFind
	actSettings
	actZoom
)

// Directions of actFocus, in the shortcut's arg.
const (
	dirLeft = iota
	dirRight
	dirUp
	dirDown
)

// shortcut binds a key to an action. Tako's keys use the platform's primary
// modifier (⌘ on macOS); elsewhere Ctrl+Shift, so plain Ctrl keys reach the
// shell, and Alt for the second modifier of two-modifier shortcuts.
type shortcut struct {
	act actID
	arg int
	key ui.Key
	// glyph is the key as printed in a label.
	glyph string
	// mac and other are the modifiers on macOS and everywhere else.
	mac, other ui.Modifiers
	// otherKey replaces key (and otherGlyph glyph) off macOS, where the
	// macOS combination would clash with another shortcut.
	otherKey   ui.Key
	otherGlyph string
	// group is the entry of the DESIGN.md §4.6 table the shortcut belongs to;
	// empty for shortcuts the table does not list.
	group string
}

const (
	cmd   = ui.Super
	ctrlS = ui.Ctrl | ui.Shift
	ctrlA = ui.Ctrl | ui.Shift | ui.Alt
)

// shortcutTable is the one list of shortcuts: it drives key handling, the
// hints in the palette and menus, and docs/SHORTCUTS.md, and
// TestShortcutsMatchDesign checks it against DESIGN.md.
var shortcutTable = buildShortcutTable()

const (
	osMac      = "darwin"
	groupFocus = "⌘⌥← ↑ → ↓"
)

func buildShortcutTable() []shortcut {
	t := []shortcut{
		{act: actPalette, key: ui.KeyP, glyph: "P", mac: cmd, other: ctrlS, group: "⌘P"},
		{act: actSidebar, key: ui.KeyB, glyph: "B", mac: cmd, other: ctrlS, group: "⌘B"},
		{act: actAddHost, key: ui.KeyN, glyph: "N", mac: cmd, other: ctrlS, group: "⌘N"},
		{act: actNewTab, key: ui.KeyT, glyph: "T", mac: cmd, other: ctrlS, group: "⌘T"},
		{act: actCloseTab, key: ui.KeyW, glyph: "W", mac: cmd, other: ctrlS, group: "⌘W"},
		{act: actCloseBlock, key: ui.KeyW, glyph: "W", mac: cmd | ui.Shift, other: ctrlA, group: "⌘⇧W"},
		{act: actFocus, arg: dirLeft, key: ui.KeyLeft, glyph: "←", mac: cmd | ui.Alt, other: ctrlA, group: groupFocus},
		{act: actFocus, arg: dirUp, key: ui.KeyUp, glyph: "↑", mac: cmd | ui.Alt, other: ctrlA, group: groupFocus},
		{act: actFocus, arg: dirRight, key: ui.KeyRight, glyph: "→", mac: cmd | ui.Alt, other: ctrlA, group: groupFocus},
		{act: actFocus, arg: dirDown, key: ui.KeyDown, glyph: "↓", mac: cmd | ui.Alt, other: ctrlA, group: groupFocus},
		{act: actSyncBlock, key: ui.KeyS, glyph: "S", mac: cmd | ui.Alt, other: ctrlA, group: "⌘⌥S"},
		// ⌘⌥⇧S would be the same keys as ⌘⌥S with Ctrl+Shift+Alt, so off
		// macOS the all-blocks toggle is on A.
		{
			act: actSyncAll, key: ui.KeyS, glyph: "S", mac: cmd | ui.Alt | ui.Shift, other: ctrlA,
			otherKey: ui.KeyA, otherGlyph: "A", group: "⌘⌥⇧S",
		},
		{act: actMonitor, key: ui.KeyM, glyph: "M", mac: cmd | ui.Alt, other: ctrlA, group: "⌘⌥M"},
		// ⌘⇧T would be the new-tab key plus Shift off macOS: Alt joins it.
		{act: actTransfer, key: ui.KeyT, glyph: "T", mac: cmd | ui.Shift, other: ctrlA, group: "⌘⇧T"},
		{act: actMinimap, key: ui.KeyG, glyph: "G", mac: cmd | ui.Alt, other: ctrlA, group: "⌘⌥G"},
		{act: actFind, key: ui.KeyF, glyph: "F", mac: cmd, other: ctrlS, group: "⌘F"},
		{act: actSettings, key: ui.KeyComma, glyph: ",", mac: cmd, other: ctrlS, group: "⌘,"},
		{act: actZoom, key: ui.KeyEnter, glyph: "↵", mac: cmd | ui.Shift, other: ctrlS},
	}
	digits := []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}
	for i, k := range digits {
		t = append(t, shortcut{
			act: actGotoTab, arg: i, key: k, glyph: string(rune('1' + i)), mac: cmd, other: ctrlS, group: "⌘1…9",
		})
	}
	return t
}

func (s shortcut) mods() ui.Modifiers {
	if goos == osMac {
		return s.mac
	}
	return s.other
}

func (s shortcut) keyOf() (ui.Key, string) {
	if goos != osMac && s.otherKey != 0 {
		return s.otherKey, s.otherGlyph
	}
	return s.key, s.glyph
}

// label renders the shortcut for display: ⌘⌥S on macOS, Ctrl+Shift+Alt+S
// elsewhere.
func (s shortcut) label() string {
	_, glyph := s.keyOf()
	m := s.mods()
	if goos == osMac {
		var b strings.Builder
		if m&ui.Ctrl != 0 {
			b.WriteString("⌃")
		}
		if m&ui.Alt != 0 {
			b.WriteString("⌥")
		}
		if m&ui.Shift != 0 {
			b.WriteString("⇧")
		}
		if m&ui.Super != 0 {
			b.WriteString("⌘")
		}
		return b.String() + glyph
	}
	var parts []string
	if m&ui.Ctrl != 0 {
		parts = append(parts, "Ctrl")
	}
	if m&ui.Shift != 0 {
		parts = append(parts, "Shift")
	}
	if m&ui.Alt != 0 {
		parts = append(parts, "Alt")
	}
	return strings.Join(append(parts, glyph), "+")
}

// macLabel is the label of the macOS convention, the one DESIGN.md prints.
func (s shortcut) macLabel() string {
	saved := goos
	goos = osMac
	defer func() { goos = saved }()
	return s.label()
}

// shortcutFor returns the first shortcut of an action.
func shortcutFor(act actID) (shortcut, bool) {
	for _, s := range shortcutTable {
		if s.act == act {
			return s, true
		}
	}
	return shortcut{}, false
}

// hint is the label of an action's shortcut, "" when it has none.
func hint(act actID) string {
	if s, ok := shortcutFor(act); ok {
		return s.label()
	}
	return ""
}

// menuKey shows an action's shortcut on a menu item.
func menuKey(it *ui.MenuItem, act actID) *ui.MenuItem {
	if s, ok := shortcutFor(act); ok {
		k, _ := s.keyOf()
		return it.Shortcut(s.mods(), k)
	}
	return it
}

// shortcuts runs the action of the key pressed this frame, if any.
func (a *App) shortcuts(c *ui.Context) {
	for _, s := range shortcutTable {
		k, _ := s.keyOf()
		if c.Shortcut(s.mods(), k) {
			a.do(s.act, s.arg)
			break
		}
	}
	if a.pendingToast != "" {
		c.Toast(a.pendingToast)
		a.pendingToast = ""
	}
}

// do carries out an action; shortcuts, palette commands and menus share it.
func (a *App) do(act actID, arg int) { //nolint:gocyclo // one case per action
	m := a.d.Model
	b := m.Focused()
	switch act {
	case actPalette:
		a.openPalette()
	case actSidebar:
		a.sidebarCollapsed = !a.sidebarCollapsed
	case actAddHost:
		a.openAddHost()
	case actNewTab:
		if b == nil {
			a.openLocal()
			return
		}
		if err := m.AddTab(b); err != nil {
			a.toast(a.T(kErrOpenLocal, err))
		}
		a.focusBlock(b)
	case actCloseTab:
		if b != nil {
			m.CloseTab(b, b.Active)
		}
	case actCloseBlock:
		if b != nil {
			m.CloseBlock(b)
		}
	case actGotoTab:
		if b != nil {
			m.SetActive(b, arg)
			a.focusBlock(b)
		}
	case actFocus:
		dx, dy := [4]int{-1, 1, 0, 0}[arg], [4]int{0, 0, -1, 1}[arg]
		m.MoveFocus(dx, dy)
		if f := m.Focused(); f != nil {
			a.focusBlock(f)
		}
	case actSyncBlock:
		if b != nil {
			m.SetSynced(b, !b.Synced)
		}
	case actSyncAll:
		m.ToggleSyncAll()
	case actMonitor, actTransfer:
		a.toast(a.T(kSoon))
	case actMinimap:
		p := m.MinimapPrefs()
		p.Hidden = !p.Hidden
		m.SetMinimap(p)
	case actFind:
		a.openFind(b)
	case actSettings:
		a.settings.open = true
	case actZoom:
		if b != nil {
			m.ToggleZoom(b)
		}
	}
}

func (a *App) openPalette() {
	a.palette.open, a.palette.query, a.palette.sel = true, "", 0
}
