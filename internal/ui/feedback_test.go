package ui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/settings"
	tui "github.com/zdva/tako/internal/ui"
)

// closeTabButtons returns the tabs on screen, left to right. Each tab's ×
// button is folded into the tab for assistive technology, so the tests click
// it at its place: the right end of the tab (padding 4, button 16).
func (h *harness) closeTabButtons() []ui.AccessInfo {
	h.t.Helper()
	h.tt.Frame()
	h.tt.Advance(500 * time.Millisecond) // the minimap glides off to its corner
	h.tt.Frame()
	var out []ui.AccessInfo
	for _, info := range h.tt.AccessAll() {
		if info.Role == ui.RoleTab && strings.HasPrefix(info.Name, "shell ") {
			out = append(out, info)
		}
	}
	slices.SortFunc(out, func(a, b ui.AccessInfo) int {
		switch {
		case a.Bounds.X < b.Bounds.X:
			return -1
		case a.Bounds.X > b.Bounds.X:
			return 1
		}
		return 0
	})
	return out
}

func (h *harness) clickInfo(info ui.AccessInfo) {
	h.t.Helper()
	x, y := float32(info.Bounds.X+info.Bounds.W-4-8), float32(info.Bounds.Y+info.Bounds.H/2)
	h.tt.Move(x, y)
	h.tt.Frame()
	h.tt.ClickAt(x, y)
	h.tt.Frame()
	h.tt.Frame()
}

// TestTabs_CloseWithTheMouse closes the first, the middle and the last tab with
// the tab's × button, then the only one left, which closes the block.
func TestTabs_CloseWithTheMouse(t *testing.T) {
	for _, tc := range []struct {
		name  string
		pick  int // index of the tab to close among three
		after int // tabs left
	}{
		{"first", 0, 2},
		{"middle", 1, 2},
		{"last", 2, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{})
			h.openAll(1)
			b := h.app.Model.Blocks()[0]
			for range 2 {
				if err := h.app.Model.AddTab(b); err != nil {
					t.Fatal(err)
				}
			}
			h.waitFor("three tabs", func() bool { return len(b.Tabs) == 3 })
			keep := []string{}
			for i, tab := range b.Tabs {
				if i != tc.pick {
					keep = append(keep, tab.ID)
				}
			}
			// The × shows on hover; move over the tab first, as a user does.
			btns := h.closeTabButtons()
			if len(btns) != 3 {
				t.Fatalf("%d close buttons, want 3", len(btns))
			}
			h.clickInfo(btns[tc.pick])
			h.waitFor("tab closed", func() bool { return len(b.Tabs) == tc.after })
			for i, tab := range b.Tabs {
				if tab.ID != keep[i] {
					t.Fatalf("tab %d is %s, want %s", i, tab.ID, keep[i])
				}
			}
			if b.ActiveTab() == nil {
				t.Fatal("no active tab after closing")
			}
			// Close the rest one by one; the block goes with the last.
			for n := len(b.Tabs); n > 0; n-- {
				btns = h.closeTabButtons()
				if len(btns) != n {
					t.Fatalf("%d tabs on screen, want %d", len(btns), n)
				}
				h.clickInfo(btns[0])
				h.waitFor("one tab fewer", func() bool { return len(b.Tabs) == n-1 })
			}
			if len(h.app.Model.Blocks()) != 0 {
				t.Fatal("the block should close with its last tab")
			}
		})
	}
}

// TestTabs_CloseWithTheShortcut closes tabs with Ctrl+Shift+W until the block
// is gone.
func TestTabs_CloseWithTheShortcut(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	b := h.app.Model.Blocks()[0]
	for range 2 {
		if err := h.app.Model.AddTab(b); err != nil {
			t.Fatal(err)
		}
	}
	h.app.Model.SetActive(b, 1) // the middle one
	h.tt.Frame()
	for want := 2; want >= 0; want-- {
		h.tt.Key(ui.Ctrl|ui.Shift, ui.KeyW)
		h.tt.Frame()
		h.tt.Frame()
		if want == 0 {
			if len(h.app.Model.Blocks()) != 0 {
				t.Fatal("the block should close with its last tab")
			}
			break
		}
		if len(b.Tabs) != want {
			t.Fatalf("tabs = %d, want %d", len(b.Tabs), want)
		}
	}
}

