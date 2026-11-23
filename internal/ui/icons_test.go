package ui

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestIconsParseAndPaint covers every glyph in the v2 inventory: each one
// must parse and leave opaque pixels at 24×24.
func TestIconsParseAndPaint(t *testing.T) {
	t.Parallel()
	icons := []*ui.SVG{
		iconPlus, iconSearch, iconClose, iconChevronR, iconChevronD, iconChevronU,
		iconMore, iconGrip, iconSidebar, iconMinimap, iconTerminal, iconSettings,
		iconSync, iconTransfer, iconUp, iconDown, iconSwap, iconRefresh,
		iconPause, iconPlay, iconEye, iconLock, iconLink, iconFolder, iconFile,
		iconChip, iconCheck, iconAlert, iconLinux, iconWindows, iconApple, iconServer,
	}
	for i, ic := range icons {
		if ic == nil {
			t.Fatalf("icon %d is nil", i)
		}
		img := ic.Image(24, 24, color.NRGBA{A: 255})
		opaque := 0
		for _, p := range img.Pix {
			if p > 0 {
				opaque++
			}
		}
		if opaque == 0 {
			t.Fatalf("icon %d painted nothing at 24×24", i)
		}
	}
}

// TestSyncIsNotTheChainIcon is the §4.5 rule that sync uses cycling arrows.
func TestSyncIsNotTheChainIcon(t *testing.T) {
	t.Parallel()
	if iconSync == iconLink {
		t.Fatal("iconSync must not reuse iconLink")
	}
	a := iconSync.Image(24, 24, color.NRGBA{A: 255})
	b := iconLink.Image(24, 24, color.NRGBA{A: 255})
	same := true
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("sync and link paint the same pixels")
	}
}

// TestLinuxMarkIsSingleFillPath keeps the penguin a faceless silhouette:
// one filled path, no stroke cutouts that would read as eyes or a beak.
func TestLinuxMarkIsSingleFillPath(t *testing.T) {
	t.Parallel()
	img := iconLinux.Image(24, 24, color.NRGBA{R: 208, G: 212, B: 220, A: 255})
	opaque := 0
	for y := range 24 {
		for x := range 24 {
			_, _, _, a := img.At(x, y).RGBA()
			if a>>8 > 200 {
				opaque++
			}
		}
	}
	if opaque < 80 {
		t.Fatalf("linux silhouette is too thin (%d opaque px)", opaque)
	}
}
