package theme_test

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/theme"
)

// TestTokensMatchDesignDoc keeps the code and docs/DESIGN.md in step: every
// solid color in the §2.1 table must be what the code uses.
func TestTokensMatchDesignDoc(t *testing.T) {
	t.Parallel()
	doc, err := os.ReadFile("../../docs/DESIGN.md")
	if err != nil {
		t.Fatal(err)
	}
	light, dark := theme.Light(), theme.Dark()
	want := map[string][2]ui.Color{
		"--bg":      {light.Bg, dark.Bg},
		"--sidebar": {light.Sidebar, dark.Sidebar},
		"--surface": {light.Surface, dark.Surface}, "--surface-2": {light.Surface2, dark.Surface2},
		"--surface-3": {light.Surface3, dark.Surface3}, "--surface-4": {light.Surface4, dark.Surface4},
		"--text": {light.Text, dark.Text}, "--text-2": {light.Text2, dark.Text2}, "--text-3": {light.Text3, dark.Text3},
		"--accent": {light.Accent, dark.Accent},
		"--green":  {light.Green, dark.Green}, "--amber": {light.Amber, dark.Amber}, "--red": {light.Red, dark.Red},
		"--blue": {light.Blue, dark.Blue}, "--purple": {light.Purple, dark.Purple}, "--teal": {light.Teal, dark.Teal},
	}
	row := regexp.MustCompile("(?m)^\\| `(--[a-z0-9-]+)` \\| `(#[0-9A-Fa-f]{6})` \\| `(#[0-9A-Fa-f]{6})` \\|")
	seen := 0
	for _, m := range row.FindAllStringSubmatch(string(doc), -1) {
		pair, ok := want[m[1]]
		if !ok {
			continue
		}
		seen++
		if got := pair[0]; got != theme.Hex(strings.ToUpper(m[2])) {
			t.Errorf("%s light = %v, doc says %s", m[1], got, m[2])
		}
		if got := pair[1]; got != theme.Hex(strings.ToUpper(m[3])) {
			t.Errorf("%s dark = %v, doc says %s", m[1], got, m[3])
		}
	}
	if seen != len(want) {
		t.Errorf("matched %d token rows in DESIGN.md, want %d", seen, len(want))
	}
}

func TestUIThemeFollowsTokens(t *testing.T) {
	t.Parallel()
	for _, tk := range []theme.Tokens{theme.Light(), theme.Dark()} {
		th := tk.UI()
		if th.Dark != tk.Dark || th.Accent != tk.Accent || th.Background != tk.Bg {
			t.Errorf("dark=%v: ui theme does not follow tokens", tk.Dark)
		}
		if got := tk.Terminal(); got.Background != tk.TermBg {
			t.Errorf("dark=%v: terminal background %v", tk.Dark, got.Background)
		}
	}
}

func TestHexPanicsOnGarbage(t *testing.T) {
	t.Parallel()
	defer func() {
		if recover() == nil {
			t.Error("Hex did not panic on a malformed color")
		}
	}()
	theme.Hex("red")
}
