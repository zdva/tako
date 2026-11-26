package ui

import (
	"slices"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/layout"
	"github.com/zdva/tako/internal/workspace"
)

// Metrics of the main area.
const (
	// areaPad is the margin around the blocks.
	areaPad = gridGap
)

// minBlock is the smallest a block gets when a divider is dragged: room for
// the header, the footer and a few lines of terminal.
var (
	minBlock        = layout.Size{W: 240, H: blockHead + blockFoot + 96}
	minBlockCompact = layout.Size{W: 170, H: blockHead + blockFoot + 64}
)

// dividerDrag is a divider being dragged.
type dividerDrag struct {
	active bool
	path   string
	// guide is the snap line to show; Valid while the divider is on 1/3, 1/2
	// or 2/3.
	guide layout.Guide
}

// main is the area under the toolbar: the blocks in their layout, the
// dividers between them and the minimap, or the empty state.
func (a *App) main(c *ui.Context) {
	m := a.d.Model
	if a.pendingClip != "" {
		c.WriteClipboard(a.pendingClip)
		a.pendingClip = ""
	}
	// Keep this frame's block list independent from workspace mutations caused
	// by controls rendered below. Model removals delete from the backing array;
	// iterating the live slice after a close can otherwise reach a nil tail.
	blocks := slices.Clone(m.Blocks())
	if len(blocks) == 0 {
		a.emptyState(c)
		return
	}
	vp := a.viewport(c)
	instant := a.div.active || vp != a.lastViewport
	a.lastViewport = vp

	area := ui.Box(c).Key("main-area").Grow(1).MinWidth(0).MinHeight(0)
	zoomed := m.Zoomed()
	place := m.Placement()
	// When the window cannot hold the automatic grid at the blocks' minimum
	// size, the grid is taller than the window and scrolls; the minimap
	// stays put above it.
	scrolls := zoomed == nil && m.Scrolls()
	blocksIn := func(fn func()) { fn() }
	if scrolls {
		content := m.Content()
		blocksIn = func(fn func()) {
			ui.Scroll(c).Key("main-scroll").Absolute().Left(0).Top(0).Right(0).Bottom(0).Children(func() {
				ui.Box(c).Key("main-content").Height(content.H + 2*areaPad).Shrink(0).Children(fn)
			})
		}
	}
	area.Children(func() {
		blocksIn(a.mainBlocks(c, blocks, place, zoomed, vp, instant, scrolls))
		a.minimap(c, vp)
	})
}

// mainBlocks draws the blocks in their places and, unless the grid scrolls
// or a block is zoomed, the dividers between them.
func (a *App) mainBlocks(c *ui.Context, blocks []*workspace.Block, place layout.Placement, zoomed *workspace.Block, vp layout.Size, instant, scrolls bool) func() {
	return func() {
		for _, b := range blocks {
			cell, ok := place.Rect(string(b.ID))
			if zoomed != nil {
				if b != zoomed {
					continue
				}
				cell, ok = layout.Rect{W: vp.W, H: vp.H}, true
			}
			if !ok {
				continue
			}
			cell.X += areaPad
			cell.Y += areaPad
			a.block(c, b, cell, instant)
		}
		if zoomed == nil && !scrolls {
			for _, d := range place.Dividers {
				a.divider(c, d)
			}
			a.guideLine(c)
		}
	}
}

// viewport is the room the blocks share: the window minus the sidebar,
// toolbar, status bar and margins.
func (a *App) viewport(c *ui.Context) layout.Size {
	w, h := c.Size()
	if !a.sidebarCollapsed {
		w -= sidebarW + 1
	}
	vp := layout.Size{W: max(w-2*areaPad, 1), H: max(h-toolbarH-statusH-2*areaPad, 1)}
	floor := minBlock
	if a.d.Settings.Values().CompactBlocks {
		floor = minBlockCompact
	}
	a.d.Model.SetViewport(workspace.Viewport{Size: vp, Gap: gridGap, Min: floor})
	return vp
}

// divider is the strip between two blocks, which the pointer drags. It snaps
// to 1/3, 1/2 and 2/3 with a guide line; Alt turns snapping off.
func (a *App) divider(c *ui.Context, d layout.Divider) {
	r := d.Rect
	el := ui.Box(c).Key("div:"+d.Path).Absolute().Left(areaPad+r.X).Top(areaPad+r.Y).Size(r.W, r.H)
	cursor := ui.CursorResizeEW
	if d.Axis == layout.Rows {
		cursor = ui.CursorResizeNS
	}
	el.Cursor(cursor).Label(a.T(kResizeBlocks))
	el.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind { //nolint:exhaustive // only pointer events reach a pointer handler
		case ui.InputPointerDown:
			if ev.Button != 0 {
				return false
			}
			a.div = dividerDrag{active: true, path: d.Path}
			return true
		case ui.InputPointerMove:
			if !a.div.active || a.div.path != d.Path {
				return false
			}
			// ev is relative to the divider's strip, whose place in the
			// viewport is d.Rect: no need to know where the area is.
			pos := r.X + ev.X
			if d.Axis == layout.Rows {
				pos = r.Y + ev.Y
			}
			a.div.guide = a.d.Model.DragDivider(d.Path, pos, a.mods(ev)&ui.Alt == 0)
			return true
		case ui.InputPointerUp:
			a.div = dividerDrag{}
			return true
		}
		return false
	})
	active := a.div.active && a.div.path == d.Path
	if active || el.Hovered() {
		el.Draw(func(p *ui.Painter, box ui.Rect) {
			col := a.tk.BorderStrong
			if active {
				col = a.tk.Accent
			}
			const thick = 3
			if d.Axis == layout.Cols {
				p.Fill(ui.Rect{X: box.X + (box.W-thick)/2, Y: box.Y + box.H/2 - 24, W: thick, H: 48}, col, thick/2)
			} else {
				p.Fill(ui.Rect{X: box.X + box.W/2 - 24, Y: box.Y + (box.H-thick)/2, W: 48, H: thick}, col, thick/2)
			}
		})
	}
}

// guideLine draws the snap line while a divider sits on 1/3, 1/2 or 2/3.
func (a *App) guideLine(c *ui.Context) {
	g := a.div.guide
	if !a.div.active || !g.Valid {
		return
	}
	const thick = 2
	line := ui.Box(c).Key("guide").Absolute().Background(a.tk.Accent).ZIndex(5)
	if g.Axis == layout.Cols {
		line.Left(areaPad+g.Pos-thick/2).Top(areaPad+g.Span.Y).Size(thick, g.Span.H)
	} else {
		line.Left(areaPad+g.Span.X).Top(areaPad+g.Pos-thick/2).Size(g.Span.W, thick)
	}
}
