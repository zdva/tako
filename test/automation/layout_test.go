//go:build e2e

package automation

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// TestWindow_LaunchLayoutTabsOpacity is the real-window check of the
// launch and layout feedback: no system credential prompt, compact blocks
// stays off, a narrow window keeps block controls inside the window with
// gaps between them, closing a tab does not kill the process, and the
// minimap opacity slider changes both the saved value and the pixels.
func TestWindow_LaunchLayoutTabsOpacity(t *testing.T) {
	s := startSession(t, launchOpts{size: "720x480"})
	s.noKeychain()
	s.noDialog()
	s.mustKey("footer:local")
	s.mustKey("footer:settings")

	s.pressKey("footer:settings")
	s.settle()
	sw := s.mustKey("settings-compact")
	if sw.has("checked") {
		t.Fatal("compact blocks is on; the default is off")
	}
	s.pressKey("settings-done")
	s.settle()
	if settingsCompact(filepath.Join(s.dirs.Config, "settings.json")) {
		t.Fatal("settings.json turned compact blocks on")
	}
	s.noKeychain()

	box := s.contentBox()
	t.Logf("content %.0f×%.0f", box.Width, box.Height)
	if box.Width < 680 || box.Width > 900 || box.Height < 430 || box.Height > 700 {
		t.Fatalf("content is %.0f×%.0f; TAKO_WINDOW=720x480 should open a narrow window, not a fullscreen one", box.Width, box.Height)
	}

	for range 4 {
		s.openLocal()
	}
	s.checkBlockLayout(box)

	s.closeOneTab()
	if _, err := s.cl.call("windows", nil); err != nil {
		t.Fatalf("the app stopped answering after a tab was closed: %v", err)
	}

	s.checkOpacitySlider()
	if settingsCompact(filepath.Join(s.dirs.Config, "settings.json")) {
		t.Fatal("the opacity check turned compact blocks on")
	}
}

func (s *session) openLocal() {
	s.t.Helper()
	before := len(s.tree().byLabel("Terminal"))
	s.pressKey("footer:local")
	s.settle()
	s.wait(15*time.Second, "a new local terminal", func() bool {
		return len(s.tree().byLabel("Terminal")) > before
	})
}

func (s *session) checkBlockLayout(box rect) {
	s.t.Helper()
	tr := s.tree()
	mores := tr.byKey("block-more")
	if len(mores) != 4 {
		s.t.Fatalf("%d block-more controls, want 4", len(mores))
	}
	for i, more := range mores {
		if more.Bounds.Width < 16 || more.Bounds.Height < 16 {
			s.t.Errorf("block-more %d is %.0f×%.0f", i, more.Bounds.Width, more.Bounds.Height)
		}
		if more.Bounds.X < -1 || more.right() > box.Width+1 {
			s.t.Errorf("block-more %d spans %.0f–%.0f, outside the content width %.0f", i, more.Bounds.X, more.right(), box.Width)
		}
		sync, okS := nearestLeft(tr.byKey("sync"), more, 24)
		newer, okN := nearestLeft(tr.byKey("tab-new"), more, 24)
		if !okS || !okN {
			s.t.Errorf("block %d is missing its sync or new-tab control", i)
			continue
		}
		if newer.right() > sync.Bounds.X+1 || sync.right() > more.Bounds.X+1 {
			s.t.Errorf("header controls overlap: new-tab ends %.0f, sync %.0f–%.0f, more starts %.0f", newer.right(), sync.Bounds.X, sync.right(), more.Bounds.X)
		}
		if more.Bounds.X-sync.right() < 2 {
			s.t.Errorf("sync and more are %.0f apart, want a gap", more.Bounds.X-sync.right())
		}
		for _, ctrl := range []node{newer, sync, more} {
			if ctrl.Bounds.X < -1 || ctrl.right() > box.Width+1 {
				s.t.Errorf("%s spans %.0f–%.0f, outside the content width %.0f", ctrl.Key, ctrl.Bounds.X, ctrl.right(), box.Width)
			}
		}
	}
	slices.SortFunc(mores, func(a, b node) int {
		switch {
		case a.Bounds.Y < b.Bounds.Y-8:
			return -1
		case a.Bounds.Y > b.Bounds.Y+8:
			return 1
		case a.Bounds.X < b.Bounds.X:
			return -1
		case a.Bounds.X > b.Bounds.X:
			return 1
		default:
			return 0
		}
	})
	for i := 1; i < len(mores); i++ {
		a, b := mores[i-1], mores[i]
		if intersects(a.Bounds, b.Bounds) {
			s.t.Errorf("block headers overlap: %.0f,%.0f and %.0f,%.0f", a.Bounds.X, a.Bounds.Y, b.Bounds.X, b.Bounds.Y)
		}
		dy := b.Bounds.Y - a.Bounds.Y
		if dy > 20 && dy < 80 {
			s.t.Errorf("stacked blocks are only %.0f apart; a block should keep its minimum height", dy)
		}
	}
	onScreen := 0
	for _, more := range mores {
		if more.Bounds.Y >= -1 && more.bottom() <= box.Height+1 {
			onScreen++
		}
	}
	if onScreen == 0 {
		s.t.Fatal("every block header is outside the window")
	}
}

