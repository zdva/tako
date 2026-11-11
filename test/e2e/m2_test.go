//go:build e2e && linux

package e2e

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/wei-dong/tako/internal/workspace"
)

// Real-window checks of the M2 features, under Xvfb with the packaged build.

// The window is placed at 0,0 and sized 1440x900 by layoutWindow; these are
// the places of things in a window with six blocks (three columns, two rows)
// and the minimap in its default corner.
const (
	minimapX, minimapY = 1340, 790 // inside the minimap panel, on a square
	colDividerX        = 647       // between the first and second column
	colDividerY        = 253
	block1X, block1Y   = 400, 300 // inside the first block
)

var sixHosts = []string{"web-01", "web-02", "db-01", "gpu-01", "win-build", "mac-mini"}

func (f *fixture) sixSynced(settings map[string]any) {
	f.t.Helper()
	settings["compactBlocks"] = true
	f.writeSettings(settings)
	f.seedDemo(sixHosts, map[string]bool{"web-01": true, "web-02": true, "db-01": true})
}

// dragMouse presses at (x, y), moves to (tx, ty) in steps and, unless hold,
// releases there.
func dragMouse(t *testing.T, x, y, tx, ty int, hold bool) {
	t.Helper()
	xdo(t, "mousemove", strconv.Itoa(x), strconv.Itoa(y))
	time.Sleep(150 * time.Millisecond)
	xdo(t, "mousedown", "1")
	const steps = 12
	for i := 1; i <= steps; i++ {
		xdo(t, "mousemove", strconv.Itoa(x+(tx-x)*i/steps), strconv.Itoa(y+(ty-y)*i/steps))
		time.Sleep(30 * time.Millisecond)
	}
	if !hold {
		xdo(t, "mouseup", "1")
	}
}

func readImage(t *testing.T, path string) image.Image {
	t.Helper()
	fh, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fh.Close() }()
	img, err := png.Decode(fh)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// changedShare is the share of pixels in r that differ between two images by
// more than tol (summed over the channels).
func changedShare(a, b image.Image, r image.Rectangle, tol int) float64 {
	n, changed := 0, 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ar, ag, ab, _ := a.At(x, y).RGBA()
			br, bg, bb, _ := b.At(x, y).RGBA()
			d := absInt(int(ar>>8)-int(br>>8)) + absInt(int(ag>>8)-int(bg>>8)) + absInt(int(ab>>8)-int(bb>>8))
			n++
			if d > tol {
				changed++
			}
		}
	}
	return float64(changed) / float64(n)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (f *fixture) session() workspace.State {
	f.t.Helper()
	data, err := os.ReadFile(f.dirs.Session())
	if err != nil {
		f.t.Fatal(err)
	}
	var st workspace.State
	if err := json.Unmarshal(data, &st); err != nil {
		f.t.Fatal(err)
	}
	return st
}

// TestM2_NineBlockFloodFrameRate is TECH-PLAN M2 acceptance 8: nine synced
// blocks all printing as fast as they can still draw at 18 frames a second or
// better on the CPU renderer (§12.2).
func TestM2_NineBlockFloodFrameRate(t *testing.T) {
	f := newFixture(t)
	f.warmFontCache()
	all := make([]string, 0, len(demoHosts))
	synced := map[string]bool{}
	for _, d := range demoHosts {
		all = append(all, d.name)
		synced[d.name] = true
	}
	f.writeSettings(map[string]any{"compactBlocks": true})
	f.seedDemo(all, synced)
	r := f.startEnv("LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8", "MYGO_FRAME_STATS=all")
	r.windowAppears()
	layoutWindow(t, r)
	r.connected(9)
	time.Sleep(1500 * time.Millisecond)

	// One command, typed once, runs in all nine blocks.
	typeInto(t, block1X, 150, "while :; do seq 1 400; done")
	time.Sleep(3 * time.Second) // let the flood build up
	fps, mean := f.measureFrames(10 * time.Second)
	t.Logf("nine synced blocks flooding: %.1f frames/s, mean frame %v", fps, mean.Round(100*time.Microsecond))
	if fps < 18 {
		t.Errorf("%.1f frames/s < 18 (TECH-PLAN §12.2)", fps)
	}
	if dir := os.Getenv("TAKO_SHOTS"); dir != "" {
		shot(t, dir, "flood-9-synced")
	}
	xdo(t, "key", "ctrl+c")
}

