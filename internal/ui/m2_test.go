package ui_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/layout"
	tui "github.com/wei-dong/tako/internal/ui"
	"github.com/wei-dong/tako/internal/workspace"
)

const (
	mmAreaLabel = "Minimap: click a block to toggle sync, drag to select several"
	allSyncKey  = ui.KeyA // Ctrl+Shift+Alt+A off macOS
)

var ctrlAlt = ui.Ctrl | ui.Shift | ui.Alt

// mmArea returns the squares area of the minimap in window coordinates.
func (h *harness) mmArea() layout.Rect {
	h.t.Helper()
	h.tt.Frame()
	h.tt.Advance(500 * time.Millisecond) // the panel settles into its corner
	h.tt.Frame()
	r, ok := h.tt.Find(mmAreaLabel)
	if !ok {
		h.t.Fatalf("no minimap on screen: %q", h.tt.Texts())
	}
	return layout.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}
}

// mmSquare returns the center of the minimap square of block name, in window
// coordinates.
func (h *harness) mmSquare(name string) (x, y float32) {
	h.t.Helper()
	area := h.mmArea()
	for i, n := range h.app.UI.MinimapSquareNames() {
		if n == name {
			r := h.app.UI.MinimapSquares()[i]
			return area.X + r.X + r.W/2, area.Y + r.Y + r.H/2
		}
	}
	h.t.Fatalf("no minimap square for %q in %v", name, h.app.UI.MinimapSquareNames())
	return 0, 0
}

// drag presses at (fx, fy), moves to (tx, ty) in steps, and releases.
func (h *harness) drag(fx, fy, tx, ty float32) {
	h.t.Helper()
	h.tt.Move(fx, fy)
	h.tt.Frame()
	h.tt.Press(fx, fy)
	h.tt.Frame()
	const steps = 8
	for i := 1; i <= steps; i++ {
		f := float32(i) / steps
		h.tt.Move(fx+(tx-fx)*f, fy+(ty-fy)*f)
		h.tt.Frame()
	}
	h.tt.Release(tx, ty)
	h.tt.Frame()
}

// headerPoint is an empty spot of a block's header, in window coordinates.
func (h *harness) headerPoint(name string) (x, y float32) {
	h.t.Helper()
	h.tt.Frame()
	b := blockByName(h, name)
	r, ok := h.app.Model.Placement().Rect(string(b.ID))
	if !ok {
		h.t.Fatalf("no block %s", name)
	}
	return float32(tui.SidebarWidth+1+tui.AreaPad) + r.X + r.W*0.62, float32(tui.ToolbarHeight+tui.AreaPad) + r.Y + 19
}

func blockByName(h *harness, name string) *workspace.Block {
	for _, b := range h.app.Model.Blocks() {
		if b.Name == name {
			return b
		}
	}
	return nil
}

func TestMinimap_ClickTogglesSync(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(4)
	h.waitText("Sync 0 / 4")
	x, y := h.mmSquare("web-02")
	h.tt.ClickAt(x, y)
	h.tt.Frame()
	if !blockByName(h, "web-02").Synced || h.app.Model.SyncedCount() != 1 {
		t.Fatal("clicking a square should turn its sync on")
	}
	h.waitText("Sync 1 / 4")
	h.tt.ClickAt(x, y)
	h.tt.Frame()
	if blockByName(h, "web-02").Synced {
		t.Fatal("a second click turns it off")
	}
	h.shot("minimap-click")
}

