package workspace_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/wei-dong/tako/internal/layout"
	"github.com/wei-dong/tako/internal/testutil/memssh"
	"github.com/wei-dong/tako/internal/workspace"
)

func (e *env) open(t *testing.T, n int) []*workspace.Block {
	t.Helper()
	var out []*workspace.Block
	for i := range n {
		b, err := e.m.OpenHost(e.host("h"+string(rune('a'+i))), false)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func ids(bs []*workspace.Block) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = string(b.ID)
	}
	return out
}

func TestAutoLayoutThenFirstDragMakesItManual(t *testing.T) {
	e := newEnv(t)
	bs := e.open(t, 4)
	if e.m.Manual() {
		t.Fatal("a fresh workspace is automatic")
	}
	p := e.m.Placement()
	if len(p.Cells) != 4 || len(p.Dividers) != 3 {
		t.Fatalf("2x2 grid: %d cells, %d dividers", len(p.Cells), len(p.Dividers))
	}
	// Drag the root divider (between the two rows) near the middle: it snaps.
	g := e.m.DragDivider("", 403, true)
	if !e.m.Manual() || !g.Valid {
		t.Fatalf("manual = %v, guide = %+v", e.m.Manual(), g)
	}
	// A new block opened now goes beside the focused one, not at the end.
	e.m.Focus(bs[0])
	nb, err := e.m.OpenHost(e.host("new"), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.m.Tree().IDs()) != 5 || !e.m.Tree().Has(string(nb.ID)) {
		t.Fatalf("tree = %v", e.m.Tree().IDs())
	}
	r0, _ := e.m.Placement().Rect(string(bs[0].ID))
	rn, _ := e.m.Placement().Rect(string(nb.ID))
	if r0.Y != rn.Y && r0.X != rn.X {
		t.Errorf("the new block %+v is not next to the focused one %+v", rn, r0)
	}
	e.m.CloseBlock(nb)
	if len(e.m.Tree().IDs()) != 4 {
		t.Fatalf("closing a block must leave the tree: %v", e.m.Tree().IDs())
	}
	e.m.ResetLayout()
	if e.m.Manual() {
		t.Error("ResetLayout must go back to the grid")
	}
}

func TestPresetsAndReposition(t *testing.T) {
	e := newEnv(t)
	bs := e.open(t, 3)
	e.m.ApplyPreset(workspace.PresetRow)
	if got := e.m.Tree().IDs(); !slices.Equal(got, ids(bs)) {
		t.Fatalf("row = %v", got)
	}
	e.m.Reposition(bs[0], bs[2])
	if got := e.m.Tree().IDs(); !slices.Equal(got, []string{string(bs[2].ID), string(bs[1].ID), string(bs[0].ID)}) {
		t.Fatalf("manual layouts swap: %v", got)
	}
	e.m.ApplyPreset(workspace.PresetAuto)
	e.m.Reposition(bs[0], bs[2])
	if got := e.m.Blocks(); got[2] != bs[0] || got[0] != bs[1] {
		t.Fatalf("the grid moves the block: %v", ids(got))
	}
	e.m.Dock(bs[1], bs[2], layout.Left)
	if !e.m.Manual() || e.m.Tree().Root == nil {
		t.Fatal("docking makes the layout manual")
	}
}

func TestMoveFocusFollowsTheLayout(t *testing.T) {
	e := newEnv(t)
	bs := e.open(t, 3) // a b / c (wide)
	e.m.Focus(bs[2])
	e.m.MoveFocus(0, -1)
	if e.m.Focused() != bs[0] && e.m.Focused() != bs[1] {
		t.Error("up from the wide bottom block reaches a top one")
	}
	e.m.Focus(bs[1])
	e.m.MoveFocus(-1, 0)
	if e.m.Focused() != bs[0] {
		t.Error("left from b is a")
	}
	e.m.MoveFocus(-1, 0)
	if e.m.Focused() != bs[0] {
		t.Error("nothing is left of a")
	}
}

// TestManualLayoutAndSyncSurviveARestart is TECH-PLAN M2 acceptance 6.
func TestManualLayoutAndSyncSurviveARestart(t *testing.T) {
	e := newEnv(t)
	bs := e.open(t, 4)
	e.m.DragDivider("", 300, false)
	e.m.DragDivider("A", 380, false)
	e.m.SyncOnly([]*workspace.Block{bs[1], bs[3]})
	e.m.SetMinimap(workspace.Minimap{Corner: layout.TopLeft, Opacity: 0.8})
	e.m.Rename(bs[2], "db")
	before := e.m.Placement()
	snap := e.m.Snapshot()
	if !snap.Layout.Manual {
		t.Fatal("the snapshot must carry the manual layout")
	}

	e.m.Close()
	if n := e.m.Restore(snap); n != 4 {
		t.Fatalf("restored %d", n)
	}
	got := e.m.Blocks()
	if !e.m.Manual() || !e.m.Bus().Synced(got[1].ID) || !e.m.Bus().Synced(got[3].ID) || e.m.Bus().Synced(got[0].ID) {
		t.Fatal("manual layout or sync state lost")
	}
	if got[2].Name != "db" {
		t.Errorf("rename lost: %q", got[2].Name)
	}
	after := e.m.Placement()
	for i, c := range before.Cells {
		want, _ := after.Rect(string(got[i].ID))
		if c.Rect != want {
			t.Errorf("block %d moved: %+v -> %+v", i, c.Rect, want)
		}
	}
	if mp := e.m.MinimapPrefs(); mp.Corner != layout.TopLeft || mp.Opacity != 0.8 {
		t.Errorf("minimap prefs = %+v", mp)
	}
}

