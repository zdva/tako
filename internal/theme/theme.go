// Package theme holds Tako's design tokens (docs/DESIGN.md §2) and turns them
// into MyGo themes. Every color in the UI comes from here.
package theme

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/scheme"
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
	Blue, Purple, Teal, Disk       ui.Color
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

	// Scheme is the terminal color scheme in use; nil means the colors above.
	Scheme *scheme.Scheme
}

// color converts a validated "#RRGGBB" of a scheme; a bad one (settings.json
// is hand-editable) falls back to fallback instead of panicking.
func color(hex string, fallback ui.Color) ui.Color {
	if !scheme.Valid(hex) {
		return fallback
	}
	return Hex(hex)
}

// WithScheme returns the tokens with the terminal colors of s. The window's
// own colors do not change: a dark terminal scheme in a light window is fine.
func (t Tokens) WithScheme(s scheme.Scheme) Tokens {
	t.Scheme = &s
	t.TermBg = color(s.Background, t.TermBg)
	t.TermFg = color(s.Foreground, t.TermFg)
	t.TermDim = color(s.Palette[8], t.TermDim)
	t.TermRed, t.TermGreen = color(s.Palette[1], t.TermRed), color(s.Palette[2], t.TermGreen)
	t.TermYellow, t.TermBlue = color(s.Palette[3], t.TermYellow), color(s.Palette[4], t.TermBlue)
	t.TermMagenta, t.TermCyan = color(s.Palette[5], t.TermMagenta), color(s.Palette[6], t.TermCyan)
	return t
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

// Light is the light appearance (DESIGN.md §2.1, indigo visual system).
func Light() Tokens {
	return Tokens{
		Bg: Hex("#F4F4F6"), Sidebar: Hex("#ECECEF"),
		Surface: Hex("#FFFFFF"), Surface2: Hex("#F6F6F8"), Surface3: Hex("#ECECF0"), Surface4: Hex("#E2E2E8"),
		Border: ui.RGBA(24, 24, 32, 0.08), BorderStrong: ui.RGBA(24, 24, 32, 0.16),
		Text: Hex("#1A1A1E"), Text2: Hex("#6A6A74"), Text3: Hex("#9797A1"),
		Accent: Hex("#4A5FC7"), AccentSoft: ui.RGBA(74, 95, 199, 0.11), AccentRing: ui.RGBA(74, 95, 199, 0.30),
		Green: Hex("#198A61"), Amber: Hex("#A36E16"), Red: Hex("#D4474D"),
		Blue: Hex("#2776C5"), Purple: Hex("#8056C7"), Teal: Hex("#0C8F99"), Disk: Hex("#66778D"),
		Scrim:      ui.RGBA(20, 20, 28, 0.28),
		SyncBorder: ui.RGBA(74, 95, 199, 0.55),
		Glass3:     ui.RGBA(255, 255, 255, 0.97),
		TermBg:     Hex("#FFFFFF"), TermFg: Hex("#2A2A30"), TermDim: Hex("#9A9AA6"),
		TermRed: Hex("#D3403F"), TermGreen: Hex("#1F8F50"), TermYellow: Hex("#A56A00"),
		TermBlue: Hex("#2F6BE0"), TermMagenta: Hex("#A33FB5"), TermCyan: Hex("#0C8C99"),
	}
}

// Dark is the dark appearance (DESIGN.md §2.1, indigo visual system).
func Dark() Tokens {
	return Tokens{
		Dark: true,
		Bg:   Hex("#0B0B0D"), Sidebar: Hex("#101014"),
		Surface: Hex("#17171C"), Surface2: Hex("#1E1E24"), Surface3: Hex("#26262D"), Surface4: Hex("#32323A"),
		Border: ui.RGBA(255, 255, 255, 0.08), BorderStrong: ui.RGBA(255, 255, 255, 0.16),
		Text: Hex("#F2F2F5"), Text2: Hex("#A1A1AB"), Text3: Hex("#6E6E78"),
		Accent: Hex("#566FCB"), AccentSoft: ui.RGBA(86, 111, 203, 0.16), AccentRing: ui.RGBA(86, 111, 203, 0.40),
		Green: Hex("#48C98C"), Amber: Hex("#D4A846"), Red: Hex("#F16C74"),
		Blue: Hex("#6F9CEB"), Purple: Hex("#AA8BE8"), Teal: Hex("#42C7C6"), Disk: Hex("#95A9C7"),
		Scrim:      ui.RGBA(0, 0, 0, 0.45),
		SyncBorder: ui.RGBA(86, 111, 203, 0.62),
		Glass3:     ui.RGBA(23, 23, 28, 0.97),
		TermBg:     Hex("#0C0C10"), TermFg: Hex("#D8D8DE"), TermDim: Hex("#6E6E78"),
		TermRed: Hex("#FF6B6B"), TermGreen: Hex("#5FD38D"), TermYellow: Hex("#F0C45A"),
		TermBlue: Hex("#6AA6FF"), TermMagenta: Hex("#C792EA"), TermCyan: Hex("#56D4DD"),
	}
}

// ShadowSm paints the small elevation (DESIGN.md §2.4).
func (t Tokens) ShadowSm(el ui.Element) {
	if t.Dark {
		el.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.35))
		return
	}
	el.Shadow(0, 1, 2, 0, ui.RGBA(24, 24, 32, 0.06))
}

// ShadowMd paints the card elevation.
func (t Tokens) ShadowMd(el ui.Element) {
	if t.Dark {
		el.Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.35))
		el.Shadow(0, 8, 20, 0, ui.RGBA(0, 0, 0, 0.32))
		return
	}
	el.Shadow(0, 1, 2, 0, ui.RGBA(24, 24, 32, 0.05))
	el.Shadow(0, 6, 16, 0, ui.RGBA(24, 24, 32, 0.08))
}

// ShadowLg paints the overlay elevation.
func (t Tokens) ShadowLg(el ui.Element) {
	if t.Dark {
		el.Shadow(0, 2, 4, 0, ui.RGBA(0, 0, 0, 0.40))
		el.Shadow(0, 24, 60, 0, ui.RGBA(0, 0, 0, 0.55))
		return
	}
	el.Shadow(0, 2, 4, 0, ui.RGBA(24, 24, 32, 0.06))
	el.Shadow(0, 20, 48, 0, ui.RGBA(24, 24, 32, 0.16))
}

// TagPalette is the colors a tag can have, in the order DESIGN.md lists them.
var TagPalette = []string{"#A36E16", "#0C8F99", "#2776C5", "#9C9CA6", "#8056C7", "#D4474D"}

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
	// The first family the machine has wins; system-ui covers the rest.
	th.Font = "Inter, PingFang SC, Microsoft YaHei UI, Noto Sans CJK SC, system-ui"
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
	if sc := t.Scheme; sc != nil {
		th.Cursor = color(sc.Cursor, t.TermFg)
		th.Selection = color(sc.Selection, t.TermFg.Alpha(0.3))
		for i, c := range sc.Palette {
			th.Palette[i] = color(c, th.Palette[i])
		}
		return th
	}
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
