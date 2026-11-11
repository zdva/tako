package ui_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/monitor"
	"github.com/wei-dong/tako/internal/settings"
	"github.com/wei-dong/tako/internal/transfer"
	"github.com/wei-dong/tako/internal/transport"
)

// monitorFixture answers the collector's commands with recorded Linux output
// and, for the GPU machine, the NVIDIA section.
func monitorFixture(t *testing.T, gpu bool) func(string) (string, int) {
	t.Helper()
	read := func(name string) string {
		data, err := os.ReadFile(filepath.Join("..", "monitor", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	one, two, info, nv := read("linux-1.txt"), read("linux-2.txt"), read("linux-info.txt"), read("nvidia-8.txt")
	var n atomic.Int32
	return func(cmd string) (string, int) {
		switch cmd {
		case monitor.ProbeCommand:
			return "Linux\n", 0
		case monitor.Linux{}.InfoCommand():
			return info, 0
		case monitor.Linux{}.Command():
			out := one
			if n.Add(1)%2 == 0 {
				out = two
			}
			if gpu {
				out += "##NVIDIA\n" + nv
			}
			return out, 0
		}
		return "ran: " + cmd + "\n", 0
	}
}

// fastMonitor samples every second.
func (h *harness) fastMonitor() {
	h.t.Helper()
	err := h.app.Settings.Update(func(v *settings.Values) { v.Monitor.IntervalSec = 1 })
	if err != nil {
		h.t.Fatal(err)
	}
}

// seedHistory fills a host's series with a day-in-the-life of the last
// minutes, so charts have something to show.
func (h *harness) seedHistory(id model.ID, n int, gpu bool) {
	h.t.Helper()
	s := h.app.Monitor.Series(id)
	if s == nil {
		h.t.Fatal("host is not watched")
	}
	now := time.Now()
	for i := range n {
		f := float64(i)
		p := monitor.Sample{
			At:      now.Add(-time.Duration(n-i) * time.Second),
			Rates:   true,
			CPU:     55 + 25*sin(f/9) + 6*sin(f/2.3),
			MemUsed: 58 << 30, MemTotal: 128 << 30, MemCached: 20 << 30,
			DiskRead: 40e6 + 20e6*sin(f/7), DiskWrite: 25e6 + 10e6*sin(f/5), DiskSplit: true,
			NetRx: 300e6 + 120e6*sin(f/11), NetTx: 80e6 + 30e6*sin(f/6),
			Disks: []monitor.Disk{{Mount: "/", Total: 500 << 30, Used: 220 << 30}, {Mount: "/data", Total: 7 << 40, Used: 6 << 40}},
		}
		p.CPUUser, p.CPUSystem = p.CPU*0.7, p.CPU*0.3
		if gpu {
			for g := range 8 {
				p.GPUs = append(p.GPUs, monitor.GPU{
					Index: g, Name: "NVIDIA A100-SXM4-80GB",
					Util: min(max(78+18*sin(f/8+float64(g)), 5), 100), MemUsed: uint64(52+g) << 30, MemTotal: 80 << 30, TempC: 60 + float64(g),
				})
			}
		}
		s.Add(p)
	}
}

func TestMonitor_FooterBarsFollowTheMachine(t *testing.T) {
	h := newHarness(t, harnessOpts{exec: monitorFixture(t, false)})
	h.fastMonitor()
	h.openAll(1)
	id := blockByName(h, "web-01").HostID
	h.waitFor("two samples", func() bool {
		s := h.app.Monitor.Series(id)
		if s == nil {
			return false
		}
		for _, p := range s.Window(time.Minute, time.Now()) {
			if p.Rates && p.CPU > 0 {
				return true
			}
		}
		return false
	})
	h.app.Monitor.SetPaused(true)
	h.seedHistory(id, 60, true)
	h.waitFor("footer shows percentages", func() bool { return h.tt.HasText("GPU") && h.tt.HasText("%") })
	h.shot("footer-bars")
}

func TestMonitor_PageOpensFromFooterAndCloses(t *testing.T) {
	h := newHarness(t, harnessOpts{exec: monitorFixture(t, true)})
	h.fastMonitor()
	h.openAll(2)
	id := blockByName(h, "web-01").HostID
	h.waitFor("info", func() bool {
		s := h.app.Monitor.Series(id)
		if s == nil {
			return false
		}
		_, ok := s.Info()
		return ok
	})
	h.seedHistory(id, 280, true)
	h.app.Monitor.SetPaused(true)
	h.tt.Advance(600 * time.Millisecond) // blocks settle into place
	h.tt.Frame()
	r, ok := h.tt.Find("Open the monitor of web-01")
	if !ok {
		t.Fatal("no footer on web-01")
	}
	h.tt.ClickAt(r.X+r.W-8, r.Y+r.H/2)
	h.waitText("Back to terminals")
	if h.app.UI.MonitorOpen() != id {
		t.Fatalf("monitor open on %q, want %q", h.app.UI.MonitorOpen(), id)
	}
	for _, want := range []string{"web-01", "GPU", "Disks", "About", "5 min", "nothing is installed on the server"} {
		if !h.tt.HasText(want) {
			t.Errorf("monitor page lacks %q; screen: %q", want, h.tt.Texts())
		}
	}
	h.shot("monitor-page")
	h.click("1 h")
	h.shot("monitor-page-1h")
	h.click("Back to terminals")
	if h.app.UI.MonitorOpen() != "" || h.tt.HasText("Back to terminals") {
		t.Fatal("the back button did not close the page")
	}
}

func TestMonitor_ThresholdColors(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	for p, want := range map[float64]string{10: "green", 69.9: "green", 70: "amber", 89: "amber", 90: "red", 100: "red"} {
		if got := h.app.UI.Level(p); got != want {
			t.Errorf("level(%v) = %s, want %s", p, got, want)
		}
	}
	if err := h.app.Settings.Update(func(v *settings.Values) { v.Monitor.WarnPct, v.Monitor.CritPct = 40, 60 }); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[float64]string{39: "green", 40: "amber", 60: "red"} {
		if got := h.app.UI.Level(p); got != want {
			t.Errorf("custom level(%v) = %s, want %s", p, got, want)
		}
	}
}

func TestMonitor_OffStopsSampling(t *testing.T) {
	var calls atomic.Int32
	base := monitorFixture(t, false)
	h := newHarness(t, harnessOpts{exec: func(cmd string) (string, int) {
		if cmd == (monitor.Linux{}).Command() {
			calls.Add(1)
		}
		return base(cmd)
	}})
	if err := h.app.Settings.Update(func(v *settings.Values) { v.Monitor.Off, v.Monitor.IntervalSec = true, 1 }); err != nil {
		t.Fatal(err)
	}
	h.openAll(1)
	for range 6 {
		time.Sleep(300 * time.Millisecond)
		h.tt.Frame()
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("a switched-off monitor still sampled %d times", n)
	}
}

// homes gives every host (and this computer) its own folder on this
// computer, so the transfer sheet can be driven without SFTP.
type homes struct {
	byHost map[model.ID]string
	local  string
}

func (p homes) Endpoint(_ context.Context, l transfer.Location) (transfer.Endpoint, error) {
	home := p.local
	if l.Host != "" {
		home = p.byHost[l.Host]
	}
	return homeEndpoint{Endpoint: transfer.Local(), home: home}, nil
}

func (p homes) Reset(transfer.Location) {}

type homeEndpoint struct {
	transfer.Endpoint
	home string
}

func (e homeEndpoint) FS() transport.FS { return homeFS{FS: e.Endpoint.FS(), home: e.home} }

type homeFS struct {
	transport.FS
	home string
}

func (f homeFS) Home() (string, error) { return f.home, nil }

func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// transferHarness has two hosts with a folder each.
func transferHarness(t *testing.T) (h *harness, src, dst, local string) {
	t.Helper()
	root := t.TempDir()
	src, dst, local = filepath.Join(root, "web-01"), filepath.Join(root, "web-02"), filepath.Join(root, "me")
	for _, d := range []string{src, dst, local} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := homes{byHost: map[model.ID]string{}, local: local}
	h = newHarness(t, harnessOpts{files: files})
	h.openAll(2)
	files.byHost[blockByName(h, "web-01").HostID] = src
	files.byHost[blockByName(h, "web-02").HostID] = dst
	return h, src, dst, local
}

func (h *harness) openSheetFrom(name string) {
	h.t.Helper()
	h.app.UI.OpenTransfer(blockByName(h, name).HostID)
	h.waitText("File transfer")
}

func TestTransfer_SheetCopiesBetweenServers(t *testing.T) {
	h, src, dst, _ := transferHarness(t)
	write(t, filepath.Join(src, "backup.tar"), 3<<20)
	write(t, filepath.Join(src, "logs", "a.log"), 1000)
	h.openSheetFrom("web-01")
	h.waitText("backup.tar")
	for _, want := range []string{"Source", "Target", "Via this computer", "Direct scp / rsync", "Transfer queue", "Select files on the left"} {
		if !h.tt.HasText(want) {
			t.Errorf("the sheet lacks %q; screen: %q", want, h.tt.Texts())
		}
	}
	h.click("backup.tar")
	h.waitText("Selected 1")
	h.shot("transfer-select")
	h.click("Start transfer  ↵")
	h.waitFor("the job to finish", func() bool { return h.tt.HasText("Size matches") })
	got, err := os.ReadFile(filepath.Join(dst, "backup.tar"))
	if err != nil || len(got) != 3<<20 {
		t.Fatalf("the copy: %d bytes, %v", len(got), err)
	}
	if !h.tt.HasText("Server to server") {
		t.Fatal("the job has no server-to-server tag")
	}
	h.shot("transfer-done")
	h.click("Clear finished")
	h.waitFor("an empty queue", func() bool { return h.tt.HasText("Nothing is being transferred") })
}

func TestTransfer_ConflictAsksAndOverwrite(t *testing.T) {
	h, src, dst, _ := transferHarness(t)
	write(t, filepath.Join(src, "same.bin"), 2000)
	write(t, filepath.Join(dst, "same.bin"), 10)
	h.openSheetFrom("web-01")
	h.waitText("same.bin")
	h.click("same.bin")
	h.click("Start transfer  ↵")
	h.waitText("already exists")
	h.shot("transfer-conflict")
	h.click("Overwrite")
	h.waitFor("the job to finish", func() bool { return h.tt.HasText("Size matches") })
	if fi, err := os.Stat(filepath.Join(dst, "same.bin")); err != nil || fi.Size() != 2000 {
		t.Fatalf("after overwrite: %v, %v", fi, err)
	}
}

func TestTransfer_DropUploadsAndDirectIsOffByDefault(t *testing.T) {
	h, _, dst, local := transferHarness(t)
	write(t, filepath.Join(local, "notes.txt"), 500)
	h.openSheetFrom("web-01")
	h.waitText("Target")

	// Direct mode refuses until the setting allows it.
	h.click("Direct scp / rsync")
	h.waitText("Direct mode is off")
	if h.tt.HasText("Direct mode needs") {
		t.Fatal("the wrong reason for refusing direct mode")
	}

	r, ok := h.tt.Find("Target")
	if !ok {
		t.Fatal("no target pane")
	}
	if !h.tt.DropFiles(r.X+100, r.Y+120, filepath.Join(local, "notes.txt")) {
		t.Fatal("the target pane took no dropped files")
	}
	h.waitFor("the upload", func() bool {
		_, err := os.Stat(filepath.Join(dst, "notes.txt"))
		return err == nil
	})
	h.waitFor("the upload tag", func() bool { return h.tt.HasText("Upload") })
}

func TestTransfer_SettingsReachTheQueue(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.app.UI.OpenTransfer("")
	h.waitText("File transfer")
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.tt.HasText("File transfer") {
		t.Fatal("Escape did not close the sheet")
	}
}

func sin(x float64) float64 { return math.Sin(x) }
