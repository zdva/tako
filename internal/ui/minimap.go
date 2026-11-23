package ui

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/layout"
	"github.com/zdva/tako/internal/workspace"
)

// Metrics of the minimap (DESIGN.md, Final: about 176 px wide).
const (
	mmWidth      = 176
	mmPad        = 8
	mmHeaderH    = 22
	mmSquaresW   = mmWidth - 2*mmPad
	mmMargin     = 12
	mmDragSlop   = 4
	mmSquareGap  = 3
	mmFade       = 150 * time.Millisecond
	mmMinSquares = 56
	mmMaxSquares = 104
)

type mmMode int

const (
	mmIdle    mmMode = iota
	mmPending        // pressed on a square: a click, or the start of a drag
	mmDrag           // dragging a square to another place
	mmBox            // rubber-band selecting from empty space
)

// mmSquare is one block's square, in the squares area's coordinates.
type mmSquare struct {
	b    *workspace.Block
	rect layout.Rect
}

// minimapState is the gesture and geometry of the floating minimap.
type minimapState struct {
	mode    mmMode
	squares []mmSquare
	dragged *workspace.Block
	down    ui.Point
	cur     ui.Point
	hover   int
	menuIdx int
	offset  ui.Point
	boxMods ui.Modifiers
}

// minimap draws the floating panel in the main area: one square per block,
// laid out like the blocks are. Click a square to toggle its sync, drag from
// empty space to select several (⇧ adds, ⌥ inverts), drag a square to
// rearrange, drag the panel to move it to a corner, and use the slider for how
// opaque it is at rest. It turns opaque while the pointer is over it.
func (a *App) minimap(c *ui.Context, vp layout.Size) {
	m := a.d.Model
	prefs := m.MinimapPrefs()
	if prefs.Hidden {
		return
	}
	mm := &a.mm
	// The squares map the blocks' whole layout, which is taller than the
	// window when the grid scrolls.
	content := vp
	if m.Zoomed() == nil {
		content = m.Content()
	}
	sqH := min(max(float32(math.Round(float64(mmSquaresW*content.H/content.W))), mmMinSquares), mmMaxSquares)
	panelW, panelH := float32(mmWidth), float32(mmPad*2+mmHeaderH+6)+sqH
	a.mmGeometry(sqH, content)

	areaSize := layout.Size{W: vp.W + 2*areaPad, H: vp.H + 2*areaPad}
	corners := layout.CornerOrigins(areaSize, layout.Size{W: panelW, H: panelH}, mmMargin)
	points := make([]ui.Point, len(corners))
	for i, r := range corners {
		points[i] = ui.Point{X: r.X, Y: r.Y}
	}
	bounds := ui.Rect{
		X: corners[layout.TopLeft].X, Y: corners[layout.TopLeft].Y,
		W: corners[layout.TopRight].X - corners[layout.TopLeft].X,
		H: corners[layout.BottomLeft].Y - corners[layout.TopLeft].Y,
	}

	panel := ui.Column(c).Key("minimap").Absolute().Left(0).Top(0).Size(panelW, panelH).ZIndex(10)
	hot := panel.Hovered() || mm.mode != mmIdle
	target := float32(prefs.Opacity)
	if hot {
		target = 1
	}
	op := panel.AnimateTo("opacity", target, ui.Animation{Duration: mmFade, Ease: motionEase, NonMotion: true})
	panel.Radius(22).Background(a.tk.Glass3.Alpha(op)).Border(1, a.tk.BorderStrong.Alpha(max(op, 0.6)))
	a.tk.ShadowMd(panel)
	panel.Padding(mmPad).Gap(6)
	panel.Draggable(&mm.offset, ui.DraggableOptions{Bounds: &bounds, Rigid: true, Snap: ui.Nearest(points...)})
	panel.ContextMenu(a.minimapMenu)
	if !panel.Dragging() {
		if _, _, ended := panel.DragEnded(); ended {
			m.SetMinimap(workspace.Minimap{Hidden: prefs.Hidden, Corner: nearestCorner(points, mm.offset), Opacity: prefs.Opacity})
			prefs = m.MinimapPrefs()
		}
		mm.offset = points[prefs.Corner]
	}
	panel.Children(func() {
		a.mmHeader(c, prefs)
		a.mmSquares(c, sqH, op)
	})
}

