package ui

import (
	"embed"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/theme"
)

//go:embed assets/brand.png assets/brand-light.png
var assetFS embed.FS

var (
	brandOnce       sync.Once
	brandDarkImage  ui.ImageSource
	brandLightImage ui.ImageSource
)

// brand returns the octopus logo for the current appearance, decoded once.
func brand(dark bool) ui.ImageSource {
	brandOnce.Do(func() {
		for path, target := range map[string]*ui.ImageSource{
			"assets/brand.png":       &brandDarkImage,
			"assets/brand-light.png": &brandLightImage,
		} {
			data, err := assetFS.ReadFile(path)
			if err != nil {
				continue
			}
			*target, _ = ui.DecodeImage(data)
		}
	})
	if dark {
		return brandDarkImage
	}
	return brandLightImage
}

// Metrics from DESIGN.md §2.3.
const (
	sidebarW  = 248
	toolbarH  = 44
	blockHead = 40
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

// osTile draws a host's system mark in the shared 26-tile (radius 8, glyph
// 18) scaled to size. Linux, Windows, macOS and the local prompt share one
// weight; anything else is the plain server.
func (a *App) osTile(c *ui.Context, os model.OSKind, local bool, size float32) {
	bg, fg, ic := a.osMark(os, local)
	ui.Box(c).Size(size, size).Radius(size * 8 / 26).Background(bg).Center().Children(func() {
		ui.Icon(c, ic).FontSize(size * 18 / 26).TextColor(fg)
	})
}

// osMark is the tile fill, the glyph color and the drawing for one system.
func (a *App) osMark(os model.OSKind, local bool) (bg, fg ui.Color, ic *ui.SVG) {
	dark := a.tk.Dark
	switch {
	case local:
		return markColor(dark, "#2A2A30", "#D0D0D6", "#E8E8EC", "#3A3A40", iconTerminal)
	case os == model.OSWindows:
		return markColor(dark, "#1C2740", "#8BB4FF", "#E8EEF8", "#2C5BB0", iconWindows)
	case os == model.OSMacOS:
		return markColor(dark, "#2C2C33", "#E6E6EA", "#E8E8EC", "#3A3A40", iconApple)
	case os == model.OSLinux:
		return markColor(dark, "#2A2D34", "#D0D4DC", "#E8EAEE", "#3A3E46", iconLinux)
	default:
		return markColor(dark, "#2A2A30", "#C8C8D0", "#E8E8EC", "#3A3A40", iconServer)
	}
}

func markColor(dark bool, dbg, dfg, lbg, lfg string, ic *ui.SVG) (ui.Color, ui.Color, *ui.SVG) {
	if dark {
		return theme.Hex(dbg), theme.Hex(dfg), ic
	}
	return theme.Hex(lbg), theme.Hex(lfg), ic
}

// iconButton is a small borderless icon button; label names it for assistive
// technology and tests.
func (a *App) iconButton(c *ui.Context, ic *ui.SVG, label string, size float32) ui.Element {
	b := ui.Button(c, "").Label(label).Tooltip(label)
	b.Size(size, size).Padding(0).Radius(8).Background(ui.Transparent).Border(0, ui.Transparent)
	glyph := float32(16)
	if size < 28 {
		glyph = size * 0.55
	}
	b.Children(func() { ui.Icon(c, ic).FontSize(glyph).TextColor(a.tk.Text2) })
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
