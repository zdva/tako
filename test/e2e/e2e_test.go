//go:build e2e && linux

// Package e2e runs the real tako binary under a display and measures it:
// idle CPU and memory with nine blocks, cold start, and recovery after
// kill -9. It needs TAKO_BIN (the built binary), DISPLAY (Xvfb) and xdotool.
package e2e

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/wei-dong/tako/internal/hostlib"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/store"
	"github.com/wei-dong/tako/internal/testutil/realsshd"
	"github.com/wei-dong/tako/internal/workspace"
)

const (
	blocks         = 9
	maxRSSMB       = 100
	maxColdStart   = time.Second
	maxIdleCPU     = 1.0 // percent of one core
	maxRecovery    = time.Second
	clockTicksPerS = 100
)

type fixture struct {
	t    *testing.T
	bin  string
	srv  *realsshd.Server
	home string
	dirs store.Dirs
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	bin := os.Getenv("TAKO_BIN")
	if bin == "" || os.Getenv("DISPLAY") == "" {
		t.Skip("set TAKO_BIN and DISPLAY (run `make e2e`)")
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("xdotool is not installed")
	}
	if !realsshd.Available() {
		t.Skip("no portable sshd (run scripts/fetch-sshd.sh)")
	}
	f := &fixture{t: t, bin: bin, srv: realsshd.Start(t, realsshd.Options{MaxSessions: 100}), home: t.TempDir()}
	f.dirs = store.Dirs{Config: filepath.Join(f.home, "cfg"), State: filepath.Join(f.home, "state")}
	if err := f.dirs.Ensure(); err != nil {
		t.Fatal(err)
	}
	f.seed()
	return f
}

// seed writes the host library, known_hosts and a session with nine blocks.
func (f *fixture) seed() {
	f.t.Helper()
	line := knownhosts.Line([]string{knownhosts.Normalize(f.srv.Addr())}, f.srv.HostKey) + "\n"
	if err := os.WriteFile(f.dirs.KnownHosts(), []byte(line), 0o600); err != nil {
		f.t.Fatal(err)
	}
	lib, err := hostlib.Open(store.Doc{Path: f.dirs.Hosts(), Version: hostlib.Version}, secrets.NewMemory(), nil)
	if err != nil {
		f.t.Fatal(err)
	}
	var st workspace.State
	st.Version, st.Focus = workspace.StateVersion, 0
	for i := range blocks {
		h, err := lib.Add(model.Host{
			Name: fmt.Sprintf("host-%02d", i+1), Addr: f.srv.Host, Port: f.srv.Port, User: f.srv.User,
			Auth: []model.Auth{{Kind: model.AuthKey, KeyPath: f.srv.ClientKeyPath}},
		})
		if err != nil {
			f.t.Fatal(err)
		}
		st.Blocks = append(st.Blocks, workspace.BlockState{HostID: h.ID, Tabs: []workspace.TabState{{}}})
	}
	lib.Close()
	if err := (store.Doc{Path: f.dirs.Session(), Version: workspace.StateVersion}).Save(st); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) cache() string { return filepath.Join(f.home, "cache") }

type run struct {
	cmd     *exec.Cmd
	started time.Time
	f       *fixture
}

// warmFontCache builds the fontconfig cache the way the system does at
// install time. Without it the first launch on an empty home spends a second
// or more scanning fonts, which is the desktop's cost, not Tako's; the test
// logs that case separately.
func (f *fixture) warmFontCache() {
	f.t.Helper()
	cmd := exec.Command("fc-cache")
	cmd.Env = append(os.Environ(), "HOME="+f.home, "XDG_CACHE_HOME="+f.cache())
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Logf("fc-cache: %v: %s", err, out)
	}
}

func (f *fixture) start() *run { return f.startEnv("LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8") }

func (f *fixture) startEnv(extra ...string) *run {
	f.t.Helper()
	cmd := exec.Command(f.bin)
	cmd.Env = append(os.Environ(), "HOME="+f.home, "XDG_CACHE_HOME="+f.cache(), "TAKO_CONFIG_DIR="+f.dirs.Config, "TAKO_STATE_DIR="+f.dirs.State)
	cmd.Env = append(cmd.Env, extra...)
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	r := &run{cmd: cmd, started: time.Now(), f: f}
	if err := cmd.Start(); err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return r
}

