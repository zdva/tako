package ui_test

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestA11y_MainControlsHaveRolesAndNames(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01", "prod")
	h.tt.Frame()
	for _, want := range []struct {
		role ui.Role
		name string
	}{
		{ui.RoleButton, "Add host"},
		{ui.RoleButton, "Local terminal"},
		{ui.RoleButton, "Settings"},
		{ui.RoleButton, "Show or hide the sidebar"},
		{ui.RoleTextField, "Filter hosts or tags"},
	} {
		if _, ok := h.tt.AccessByRole(want.role, want.name); !ok {
			t.Errorf("no %v named %q; screen %q", want.role, want.name, h.tt.Texts())
		}
	}
}

func TestA11y_TabVisitsTheMainControlsAndShiftTabComesBack(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01", "prod")
	h.tt.Frame()
	want := []string{"Filter hosts or tags", "Add host", "Local terminal", "Settings"}
	visited := map[string]bool{}
	for range 60 {
		h.tt.Key(0, ui.KeyTab)
		for _, name := range want {
			if h.tt.Focused(name) {
				visited[name] = true
			}
		}
	}
	for _, name := range want {
		if !visited[name] {
			t.Errorf("Tab never reached %q", name)
		}
	}
	// Shift+Tab walks back to the field it started from.
	back := map[string]bool{}
	for range 60 {
		h.tt.Key(ui.Shift, ui.KeyTab)
		for _, name := range want {
			if h.tt.Focused(name) {
				back[name] = true
			}
		}
	}
	if !back["Filter hosts or tags"] {
		t.Error("Shift+Tab never came back to the filter field")
	}
}

func TestSettings_CompactBlocksStartsOff(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	if h.prefs().CompactBlocks {
		t.Fatal("compact blocks is on before settings is opened")
	}
	h.openSettings("general")
	sw, ok := h.tt.AccessByRole(ui.RoleSwitch, "Compact blocks")
	if !ok {
		t.Fatal("no compact-blocks switch")
	}
	if sw.Checked {
		t.Fatal("compact blocks reads as on; the default is off")
	}
}

func TestA11y_SettingsKeepsKeyboardFocusInsideTheWindow(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openSettings("")
	for range 40 {
		h.tt.Key(0, ui.KeyTab)
		for _, behind := range []string{"Add host", "Local terminal", "Filter hosts or tags"} {
			if h.tt.Focused(behind) {
				t.Fatalf("focus escaped to %q behind the settings window", behind)
			}
		}
	}
}

func TestA11y_SettingsStateIsExposed(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openSettings("terminal")
	sel, ok := h.tt.AccessByRole(ui.RoleRadio, "Tako (follows appearance)")
	if !ok || !sel.Checked {
		t.Fatalf("the default scheme should read as selected: %+v %v", sel, ok)
	}
	h.clickRadio("Nord")
	now, _ := h.tt.AccessByRole(ui.RoleRadio, "Nord")
	old, _ := h.tt.AccessByRole(ui.RoleRadio, "Tako (follows appearance)")
	if !now.Checked || old.Checked {
		t.Errorf("selection did not move: nord=%v default=%v", now.Checked, old.Checked)
	}
	h.openSettings("general")
	if g, ok := h.tt.Access("Language"); !ok {
		t.Errorf("the language control has no accessible name: %+v", g)
	}
	if !slices.Contains(h.tt.Texts(), "Settings") {
		t.Error("the window has no title")
	}
}

func TestIME_ComposingInATextFieldShowsTheCaretAndCommits(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openSettings("data")
	h.tt.Frame()
	info, ok := h.tt.AccessByRole(ui.RoleTextField, "Xshell, SecureCRT, Termius or Tako file or folder")
	if !ok {
		t.Fatal("no import field")
	}
	h.tt.ClickAt(float32(info.Bounds.X+20), float32(info.Bounds.Y+info.Bounds.H/2))
	h.tt.Frame()
	if active, none := h.tt.TextInputActive(); !active || none {
		t.Fatalf("input methods should be on in a text field: active=%v none=%v", active, none)
	}
	h.tt.Compose("ni hao", 2)
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok || caret.X < info.Bounds.X || caret.X > info.Bounds.X+info.Bounds.W || caret.Y < info.Bounds.Y {
		t.Errorf("the candidate window would open at %v, off the field %v", caret, info.Bounds)
	}
	h.tt.Compose("", 0)
	h.tt.Type("你好")
	h.tt.Frame()
	got, _ := h.tt.AccessByRole(ui.RoleTextField, "Xshell, SecureCRT, Termius or Tako file or folder")
	if got.Value != "你好" {
		t.Errorf("the committed text is %q", got.Value)
	}
}

func TestA11y_KeyboardActivatesButtonsAndRows(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01", "prod")
	h.tt.Frame()
	focus := func(name string) {
		t.Helper()
		for range 60 {
			if h.tt.Focused(name) {
				return
			}
			h.tt.Key(0, ui.KeyTab)
		}
		t.Fatalf("Tab never reached %q", name)
	}
	focus("Settings")
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if !h.tt.HasText("Appearance") && !h.tt.HasText("Language") {
		t.Fatalf("Enter on Settings did not open it; screen %q", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	focus("web-01, tako@127.0.0.1")
	h.tt.Key(0, ui.KeySpace)
	h.waitText("Trust and connect")
}

func TestIME_TerminalTakesCompositionAtItsCursor(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	h.tt.ClickAt(700, 380)
	h.tt.Frame()
	active, none := h.tt.TextInputActive()
	if !active || none {
		t.Fatalf("the terminal should accept input methods: active=%v none=%v", active, none)
	}
	h.tt.Compose("zhong", 5)
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok || caret.X < sidebarEdge || caret.Y < 40 {
		t.Errorf("the candidate window would open at %v (ok=%v), away from the terminal", caret, ok)
	}
	h.tt.Compose("", 0)
}

// sidebarEdge is the left edge of the grid.
const sidebarEdge = 200