// nearestLeft returns the control just left of ref on the same header row.
func nearestLeft(all []node, ref node, row float64) (node, bool) {
	best := node{}
	found := false
	bestX := -1.0
	for _, n := range all {
		if absFloat(n.Bounds.Y-ref.Bounds.Y) > row || n.right() > ref.Bounds.X+1 {
			continue
		}
		if !found || n.Bounds.X > bestX {
			best, bestX, found = n, n.Bounds.X, true
		}
	}
	return best, found
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func (s *session) closeOneTab() {
	s.t.Helper()
	tr := s.tree()
	var header node
	for _, more := range tr.byKey("block-more") {
		if more.Bounds.Y >= -1 {
			header = more
			break
		}
	}
	if header.ID == "" {
		s.t.Fatal("no on-screen block to close a tab in")
	}
	newer, ok := nearestLeft(tr.byKey("tab-new"), header, 24)
	if !ok {
		s.t.Fatal("no new-tab button on the first block")
	}
	before := tabsNear(tr, newer.Bounds.Y)
	s.pressID(newer.ID)
	s.settle()
	s.wait(5*time.Second, "a second tab", func() bool {
		return len(tabsNear(s.tree(), newer.Bounds.Y)) > len(before)
	})
	tabs := tabsNear(s.tree(), newer.Bounds.Y)
	slices.SortFunc(tabs, func(a, b node) int {
		switch {
		case a.Bounds.X < b.Bounds.X:
			return -1
		case a.Bounds.X > b.Bounds.X:
			return 1
		default:
			return 0
		}
	})
	// The × sits at the right of the tab (padding 4, button 16) and is
	// folded into the tab for assistive technology.
	tab := tabs[0]
	x, y := tab.right()-12, tab.Bounds.cy()
	s.pointer("pointerMove", x, y)
	s.settle()
	s.click(x, y)
	s.settle()
	s.wait(5*time.Second, "the tab to close", func() bool {
		return len(tabsNear(s.tree(), newer.Bounds.Y)) == len(before)
	})
}

func tabsNear(tr uiTree, y float64) []node {
	var out []node
	for _, n := range tr.byRole("tab") {
		if absFloat(n.Bounds.Y-y) < 30 {
			out = append(out, n)
		}
	}
	return out
}

func (s *session) checkOpacitySlider() {
	s.t.Helper()
	sl := s.mustKey("minimap-opacity")
	if sl.Now == nil || *sl.Now < 0.45 || *sl.Now > 0.55 {
		s.t.Fatalf("opacity slider starts at %v, want about 0.5", sl.Now)
	}
	sx, sy := s.opacitySample()
	leftX := sl.Bounds.X + 1
	leftY := sl.Bounds.cy()
	s.pointer("pointerMove", leftX, leftY)
	s.settle()
	s.pointer("pointerDown", leftX, leftY)
	s.settle()
	lowImg, w, h := s.shot()
	box := s.contentBox()
	low := s.pixel(lowImg, w, h, box, sx, sy)
	held := s.mustKey("minimap-opacity")
	if held.Now == nil || *held.Now > 0.42 {
		s.t.Fatalf("pressing the low end of the slider left opacity at %v", held.Now)
	}
	rightX := held.Bounds.X + held.Bounds.Width - 1
	s.pointer("pointerMove", rightX, leftY)
	s.settle()
	highImg, w, h := s.shot()
	high := s.pixel(highImg, w, h, box, sx, sy)
	dragged := s.mustKey("minimap-opacity")
	if dragged.Now == nil || *dragged.Now < 0.9 {
		s.t.Fatalf("dragging the slider to the right left opacity at %v", dragged.Now)
	}
	if d := colorDist(low, high); d < 8 {
		s.t.Errorf("the minimap pixel did not change with the slider (%v vs %v, distance %d)", low, high, d)
	}
	s.pointer("pointerUp", rightX, leftY)
	s.settle()
	s.wait(4*time.Second, "session.json to store the opacity", func() bool {
		op, ok := readOpacity(s.dirs.Session())
		return ok && op >= 0.9
	})
}

// opacitySample is a point on the minimap panel, not on the slider thumb,
// so a capture there shows the resting glass rather than the control.
func (s *session) opacitySample() (x, y float64) {
	s.t.Helper()
	tr := s.tree()
	if sq, ok := tr.one("mm-squares"); ok {
		return sq.Bounds.cx(), sq.Bounds.Y - 3
	}
	sl, ok := tr.one("minimap-opacity")
	if !ok {
		s.t.Fatal("no minimap opacity slider")
	}
	return sl.Bounds.X - 8, sl.Bounds.cy()
}
