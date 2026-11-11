package monitor_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wei-dong/tako/internal/monitor"
)

// fakeHost is a machine whose command output the test controls.
type fakeHost struct {
	mu    sync.Mutex
	down  bool
	calls int
	out   []byte
}

func (f *fakeHost) exec(_ context.Context, cmd string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.down {
		return nil, errors.New("not connected")
	}
	if cmd == (monitor.Linux{}).InfoCommand() {
		return fixtureBytes("linux-info.txt"), nil
	}
	return f.out, nil
}

func (f *fakeHost) setDown(v bool) {
	f.mu.Lock()
	f.down = v
	f.mu.Unlock()
}

func fixtureBytes(name string) []byte {
	b, err := readFixture(name)
	if err != nil {
		panic(err)
	}
	return b
}

type collected struct {
	mu      sync.Mutex
	samples []monitor.Sample
	infos   int
	fails   int
}

func (c *collected) snapshot() (samples []monitor.Sample, infos, fails int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]monitor.Sample(nil), c.samples...), c.infos, c.fails
}

func (c *collected) opts(h *fakeHost) monitor.SamplerOptions {
	return monitor.SamplerOptions{
		Collector: monitor.Linux{}, Exec: h.exec, Interval: 20 * time.Millisecond, Jitter: -1,
		OnSample: func(s monitor.Sample) { c.mu.Lock(); c.samples = append(c.samples, s); c.mu.Unlock() },
		OnInfo:   func(monitor.Info) { c.mu.Lock(); c.infos++; c.mu.Unlock() },
		OnFail:   func(error) { c.mu.Lock(); c.fails++; c.mu.Unlock() },
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSampler_SamplesOnATimerAndReadsInfoOnce(t *testing.T) {
	h := &fakeHost{out: fixtureBytes("linux-1.txt")}
	var c collected
	s := monitor.StartSampler(t.Context(), c.opts(h))
	defer s.Close()
	eventually(t, "five samples", func() bool { n, _, _ := c.snapshot(); return len(n) >= 5 })
	samples, infos, _ := c.snapshot()
	if infos != 1 {
		t.Errorf("info read %d times, want once", infos)
	}
	if samples[0].Rates {
		t.Error("the first sample has no rates")
	}
}

// A failing connection is reported once, forgets the counters (a speed across
// a gap is wrong), and recovers without help.
func TestSampler_FailsOnceAndRecovers(t *testing.T) {
	h := &fakeHost{out: fixtureBytes("linux-1.txt")}
	var c collected
	s := monitor.StartSampler(t.Context(), c.opts(h))
	defer s.Close()
	eventually(t, "samples", func() bool { n, _, _ := c.snapshot(); return len(n) >= 3 })

	h.setDown(true)
	eventually(t, "the failure", func() bool { _, _, f := c.snapshot(); return f == 1 })
	before, _, _ := c.snapshot()
	h.setDown(false)
	eventually(t, "samples again", func() bool { n, _, _ := c.snapshot(); return len(n) > len(before)+1 })
	samples, _, fails := c.snapshot()
	if fails != 1 {
		t.Errorf("%d failures reported for one outage", fails)
	}
	if samples[len(before)].Rates {
		t.Error("the first sample after an outage must not claim rates")
	}
}

func TestSampler_PauseStopsAndResumeContinues(t *testing.T) {
	h := &fakeHost{out: fixtureBytes("linux-1.txt")}
	var c collected
	s := monitor.StartSampler(t.Context(), c.opts(h))
	defer s.Close()
	eventually(t, "samples", func() bool { n, _, _ := c.snapshot(); return len(n) >= 2 })
	s.Pause()
	time.Sleep(60 * time.Millisecond) // a sample in flight may still land
	n1, _, _ := c.snapshot()
	time.Sleep(100 * time.Millisecond)
	n2, _, _ := c.snapshot()
	if len(n2) != len(n1) {
		t.Errorf("paused sampler took %d samples", len(n2)-len(n1))
	}
	s.Resume()
	eventually(t, "samples after resume", func() bool { n, _, _ := c.snapshot(); return len(n) > len(n2)+1 })
}

func TestSampler_JitterSpreadsTheInterval(t *testing.T) {
	h := &fakeHost{out: fixtureBytes("linux-1.txt")}
	var c collected
	o := c.opts(h)
	o.Jitter = 0.5
	var asked atomic.Int32
	o.Rand = func() float64 { asked.Add(1); return 0 } // the lowest draw: interval × 0.5
	s := monitor.StartSampler(t.Context(), o)
	defer s.Close()
	eventually(t, "samples", func() bool { n, _, _ := c.snapshot(); return len(n) >= 3 })
	if asked.Load() == 0 {
		t.Error("the jitter was never drawn")
	}
}

func TestSampler_CloseStopsTheGoroutine(t *testing.T) {
	h := &fakeHost{out: fixtureBytes("linux-1.txt")}
	var c collected
	s := monitor.StartSampler(t.Context(), c.opts(h))
	s.Close()
	s.Close() // twice is fine
}

func TestSampler_LatencyFromPing(t *testing.T) {
	h := &fakeHost{out: fixtureBytes("linux-1.txt")}
	var c collected
	o := c.opts(h)
	o.Ping = func(context.Context) (time.Duration, error) { return 7 * time.Millisecond, nil }
	s := monitor.StartSampler(t.Context(), o)
	defer s.Close()
	eventually(t, "a sample", func() bool { n, _, _ := c.snapshot(); return len(n) >= 1 })
	if n, _, _ := c.snapshot(); n[0].Latency != 7*time.Millisecond {
		t.Errorf("latency %v", n[0].Latency)
	}
}