// TestM2_ManualLayoutSurvivesRestart is acceptance 6 in the real window: drag
// a divider, kill the app without any chance to clean up, start it again, and
// the layout and the sync state come back.
func TestM2_ManualLayoutSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	f.sixSynced(map[string]any{})
	r := startShots(t, f, "en_US.UTF-8")
	r.connected(6)
	time.Sleep(1200 * time.Millisecond)
	if f.session().Layout.Manual {
		t.Fatal("the layout should be automatic before any divider is dragged")
	}
	dragMouse(t, colDividerX, colDividerY, colDividerX+120, colDividerY, false)
	time.Sleep(2 * time.Second) // the debounced save
	before := f.session()
	if !before.Layout.Manual {
		t.Fatal("dragging a divider did not make the layout manual")
	}
	synced := 0
	for _, b := range before.Blocks {
		if b.Synced {
			synced++
		}
	}
	if synced != 3 {
		t.Fatalf("%d synced blocks saved, want 3", synced)
	}
	if err := r.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = r.cmd.Wait()
	for f.established() > 0 {
		time.Sleep(20 * time.Millisecond)
	}

	r2 := startShots(t, f, "en_US.UTF-8")
	r2.connected(6)
	time.Sleep(2500 * time.Millisecond)
	after := f.session()
	if !after.Layout.Manual || len(after.Layout.Tree.IDs()) != 6 {
		t.Fatalf("after the restart the layout is %+v", after.Layout)
	}
	if got, want := after.Layout.Tree.Root.Ratio, before.Layout.Tree.Root.Ratio; got != want {
		t.Errorf("the root divider moved from %v to %v across the restart", want, got)
	}
}

func TestScreenshots_M2(t *testing.T) {
	dir := shotsDir(t)
	f := newFixture(t)
	f.sixSynced(map[string]any{"appearance": "dark"})
	r := startShots(t, f, "en_US.UTF-8")
	r.connected(6)
	time.Sleep(1200 * time.Millisecond)
	typeInto(t, block1X, block1Y, "df -h / | head -n 2", "uptime")
	xdo(t, "mousemove", "800", "880")
	shot(t, dir, "minimap-rest")
	rest := readImage(t, filepath.Join(dir, "minimap-rest.png"))

	xdo(t, "mousemove", strconv.Itoa(minimapX), strconv.Itoa(minimapY))
	time.Sleep(400 * time.Millisecond)
	shot(t, dir, "minimap-hover")
	hover := readImage(t, filepath.Join(dir, "minimap-hover.png"))
	panel := image.Rect(1262, 712, 1425, 760)
	share := changedShare(rest, hover, panel, 6)
	t.Logf("%.0f%% of the minimap header pixels change between rest and hover", share*100)
	if share < 0.2 {
		t.Errorf("the minimap looks the same at rest and under the pointer (%.0f%% changed): it should be translucent at rest, opaque on hover", share*100)
	}

	// Out to the right, then back near where it was: it snaps to a third.
	dragMouse(t, colDividerX, colDividerY, 740, colDividerY, true)
	for _, x := range []int{700, 670, 652} {
		xdo(t, "mousemove", strconv.Itoa(x), strconv.Itoa(colDividerY))
		time.Sleep(250 * time.Millisecond) // the CPU renderer needs a moment per frame
	}
	time.Sleep(400 * time.Millisecond)
	shot(t, dir, "divider-snap")
	xdo(t, "mouseup", "1")

	xdo(t, "mousemove", "500", "62")
	xdo(t, "click", "3")
	shot(t, dir, "menu-block")
	xdo(t, "key", "Escape")

	xdo(t, "mousemove", strconv.Itoa(minimapX), strconv.Itoa(minimapY), "click", "3")
	shot(t, dir, "menu-minimap")
	xdo(t, "key", "Escape")

	xdo(t, "key", "ctrl+shift+p")
	time.Sleep(300 * time.Millisecond)
	xdo(t, "type", "--delay", "60", "sync")
	shot(t, dir, "palette-sync")
	xdo(t, "key", "Escape")
}

func TestScreenshots_M2Light(t *testing.T) {
	dir := shotsDir(t)
	f := newFixture(t)
	f.sixSynced(map[string]any{})
	r := startShots(t, f, "zh_CN.UTF-8")
	r.connected(6)
	time.Sleep(1200 * time.Millisecond)
	typeInto(t, block1X, block1Y-150, "df -h / | head -n 2", "uptime")
	xdo(t, "mousemove", "800", "880")
	shot(t, dir, "main-light")
}