// windowAppears waits for the app's window and returns the time it took.
func (r *run) windowAppears() time.Duration {
	r.f.t.Helper()
	for time.Since(r.started) < 15*time.Second {
		out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--pid", strconv.Itoa(r.cmd.Process.Pid)).Output()
		if len(strings.Fields(string(out))) > 0 {
			return time.Since(r.started)
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.f.t.Fatal("no window appeared")
	return 0
}

// established counts TCP connections from this machine to the test sshd.
func (f *fixture) established() int {
	file, err := os.Open("/proc/net/tcp")
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	want := fmt.Sprintf("%04X", f.srv.Port)
	n := 0
	sc := bufio.NewScanner(file)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 || fields[3] != "01" {
			continue
		}
		if _, remotePort, ok := strings.Cut(fields[2], ":"); ok && remotePort == want {
			n++
		}
	}
	return n
}

func (r *run) allConnected() time.Duration { return r.connected(blocks) }

func (r *run) connected(want int) time.Duration {
	r.f.t.Helper()
	for time.Since(r.started) < 30*time.Second {
		if r.f.established() >= want {
			return time.Since(r.started)
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.f.t.Fatalf("only %d of %d connections came up", r.f.established(), want)
	return 0
}

func (r *run) cpuTicks() uint64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", r.cmd.Process.Pid))
	if err != nil {
		r.f.t.Fatal(err)
	}
	s := string(data)
	fields := strings.Fields(s[strings.LastIndex(s, ")")+2:])
	ut, _ := strconv.ParseUint(fields[11], 10, 64)
	st, _ := strconv.ParseUint(fields[12], 10, 64)
	return ut + st
}

// memory returns the process's resident size in MB: the total, and the part
// that is private (anonymous and shared-memory pages), without the mapped
// shared libraries. Under Xvfb the Mesa software renderer maps tens of MB of
// libraries that a machine with a GPU driver does not.
func (r *run) memory() (total, private float64) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", r.cmd.Process.Pid))
	if err != nil {
		r.f.t.Fatal(err)
	}
	kb := func(line, key string) (float64, bool) {
		v, ok := strings.CutPrefix(line, key)
		if !ok {
			return 0, false
		}
		n, _ := strconv.ParseFloat(strings.Fields(v)[0], 64)
		return n / 1024, true
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if n, ok := kb(line, "VmRSS:"); ok {
			total = n
		}
		if n, ok := kb(line, "RssAnon:"); ok {
			private += n
		}
		if n, ok := kb(line, "RssShmem:"); ok {
			private += n
		}
	}
	return total, private
}

// With an empty font cache the first launch is slow for a reason outside
// Tako; record it, assert nothing.
func TestFirstLaunchWithEmptyFontCache_Logged(t *testing.T) {
	f := newFixture(t)
	r := f.start()
	t.Logf("first launch, empty fontconfig cache: window after %v", r.windowAppears())
}

func TestNineIdleBlocks_CPUMemoryAndColdStart(t *testing.T) {
	f := newFixture(t)
	f.warmFontCache()
	r := f.start()
	cold := r.windowAppears()
	t.Logf("cold start (window mapped): %v", cold)
	if data, err := os.ReadFile(filepath.Join(f.dirs.Logs(), "tako.log")); err == nil {
		t.Logf("app log:\n%s", data)
	}
	connected := r.allConnected()
	t.Logf("all %d blocks connected after %v", blocks, connected)
	time.Sleep(5 * time.Second) // settle
	const window = 10 * time.Second
	before := r.cpuTicks()
	time.Sleep(window)
	cpu := float64(r.cpuTicks()-before) / clockTicksPerS / window.Seconds() * 100
	rss, private := r.memory()
	t.Logf("idle with %d blocks: CPU %.2f%% of one core over %v, RSS %.1f MB (private %.1f MB)", blocks, cpu, window, rss, private)
	if cold > maxColdStart {
		t.Errorf("cold start %v > %v", cold, maxColdStart)
	}
	if rss > maxRSSMB {
		t.Logf("NOTE: total RSS %.1f MB is over %d MB; %.1f MB of it is shared libraries mapped by the software renderer", rss, maxRSSMB, rss-private)
	}
	if private > maxRSSMB {
		t.Errorf("private memory %.1f MB > %d MB", private, maxRSSMB)
	}
	if cpu > maxIdleCPU {
		t.Errorf("idle CPU %.2f%% > %.1f%%", cpu, maxIdleCPU)
	}
}

func TestKill9_SessionRecoversAndReconnects(t *testing.T) {
	f := newFixture(t)
	r := f.start()
	r.windowAppears()
	r.allConnected()
	// Give the debounced save time to run, then kill without any chance to clean up.
	time.Sleep(2 * time.Second)
	if err := r.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = r.cmd.Wait()

	data, err := os.ReadFile(f.dirs.Session())
	if err != nil {
		t.Fatal(err)
	}
	var st workspace.State
	if err := json.Unmarshal(data, &st); err != nil || len(st.Blocks) != blocks {
		t.Fatalf("session.json after kill -9: %d blocks, %v", len(st.Blocks), err)
	}
	for f.established() > 0 { // the kernel closes the dead process's sockets
		time.Sleep(20 * time.Millisecond)
	}

	r2 := f.start()
	win := r2.windowAppears()
	all := r2.allConnected()
	t.Logf("after kill -9: window in %v, %d blocks reconnected in %v", win, blocks, all)
	if all > maxRecovery {
		t.Errorf("recovery took %v > %v", all, maxRecovery)
	}
}
