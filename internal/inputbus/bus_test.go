package inputbus

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// recorder is a destination that remembers what was written to it.
type recorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (r *recorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// waitFor polls until cond holds or the test times out.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func newBus(t *testing.T, ids ...ID) (*Bus, map[ID]*recorder) {
	t.Helper()
	bus := New(t.Context())
	t.Cleanup(bus.Close)
	recs := make(map[ID]*recorder, len(ids))
	for _, id := range ids {
		recs[id] = &recorder{}
		bus.Register(id, recs[id])
	}
	return bus, recs
}

func TestBus_Publish(t *testing.T) {
	t.Parallel()

	t.Run("synced input reaches synced blocks only", func(t *testing.T) {
		t.Parallel()
		bus, recs := newBus(t, "a", "b", "c", "d")
		bus.SetSynced("a", true)
		bus.SetSynced("b", true)
		bus.SetSynced("c", true)

		if got := bus.Publish("a", []byte("echo hi\r")); got != 2 {
			t.Fatalf("queued for %d blocks, want 2 (b and c)", got)
		}
		waitFor(t, "b and c to receive", func() bool {
			return recs["b"].String() != "" && recs["c"].String() != ""
		})
		if recs["b"].String() != "echo hi\r" || recs["c"].String() != "echo hi\r" {
			t.Fatalf("b=%q c=%q", recs["b"], recs["c"])
		}
		time.Sleep(20 * time.Millisecond)
		if recs["a"].String() != "" {
			t.Fatalf("the source must not receive its own input back, got %q", recs["a"])
		}
		if recs["d"].String() != "" {
			t.Fatalf("unsynced block received %q", recs["d"])
		}
	})

	t.Run("input typed in an unsynced block stays there", func(t *testing.T) {
		t.Parallel()
		bus, recs := newBus(t, "a", "b", "c")
		bus.SetSynced("b", true)
		bus.SetSynced("c", true)

		if got := bus.Publish("a", []byte("secret\r")); got != 0 {
			t.Fatalf("queued for %d blocks, want 0", got)
		}
		time.Sleep(20 * time.Millisecond)
		for id, r := range recs {
			if r.String() != "" {
				t.Fatalf("block %s received %q", id, r)
			}
		}
	})

	t.Run("toggling sync takes effect immediately", func(t *testing.T) {
		t.Parallel()
		bus, recs := newBus(t, "a", "b")
		bus.SetSynced("a", true)
		bus.SetSynced("b", true)
		bus.Publish("a", []byte("1"))
		waitFor(t, "first chunk", func() bool { return recs["b"].String() == "1" })

		bus.SetSynced("b", false)
		bus.Publish("a", []byte("2"))
		time.Sleep(20 * time.Millisecond)
		if recs["b"].String() != "1" {
			t.Fatalf("b = %q after being unsynced", recs["b"])
		}
	})

	t.Run("terminal replies are not broadcast", func(t *testing.T) {
		t.Parallel()
		bus, recs := newBus(t, "a", "b")
		bus.SetSynced("a", true)
		bus.SetSynced("b", true)

		if got := bus.Publish("a", []byte("\x1b[24;80R")); got != 0 {
			t.Fatalf("a cursor position report was broadcast to %d blocks", got)
		}
		bus.Publish("a", []byte("x"))
		waitFor(t, "typed text", func() bool { return recs["b"].String() == "x" })
	})

	t.Run("unregistered source is ignored", func(t *testing.T) {
		t.Parallel()
		bus, _ := newBus(t, "a")
		if got := bus.Publish("ghost", []byte("x")); got != 0 {
			t.Fatalf("queued for %d blocks, want 0", got)
		}
	})
}

func TestBus_SlowDestinationDoesNotBlockOthers(t *testing.T) {
	t.Parallel()
	const queue, chunks = 8, 40
	bus := New(t.Context(), WithQueueSize(queue))
	t.Cleanup(bus.Close)

	block := make(chan struct{})
	var release sync.Once
	// Registered after bus.Close, so it runs first and unblocks the writer
	// even when the test fails early.
	t.Cleanup(func() { release.Do(func() { close(block) }) })
	slow := writerFunc(func(p []byte) (int, error) {
		<-block
		return len(p), nil
	})
	fast := &recorder{}
	bus.Register("src", &recorder{})
	bus.Register("slow", slow)
	bus.Register("fast", fast)
	for _, id := range []ID{"src", "slow", "fast"} {
		bus.SetSynced(id, true)
	}

	for i := 1; i <= chunks; i++ {
		bus.Publish("src", []byte("k"))
		waitFor(t, "fast block to keep up", func() bool {
			return strings.Count(fast.String(), "k") == i
		})
	}
	if got := bus.Stats("slow").Dropped; got == 0 {
		t.Fatal("the stalled block should have dropped chunks once its queue was full")
	}
	if got := bus.Stats("fast").Dropped; got != 0 {
		t.Fatalf("the healthy block dropped %d chunks", got)
	}
}

func TestBus_Unregister(t *testing.T) {
	t.Parallel()
	bus, recs := newBus(t, "a", "b")
	bus.SetSynced("a", true)
	bus.SetSynced("b", true)
	bus.Unregister("b")
	if got := bus.Publish("a", []byte("x")); got != 0 {
		t.Fatalf("queued for %d blocks after unregister", got)
	}
	time.Sleep(10 * time.Millisecond)
	if recs["b"].String() != "" {
		t.Fatalf("unregistered block received %q", recs["b"])
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