func nearestCorner(points []ui.Point, p ui.Point) layout.Corner {
	best, bestD := 0, float32(math.MaxFloat32)
	for i, q := range points {
		dx, dy := q.X-p.X, q.Y-p.Y
		if d := dx*dx + dy*dy; d < bestD {
			best, bestD = i, d
		}
	}
	return layout.Corner(best)
}

// mmGeometry scales the blocks' layout into the squares area.
func (a *App) mmGeometry(sqH float32, vp layout.Size) {
	m := a.d.Model
	place := m.Placement()
	sx, sy := mmSquaresW/vp.W, sqH/vp.H
	mm := &a.mm
	mm.squares = mm.squares[:0]
	for _, cell := range place.Cells {
		b := blockByID(m.Blocks(), cell.ID)
		if b == nil {
			continue
		}
		r := cell.Rect
		mm.squares = append(mm.squares, mmSquare{b: b, rect: layout.Rect{
			X: r.X*sx + mmSquareGap/2, Y: r.Y*sy + mmSquareGap/2,
			W: max(r.W*sx-mmSquareGap, 4), H: max(r.H*sy-mmSquareGap, 4),
		}})
	}
}

func blockByID(blocks []*workspace.Block, id string) *workspace.Block {
	for _, b := range blocks {
		if string(b.ID) == id {
			return b
		}
	}
	return nil
}

func (a *App) mmHeader(c *ui.Context, prefs workspace.Minimap) {
	m := a.d.Model
	ui.Row(c).Key("mm-head").Height(mmHeaderH).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconGrip).FontSize(12).TextColor(a.tk.Text3).Cursor(ui.CursorGrab).Label(a.T(kMMHandle))
		ui.Text(c, a.T(kMMTitle, m.SyncedCount(), len(m.Blocks()))).FontSize(11).Bold().TextColor(a.tk.Text2).NoWrap()
		ui.Spacer(c)
		v := float64(prefs.Opacity)
		ui.Slider(c, &v, float64(workspace.MinOpacity), 1).Label(a.T(kMMOpacity)).Width(48)
		if float32(v) != prefs.Opacity {
			prefs.Opacity = float32(v)
			m.SetMinimap(prefs)
		}
	})
}

// mmSquares is the interactive area. Squares are painted, not elements, and
// one HandleInput tells click, box and drag apart.
func (a *App) mmSquares(c *ui.Context, h, op float32) {
	mm := &a.mm
	area := ui.Box(c).Key("mm-squares").Size(mmSquaresW, h).Cursor(ui.CursorPointer).Label(a.T(kMMArea))
	area.Draw(func(p *ui.Painter, r ui.Rect) { a.mmPaint(p, r, op) })
	area.HandleInput(a.mmInput)
	area.ContextMenu(func(menu *ui.Menu) {
		if mm.menuIdx >= 0 && mm.menuIdx < len(mm.squares) {
			a.minimapSquareMenu(menu, mm.squares[mm.menuIdx].b)
			return
		}
		a.minimapMenu(menu)
	})
}

func (a *App) mmHit(x, y float32) int { return layout.HitSquare(a.mmRects(), x, y) }

func (a *App) mmRects() []layout.Rect {
	rects := make([]layout.Rect, len(a.mm.squares))
	for i, s := range a.mm.squares {
		rects[i] = s.rect
	}
	return rects
}

