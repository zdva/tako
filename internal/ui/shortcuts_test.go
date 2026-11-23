package ui_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	tui "github.com/zdva/tako/internal/ui"
)

// designShortcuts reads the shortcut table of docs/DESIGN.md §4.6: the keys
// in the first column, as written for macOS.
func designShortcuts(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile("../../docs/DESIGN.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(data), "### 4.6")
	if !ok {
		t.Fatal("DESIGN.md has no §4.6")
	}
	section, _, _ = strings.Cut(section, "### 4.7")
	row := regexp.MustCompile(`(?m)^\| (⌘[^|]*?) \| [^|]+ \|$`)
	var keys []string
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		keys = append(keys, m[1])
	}
	if len(keys) < 14 {
		t.Fatalf("read only %d shortcuts from DESIGN.md §4.6: %v", len(keys), keys)
	}
	return keys
}

// TestShortcuts_MatchTheDesignTable is TECH-PLAN M2 acceptance 7: every
// shortcut of the DESIGN operation→key table exists, in the macOS spelling,
// and off macOS is the same keys with Ctrl+Shift for ⌘ (and Alt as the second
// modifier where a ⌥ or ⇧ comes with it).
func TestShortcuts_MatchTheDesignTable(t *testing.T) {
	design := designShortcuts(t)
	have := map[string]bool{}
	for _, s := range tui.Shortcuts() {
		if s.Group != "" {
			have[s.Group] = true
		}
	}
	for _, k := range design {
		if !have[k] {
			t.Errorf("DESIGN.md §4.6 has %q, the shortcut table does not", k)
		}
	}
	want := map[string]bool{}
	for _, k := range design {
		want[k] = true
	}
	for g := range have {
		if !want[g] {
			t.Errorf("the shortcut table has %q, DESIGN.md §4.6 does not", g)
		}
	}

	defer tui.SetGOOS("linux")()
	for _, s := range tui.Shortcuts() {
		if s.Group == "" {
			continue
		}
		if !strings.HasPrefix(s.Label, "Ctrl+Shift") {
			t.Errorf("%q is %q off macOS, want Ctrl+Shift…", s.Group, s.Label)
		}
		second := strings.ContainsAny(s.Group, "⌥⇧")
		if got := strings.Contains(s.Label, "Alt"); got != second {
			t.Errorf("%q is %q off macOS: Alt is %v, want %v", s.Group, s.Label, got, second)
		}
	}
}

func TestShortcutBinding_FollowsTheRunningOS(t *testing.T) {
	t.Cleanup(tui.SetGOOS("darwin"))
	b, ok := tui.ShortcutBindingOf("palette")
	if !ok || b.Mods != ui.Super || b.Key != ui.KeyP {
		t.Fatalf("darwin palette = %+v %v", b, ok)
	}
	b, ok = tui.ShortcutBindingOf("sync-all")
	if !ok || b.Mods != ui.Super|ui.Alt|ui.Shift || b.Key != ui.KeyS {
		t.Fatalf("darwin sync-all = %+v", b)
	}
	b, ok = tui.ShortcutBindingOf("close-block")
	if !ok || b.Mods != ui.Super|ui.Shift || b.Key != ui.KeyW {
		t.Fatalf("darwin close-block = %+v", b)
	}
	b, ok = tui.ShortcutBindingOf("close-tab")
	if !ok || b.Mods != ui.Super || b.Key != ui.KeyW {
		t.Fatalf("darwin close-tab = %+v", b)
	}
	b, ok = tui.ShortcutBindingOf("focus-right")
	if !ok || b.Mods != ui.Super|ui.Alt || b.Key != ui.KeyRight {
		t.Fatalf("darwin focus-right = %+v", b)
	}
	tui.SetGOOS("linux")
	b, ok = tui.ShortcutBindingOf("palette")
	if !ok || b.Mods != ui.Ctrl|ui.Shift || b.Key != ui.KeyP {
		t.Fatalf("linux palette = %+v", b)
	}
	b, ok = tui.ShortcutBindingOf("sync-all")
	if !ok || b.Mods != ui.Ctrl|ui.Shift|ui.Alt || b.Key != ui.KeyA {
		t.Fatalf("linux sync-all = %+v", b)
	}
	b, ok = tui.ShortcutBindingOf("close-block")
	if !ok || b.Mods != ui.Ctrl|ui.Shift|ui.Alt || b.Key != ui.KeyW {
		t.Fatalf("linux close-block = %+v", b)
	}
	b, ok = tui.ShortcutBindingOf("zoom")
	if !ok || b.Mods != ui.Ctrl|ui.Shift || b.Key != ui.KeyEnter {
		t.Fatalf("linux zoom = %+v", b)
	}
	if _, ok := tui.ShortcutBindingOf("nope"); ok {
		t.Fatal("an unknown action has a binding")
	}
}

func TestShortcuts_DarwinBindingOpensThePalette(t *testing.T) {
	t.Cleanup(tui.SetGOOS("darwin"))
	h := newHarness(t, harnessOpts{})
	h.press("palette")
	h.waitText("Commands")
	h.tt.Key(0, ui.KeyEscape)
	h.waitGone("Commands")
	h.press("add-host")
	h.waitText("Test connection")
}

// Two shortcuts must not share keys, on either convention.
func TestShortcuts_NoTwoShareKeys(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		restore := tui.SetGOOS(goos)
		seen := map[string]string{}
		for _, s := range tui.Shortcuts() {
			if prev, dup := seen[s.Label]; dup {
				t.Errorf("%s: %q and %q are both %q", goos, prev, s.Group, s.Label)
			}
			seen[s.Label] = s.Group
		}
		restore()
	}
}
