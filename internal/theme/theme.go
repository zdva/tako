// Package theme holds Tako's design tokens (docs/DESIGN.md §2) and turns them
// into MyGo themes. Every color in the UI comes from here.
package theme

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// Tokens is one appearance's palette.
type Tokens struct {
	Dark bool

	Bg, Sidebar                    ui.Color
	Surface, Surface2              ui.Color
	Surface3, Surface4             ui.Color
	Border, BorderStrong           ui.Color
	Text, Text2, Text3             ui.Color
	Accent, AccentSoft, AccentRing ui.Color
	Green, Amber, Red              ui.Color
	Blue, Purple, Teal             ui.Color
	Scrim                          ui.Color
	// SyncBorder is the 1px border of a block that takes part in input sync.
	SyncBorder ui.Color
	// Glass3 is the minimap panel's surface; its alpha is scaled down while the
	// panel is at rest, so it is translucent then and opaque when hovered or
	// dragged (DESIGN.md, the Final section).
	Glass3 ui.Color

	TermBg, TermFg, TermDim ui.Color
	TermRed, TermGreen      ui.Color
	TermYellow, TermBlue    ui.Color
	TermMagenta, TermCyan   ui.Color
}

// Hex parses "#RRGGBB" and panics on a malformed constant; it is for the
// token tables only.
func Hex(s string) ui.Color {
	if len(s) != 7 || s[0] != '#' {
		panic(fmt.Sprintf("theme: bad color %q", s))
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		panic(fmt.Sprintf("theme: bad color %q: %v", s, err))
	}
	return ui.RGB(uint8(v>>16), uint8(v>>8), uint8(v)) //nolint:gosec // v is parsed with 24 bits, each shift leaves at most 8
}

// Light is the light appearance.
func Light() Tokens {
	return Tokens{
		Bg: Hex("#F4F4F6"), Sidebar: Hex("#EDEDF0"),
		Surface: Hex("#FFFFFF"), Surface2: Hex("#F8F8FA"), Surface3: Hex("#EFEFF2"), Surface4: Hex("#E6E6EA"),
		Border: ui.RGBA(24, 24, 32, 0.09), BorderStrong: ui.RGBA(24, 24, 32, 0.16),
		Text: Hex("#1D1D20"), Text2: Hex("#6A6A74"), Text3: Hex("#9C9CA6"),
		Accent: Hex("#EE6A5B"), AccentSoft: ui.RGBA(238, 106, 91, 0.11), AccentRing: ui.RGBA(238, 106, 91, 0.30),
		Green: Hex("#1EA672"), Amber: Hex("#E39A12"), Red: Hex("#E5484D"),
		Blue: Hex("#3B7BF6"), Purple: Hex("#8A63D2"), Teal: Hex("#0FA3B1"),
		Scrim:      ui.RGBA(20, 20, 28, 0.28),
		SyncBorder: ui.RGBA(238, 106, 91, 0.55),
		Glass3:     ui.RGBA(255, 255, 255, 0.97),
		TermBg:     Hex("#FFFFFF"), TermFg: Hex("#2A2A30"), TermDim: Hex("#9A9AA6"),
		TermRed: Hex("#D3403F"), TermGreen: Hex("#1F8F50"), TermYellow: Hex("#A56A00"),
		TermBlue: Hex("#2F6BE0"), TermMagenta: Hex("#A33FB5"), TermCyan: Hex("#0C8C99"),
	}
}

// Dark is the dark appearance.
func Dark() Tokens {
	return Tokens{
		Dark: true,
		Bg:   Hex("#151517"), Sidebar: Hex("#1B1B1E"),
		Surface: Hex("#212124"), Surface2: Hex("#27272B"), Surface3: Hex("#2E2E33"), Surface4: Hex("#38383E"),
		Border: ui.RGBA(255, 255, 255, 0.085), BorderStrong: ui.RGBA(255, 255, 255, 0.16),
		Text: Hex("#EDEDF0"), Text2: Hex("#A2A2AC"), Text3: Hex("#6E6E78"),
		Accent: Hex("#FF7B6A"), AccentSoft: ui.RGBA(255, 123, 106, 0.15), AccentRing: ui.RGBA(255, 123, 106, 0.38),
		Green: Hex("#3DD68C"), Amber: Hex("#F5B83D"), Red: Hex("#FF6369"),
		Blue: Hex("#5B9BFF"), Purple: Hex("#B18CFF"), Teal: Hex("#3CC9D6"),
		Scrim:      ui.RGBA(0, 0, 0, 0.45),
		SyncBorder: ui.RGBA(255, 123, 106, 0.60),
		Glass3:     ui.RGBA(33, 33, 36, 0.97),
		TermBg:     Hex("#151517"), TermFg: Hex("#D8D8DE"), TermDim: Hex("#6E6E78"),
		TermRed: Hex("#FF6B6B"), TermGreen: Hex("#5FD38D"), TermYellow: Hex("#F0C45A"),
		TermBlue: Hex("#6AA6FF"), TermMagenta: Hex("#C792EA"), TermCyan: Hex("#56D4DD"),
	}
}

// TagPalette is the colors a tag can have, in the order DESIGN.md lists them.
var TagPalette = []string{"#E39A12", "#0FA3B1", "#3B7BF6", "#9C9CA6", "#8A63D2", "#E5484D"}

// UI returns the MyGo theme for the tokens.
func (t Tokens) UI() *ui.Theme {
	th := ui.LightTheme()
	if t.Dark {
		th = ui.DarkTheme()
	}
	th.Background = t.Bg
	th.Surface = t.Surface
	th.SurfaceHover = t.Surface3
	th.SurfacePressed = t.Surface4
	th.Border = t.BorderStrong
	th.Text = t.Text
	th.TextMuted = t.Text2
	th.Accent = t.Accent
	th.AccentHover = t.Accent
	th.AccentPressed = t.Accent
	th.AccentText = ui.RGB(255, 255, 255)
	th.InactiveAccent = t.Accent
	th.Danger = t.Red
	th.Warning = t.Amber
	th.Success = t.Green
	th.Focus = t.AccentRing
	th.Radius = 8
	th.FontSize = 13
	th.Spacing = 4
	return th
}

// Terminal returns the terminal colors for the tokens.
func (t Tokens) Terminal() *terminal.Theme {
	th := terminal.LightTheme()
	if t.Dark {
		th = terminal.DarkTheme()
	}
	th.Foreground, th.Background = t.TermFg, t.TermBg
	th.Cursor = t.TermFg
	th.Selection = t.AccentSoft
	// Palette order: black, red, green, yellow, blue, magenta, cyan, white,
	// then the bright kinds, which reuse the normal ones.
	p := [8]ui.Color{t.TermFg, t.TermRed, t.TermGreen, t.TermYellow, t.TermBlue, t.TermMagenta, t.TermCyan, t.TermDim}
	for i, c := range p {
		th.Palette[i] = c
		th.Palette[i+8] = c
	}
	return th
}