func (a *App) mmInput(ev ui.InputEvent) bool {
	mm := &a.mm
	pt := ui.Point{X: ev.X, Y: ev.Y}
	switch ev.Kind { //nolint:exhaustive // only pointer events reach a pointer handler
	case ui.InputPointerDown:
		if ev.Button != 0 {
			mm.menuIdx = a.mmHit(ev.X, ev.Y)
			return false
		}
		mm.down, mm.cur, mm.boxMods = pt, pt, a.mods(ev)
		mm.mode = mmBox
		if i := a.mmHit(ev.X, ev.Y); i >= 0 {
			mm.mode, mm.dragged = mmPending, mm.squares[i].b
		}
		return true
	case ui.InputPointerMove:
		mm.cur = pt
		mm.hover = a.mmHit(ev.X, ev.Y)
		switch mm.mode {
		case mmPending:
			if math.Hypot(float64(pt.X-mm.down.X), float64(pt.Y-mm.down.Y)) > mmDragSlop {
				mm.mode = mmDrag
				if a.mods(ev)&(ui.Shift|ui.Alt) != 0 {
					// ⇧ or ⌥ held: a rubber band from a square too (⇧ adds,
					// ⌥ inverts), since the squares leave little empty space.
					mm.mode, mm.dragged = mmBox, nil
				}
			}
		case mmDrag:
			a.mmReflow()
		case mmBox:
			mm.boxMods = a.mods(ev)
		case mmIdle:
		}
		return mm.mode != mmIdle
	case ui.InputPointerUp:
		if ev.Button != 0 {
			return false
		}
		a.mmFinish(ev.Canceled)
		return true
	}
	return false
}

// mmReflow moves the dragged block to the slot under the pointer while the
// pointer is still down, so that the blocks in the main area rearrange live.
func (a *App) mmReflow() {
	mm := &a.mm
	slot := layout.NearestSlot(a.mmRects(), mm.cur.X, mm.cur.Y)
	if slot < 0 || mm.dragged == nil {
		return
	}
	target := mm.squares[slot].b
	if target == mm.dragged {
		return
	}
	a.d.Model.Reposition(mm.dragged, target)
	a.mmRelayout()
}

// mmRelayout recomputes the squares after the model changed under a drag.
func (a *App) mmRelayout() {
	vp := a.lastViewport
	if vp.W <= 0 || vp.H <= 0 {
		return
	}
	sqH := min(max(float32(math.Round(float64(mmSquaresW*vp.H/vp.W))), mmMinSquares), mmMaxSquares)
	a.mmGeometry(sqH, vp)
}

// mmFinish ends a gesture and applies it to the model.
func (a *App) mmFinish(canceled bool) {
	mm := &a.mm
	m := a.d.Model
	defer func() { mm.mode, mm.dragged = mmIdle, nil }()
	if canceled {
		return
	}
	switch mm.mode {
	case mmPending:
		if mm.dragged != nil {
			m.SetSynced(mm.dragged, !mm.dragged.Synced)
		}
	case mmBox:
		picked := a.mmPicked()
		if len(picked) == 0 {
			return
		}
		a.applyBox(picked, mm.boxMods)
	case mmDrag, mmIdle:
		// The drag already moved the blocks while it went.
	}
}

// mmPicked returns the blocks the rubber band touches.
func (a *App) mmPicked() []*workspace.Block {
	mm := &a.mm
	box := layout.Rect{X: mm.down.X, Y: mm.down.Y, W: mm.cur.X - mm.down.X, H: mm.cur.Y - mm.down.Y}
	var out []*workspace.Block
	for _, i := range layout.SelectBox(a.mmRects(), box) {
		out = append(out, mm.squares[i].b)
	}
	return out
}

// applyBox turns sync on or off for the blocks a box selected: all of them the
// same way (off when all were on, else on); with ⇧ on for all; with ⌥ each one
// inverted.
func (a *App) applyBox(picked []*workspace.Block, mods ui.Modifiers) {
	m := a.d.Model
	switch {
	case mods&ui.Alt != 0:
		for _, b := range picked {
			m.SetSynced(b, !b.Synced)
		}
	case mods&ui.Shift != 0:
		m.SetSyncedMany(picked, true)
	default:
		allOn := true
		for _, b := range picked {
			allOn = allOn && b.Synced
		}
		m.SetSyncedMany(picked, !allOn)
	}
}

