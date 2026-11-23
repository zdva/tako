package ui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/layout"
)

func (h *harness) openAll(n int) {
	h.t.Helper()
	for i := range n {
		host := h.addHost(fmt.Sprintf("web-%02d", i+1), "prod")
		if i == 0 {
			// The first connection asks about the host key; later ones share it.
			if _, err := h.app.Model.OpenHost(host, false); err != nil {
				h.t.Fatal(err)
			}
			h.waitText("Trust and connect")
			h.click("Trust and connect")
			h.waitFor("first block connected", func() bool { return h.connected(1) })
			continue
		}
		if _, err := h.app.Model.OpenHost(host, true); err != nil {
			h.t.Fatal(err)
		}
	}
	h.waitFor("all blocks connected", func() bool { return h.connected(n) })
}

func TestBlocks_AutoLayoutFollowsCount(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	for n, want := range map[int][2]int{1: {1, 1}, 2: {2, 1}, 4: {2, 2}, 6: {3, 2}} {
		cols, rows := layout.Dims(n)
		if [2]int{cols, rows} != want {
			t.Fatalf("Dims(%d) = %d×%d, want %v", n, cols, rows, want)
		}
	}
	h.openAll(4)
	h.tt.SetSize(1700, 1000)
	h.waitText("Sync 0 / 4")
	h.shot("grid-4")
}

func TestBlocks_OpenSameHostAgainFocusesAndNewBlockOnRequest(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	host := h.app.Library.Hosts()[0]
	if _, err := h.app.Model.OpenHost(host, false); err != nil {
		t.Fatal(err)
	}
	if n := len(h.app.Model.Blocks()); n != 1 {
		t.Fatalf("blocks = %d, want the existing one reused", n)
	}
	if _, err := h.app.Model.OpenHost(host, true); err != nil {
		t.Fatal(err)
	}
	if n := len(h.app.Model.Blocks()); n != 2 {
		t.Fatalf("blocks = %d, want a second block on request", n)
	}
}

func TestDisconnectBanner_ShowsWhileServerIsGoneAndKeepsScrollback(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	h.srv.Stop() // refuses new connections too, so the outage lasts
	h.waitText("Reconnect now")
	h.shot("disconnected")
	if !h.tt.HasText("web-01") || !h.tt.HasText("Sync 0 / 1") {
		t.Fatal("the block disappeared during the outage")
	}
	if n := len(h.app.Model.Blocks()); n != 1 {
		t.Fatalf("blocks = %d during the outage", n)
	}
	h.click("Reconnect now") // must not panic or drop the block
	h.waitText("Reconnect now")
}

func TestSidebar_ContextMenuOfHostRow(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01", "prod")
	h.tt.Frame()
	if err := h.tt.RightClick("web-01"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	menu := strings.Join(h.tt.Menu(), "|")
	for _, want := range []string{"Open", "Edit", "Copy address", "Copy SSH command", "Delete"} {
		if !strings.Contains(menu, want) {
			t.Errorf("host menu %q lacks %q", menu, want)
		}
	}
	h.shot("host-menu")
	if err := h.tt.ChooseMenuItem("Copy SSH command"); err != nil {
		t.Fatal(err)
	}
	if got := h.tt.Clipboard(); !strings.HasPrefix(got, "ssh ") || !strings.Contains(got, "tako@127.0.0.1") {
		t.Fatalf("clipboard = %q", got)
	}
}

func TestSidebar_DeleteAsksFirst(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01")
	h.tt.Frame()
	if err := h.tt.RightClick("web-01"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if err := h.tt.ChooseMenuItem("Delete"); err != nil {
		t.Fatal(err)
	}
	h.waitText("Delete web-01?")
	if h.app.Library.Len() != 1 {
		t.Fatal("deleted without confirmation")
	}
	h.click("Delete")
	h.waitFor("deletion", func() bool { return h.app.Library.Len() == 0 })
}

func TestSidebar_FilterNarrowsHosts(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01", "prod")
	h.addHost("db-01", "data")
	h.typeInto("Filter hosts or tags", "db")
	h.waitGone("web-01")
	if !h.tt.HasText("db-01") {
		t.Fatalf("db-01 vanished: %q", h.tt.Texts())
	}
	h.typeInto("Filter hosts or tags", "zzz")
	h.waitText("No host matches")
}

func TestPalette_OpensHostWithEnter(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01")
	h.addHost("db-01")
	h.press("palette")
	h.tt.Frame()
	h.typeInto("Search hosts, commands, tags…", "")
	h.tt.Type("db")
	h.waitText("tako@127.0.0.1")
	h.shot("palette")
	h.tt.Key(0, ui.KeyEnter)
	h.waitFor("a block", func() bool { return len(h.app.Model.Blocks()) == 1 })
	if got := h.app.Model.Blocks()[0].Name; got != "db-01" {
		t.Fatalf("opened %q, want db-01", got)
	}
}

func TestPalette_EscapeClosesAndCommandsRun(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.press("palette")
	h.waitText("Commands")
	h.tt.Key(0, ui.KeyEscape)
	h.waitGone("Commands")

	h.press("palette")
	h.waitText("Commands")
	h.tt.Type("local")
	h.tt.Key(0, ui.KeyEnter)
	h.waitFor("a local block", func() bool { return len(h.app.Model.Blocks()) == 1 })
}

func TestShortcuts_NewHostAndLocalTerminal(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.press("add-host")
	h.waitText("Test connection")
	h.tt.Key(0, ui.KeyEscape)
	h.waitGone("Test connection")
	h.press("new-tab")
	h.waitFor("a local block", func() bool { return len(h.app.Model.Blocks()) == 1 })
	h.shot("local-terminal")
}

func TestSettings_SwitchLanguage(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.press("settings")
	h.waitText("Appearance")
	h.click("简体中文")
	h.waitText("语言")
	h.shot("settings-zh")
}

func TestTabs_AddAndClose(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	b := h.app.Model.Blocks()[0]
	h.click("New tab")
	h.waitFor("two tabs", func() bool { return len(b.Tabs) == 2 })
	h.shot("two-tabs")
}
