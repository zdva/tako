package monitor_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/monitor"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

func readFixture(name string) ([]byte, error) {
	return os.ReadFile(filepath.Join("testdata", name))
}

func fixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var t0 = time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)

func near(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.3f, want %.3f ± %.3f", name, got, want, tol)
	}
}

// Linux, recorded on a real machine.
func TestLinux_RecordedOutput(t *testing.T) {
	c := monitor.Linux{}
	first, raw, err := c.Parse(fixture(t, "linux-1.txt"), monitor.Raw{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Rates {
		t.Error("the first sample cannot have rates")
	}
	if first.MemTotal != 16386416*1024 {
		t.Errorf("MemTotal = %d", first.MemTotal)
	}
	if want := uint64(16386416-7781576) * 1024; first.MemUsed != want {
		t.Errorf("MemUsed = %d, want total - available = %d", first.MemUsed, want)
	}
	if first.Load != [3]float64{0.07, 0.62, 0.95} {
		t.Errorf("Load = %v", first.Load)
	}
	if len(first.Disks) == 0 || first.Disks[0].Mount != "/" || first.Disks[0].Total == 0 {
		t.Errorf("Disks = %+v", first.Disks)
	}
	if len(first.GPUs) != 0 {
		t.Errorf("no GPU tools on this machine, got %v", first.GPUs)
	}

	second, _, err := c.Parse(fixture(t, "linux-2.txt"), raw, t0.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !second.Rates {
		t.Fatal("the second sample has rates")
	}
	if second.CPU < 0 || second.CPU > 100 || len(second.Cores) != 8 {
		t.Errorf("CPU %.1f, cores %d", second.CPU, len(second.Cores))
	}
	if second.CPU+1e-9 < second.CPUUser || second.CPUUser+second.CPUSystem > 100.01 {
		t.Errorf("CPU %.2f user %.2f system %.2f do not add up", second.CPU, second.CPUUser, second.CPUSystem)
	}
}

// Linux with counters made up so the arithmetic can be checked exactly.
func TestLinux_RatesFromCounters(t *testing.T) {
	mk := func(user, system, idle, rd, wr, rx, tx int) []byte {
		return fmt.Appendf(nil, `##STAT
cpu  %d 0 %d %d 0 0 0 0 0 0
cpu0 %d 0 %d %d 0 0 0 0 0 0
##LOAD
1.00 2.00 3.00 1/100 5
##MEM
MemTotal:       1000 kB
MemAvailable:    250 kB
Buffers:          50 kB
Cached:          100 kB
Shmem:            10 kB
SReclaimable:     20 kB
##DISKSTATS
 259 0 nvme0n1 1 0 %d 0 1 0 %d 0 0 0 0 0 0 0 0 0 0
 259 1 nvme0n1p1 1 0 999999 0 1 0 999999 0 0 0 0 0 0 0 0 0 0
   7 0 loop0 1 0 999999 0 1 0 999999 0 0 0 0 0 0 0 0 0 0
##NET
Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 99999 1 0 0 0 0 0 0 99999 1 0 0 0 0 0 0
  eth0: %d 1 0 0 0 0 0 0 %d 1 0 0 0 0 0 0
docker0: 55555 1 0 0 0 0 0 0 55555 1 0 0 0 0 0 0
##DF
Filesystem     1024-blocks    Used Available Capacity Mounted on
/dev/sda1           1000000  250000    750000      25%% /
tmpfs                  1000       0      1000       0%% /dev/shm
/dev/sdb1           2000000 1900000    100000      95%% /data
/dev/sdb1           2000000 1900000    100000      95%% /srv/bind
`, user, system, idle, user, system, idle, rd, wr, rx, tx)
	}
	c := monitor.Linux{}
	_, raw, err := c.Parse(mk(100, 50, 850, 0, 0, 0, 0), monitor.Raw{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	// 200 jiffies passed: 40 user, 20 system, 140 idle; 2 s later.
	s, _, err := c.Parse(mk(140, 70, 990, 4000, 2000, 10000, 4000), raw, t0.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "CPU", s.CPU, 30, 1e-9)
	near(t, "CPUUser", s.CPUUser, 20, 1e-9)
	near(t, "CPUSystem", s.CPUSystem, 10, 1e-9)
	near(t, "core0", s.Cores[0], 30, 1e-9)
	near(t, "DiskRead", s.DiskRead, 4000*512/2.0, 1e-9)
	near(t, "DiskWrite", s.DiskWrite, 2000*512/2.0, 1e-9)
	near(t, "NetRx", s.NetRx, 5000, 1e-9)
	near(t, "NetTx", s.NetTx, 2000, 1e-9)
	if s.MemTotal != 1000*1024 || s.MemUsed != 750*1024 {
		t.Errorf("memory used %d of %d", s.MemUsed, s.MemTotal)
	}
	if want := uint64(50+100+20-10) * 1024; s.MemCached != want {
		t.Errorf("MemCached = %d, want %d", s.MemCached, want)
	}
	if len(s.Disks) != 2 || s.Disks[1].Mount != "/data" {
		t.Fatalf("Disks = %+v: tmpfs is dropped and a bind mount listed once at its shortest path", s.Disks)
	}
	near(t, "data disk", s.Disks[1].Percent(), 95, 1e-9)
	near(t, "disk max", s.DiskMax(), 95, 1e-9)
}

func TestLinux_CounterGoingBackGivesNoRate(t *testing.T) {
	c := monitor.Linux{}
	mk := func(rx int) []byte {
		return fmt.Appendf(nil, "##STAT\ncpu  10 0 10 80 0 0 0 0 0 0\n##MEM\nMemTotal: 10 kB\n##NET\n eth0: %d 1 0 0 0 0 0 0 %d 1 0 0 0 0 0 0\n", rx, rx)
	}
	_, raw, _ := c.Parse(mk(5000), monitor.Raw{}, t0)
	s, _, _ := c.Parse(mk(100), raw, t0.Add(time.Second)) // a reboot reset the counters
	if s.NetRx != 0 || s.NetTx != 0 {
		t.Errorf("net after a counter reset = %v/%v, want 0", s.NetRx, s.NetTx)
	}
}

func TestLinux_Info(t *testing.T) {
	in, err := monitor.Linux{}.ParseInfo(fixture(t, "linux-info.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if in.System != "Debian GNU/Linux 13 (trixie)" || in.Kernel != "Linux 6.12.113+" || in.Cores != 8 || in.MemTotal != 16386416*1024 {
		t.Errorf("Info = %+v", in)
	}
	if in.CPU != "Intel(R) Xeon(R) Processor" || in.Uptime < 6*time.Hour {
		t.Errorf("Info = %+v", in)
	}
}

func TestLinux_GPUSections(t *testing.T) {
	base := fixture(t, "linux-1.txt")
	nv := append(append([]byte{}, base...), append([]byte("##NVIDIA\n"), fixture(t, "nvidia-8.txt")...)...)
	s, _, err := monitor.Linux{}.Parse(nv, monitor.Raw{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.GPUs) != 8 {
		t.Fatalf("%d GPUs", len(s.GPUs))
	}
	g := s.GPUs[2]
	if g.Index != 2 || g.Name != "NVIDIA A100-SXM4-80GB" || g.Util != 97 || g.TempC != 83 || g.PowerW != 391 {
		t.Errorf("GPU 2 = %+v", g)
	}
	if g.MemUsed != 62100*1024*1024 || g.MemTotal != 81920*1024*1024 {
		t.Errorf("GPU 2 memory = %d / %d", g.MemUsed, g.MemTotal)
	}
	near(t, "GPUUtil", s.GPUUtil(), 95, 0.1)
	used, total := s.GPUMem()
	if total != 8*81920*1024*1024 || used == 0 {
		t.Errorf("GPUMem = %d / %d", used, total)
	}

	na := append(append([]byte{}, base...), append([]byte("##NVIDIA\n"), fixture(t, "nvidia-na.txt")...)...)
	s, _, _ = monitor.Linux{}.Parse(na, monitor.Raw{}, t0)
	if len(s.GPUs) != 1 || s.GPUs[0].PowerW != 0 || s.GPUs[0].TempC != 31 {
		t.Errorf("a [N/A] field is 0, got %+v", s.GPUs)
	}

	rocm := append(append([]byte{}, base...), append([]byte("##ROCM\n"), fixture(t, "rocm-2.json")...)...)
	s, _, _ = monitor.Linux{}.Parse(rocm, monitor.Raw{}, t0)
	if len(s.GPUs) != 2 || s.GPUs[0].Util != 87 || s.GPUs[1].MemUsed != 3435134976 || s.GPUs[0].Name != "AMD Instinct MI250X" {
		t.Errorf("ROCm GPUs = %+v", s.GPUs)
	}
}

func TestLinux_NoSectionsIsNoData(t *testing.T) {
	if _, _, err := (monitor.Linux{}).Parse([]byte("sh: 1: foo: not found\n"), monitor.Raw{}, t0); err == nil {
		t.Error("garbage must be an error")
	}
}

// macOS, from hand-composed fixtures (see testdata/README.md).
func TestDarwin_Fixtures(t *testing.T) {
	c := monitor.Darwin{}
	s1, raw, err := c.Parse(fixture(t, "darwin-1.txt"), monitor.Raw{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "CPU", s1.CPU, 100-84.22, 1e-6) // the second top reading, not the first
	near(t, "CPUUser", s1.CPUUser, 5.26, 1e-6)
	if s1.Load != [3]float64{2.34, 2.10, 2.05} {
		t.Errorf("Load = %v", s1.Load)
	}
	const page = 16384
	if s1.MemTotal != 17179869184 {
		t.Errorf("MemTotal = %d", s1.MemTotal)
	}
	if want := uint64(370000-10000+150000+180000) * page; s1.MemUsed != want {
		t.Errorf("MemUsed = %d, want app + wired + compressed = %d", s1.MemUsed, want)
	}
	if want := uint64(250000+10000) * page; s1.MemCached != want {
		t.Errorf("MemCached = %d, want %d", s1.MemCached, want)
	}
	if len(s1.Disks) != 2 || s1.Disks[0].Mount != "/" || s1.Disks[1].Mount != "/System/Volumes/Data" {
		t.Errorf("Disks = %+v: the data volume is kept, other system volumes are not", s1.Disks)
	}
	s2, _, err := c.Parse(fixture(t, "darwin-2.txt"), raw, t0.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	near(t, "NetRx", s2.NetRx, 60000000/2.0, 1) // en0 only: lo0, gif0 and utun0 are not counted
	near(t, "NetTx", s2.NetTx, 10000000/2.0, 1)
	near(t, "DiskIO", s2.DiskRead, 200*1024*1024/2.0, 1)
	if s2.DiskSplit {
		t.Error("macOS cannot split disk reads from writes")
	}
}

func TestDarwin_Info(t *testing.T) {
	in, err := monitor.Darwin{}.ParseInfo(fixture(t, "darwin-info.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if in.System != "macOS 14.5" || in.Kernel != "Darwin 23.5.0" || in.CPU != "Apple M2 Pro" || in.Cores != 12 || in.MemTotal != 17179869184 {
		t.Errorf("Info = %+v", in)
	}
	if want := time.Duration(1731548000-1731200000) * time.Second; in.Uptime != want {
		t.Errorf("Uptime = %v, want %v", in.Uptime, want)
	}
}

// Windows, from hand-composed fixtures.
func TestWindows_Fixtures(t *testing.T) {
	c := monitor.Windows{}
	s, _, err := c.Parse(fixture(t, "windows-1.json"), monitor.Raw{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Rates {
		t.Error("CIM returns rates, so the first sample has them")
	}
	near(t, "CPU", s.CPU, 23, 1e-9)
	near(t, "CPUSystem", s.CPUSystem, 8, 1e-9)
	if s.MemTotal != 32<<30 || s.MemUsed != 32<<30-20<<30 {
		t.Errorf("memory %d used of %d", s.MemUsed, s.MemTotal)
	}
	if len(s.Disks) != 2 || s.Disks[0].Mount != "C:" || s.Disks[0].Used != 511101108224-212345678848 {
		t.Errorf("Disks = %+v", s.Disks)
	}
	near(t, "NetRx", s.NetRx, 1250000, 1e-9)
	near(t, "DiskWrite", s.DiskWrite, 2097152, 1e-9)

	// PowerShell can print progress records around the JSON, and unrolls a
	// single disk into a bare object.
	s, _, err = c.Parse(fixture(t, "windows-single-disk.json"), monitor.Raw{}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Disks) != 1 || s.Disks[0].Mount != "C:" || len(s.GPUs) != 1 || s.GPUs[0].Name != "NVIDIA GeForce RTX 4070" {
		t.Errorf("Disks %+v GPUs %+v", s.Disks, s.GPUs)
	}
}

func TestWindows_InfoAndCommand(t *testing.T) {
	in, err := monitor.Windows{}.ParseInfo(fixture(t, "windows-info.json"))
	if err != nil {
		t.Fatal(err)
	}
	if in.System != "Microsoft Windows 11 Pro" || in.Cores != 24 || in.GPUDriver != "551.86" || in.Uptime < 26*time.Hour {
		t.Errorf("Info = %+v", in)
	}
	cmd := monitor.Windows{}.Command()
	if !strings.HasPrefix(cmd, "powershell -NoProfile -NonInteractive") || !strings.Contains(cmd, "-EncodedCommand ") {
		t.Errorf("command = %.80s", cmd)
	}
	if strings.ContainsAny(cmd, "'\"`$|;") {
		t.Errorf("the encoded command must survive cmd.exe and any shell: %q", cmd)
	}
}

func TestDetectOS(t *testing.T) {
	cases := []struct {
		out  string
		err  error
		want model.OSKind
	}{
		{"Linux\n", nil, model.OSLinux},
		{"Darwin\n", nil, model.OSMacOS},
		{"'uname' is not recognized as an internal or external command", errors.New("exit 1"), model.OSWindows},
		{"", errors.New("exit 1"), model.OSWindows},
		{"FreeBSD\n", nil, model.OSLinux},
	}
	for _, tc := range cases {
		if got := monitor.DetectOS([]byte(tc.out), tc.err); got != tc.want {
			t.Errorf("DetectOS(%q, %v) = %v, want %v", tc.out, tc.err, got, tc.want)
		}
	}
	for _, os := range []model.OSKind{model.OSLinux, model.OSMacOS, model.OSWindows, ""} {
		if monitor.ForOS(os).Command() == "" {
			t.Errorf("no command for %q", os)
		}
	}
}

func TestSeries(t *testing.T) {
	var s monitor.Series
	if _, ok := s.Latest(); ok {
		t.Error("an empty series has no latest")
	}
	for i := range 10 {
		s.Add(monitor.Sample{At: t0.Add(time.Duration(i) * time.Minute), Rates: i > 0, CPU: float64(i)})
	}
	if got := s.Window(5*time.Minute, t0.Add(9*time.Minute)); len(got) != 6 || got[0].CPU != 4 {
		t.Errorf("window of 5 minutes = %d samples starting at CPU %v", len(got), got[0].CPU)
	}
	if l, _ := s.Latest(); l.CPU != 9 {
		t.Errorf("latest CPU %v", l.CPU)
	}
	s.Fail(errors.New("down"))
	if f, err := s.Failing(); !f || err == nil {
		t.Error("Fail was not recorded")
	}
	s.Add(monitor.Sample{At: t0, Rates: true})
	if f, _ := s.Failing(); f {
		t.Error("a new sample clears the failure")
	}
	for range 4000 {
		s.Add(monitor.Sample{At: t0})
	}
	if s.Len() != 3600 {
		t.Errorf("the series holds %d samples, want it capped at 3600", s.Len())
	}
}

func FuzzParsers(f *testing.F) {
	for _, n := range []string{"linux-1.txt", "darwin-1.txt", "windows-1.json", "nvidia-8.txt", "rocm-2.json", "windows-single-disk.json"} {
		data, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		for _, c := range []monitor.Collector{monitor.Linux{}, monitor.Darwin{}, monitor.Windows{}} {
			s, raw, err := c.Parse(data, monitor.Raw{}, t0)
			if err != nil {
				continue
			}
			// Whatever the input, the numbers the UI draws stay in range.
			s2, _, _ := c.Parse(data, raw, t0.Add(time.Second))
			for _, v := range []monitor.Sample{s, s2} {
				if v.CPU < 0 || v.CPU > 100 || v.MemUsed > v.MemTotal && v.MemTotal != 0 {
					t.Fatalf("out of range: %+v", v)
				}
				if v.NetRx < 0 || v.NetTx < 0 || v.DiskRead < 0 || v.DiskWrite < 0 {
					t.Fatalf("negative rate: %+v", v)
				}
			}
			_, _ = c.ParseInfo(data)
		}
	})
}

func TestHub_WatchesProbesAndReportsOnlyVisibleChanges(t *testing.T) {
	h := &fakeHost{out: fixtureBytes("linux-1.txt")}
	exec := func(ctx context.Context, cmd string) ([]byte, error) {
		if cmd == monitor.ProbeCommand {
			return []byte("Linux\n"), nil
		}
		return h.exec(ctx, cmd)
	}
	var mu sync.Mutex
	var changes int
	hub := monitor.NewHub(monitor.HubOptions{
		Interval: 10 * time.Millisecond, Jitter: -1,
		OnChange: func(model.ID) { mu.Lock(); changes++; mu.Unlock() },
	})
	defer hub.Close()
	hub.Watch(monitor.Target{Host: "h1", Exec: exec})
	eventually(t, "samples", func() bool { s := hub.Series("h1"); return s != nil && s.Len() >= 8 })
	mu.Lock()
	n := changes
	mu.Unlock()
	if n >= 8 {
		t.Errorf("%d notifications for %d identical samples: the idle fleet would redraw", n, hub.Series("h1").Len())
	}
	if n == 0 {
		t.Error("never told the window")
	}

	hub.SetFocus("h1")
	mu.Lock()
	before := changes
	mu.Unlock()
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	after := changes
	mu.Unlock()
	if after-before < 3 {
		t.Errorf("a focused host reported only %d times in 100 ms", after-before)
	}
	hub.SetFocus("")

	hub.SetInterval(5 * time.Millisecond)
	hub.SetPaused(true)
	time.Sleep(50 * time.Millisecond)
	l1 := hub.Series("h1").Len()
	time.Sleep(60 * time.Millisecond)
	if l2 := hub.Series("h1").Len(); l2 != l1 {
		t.Errorf("paused hub sampled %d times", l2-l1)
	}
	hub.SetPaused(false)
	eventually(t, "samples after unpause", func() bool { return hub.Series("h1").Len() > l1+2 })

	if !hub.Watching("h1") || hub.Watching("nope") {
		t.Error("Watching")
	}
	hub.Forget("h1")
	if hub.Series("h1") != nil {
		t.Error("history kept after Forget")
	}
}

// A fleet whose readings wobble by a point or two is told to the window
// rarely, and in merged calls, not once per host per sample.
func TestHub_MergesWobbleIntoFewWakes(t *testing.T) {
	one, two := fixtureBytes("linux-1.txt"), fixtureBytes("linux-2.txt")
	var n atomic.Int64
	exec := func(_ context.Context, cmd string) ([]byte, error) {
		switch cmd {
		case monitor.ProbeCommand:
			return []byte("Linux\n"), nil
		case monitor.Linux{}.InfoCommand():
			return fixtureBytes("linux-info.txt"), nil
		}
		// Alternating counters: the CPU reading swings between samples.
		if n.Add(1)%2 == 0 {
			return two, nil
		}
		return one, nil
	}
	var wakes atomic.Int64
	hub := monitor.NewHub(monitor.HubOptions{
		Interval: 5 * time.Millisecond, Jitter: -1, MinGap: 300 * time.Millisecond,
		OnChange: func(model.ID) { wakes.Add(1) },
	})
	defer hub.Close()
	for i := range 6 {
		hub.Watch(monitor.Target{Host: model.ID(fmt.Sprint("h", i)), OS: model.OSLinux, Exec: exec})
	}
	time.Sleep(1700 * time.Millisecond) // the first sample of a host is followed by a short pause
	samples := 0
	for i := range 6 {
		samples += hub.Series(model.ID(fmt.Sprint("h", i))).Len()
	}
	got := wakes.Load()
	// At most one call per gap (plus the first), whatever the sample count.
	if got > 7 {
		t.Errorf("%d wake-ups in 1.7 s for %d samples with a 300 ms gap", got, samples)
	}
	if got == 0 {
		t.Error("the window was never told")
	}
	if samples < 100 {
		t.Errorf("only %d samples: the test measured nothing", samples)
	}
}

func TestHub_ProbesTheOSAndRetriesUntilReachable(t *testing.T) {
	var calls int
	var mu sync.Mutex
	exec := func(_ context.Context, cmd string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if cmd == monitor.ProbeCommand {
			calls++
			if calls == 1 {
				return nil, errors.New("not connected")
			}
			return []byte("Darwin\n"), nil
		}
		if cmd == (monitor.Darwin{}).InfoCommand() {
			return fixtureBytes("darwin-info.txt"), nil
		}
		return fixtureBytes("darwin-1.txt"), nil
	}
	hub := monitor.NewHub(monitor.HubOptions{Interval: 10 * time.Millisecond, Jitter: -1})
	defer hub.Close()
	hub.Watch(monitor.Target{Host: "mac", Exec: exec})
	// The first probe fails on a fresh connection and the second says Darwin;
	// the first wait is a second, so allow for it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if s := hub.Series("mac"); s != nil && s.Len() > 0 {
			if p, ok := s.Latest(); ok && p.MemTotal > 0 {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no macOS sample arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