// mmPaint draws the squares, the selection box and the drag.
func (a *App) mmPaint(p *ui.Painter, r ui.Rect, op float32) {
	mm := &a.mm
	tk := a.tk
	alpha := min(op+0.25, 1)
	picked := map[*workspace.Block]bool{}
	if mm.mode == mmBox {
		for _, b := range a.mmPicked() {
			picked[b] = true
		}
	}
	for i, s := range mm.squares {
		rect := ui.Rect{X: r.X + s.rect.X, Y: r.Y + s.rect.Y, W: s.rect.W, H: s.rect.H}
		b := s.b
		fill, fg, edge := tk.Surface.Alpha(alpha), tk.Text2, tk.BorderStrong.Alpha(alpha)
		if b.Synced {
			fill, fg, edge = tk.Accent.Alpha(alpha), ui.RGB(255, 255, 255), tk.Accent.Alpha(alpha)
		}
		if mm.mode == mmDrag && b == mm.dragged {
			fill = fill.Alpha(0.35)
			fg = fg.Alpha(0.5)
		}
		p.Fill(rect, fill, 6)
		p.Stroke(rect, edge, 6, 1)
		if i == mm.hover && mm.mode == mmIdle {
			p.Stroke(rect, tk.Text.Alpha(0.35), 6, 1.5)
		}
		if picked[b] {
			p.StrokeDashed(ui.Rect{X: rect.X - 1, Y: rect.Y - 1, W: rect.W + 2, H: rect.H + 2}, tk.Accent, 7, 1.5)
		}
		a.mmLabel(p, rect, b.Name, fg)
	}
	switch mm.mode { //nolint:exhaustive // nothing is drawn while idle or pending
	case mmBox:
		bx := layout.Rect{X: mm.down.X, Y: mm.down.Y, W: mm.cur.X - mm.down.X, H: mm.cur.Y - mm.down.Y}.Normalize()
		box := ui.Rect{X: r.X + bx.X, Y: r.Y + bx.Y, W: bx.W, H: bx.H}
		p.Fill(box, tk.Accent.Alpha(0.10), 4)
		p.StrokeDashed(box, tk.Accent, 4, 1.25)
	case mmDrag:
		if mm.dragged == nil {
			return
		}
		w, h := float32(52), float32(30)
		ghost := ui.Rect{X: r.X + mm.cur.X - w/2, Y: r.Y + mm.cur.Y - h/2, W: w, H: h}
		p.Shadow(ghost, 6, 0, 4, 10, 0, ui.RGBA(0, 0, 0, 0.35))
		p.Fill(ghost, tk.Accent, 6)
		a.mmLabel(p, ghost, mm.dragged.Name, ui.RGB(255, 255, 255))
	}
}

// mmLabel draws a block's name centered in its square, shortened to fit.
func (a *App) mmLabel(p *ui.Painter, rect ui.Rect, name string, col ui.Color) {
	runes := []rune(name)
	span := func(s string) ui.Span { return ui.Span{Text: s, Size: 10.5, Weight: 600, Color: col} }
	w, h := p.MeasureText(0, span(name))
	for w > rect.W-6 && len(runes) > 1 {
		runes = runes[:len(runes)-1]
		w, h = p.MeasureText(0, span(string(runes)+"…"))
		name = string(runes) + "…"
	}
	p.RichText(rect.X+(rect.W-w)/2, rect.Y+(rect.H-h)/2, 0, span(name))
}

// mods returns the modifiers held during a pointer event.
func (a *App) mods(ev ui.InputEvent) ui.Modifiers { return ev.Mods | a.heldMods }