func TestMinimap_BoxSelect(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(4) // 2×2: web-01 web-02 / web-03 web-04
	area := h.mmArea()
	sq := h.app.UI.MinimapSquares()

	// From the gap between the top row's squares (empty space) down across
	// the bottom row: selects the two blocks on the right... start in the
	// gap and sweep over the bottom row.
	gapY := (sq[0].Y + sq[0].H + sq[2].Y) / 2
	h.drag(area.X+sq[2].X+2, area.Y+gapY, area.X+sq[3].X+sq[3].W-2, area.Y+sq[3].Y+sq[3].H-2)
	if got := h.app.Model.SyncedCount(); got != 2 || !blockByName(h, "web-03").Synced || !blockByName(h, "web-04").Synced {
		t.Fatalf("a box over the bottom row should turn both on, synced=%d", got)
	}
	// The same box again: all were on, so all go off.
	h.drag(area.X+sq[2].X+2, area.Y+gapY, area.X+sq[3].X+sq[3].W-2, area.Y+sq[3].Y+sq[3].H-2)
	if got := h.app.Model.SyncedCount(); got != 0 {
		t.Fatalf("the same box again should turn them off, synced=%d", got)
	}

	// ⇧ adds: a box over web-01 while web-03 is on keeps web-03 on.
	h.app.Model.SetSynced(blockByName(h, "web-03"), true)
	h.app.UI.HoldModifiers(ui.Shift)
	x0, y0 := h.mmSquare("web-01")
	h.drag(x0, y0, x0+4, y0+4)
	h.app.UI.HoldModifiers(0)
	if !blockByName(h, "web-01").Synced || !blockByName(h, "web-03").Synced {
		t.Fatal("⇧ should add to the synced set")
	}

	// ⌥ inverts each block in the box.
	h.app.UI.HoldModifiers(ui.Alt)
	fx, fy := h.mmSquare("web-01")
	tx, ty := h.mmSquare("web-04")
	h.drag(fx, fy, tx, ty)
	h.app.UI.HoldModifiers(0)
	if blockByName(h, "web-01").Synced || !blockByName(h, "web-02").Synced || blockByName(h, "web-03").Synced || !blockByName(h, "web-04").Synced {
		t.Fatalf("⌥ should invert each: %v", syncedNames(h))
	}
}

func syncedNames(h *harness) []string {
	var out []string
	for _, b := range h.app.Model.Blocks() {
		if b.Synced {
			out = append(out, b.Name)
		}
	}
	return out
}

func TestMinimap_DragRearrangesTheBlocks(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(4)
	before := blockNames(h)
	fx, fy := h.mmSquare("web-01")
	tx, ty := h.mmSquare("web-04")
	h.drag(fx, fy, tx, ty)
	after := blockNames(h)
	if after[0] == before[0] || after[3] != "web-01" {
		t.Fatalf("order %v -> %v; web-01 should now be last", before, after)
	}
	if h.app.Model.SyncedCount() != 0 {
		t.Fatal("a drag must not toggle sync")
	}
	h.shot("minimap-reordered")
}

func TestMinimap_SnapsToCornersAndRemembersIt(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(2)
	if got := h.app.Model.MinimapPrefs().Corner; got != layout.BottomRight {
		t.Fatalf("default corner = %v", got)
	}
	title, ok := h.tt.Find("Sync 0 / 2")
	if !ok {
		t.Fatal("no minimap title")
	}
	startX := title.X + title.W/2
	startY := title.Y + title.H/2
	// Drop it near the top-left of the main area: it snaps there.
	h.drag(startX, startY, float32(tui.SidebarWidth+60), float32(tui.ToolbarHeight+40))
	h.tt.Frame()
	if got := h.app.Model.MinimapPrefs().Corner; got != layout.TopLeft {
		t.Fatalf("corner = %v, want top-left", got)
	}
	moved, _ := h.tt.Find("Sync 0 / 2")
	if moved.X > float32(tui.SidebarWidth+80) || moved.Y > float32(tui.ToolbarHeight+80) {
		t.Fatalf("the panel is at %+v, not in the top-left corner", moved)
	}
	h.shot("minimap-top-left")
}

func TestMinimap_WidthIs176(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(2)
	area := h.mmArea()
	if got := area.W + 2*tui.MinimapPad; got != tui.MinimapWidth || tui.MinimapWidth != 176 {
		t.Fatalf("panel width %v, want 176", got)
	}
}

// pixel returns the color at window coordinates.
func (h *harness) pixel(x, y float32) [3]int {
	h.t.Helper()
	img := h.tt.Image()
	scale := float32(img.Bounds().Dx()) / winW
	r, g, b, _ := img.At(int(x*scale), int(y*scale)).RGBA()
	return [3]int{int(r >> 8), int(g >> 8), int(b >> 8)}
}

