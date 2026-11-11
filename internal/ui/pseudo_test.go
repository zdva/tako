package ui_test

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/i18n"
	"github.com/wei-dong/tako/internal/settings"
)

// userData matches text that is data rather than interface: shortcut hints,
// host addresses and user input.
var userData = regexp.MustCompile(`^(Ctrl|Cmd|⌘|⌃|Shift|Alt|\W)[+\w⇧⌥⌃⌘,.]*$|^(Tako|web-01|db-01|prod|Linux|macOS|Windows|SOCKS5|HTTP|English|简体中文|Solarized (Dark|Light)|Dracula|Nord|One Dark|Gruvbox Dark|Tokyo Night)$|@\d+\.\d+\.\d+\.\d+$|^~|^[\w.-]+@`)

// untranslated lists the visible texts that did not come from a table.
func untranslated(texts []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range texts {
		if seen[s] || strings.Contains(s, "⟦") || userData.MatchString(strings.TrimSpace(s)) {
			continue
		}
		seen[s] = true
		hasLetter := false
		for _, r := range s {
			if unicode.IsLetter(r) {
				hasLetter = true
				break
			}
		}
		if hasLetter {
			out = append(out, s)
		}
	}
	return out
}

func (h *harness) pseudoSurface(name string) {
	h.t.Helper()
	h.tt.Frame()
	if bad := untranslated(h.tt.Texts()); len(bad) > 0 {
		h.shot("pseudo-" + name)
		h.t.Errorf("%s: texts that bypass the language tables: %q", name, bad)
	}
	for _, s := range h.tt.Texts() {
		if r, ok := h.tt.Find(s); ok && r.W > 0 && r.H > 0 && (r.X < -1 || r.Y < -1 || r.X+r.W > winW+1 || r.Y+r.H > winH+1) {
			h.t.Errorf("%s: %q sits outside the window at %v", name, s, r)
		}
	}
}

func TestPseudoLocale_EverySurfaceComesFromTheTables(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01", "prod")
	if err := h.app.Settings.Update(func(v *settings.Values) { v.Language = i18n.Pseudo }); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	h.waitText("⟦")
	h.pseudoSurface("main")
	h.shot("pseudo-main")

	h.tt.Key(ui.Ctrl|ui.Shift, ui.KeyN)
	h.tt.Frame()
	h.pseudoSurface("host-sheet")
	h.shot("pseudo-host-sheet")
	h.tt.Key(0, ui.KeyEscape)

	h.tt.Key(ui.Ctrl|ui.Shift, ui.KeyP)
	h.tt.Frame()
	h.pseudoSurface("palette")
	h.tt.Key(0, ui.KeyEscape)

	h.openSettings("")
	for _, p := range []string{"general", "terminal", "connection", "monitor", "transfer", "data", "about"} {
		h.openSettings(p)
		h.pseudoSurface("settings-" + p)
		h.shot("pseudo-settings-" + p)
	}
	h.tt.Key(0, ui.KeyEscape)

	h.app.UI.OpenTransfer("")
	h.tt.Frame()
	// The file rows are this computer's own files; check the sheet's text.
	for _, english := range []string{"Source", "Target", "Transfer queue", "Via this computer", "Name", "Modified"} {
		if h.tt.HasText(english) {
			t.Errorf("transfer sheet: %q is not from the tables", english)
		}
	}
	if !h.tt.HasText("⟦") {
		t.Error("the transfer sheet shows no translated text")
	}
}
