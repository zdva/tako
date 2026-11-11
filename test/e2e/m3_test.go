//go:build e2e && linux

package e2e

import (
	"image"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// Real-window checks of the M3 features (monitoring and transfers), under
// Xvfb with the packaged build and a real OpenSSH server.

// meterPixels counts pixels in the blocks' area (right of the sidebar) that
// have the dark theme's green, amber or red: the footer bars. Text and borders
// use other colors.
func meterPixels(img image.Image) int {
	near := func(r, g, b uint32, want [3]int) bool {
		return absInt(int(r>>8)-want[0]) < 8 && absInt(int(g>>8)-want[1]) < 8 && absInt(int(b>>8)-want[2]) < 8
	}
	palette := [][3]int{{0x3D, 0xD6, 0x8C}, {0xF5, 0xB8, 0x3D}, {0xFF, 0x63, 0x69}}
	n := 0
	b := img.Bounds()
	for y := range min(b.Max.Y, windowH) {
		for x := 280; x < min(b.Max.X, windowW); x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			for _, want := range palette {
				if near(r, g, bl, want) {
					n++
					break
				}
			}
		}
	}
	return n
}

// nineHosts starts the app with nine connected blocks and the monitor set as
// asked, and returns the run.
func nineHosts(t *testing.T, monitor map[string]any) (*fixture, *run) {
	t.Helper()
	f := newFixture(t)
	f.writeSettings(map[string]any{"appearance": "dark", "monitor": monitor})
	f.warmFontCache()
	r := f.startEnv("LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8", "MYGO_FRAME_STATS=all")
	r.windowAppears()
	layoutWindow(t, r)
	r.allConnected()
	return f, r
}

// TestM3_NineMonitoredHostsIdle is M3 acceptance 2 in the real window: nine
// hosts sampled at the default interval keep the client's idle CPU under 1% of a core,
// and the footers really show readings (their bars appear only when the
// monitor is on).
func TestM3_NineMonitoredHostsIdle(t *testing.T) {
	// Without monitoring: the baseline of colored pixels.
	fOff, off := nineHosts(t, map[string]any{"off": true})
	time.Sleep(4 * time.Second)
	dir := t.TempDir()
	shot(t, dir, "off")
	base := meterPixels(readImage(t, filepath.Join(dir, "off.png")))
	offBefore := off.cpuTicks()
	offFPS, _ := fOff.measureFrames(10 * time.Second)
	offCPU := float64(off.cpuTicks()-offBefore) / clockTicksPerS / 10 * 100
	t.Logf("monitoring off: CPU %.2f%%, %.2f frames/s", offCPU, offFPS)
	_ = off.cmd.Process.Kill()
	_ = off.cmd.Wait()

	f, r := nineHosts(t, map[string]any{})
	time.Sleep(8 * time.Second) // samples arrive, footers fill
	shot(t, dir, "on")
	with := meterPixels(readImage(t, filepath.Join(dir, "on.png")))
	t.Logf("colored footer pixels: %d without monitoring, %d with it", base, with)
	if with < base+200 {
		t.Errorf("the footers show no bars: %d colored pixels with monitoring vs %d without", with, base)
	}

	const window = 15 * time.Second
	before := r.cpuTicks()
	fps, mean := f.measureFrames(window)
	cpu := float64(r.cpuTicks()-before) / clockTicksPerS / window.Seconds() * 100
	rss, private := r.memory()
	t.Logf("nine hosts at the default 2 s interval: %.2f frames/s (mean %v); CPU %.2f%% of one core over %v, RSS %.1f MB (private %.1f MB)", fps, mean, cpu, window, rss, private)
	if cpu > maxIdleCPU {
		t.Errorf("idle CPU %.2f%% > %.1f%% with monitoring on", cpu, maxIdleCPU)
	}
	if private > maxRSSMB {
		t.Errorf("private memory %.1f MB > %d MB", private, maxRSSMB)
	}
}

func TestScreenshots_M3(t *testing.T) {
	dir := shotsDir(t)
	f := newFixture(t)
	f.writeSettings(map[string]any{"appearance": "dark", "compactBlocks": true, "monitor": map[string]any{"intervalSec": 1}})
	f.seedDemo(sixHosts, nil)
	r := startShots(t, f, "en_US.UTF-8")
	r.connected(6)
	time.Sleep(1200 * time.Millisecond)
	typeInto(t, block1X, block1Y, "uptime")
	time.Sleep(20 * time.Second) // history for the charts
	xdo(t, "mousemove", "800", "880")
	shot(t, dir, "footers")

	// The monitor page of the focused block, then back.
	xdo(t, "mousemove", strconv.Itoa(block1X), strconv.Itoa(block1Y), "click", "1")
	xdo(t, "key", "ctrl+shift+alt+m")
	time.Sleep(1500 * time.Millisecond)
	shot(t, dir, "monitor-page")
	xdo(t, "key", "Escape")
	time.Sleep(300 * time.Millisecond)

	// The transfer sheet over two real folders: the shells report them with OSC 7.
	src, dst := filepath.Join(t.TempDir(), "backups"), filepath.Join(t.TempDir(), "restore")
	for name, size := range map[string]int{"db-2026-11-10.sql.gz": 2 << 20, "db-2026-11-09.sql.gz": 3 << 20, "wal-archive.tar": 1 << 20} {
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	osc7 := func(x int, dir string) {
		typeInto(t, x, block1Y, `printf '\033]7;file://localhost`+dir+`\007'`)
	}
	osc7(800, dst)
	osc7(block1X, src)
	xdo(t, "key", "ctrl+shift+alt+t")
	time.Sleep(2500 * time.Millisecond)
	shot(t, dir, "transfer-sheet")
	xdo(t, "key", "Escape")
}