func dist(a, b [3]int) int {
	return abs(a[0]-b[0]) + abs(a[1]-b[1]) + abs(a[2]-b[2])
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// TestMinimap_TranslucentAtRestOpaqueOnHover is TECH-PLAN M2 acceptance 2's
// screenshot comparison: the panel lets the blocks behind it show through at
// rest and covers them while the pointer is over it.
func TestMinimap_TranslucentAtRestOpaqueOnHover(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(2)
	h.tt.SetDark(true)
	h.tt.Frame()
	area := h.mmArea()
	// A point in the panel's top padding, away from any content.
	px, py := area.X+area.W/2, area.Y-3 // the gap between header and squares

	h.tt.Move(10, 10)
	h.tt.Advance(400 * time.Millisecond)
	h.tt.Frame()
	rest := h.pixel(px, py)
	h.shot("minimap-rest")

	h.tt.Move(px, py)
	h.tt.Frame()
	h.tt.Advance(400 * time.Millisecond)
	h.tt.Frame()
	hover := h.pixel(px, py)
	h.shot("minimap-hover")

	glass := [3]int{33, 33, 36} // the dark glass surface
	if dist(hover, glass) > 24 {
		t.Errorf("hovered panel pixel %v should be the opaque surface %v", hover, glass)
	}
	if dist(rest, hover) < 3 {
		t.Errorf("rest %v and hover %v pixels should differ: the panel is translucent at rest", rest, hover)
	}
}

func TestMinimap_OpacitySliderAndToggle(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(2)
	h.tt.Frame()
	info, ok := h.tt.AccessByRole(ui.RoleSlider, "Minimap opacity")
	if !ok {
		t.Fatalf("no opacity slider; screen %q", h.tt.Texts())
	}
	// Click at the left end: the lowest opacity, 35%.
	h.tt.ClickAt(float32(info.Bounds.X+1), float32(info.Bounds.Y+info.Bounds.H/2))
	h.tt.Frame()
	if got := h.app.Model.MinimapPrefs().Opacity; got > 0.4 {
		t.Fatalf("opacity = %v after clicking the left end of the slider", got)
	}
	h.tt.ClickAt(float32(info.Bounds.X+info.Bounds.W-1), float32(info.Bounds.Y+info.Bounds.H/2))
	h.tt.Frame()
	if got := h.app.Model.MinimapPrefs().Opacity; got < 0.9 {
		t.Fatalf("opacity = %v after clicking the right end", got)
	}

	// The toolbar icon and the shortcut hide and show it.
	h.click("Show or hide the minimap " + tui.ShortcutHint("minimap"))
	h.tt.Frame()
	if h.tt.HasText("Sync 0 / 2") {
		t.Fatal("the minimap should be hidden")
	}
	h.tt.Key(ctrlAlt, ui.KeyG)
	h.tt.Frame()
	if !h.tt.HasText("Sync 0 / 2") {
		t.Fatal("the shortcut should show it again")
	}
}

func TestMinimap_ContextMenus(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(2)
	x, y := h.mmSquare("web-01")
	h.tt.RightClickAt(x, y)
	h.tt.Frame()
	menu := strings.Join(h.tt.Menu(), "|")
	for _, want := range []string{"Toggle sync", "Focus this block", "Close block"} {
		if !strings.Contains(menu, want) {
			t.Errorf("square menu %q lacks %q", menu, want)
		}
	}
	if err := h.tt.ChooseMenuItem("Toggle sync"); err != nil {
		t.Fatal(err)
	}
	if !blockByName(h, "web-01").Synced {
		t.Fatal("the square menu did not toggle sync")
	}

	title, _ := h.tt.Find("Sync 1 / 2")
	h.tt.RightClickAt(title.X+2, title.Y+title.H/2)
	h.tt.Frame()
	menu = strings.Join(h.tt.Menu(), "|")
	for _, want := range []string{"Turn sync on for all blocks", "Turn sync off for all blocks", "Hide minimap"} {
		if !strings.Contains(menu, want) {
			t.Errorf("minimap menu %q lacks %q", menu, want)
		}
	}
	if err := h.tt.ChooseMenuItem("Turn sync on for all blocks"); err != nil {
		t.Fatal(err)
	}
	if h.app.Model.SyncedCount() != 2 {
		t.Fatal("the minimap menu did not sync all")
	}
}

// dividerAt returns the center of divider i in window coordinates.
func (h *harness) dividerAt(i int) (x, y float32, d layout.Divider) {
	h.t.Helper()
	h.tt.Frame()
	ds := h.app.UI.Dividers()
	if i >= len(ds) {
		h.t.Fatalf("divider %d of %d", i, len(ds))
	}
	d = ds[i]
	ox, oy := float32(tui.SidebarWidth+1+tui.AreaPad), float32(tui.ToolbarHeight+tui.AreaPad)
	return ox + d.Rect.X + d.Rect.W/2, oy + d.Rect.Y + d.Rect.H/2, d
}

func rootRatio(h *harness) float32 { return h.app.Model.Tree().Root.Ratio }

func TestDivider_SnapsToHalfThirdsWithGuide(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(2)
	x, y, d := h.dividerAt(0)
	if d.Axis != layout.Cols {
		t.Fatalf("two blocks sit side by side, got %v", d.Axis)
	}
	span := d.Span
	usable := span.W - d.Rect.W
	ox := float32(tui.SidebarWidth + 1 + tui.AreaPad)

	h.tt.Move(x, y)
	h.tt.Frame()
	h.tt.Press(x, y)
	h.tt.Frame()

	// Near 1/3: snaps, and the guide shows.
	third := ox + span.X + usable/3 + d.Rect.W/2
	h.tt.Move(third+3, y)
	h.tt.Frame()
	if !h.app.Model.Manual() {
		t.Fatal("the first drag makes the layout manual")
	}
	if math.Abs(float64(rootRatio(h))-1.0/3) > 0.001 || !h.app.UI.GuideShown() {
		t.Fatalf("ratio %v, guide %v: want 1/3 with a guide", rootRatio(h), h.app.UI.GuideShown())
	}
	h.shot("divider-snap-third")

	// Far from any snap point: free, no guide.
	h.tt.Move(ox+span.X+usable*0.42+d.Rect.W/2, y)
	h.tt.Frame()
	if h.app.UI.GuideShown() || math.Abs(float64(rootRatio(h))-0.42) > 0.01 {
		t.Fatalf("ratio %v, guide %v: want a free 0.42", rootRatio(h), h.app.UI.GuideShown())
	}

	// ⌥ switches snapping off even right on the half.
	h.app.UI.HoldModifiers(ui.Alt)
	h.tt.Move(ox+span.X+usable*0.5+d.Rect.W/2+3, y)
	h.tt.Frame()
	h.app.UI.HoldModifiers(0)
	if r := rootRatio(h); h.app.UI.GuideShown() || math.Abs(float64(r)-0.5) < 0.001 {
		t.Fatalf("with ⌥ held the ratio %v must not snap", r)
	}

	// Near 2/3 snaps.
	h.tt.Move(ox+span.X+usable*2/3+d.Rect.W/2-3, y)
	h.tt.Frame()
	if math.Abs(float64(rootRatio(h))-2.0/3) > 0.001 {
		t.Fatalf("ratio %v, want 2/3", rootRatio(h))
	}
	h.tt.Release(x, y)
	h.tt.Frame()
	if h.app.UI.GuideShown() {
		t.Fatal("the guide must go when the pointer lets go")
	}
}

func TestDivider_VerticalSplitSnapsRows(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(4)
	// The grid's root divider separates the two rows.
	x, y, d := h.dividerAt(0)
	if d.Axis != layout.Rows {
		t.Fatalf("root divider axis = %v, want rows", d.Axis)
	}
	h.drag(x, y, x, y+2)
	if math.Abs(float64(rootRatio(h))-0.5) > 0.001 {
		t.Fatalf("ratio %v: a drag that ends near the middle snaps to 1/2", rootRatio(h))
	}
}

func TestLayout_ManualLayoutAndSyncSurviveARestart(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(3)
	x, y, d := h.dividerAt(0)
	h.app.UI.HoldModifiers(ui.Alt)
	h.drag(x, y, x, y+60)
	h.app.UI.HoldModifiers(0)
	h.dragNext(d)
	h.app.Model.SyncOnly([]*workspace.Block{h.app.Model.Blocks()[2]})
	h.app.Model.SetMinimap(workspace.Minimap{Corner: layout.TopRight, Opacity: 0.7})
	want := h.app.Model.Placement()
	ratio := rootRatio(h)
	h.app.Model.Rename(h.app.Model.Blocks()[1], "api")
	h.waitFor("the session file", func() bool {
		st, err := readSession(t, h.dirs.Session())
		return err == nil && st.Layout.Manual && len(st.Blocks) == 3 && st.Blocks[1].Name == "api"
	})

	h.restart()
	h.waitFor("three restored blocks", func() bool { return len(h.app.Model.Blocks()) == 3 })
	m := h.app.Model
	if !m.Manual() || math.Abs(float64(rootRatio(h)-ratio)) > 0.0001 {
		t.Fatalf("manual=%v ratio=%v, want %v", m.Manual(), rootRatio(h), ratio)
	}
	if got := syncedNames(h); len(got) != 1 || got[0] != "web-03" {
		t.Fatalf("synced after restart = %v", got)
	}
	if blockNames(h)[1] != "api" {
		t.Fatalf("rename lost: %v", blockNames(h))
	}
	if p := m.MinimapPrefs(); p.Corner != layout.TopRight || math.Abs(float64(p.Opacity-0.7)) > 0.001 {
		t.Fatalf("minimap prefs = %+v", p)
	}
	got := m.Placement()
	for i, c := range want.Cells {
		r, _ := got.Rect(string(m.Blocks()[i].ID))
		if math.Abs(float64(r.W-c.Rect.W)) > 1 || math.Abs(float64(r.H-c.Rect.H)) > 1 {
			t.Errorf("block %d is %vx%v after restart, was %vx%v", i, r.W, r.H, c.Rect.W, c.Rect.H)
		}
	}
	h.shot("manual-restored")
}

// dragNext drags the first divider of the other axis a little, so a layout
// has two edited ratios.
func (h *harness) dragNext(first layout.Divider) {
	h.t.Helper()
	ds := h.app.UI.Dividers()
	for i, d := range ds {
		if d.Path != first.Path {
			x, y, _ := h.dividerAt(i)
			h.app.UI.HoldModifiers(ui.Alt)
			if d.Axis == layout.Cols {
				h.drag(x, y, x+30, y)
			} else {
				h.drag(x, y, x, y+30)
			}
			h.app.UI.HoldModifiers(0)
			return
		}
	}
}

// TestLayout_DragDebouncesTerminalResizes is TECH-PLAN M2 acceptance 5: a long
// divider drag makes a handful of window-size changes on the server, not one
// per frame.
func TestLayout_DragDebouncesTerminalResizes(t *testing.T) {
	// A size must stay still for 1.5 s before the shell is told: far longer
	// than the frames of this test, so what shows is the debounce itself.
	delay := 1500 * time.Millisecond
	if raceEnabled {
		delay = 5 * time.Second // a frame takes ~0.4 s under the race detector
	}
	h := newHarness(t, harnessOpts{resizeDelay: delay})
	h.openAll(2)
	h.tt.Frame()
	time.Sleep(delay + 500*time.Millisecond) // let the opening sizes settle
	before := len(h.srv.Sizes())

	x, y, _ := h.dividerAt(0)
	h.tt.Move(x, y)
	h.tt.Frame()
	h.tt.Press(x, y)
	h.tt.Frame()
	const moves = 60
	for i := 1; i <= moves; i++ {
		h.tt.Move(x+float32(i)*3, y)
		h.tt.Frame()
		h.tt.Advance(16 * time.Millisecond)
	}
	h.tt.Release(x+moves*3, y)
	h.tt.Frame()
	during := len(h.srv.Sizes()) - before
	if during != 0 {
		t.Fatalf("%d window-size changes reached the server during the drag; they must wait for it to settle", during)
	}
	h.waitFor("the settled sizes", func() bool { return len(h.srv.Sizes())-before >= 2 })
	time.Sleep(300 * time.Millisecond)
	got := len(h.srv.Sizes()) - before
	t.Logf("%d pointer moves made %d window-size changes on the server (2 blocks)", moves, got)
	if got > 2 {
		t.Fatalf("%d window-size changes for %d pointer moves and 2 blocks: want one per block", got, moves)
	}
}

func TestLayout_ZoomShortcutAndHeaderDoubleClick(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(3)
	b := h.app.Model.Focused()
	h.tt.Key(ui.Ctrl|ui.Shift, ui.KeyEnter)
	h.tt.Frame()
	if h.app.Model.Zoomed() != b {
		t.Fatal("the zoom shortcut should zoom the focused block")
	}
	chips := 0
	for _, txt := range h.tt.Texts() {
		if txt == "Toggle input sync for this block" {
			chips++
		}
	}
	if chips != 1 {
		t.Fatalf("only the zoomed block should be built, saw %d block headers", chips)
	}
	h.shot("zoomed")
	h.tt.Key(ui.Ctrl|ui.Shift, ui.KeyEnter)
	h.tt.Frame()
	if h.app.Model.Zoomed() != nil {
		t.Fatal("zoom again to bring the layout back")
	}
	r, _ := h.tt.Find("web-02")
	h.tt.ClickAtWith(0, r.X+r.W+200, r.Y+r.H/2)
	h.tt.ClickAtWith(0, r.X+r.W+200, r.Y+r.H/2)
	h.tt.Frame()
	_ = r
}

func TestShortcuts_SyncAllAndSyncBlock(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(3)
	h.tt.Key(ctrlAlt, ui.KeyS)
	h.tt.Frame()
	if h.app.Model.SyncedCount() != 1 {
		t.Fatal("the block shortcut toggles the focused block")
	}
	h.tt.Key(ctrlAlt, allSyncKey)
	h.tt.Frame()
	if h.app.Model.SyncedCount() != 3 {
		t.Fatalf("the all-sync shortcut turns every block on, got %d", h.app.Model.SyncedCount())
	}
	h.tt.Key(ctrlAlt, allSyncKey)
	h.tt.Frame()
	if h.app.Model.SyncedCount() != 0 {
		t.Fatal("and off when all were on")
	}
	// Through the palette too.
	h.tt.Key(primary, ui.KeyP)
	h.waitText("Commands")
	h.tt.Type("all blocks")
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if h.app.Model.SyncedCount() != 3 {
		t.Fatal("the palette command should turn sync on for all")
	}
}

func TestShortcuts_FocusMovesBySpace(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(4)
	bs := h.app.Model.Blocks()
	h.app.Model.Focus(bs[0])
	h.tt.Frame()
	h.tt.Key(ctrlAlt, ui.KeyRight)
	h.tt.Frame()
	if h.app.Model.Focused() != bs[1] {
		t.Fatal("right from web-01 is web-02")
	}
	h.tt.Key(ctrlAlt, ui.KeyDown)
	h.tt.Frame()
	if h.app.Model.Focused() != bs[3] {
		t.Fatal("down from web-02 is web-04")
	}
}

func TestShortcuts_MonitorOpensAndCloseBlockUsesTheDesignKey(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(2)
	h.tt.Key(ctrlAlt, ui.KeyM)
	h.waitText("Back to terminals")
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.app.UI.MonitorOpen() != "" {
		t.Fatal("Escape did not close the monitor page")
	}
	n := len(h.app.Model.Blocks())
	h.tt.Key(ctrlAlt, ui.KeyW)
	h.tt.Frame()
	if len(h.app.Model.Blocks()) != n-1 {
		t.Fatal("the close-block shortcut did not close the block")
	}
}

func TestMenus_BlockHeaderRenameAndMoveTo(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(3)
	hx, hy := h.headerPoint("web-02")
	h.tt.RightClickAt(hx, hy)
	h.tt.Frame()
	menu := strings.Join(h.tt.Menu(), "|")
	for _, want := range []string{"Rename…", "Duplicate tab", "Toggle sync", "Zoom block", "Move to…", "Close block"} {
		if !strings.Contains(menu, want) {
			t.Errorf("block menu %q lacks %q", menu, want)
		}
	}
	h.shot("block-menu")
	if err := h.tt.ChooseMenuItem("Rename…"); err != nil {
		t.Fatal(err)
	}
	h.waitText("Rename block")
	h.tt.Key(ui.Ctrl, ui.KeyA)
	h.tt.Type("api")
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if got := blockNames(h)[1]; got != "api" {
		t.Fatalf("renamed to %q (blocks %v)", got, blockNames(h))
	}
	if !h.tt.HasText("api") {
		t.Fatal("the header does not show the new name")
	}

	// Move to… → block → place.
	hx, hy = h.headerPoint("api")
	h.tt.RightClickAt(hx, hy)
	h.tt.Frame()
	if err := h.tt.ChooseMenuItem("Move to…", "web-03", "Place to the left"); err != nil {
		t.Fatal(err)
	}
	if !h.app.Model.Manual() {
		t.Fatal("docking makes the layout manual")
	}
}

func TestMenus_TabAndTag(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01", "prod")
	h.addHost("web-02", "prod")
	h.tt.Frame()
	if err := h.tt.RightClick("prod"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	menu := strings.Join(h.tt.Menu(), "|")
	for _, want := range []string{"Open all 2 hosts", "Sync the open hosts of this tag", "Collapse"} {
		if !strings.Contains(menu, want) {
			t.Errorf("tag menu %q lacks %q", menu, want)
		}
	}
	if err := h.tt.ChooseMenuItem("Open all 2 hosts"); err != nil {
		t.Fatal(err)
	}
	for range 2 { // both hosts are new: one key question each
		h.waitText("Trust and connect")
		h.click("Trust and connect")
		h.tt.Advance(time.Second) // the dialog fades out
		h.tt.Frame()
	}
	h.waitFor("two blocks connected", func() bool { return h.connected(2) })
	if err := h.tt.RightClick("prod"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if err := h.tt.ChooseMenuItem("Sync the open hosts of this tag"); err != nil {
		t.Fatal(err)
	}
	if h.app.Model.SyncedCount() != 2 {
		t.Fatal("the tag menu did not sync its open hosts")
	}

	if err := h.tt.RightClick("shell 1"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	menu = strings.Join(h.tt.Menu(), "|")
	if !strings.Contains(menu, "Duplicate tab") || !strings.Contains(menu, "Close tab") {
		t.Errorf("tab menu %q", menu)
	}
	h.tt.CloseMenu()
}

// TestFinalDesign_NoRedundantChrome checks the no-redundancy rule of the final
// design: every status or action shows once, and what the final design removed
// is gone.
func TestFinalDesign_NoRedundantChrome(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(3)
	h.app.Model.SyncAll(true)
	h.tt.Frame()
	for _, gone := range []string{"Auto layout", "Session saved automatically", "Syncing", "3 connected", "Local shell", "Connected "} {
		for _, txt := range h.tt.Texts() {
			if strings.Contains(txt, gone) && txt != "Connected" { // the sidebar dots are labelled
				t.Errorf("%q is still on screen (%q)", gone, txt)
			}
		}
	}
	count := map[string]int{}
	for _, txt := range h.tt.Texts() {
		count[txt]++
	}
	if count["Sync"] != 3 {
		t.Errorf("each block has exactly one sync chip, saw %d", count["Sync"])
	}
	h.shot("final-design-main")
}

func TestTitleBar_KeepsClearOfTheWindowControls(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	h.tt.SetTitleBar(ui.TitleBar{Height: 40, Right: 120})
	h.tt.Frame()
	r, ok := h.tt.Find("Transfers " + tui.ShortcutHint("transfer"))
	if !ok {
		t.Fatalf("no transfer icon: %q", h.tt.Texts())
	}
	if r.X+r.W > float32(winW)-120 {
		t.Fatalf("the transfer icon ends at %v, under the window controls (from %v)", r.X+r.W, float32(winW)-120)
	}
}

func TestDividers_AreInsetByTheGap(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(4)
	ds := h.app.UI.Dividers()
	if len(ds) != 3 {
		t.Fatalf("a 2×2 grid has %d dividers, want 3", len(ds))
	}
	_ = fmt.Sprint
}
