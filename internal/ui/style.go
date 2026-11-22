package ui

import (
	"embed"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/theme"
)

//go:embed assets/brand.png
var assetFS embed.FS

var (
	brandOnce sync.Once
	brandImg  ui.ImageSource
)

// brand returns the octopus logo, decoded once.
func brand() ui.ImageSource {
	brandOnce.Do(func() {
		data, err := assetFS.ReadFile("assets/brand.png")
		if err != nil {
			return
		}
		brandImg, _ = ui.DecodeImage(data)
	})
	return brandImg
}

// Metrics from DESIGN.md §2.3.
const (
	sidebarW  = 248
	toolbarH  = 44
	blockHead = 38
	blockFoot = 32
	statusH   = 28
	hostRowH  = 44
	gridGap   = 12
)

// dot draws a status dot.
func dot(c *ui.Context, col ui.Color, size float32) {
	ui.Box(c).Size(size, size).Radius(size / 2).Background(col)
}

// caption is 11px muted text.
func (a *App) caption(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(11).TextColor(a.tk.Text3)
}

// osTile draws a host's system icon on its gradient-ish colored tile.
func (a *App) osTile(c *ui.Context, os model.OSKind, local bool, size float32) {
	col, ic := theme.Hex("#4B5565"), iconServer
	switch {
	case local:
		col, ic = theme.Hex("#EF6F61"), iconTerminal
	case os == model.OSWindows:
		col, ic = theme.Hex("#3F7BEA"), iconWindows
	case os == model.OSMacOS:
		col, ic = theme.Hex("#7E7E88"), iconApple
	}
	ui.Box(c).Size(size, size).Radius(size * 0.28).Background(col).Center().Children(func() {
		ui.Icon(c, ic).FontSize(size * 0.58).TextColor(ui.RGB(255, 255, 255))
	})
}

// iconButton is a small borderless icon button; label names it for assistive
// technology and tests.
func (a *App) iconButton(c *ui.Context, ic *ui.SVG, label string, size float32) ui.Element {
	b := ui.Button(c, "").Label(label).Tooltip(label)
	b.Size(size, size).Padding(0).Radius(7).Background(ui.Transparent).Border(0, ui.Transparent)
	b.Children(func() { ui.Icon(c, ic).FontSize(size * 0.55).TextColor(a.tk.Text2) })
	return b
}

// pill draws a small rounded chip with text.
func (a *App) pill(c *ui.Context, text string, fg, bg ui.Color) {
	ui.Box(c).Padding(1, 7).Radius(8).Background(bg).Children(func() {
		ui.Text(c, text).FontSize(10.5).Bold().TextColor(fg)
	})
}

func hostMatchesOS(os model.OSKind) int {
	switch os {
	case model.OSMacOS:
		return 1
	case model.OSWindows:
		return 2
	default:
		return 0
	}
}

func osFromIndex(i int) model.OSKind {
	switch i {
	case 1:
		return model.OSMacOS
	case 2:
		return model.OSWindows
	default:
		return model.OSLinux
	}
}

// tagColor resolves a tag's palette color.
func (a *App) tagColor(name string) ui.Color {
	if hex := a.d.Library.TagColor(name); strings.HasPrefix(hex, "#") && len(hex) == 7 {
		return theme.Hex(hex)
	}
	return a.tk.Text3
}

// dialog shows body in a dialog without the toolkit's own panel look, over a
// dimmed window, while *open is true. Escape or a click outside sets *open to
// false.
func (a *App) dialog(c *ui.Context, open *bool, body func()) {
	ui.DialogBase(c, open, func(back, _ ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.32))
		body()
	})
}
