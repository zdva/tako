package ui

import (
	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/layout"
	"github.com/wei-dong/tako/internal/model"
)

// Geometry the tests need to aim the pointer.
const (
	SidebarWidth  = sidebarW
	ToolbarHeight = toolbarH
	AreaPad       = areaPad
	MinimapWidth  = mmWidth
	MinimapPad    = mmPad
)

// MinimapSquares returns the squares of the minimap, relative to its squares
// area, in the order of the layout.
func (a *App) MinimapSquares() []layout.Rect { return a.mmRects() }

// MinimapSquareNames returns the block names of MinimapSquares.
func (a *App) MinimapSquareNames() []string {
	var out []string
	for _, s := range a.mm.squares {
		out = append(out, s.b.Name)
	}
	return out
}

// Dividers returns the dividers of the current layout, relative to the main
// area's blocks.
func (a *App) Dividers() []layout.Divider { return a.d.Model.Placement().Dividers }

// GuideShown reports whether the snap guide line is up.
func (a *App) GuideShown() bool { return a.div.active && a.div.guide.Valid }

// Shortcuts returns the shortcut table for the tests.
func Shortcuts() []ShortcutInfo {
	out := make([]ShortcutInfo, 0, len(shortcutTable))
	for _, s := range shortcutTable {
		out = append(out, ShortcutInfo{Group: s.group, Mac: s.macLabel(), Label: s.label(), Act: int(s.act)})
	}
	return out
}

// ShortcutInfo describes one shortcut.
type ShortcutInfo struct {
	Group, Mac, Label string
	Act               int
}

// SetGOOS switches the convention the shortcuts are written in and returns
// the function that restores it.
func SetGOOS(os string) func() {
	saved := goos
	goos = os
	return func() { goos = saved }
}

// ShortcutHint returns the label of the shortcut of a named action.
func ShortcutHint(name string) string {
	acts := map[string]actID{"minimap": actMinimap, "transfer": actTransfer, "palette": actPalette}
	return hint(acts[name])
}

// HoldModifiers makes pointer events count as made with mods held.
func (a *App) HoldModifiers(mods ui.Modifiers) { a.heldMods = mods }

// OpenTransfer opens the transfer sheet with host as the source.
func (a *App) OpenTransfer(host model.ID) { a.openTransfer(host) }

// MonitorOpen is the host whose monitor page is open, or "".
func (a *App) MonitorOpen() model.ID { return a.mon.host }

// Level names the threshold color of a percentage: green, amber or red.
func (a *App) Level(p float64) string {
	switch a.level(p) {
	case a.tk.Red:
		return "red"
	case a.tk.Amber:
		return "amber"
	}
	return "green"
}

// PaneFolders are the folders the transfer sheet's source and target show.
func (a *App) PaneFolders() (src, dst string) { return a.xfer.src.path, a.xfer.dst.path }

// OpenSettings opens the settings window on a page ("" for the first).
func (a *App) OpenSettings(page string) {
	a.settings.open, a.settings.page = true, page
}