func TestRestoreIgnoresABrokenLayoutAndDroppedHosts(t *testing.T) {
	e := newEnv(t)
	e.open(t, 3)
	snap := e.m.Snapshot()
	snap.Layout = workspace.LayoutState{Manual: true, Tree: layout.Tree{Root: &layout.Node{Axis: layout.Cols, Ratio: .5, A: &layout.Node{ID: "b0"}}}}
	e.m.Close()
	e.m.Restore(snap)
	if e.m.Manual() {
		t.Error("a malformed layout must fall back to the grid")
	}

	// A layout that names a block that is gone is repaired around the rest.
	e.m.Close()
	e.m.Restore(e.m.Snapshot()) // nothing: the workspace is empty
	e.open(t, 3)
	e.m.ApplyPreset(workspace.PresetRow)
	snap = e.m.Snapshot()
	e.m.Close()
	snap.Layout = workspace.LayoutState{Manual: true, Tree: layout.Row([]string{"b0", "b1", "b2", "b9"}, layout.Size{W: 900, H: 600}, 8)}
	if n := e.m.Restore(snap); n != 3 {
		t.Fatalf("restored %d", n)
	}
	if !e.m.Manual() || len(e.m.Tree().IDs()) != 3 {
		t.Errorf("tree = %v", e.m.Tree().IDs())
	}
}

func TestSyncOperations(t *testing.T) {
	e := newEnv(t)
	bs := e.open(t, 3)
	e.m.SyncAll(true)
	if e.m.SyncedCount() != 3 {
		t.Fatal("SyncAll(true)")
	}
	e.m.ToggleSyncAll()
	if e.m.SyncedCount() != 0 {
		t.Fatal("ToggleSyncAll with everything on turns it off")
	}
	e.m.SetSyncedMany(bs[:1], true)
	e.m.ToggleSyncAll()
	if e.m.SyncedCount() != 3 {
		t.Fatal("ToggleSyncAll with some off turns all on")
	}
	e.m.SyncOnly(bs[2:])
	if e.m.SyncedCount() != 1 || !bs[2].Synced || !e.m.Bus().Synced(bs[2].ID) || e.m.Bus().Synced(bs[0].ID) {
		t.Fatal("SyncOnly")
	}
}

func TestZoomAndMinimapClamp(t *testing.T) {
	e := newEnv(t)
	bs := e.open(t, 2)
	e.m.ToggleZoom(bs[0])
	if e.m.Zoomed() != bs[0] {
		t.Fatal("zoom")
	}
	e.m.CloseBlock(bs[0])
	if e.m.Zoomed() != nil {
		t.Fatal("closing the zoomed block ends the zoom")
	}
	e.m.SetMinimap(workspace.Minimap{Opacity: 0.1})
	if e.m.MinimapPrefs().Opacity != workspace.MinOpacity {
		t.Fatalf("opacity = %v", e.m.MinimapPrefs().Opacity)
	}
}

// TestSyncPausesWhileDisconnectedAndResumes is TECH-PLAN M2 acceptance 4: the
// block whose connection is down stops taking mirrored input, the others keep
// working, and it takes part again after it reconnects.
func TestSyncPausesWhileDisconnectedAndResumes(t *testing.T) {
	e := newEnv(t)
	srv2, err := memssh.Start(t.Context(), memssh.Options{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv2.Stop)
	addr, hostKey := srv2.Addr(), srv2.HostSigner()
	hostB := e.host("b")
	hostB.Addr, hostB.Port = splitAddr(addr)
	e.hosts[hostB.ID] = hostB

	a, _ := e.m.OpenHost(e.host("a"), false)
	b, _ := e.m.OpenHost(hostB, false)
	c, _ := e.m.OpenHost(e.host("c"), false)
	for _, blk := range []*workspace.Block{a, b, c} {
		e.waitFor(t, "pane "+blk.Name, func() bool { return blk.ActiveTab().Pane.Connected() })
	}
	e.m.SyncAll(true)

	srv2.Stop()
	e.waitFor(t, "b to notice the outage", func() bool { return e.m.SyncPaused(b) })
	if e.m.SyncPaused(a) || e.m.SyncPaused(c) {
		t.Fatal("only the disconnected block is paused")
	}
	if !b.Synced {
		t.Fatal("pausing must not turn sync off")
	}

	a.ActiveTab().Term.Send([]byte("one\n"))
	e.waitFor(t, "a and c to get the input", func() bool { return strings.Count(string(e.srv.Typed()), "one\n") >= 2 })
	e.waitFor(t, "the drop to be counted", func() bool { return b.ActiveTab().Pane.Dropped() > 0 })

	srv2, err = memssh.Start(t.Context(), memssh.Options{Password: "pw", Addr: addr, HostKey: hostKey})
	if err != nil {
		t.Skipf("cannot rebind %s: %v", addr, err)
	}
	t.Cleanup(srv2.Stop)
	e.waitFor(t, "b to reconnect", func() bool { return !e.m.SyncPaused(b) && b.ActiveTab().Pane.Connected() })
	a.ActiveTab().Term.Send([]byte("two\n"))
	e.waitFor(t, "b to get the input again", func() bool { return strings.Contains(string(srv2.Typed()), "two\n") })
}

func splitAddr(addr string) (string, int) {
	h, ps, _ := strings.Cut(addr, ":")
	var port int
	for _, c := range ps {
		port = port*10 + int(c-'0')
	}
	return h, port
}
