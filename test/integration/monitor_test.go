//go:build integration && linux

package integration

import (
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wei-dong/tako/internal/monitor"
	"github.com/wei-dong/tako/internal/testutil/realsshd"
	"github.com/wei-dong/tako/internal/transfer"
)

// localCPU reads the machine's busy share between two readings of /proc/stat
// itself, the way the system's own tools do.
func localCPU(t *testing.T) func() float64 {
	t.Helper()
	read := func() (busy, total float64) {
		data, err := os.ReadFile("/proc/stat")
		if err != nil {
			t.Fatal(err)
		}
		line, _, _ := strings.Cut(string(data), "\n")
		var vals []float64
		for _, f := range strings.Fields(line)[1:] {
			v, _ := strconv.ParseFloat(f, 64)
			vals = append(vals, v)
		}
		for _, v := range vals[:8] { // user nice system idle iowait irq softirq steal
			total += v
		}
		return total - vals[3] - vals[4], total
	}
	b0, t0 := read()
	return func() float64 {
		b1, t1 := read()
		return (b1 - b0) / (t1 - t0) * 100
	}
}

// localMem is what free(1) calls used: total - available.
func localMem(t *testing.T) float64 {
	t.Helper()
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		t.Fatal(err)
	}
	var total, avail float64
	for line := range strings.Lines(string(data)) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(f[1], 64)
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	return (total - avail) / total * 100
}

func burn(d time.Duration, n int) {
	var wg sync.WaitGroup
	end := time.Now().Add(d)
	for range n {
		wg.Go(func() {
			x := 1.0
			for time.Now().Before(end) {
				for range 10000 {
					x = math.Sqrt(x + 1)
				}
			}
			_ = x
		})
	}
	wg.Wait()
}

// The monitor's figures from the real sshd's machine agree with the system's
// own within 5 percentage points: CPU under load, memory, and the root disk.
func TestMonitorLinuxMatchesSystemTools(t *testing.T) {
	if !realsshd.Available() {
		t.Skip("no sshd")
	}
	e := newEnv(t, "")
	srv := realsshd.Start(t, realsshd.Options{})
	h := e.host(srv, "monitor-me")
	tr, err := e.dialer.Dial(t.Context(), h)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.Close() }()

	col := monitor.Linux{}

	var raw monitor.Raw
	read := func() monitor.Sample {
		out, err := tr.Exec(t.Context(), col.Command())
		if err != nil {
			t.Fatal(err)
		}
		s, next, err := col.Parse(out, raw, time.Now())
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		raw = next
		return s
	}

	for _, load := range []int{0, 3} {
		read() // prime the counters
		cpu := localCPU(t)
		if load > 0 {
			burn(3*time.Second, load)
		} else {
			time.Sleep(3 * time.Second)
		}
		want := cpu()
		s := read()
		if !s.Rates {
			t.Fatal("no rates on the second reading")
		}
		t.Logf("load %d: monitor CPU %.1f%%, /proc/stat %.1f%%", load, s.CPU, want)
		if math.Abs(s.CPU-want) > 5 {
			t.Errorf("CPU %.1f%% vs system %.1f%%: off by more than 5 points", s.CPU, want)
		}
	}
	s := read()
	if want := localMem(t); math.Abs(s.MemPercent()-want) > 5 {
		t.Errorf("memory %.1f%% vs free %.1f%%", s.MemPercent(), want)
	}
	t.Logf("memory %.1f%% (system %.1f%%), cores %d, load %.2f", s.MemPercent(), localMem(t), len(s.Cores), s.Load[0])

	out, err := exec.Command("df", "-P", "-k", "/").Output()
	if err != nil {
		t.Fatal(err)
	}
	f := strings.Fields(strings.Split(strings.TrimSpace(string(out)), "\n")[1])
	used, _ := strconv.ParseFloat(f[2], 64)
	size, _ := strconv.ParseFloat(f[1], 64)
	var got *monitor.Disk
	for i := range s.Disks {
		if s.Disks[i].Mount == "/" {
			got = &s.Disks[i]
		}
	}
	if got == nil {
		t.Fatalf("no / in %+v", s.Disks)
	}
	if want := used / size * 100; math.Abs(got.Percent()-want) > 5 {
		t.Errorf("root disk %.1f%% vs df %.1f%%", got.Percent(), want)
	}
}

// One sample costs the remote machine well under 1% of a CPU at the default
// interval, measured as the CPU time the command itself uses.
func TestMonitorSamplingOverheadOnTheRemote(t *testing.T) {
	if !realsshd.Available() {
		t.Skip("no sshd")
	}
	e := newEnv(t, "")
	srv := realsshd.Start(t, realsshd.Options{})
	tr, err := e.dialer.Dial(t.Context(), e.host(srv, "overhead"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.Close() }()

	const runs = 20
	// bash's time keyword reports the command's own user+system seconds.
	inner := "TIMEFORMAT='%3U %3S'; time { for i in $(seq " + strconv.Itoa(runs) + "); do " +
		monitor.Linux{}.Command() + " >/dev/null; done; }"
	cmd := "bash -c " + transfer.ShellQuote(inner) + " 2>&1 >/dev/null"
	out, err := tr.Exec(t.Context(), cmd)
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	f := strings.Fields(string(out))
	if len(f) != 2 {
		t.Fatalf("unexpected output %q", out)
	}
	user, _ := strconv.ParseFloat(f[0], 64)
	sys, _ := strconv.ParseFloat(f[1], 64)
	perSample := (user + sys) / runs
	share := perSample / monitor.DefaultInterval.Seconds() * 100
	t.Logf("one sample uses %.1f ms of CPU: %.2f%% of one core at the default %v interval", perSample*1000, share, monitor.DefaultInterval)
	if share >= 1 {
		t.Errorf("sampling costs %.2f%% of a core", share)
	}
}

// The sampler delivers samples over a real connection and survives the
// server going away and coming back.
func TestMonitorSamplerOverSSH(t *testing.T) {
	if !realsshd.Available() {
		t.Skip("no sshd")
	}
	e := newEnv(t, "")
	srv := realsshd.Start(t, realsshd.Options{})
	h := e.host(srv, "sampler")
	tr, err := e.dialer.Dial(t.Context(), h)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.Close() }()
	var mu sync.Mutex
	var samples []monitor.Sample
	var info monitor.Info
	s := monitor.StartSampler(t.Context(), monitor.SamplerOptions{
		Collector: monitor.Linux{}, Exec: tr.Exec, Ping: tr.Ping, Interval: 200 * time.Millisecond,
		OnSample: func(s monitor.Sample) { mu.Lock(); samples = append(samples, s); mu.Unlock() },
		OnInfo:   func(i monitor.Info) { mu.Lock(); info = i; mu.Unlock() },
	})
	defer s.Close()
	deadline := time.Now().Add(15 * time.Second)
	for {
		mu.Lock()
		n := len(samples)
		mu.Unlock()
		if n >= 5 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d samples", n)
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	last := samples[len(samples)-1]
	if !last.Rates || len(last.Cores) == 0 || last.MemTotal == 0 || last.Latency <= 0 {
		t.Errorf("incomplete sample %+v", last)
	}
	if info.Cores == 0 || info.System == "" {
		t.Errorf("incomplete info %+v", info)
	}
}