// TestTabs_CloseFromTheTabMenu closes the first of three tabs from its
// right-click menu, which used to edit the tabs while they were being drawn.
func TestTabs_CloseFromTheTabMenu(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	b := h.app.Model.Blocks()[0]
	for range 2 {
		if err := h.app.Model.AddTab(b); err != nil {
			t.Fatal(err)
		}
	}
	tabs := h.closeTabButtons()
	if len(tabs) != 3 {
		t.Fatalf("%d tabs on screen", len(tabs))
	}
	first := tabs[0]
	h.tt.RightClickAt(float32(first.Bounds.X+10), float32(first.Bounds.Y+first.Bounds.H/2))
	h.tt.Frame()
	if err := h.tt.ChooseMenuItem("Close tab"); err != nil {
		t.Fatalf("%v; menu %q", err, h.tt.Menu())
	}
	h.tt.Frame()
	h.tt.Frame()
	if len(b.Tabs) != 2 {
		t.Fatalf("tabs = %d, want 2", len(b.Tabs))
	}
}

// TestLayout_NarrowWindowKeepsBlocksUsable opens six blocks in the smallest
// window Tako allows (720×480): the automatic grid must drop columns rather
// than squeeze blocks under their minimum width, and every block's header
// controls must stay inside the block.
func TestLayout_NarrowWindowKeepsBlocksUsable(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(6)
	h.tt.SetSize(720, 480)
	h.tt.Frame()
	h.tt.Advance(500 * time.Millisecond)
	h.tt.Frame()
	if !h.app.Model.Scrolls() {
		t.Error("six blocks at their minimum size cannot fit 720×480: the grid should scroll")
	}
	place := h.app.Model.Placement()
	if len(place.Cells) != 6 {
		t.Fatalf("%d cells", len(place.Cells))
	}
	for _, c := range place.Cells {
		if c.Rect.W < tui.MinBlockWidth-0.5 {
			t.Errorf("block %s is %.0f wide, under the minimum %d", c.ID, c.Rect.W, tui.MinBlockWidth)
		}
	}
	// Every "More actions" button sits inside its block's cell.
	var mores []ui.AccessInfo
	for _, info := range h.tt.AccessAll() {
		if info.Name == "More actions" {
			mores = append(mores, info)
		}
	}
	if len(mores) != 6 {
		t.Fatalf("%d block headers, want 6", len(mores))
	}
	areaRight := float32(720 - tui.AreaPad)
	for _, m := range mores {
		if r := float32(m.Bounds.X + m.Bounds.W); r > areaRight+0.5 {
			t.Errorf("a header's More button ends at %.0f, past the main area (%.0f)", r, areaRight)
		}
	}
	// A split of a grid that does not fit cannot be resized in the window.
	if d := h.app.UI.Dividers(); len(d) > 0 {
		h.app.Model.DragDivider(d[0].Path, d[0].Rect.Y+40, true)
		if h.app.Model.Manual() {
			t.Error("dragging a divider while the grid scrolls should change nothing")
		}
	}

	// The compact setting lets blocks get narrower, so two columns fit.
	if err := h.app.Settings.Update(func(v *settings.Values) { v.CompactBlocks = true }); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	h.tt.Frame()
	xs := map[float32]bool{}
	for _, c := range h.app.Model.Placement().Cells {
		xs[c.Rect.X] = true
	}
	if len(xs) != 2 {
		t.Errorf("compact blocks: %d columns, want 2", len(xs))
	}
}

// TestLayout_LongNameAndManyTabsKeepTheHeaderButtons gives a narrow block a
// long name and six tabs: the new-tab, sync and more buttons must stay inside
// the block.
func TestLayout_LongNameAndManyTabsKeepTheHeaderButtons(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	b := h.app.Model.Blocks()[0]
	h.app.Model.Rename(b, "production-database-primary-replica-eu-west-1")
	for range 5 {
		if err := h.app.Model.AddTab(b); err != nil {
			t.Fatal(err)
		}
	}
	h.tt.SetSize(720, 480)
	h.tt.Frame()
	h.tt.Advance(500 * time.Millisecond)
	h.tt.Frame()
	cell, _ := h.app.Model.Placement().Rect(string(b.ID))
	left := float32(tui.SidebarWidth+1+tui.AreaPad) + cell.X
	right := left + cell.W
	for _, name := range []string{"New tab", "Toggle input sync for this block", "More actions"} {
		info, ok := h.tt.Access(name)
		if !ok {
			t.Fatalf("no %q", name)
		}
		if l, r := float32(info.Bounds.X), float32(info.Bounds.X+info.Bounds.W); l < left || r > right+0.5 {
			t.Errorf("%q spans %.0f–%.0f, outside the block %.0f–%.0f", name, l, r, left, right)
		}
	}
}
