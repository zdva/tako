//go:build e2e && linux

package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/zdva/tako/internal/hostlib"
	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/secrets"
	"github.com/zdva/tako/internal/store"
	"github.com/zdva/tako/internal/workspace"
)

// Screenshots of the real window, for docs/screenshots. Run with
// TAKO_SHOTS=<dir> (see `make screenshots`).

// The size layoutWindow gives the window.
const windowW, windowH = 1440, 900

type demoHost struct {
	name, user, tag string
	os              model.OSKind
}

var demoHosts = []demoHost{
	{"web-01", "ubuntu", "生产", model.OSLinux},
	{"web-02", "ubuntu", "生产", model.OSLinux},
	{"web-03", "ubuntu", "生产", model.OSLinux},
	{"db-01", "postgres", "生产", model.OSLinux},
	{"gpu-01", "root", "GPU 集群", model.OSLinux},
	{"gpu-02", "root", "GPU 集群", model.OSLinux},
	{"win-build", "Administrator", "构建 & 办公", model.OSWindows},
	{"mac-mini", "wei", "构建 & 办公", model.OSMacOS},
	{"ci-runner", "runner", "构建 & 办公", model.OSLinux},
}

func (f *fixture) seedDemo(open []string, synced map[string]bool) {
	f.t.Helper()
	_ = os.Remove(f.dirs.Hosts())
	lib, err := hostlib.Open(store.Doc{Path: f.dirs.Hosts(), Version: hostlib.Version}, secrets.NewMemory(), nil)
	if err != nil {
		f.t.Fatal(err)
	}
	ids := map[string]model.ID{}
	for _, d := range demoHosts {
		h, err := lib.Add(model.Host{
			Name: d.name, Addr: f.srv.Host, Port: f.srv.Port, User: f.srv.User, OS: d.os, Tags: []string{d.tag},
			Auth: []model.Auth{{Kind: model.AuthKey, KeyPath: f.srv.ClientKeyPath}},
		})
		if err != nil {
			f.t.Fatal(err)
		}
		ids[d.name] = h.ID
	}
	lib.SetTagColor("生产", "#E0A030")
	lib.SetTagColor("GPU 集群", "#2BA3A3")
	lib.SetTagColor("构建 & 办公", "#3B6FE0")
	lib.Close()
	st := workspace.State{Version: workspace.StateVersion}
	for _, n := range open {
		st.Blocks = append(st.Blocks, workspace.BlockState{HostID: ids[n], Tabs: []workspace.TabState{{}}, Synced: synced[n]})
	}
	if err := (store.Doc{Path: f.dirs.Session(), Version: workspace.StateVersion}).Save(st); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) writeSettings(v map[string]any) {
	f.t.Helper()
	v["version"] = 1
	data, _ := json.Marshal(v)
	if err := os.WriteFile(filepath.Join(f.dirs.Config, "settings.json"), data, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func xdo(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("xdotool", args...).CombinedOutput(); err != nil {
		t.Logf("xdotool %v: %v %s", args, err, out)
	}
}

func shot(t *testing.T, dir, name string) {
	t.Helper()
	time.Sleep(400 * time.Millisecond)
	// Crop to the window, which layoutWindow puts at the top-left.
	py := fmt.Sprintf("from PIL import ImageGrab\nim = ImageGrab.grab(xdisplay=%q)\nim.crop((0, 0, min(im.width, %d), min(im.height, %d))).save(%q)",
		os.Getenv("DISPLAY"), windowW, windowH, filepath.Join(dir, name+".png"))
	if out, err := exec.Command("python3", "-c", py).CombinedOutput(); err != nil {
		t.Fatalf("screenshot %s: %v %s", name, err, out)
	}
}

func shotsDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("TAKO_SHOTS")
	if dir == "" {
		t.Skip("set TAKO_SHOTS to a directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// layoutWindow puts the app window at the top-left, as large as the display.
func layoutWindow(t *testing.T, r *run) {
	t.Helper()
	out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--pid", strconv.Itoa(r.cmd.Process.Pid)).Output()
	var win string
	for _, w := range splitFields(string(out)) {
		win = w
	}
	if win == "" {
		t.Fatal("no window")
	}
	xdo(t, "windowmove", win, "0", "0")
	xdo(t, "windowsize", win, strconv.Itoa(windowW), strconv.Itoa(windowH))
	xdo(t, "windowfocus", win)
	time.Sleep(300 * time.Millisecond)
}

func splitFields(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == '\n' || c == ' ' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// typeInto clicks a block's terminal and types commands there.
func typeInto(t *testing.T, x, y int, cmds ...string) {
	t.Helper()
	xdo(t, "mousemove", strconv.Itoa(x), strconv.Itoa(y), "click", "1")
	for _, c := range cmds {
		xdo(t, "type", "--delay", "20", c)
		xdo(t, "key", "Return")
		time.Sleep(300 * time.Millisecond)
	}
}

func startShots(t *testing.T, f *fixture, lang string) *run {
	t.Helper()
	f.warmFontCache()
	r := f.startEnv("LANG="+lang, "LC_ALL="+lang)
	r.windowAppears()
	layoutWindow(t, r)
	return r
}

func TestScreenshots_MainPaletteAddHost(t *testing.T) {
	dir := shotsDir(t)
	f := newFixture(t)
	six := []string{"web-01", "web-02", "db-01", "gpu-01", "win-build", "mac-mini"}
	f.writeSettings(map[string]any{"compactBlocks": true})
	f.seedDemo(six, map[string]bool{"web-01": true, "web-02": true, "db-01": true})
	r := startShots(t, f, "zh_CN.UTF-8")
	r.connected(6)
	time.Sleep(1200 * time.Millisecond)
	typeInto(t, 400, 300, "df -h / | head -n 2", "uptime")
	typeInto(t, 400, 650, "ls /")
	typeInto(t, 400, 300) // focus web-01 again, as in the design
	shot(t, dir, "main-light")

	xdo(t, "key", "ctrl+shift+p")
	time.Sleep(300 * time.Millisecond)
	xdo(t, "type", "--delay", "60", "web")
	shot(t, dir, "palette")
	xdo(t, "key", "Escape")

	xdo(t, "key", "ctrl+shift+n")
	time.Sleep(300 * time.Millisecond)
	xdo(t, "type", "--delay", "40", "web-04")
	shot(t, dir, "add-host")
	xdo(t, "key", "Escape")

	xdo(t, "key", "ctrl+shift+comma")
	shot(t, dir, "settings")
	xdo(t, "key", "Escape")
}

func TestScreenshots_Dark(t *testing.T) {
	dir := shotsDir(t)
	f := newFixture(t)
	f.writeSettings(map[string]any{"appearance": "dark", "compactBlocks": true})
	f.seedDemo([]string{"web-01", "web-02", "db-01", "gpu-01", "win-build", "mac-mini"}, map[string]bool{"web-01": true, "web-02": true, "db-01": true})
	r := startShots(t, f, "zh_CN.UTF-8")
	r.connected(6)
	time.Sleep(1200 * time.Millisecond)
	typeInto(t, 400, 300, "df -h / | head -n 2", "uptime")
	shot(t, dir, "main-dark")
}

func TestScreenshots_EmptyState(t *testing.T) {
	dir := shotsDir(t)
	f := newFixture(t)
	_ = os.Remove(f.dirs.Session())
	_ = os.Remove(f.dirs.Hosts())
	cfg := "../../internal/importer/sshconfig/testdata"
	if err := os.MkdirAll(filepath.Join(f.home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cp", "-r", cfg+"/.", filepath.Join(f.home, ".ssh")).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	r := startShots(t, f, "zh_CN.UTF-8")
	_ = r
	shot(t, dir, "empty-state")
}

func TestScreenshots_Disconnected(t *testing.T) {
	dir := shotsDir(t)
	f := newFixture(t)
	f.writeSettings(map[string]any{"compactBlocks": true})
	f.seedDemo([]string{"web-01", "web-02", "db-01", "gpu-01"}, map[string]bool{"web-01": true, "web-02": true})
	r := startShots(t, f, "zh_CN.UTF-8")
	r.connected(4)
	time.Sleep(800 * time.Millisecond)
	f.srv.Stop()
	time.Sleep(1500 * time.Millisecond)
	shot(t, dir, "disconnected")
}
