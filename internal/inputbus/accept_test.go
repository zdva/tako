package inputbus

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// typed is a keyboard session: typed text, a paste, and the answers the
// terminal writes on its own, which must stay on the machine that asked.
var typed = [][]byte{
	[]byte("ls -la\r"),
	[]byte("\x1b[12;40R"),           // cursor position report
	[]byte("\x1b[?62;22c"),          // device attributes
	[]byte("\x1b]11;rgb:1/2/3\x07"), // color query answer
	[]byte("\x1b[A"),                // an arrow key is typed input
	[]byte("日本語 ✓\r"),
	[]byte("\x1b[<0;10;5M"), // mouse report
	[]byte("echo hi\r"),
}

const typedWant = "ls -la\r\x1b[A日本語 ✓\recho hi\r"

// TestNineSyncedBlocksGetIdenticalBytes is TECH-PLAN M2 acceptance 1, first
// half: of nine synced blocks, the eight others receive exactly what was
// typed in one, byte for byte, with the terminal's own replies left out.
func TestNineSyncedBlocksGetIdenticalBytes(t *testing.T) {
	t.Parallel()
	names := make([]ID, 9)
	for i := range names {
		names[i] = ID(fmt.Sprintf("host-%d", i+1))
	}
	bus, recs := newBus(t, names...)
	for _, id := range names {
		bus.SetSynced(id, true)
	}
	for _, p := range typed {
		if got := bus.Publish(names[0], p); got != 0 && got != 8 {
			t.Fatalf("queued for %d blocks, want 8 (or 0 for a reply)", got)
		}
	}
	for _, id := range names[1:] {
		waitFor(t, string(id), func() bool { return recs[id].String() == typedWant })
	}
	if got := recs[names[0]].String(); got != "" {
		t.Errorf("the source gets nothing from the bus (its own session got it directly), got %q", got)
	}
	for _, id := range names[1:] {
		if st := bus.Stats(id); st.Dropped != 0 {
			t.Errorf("%s dropped %d chunks", id, st.Dropped)
		}
	}
}

// TestThrottledTargetDoesNotDelayTheOthers is the second half of acceptance 1:
// a target that accepts 10 KB/s lags alone; the other eight get their bytes at
// once.
func TestThrottledTargetDoesNotDelayTheOthers(t *testing.T) {
	t.Parallel()
	const rate = 10 << 10 // bytes per second
	bus := New(t.Context(), WithQueueSize(64))
	t.Cleanup(bus.Close)

	var slowGot recorder
	slow := writerFunc(func(p []byte) (int, error) {
		time.Sleep(time.Duration(len(p)) * time.Second / rate)
		return slowGot.Write(p)
	})
	src := ID("src")
	bus.Register(src, &recorder{})
	bus.SetSynced(src, true)
	bus.Register("slow", slow)
	bus.SetSynced("slow", true)
	fast := map[ID]*recorder{}
	for i := range 7 {
		id := ID(fmt.Sprintf("fast-%d", i))
		fast[id] = &recorder{}
		bus.Register(id, fast[id])
		bus.SetSynced(id, true)
	}

	chunk := []byte(strings.Repeat("x", 1024)) // 100 ms of the slow target's time
	const chunks = 20                          // 2 s of slow-target time
	start := time.Now()
	for range chunks {
		bus.Publish(src, chunk)
	}
	for id, r := range fast {
		waitFor(t, string(id), func() bool { return len(r.String()) == chunks*len(chunk) })
	}
	if took := time.Since(start); took > 500*time.Millisecond {
		t.Fatalf("the healthy blocks needed %v; the 10 KB/s target must not slow them", took)
	}
	if len(slowGot.String()) >= chunks*len(chunk) {
		t.Fatal("the throttle did not hold the slow target back; the test proves nothing")
	}
}

func FuzzStripReplies(f *testing.F) {
	for _, p := range typed {
		f.Add(p)
	}
	f.Add([]byte("\x1b"))
	f.Add([]byte("\x1b[1;2R"))
	f.Add([]byte("\x1b[M"))
	f.Add([]byte("\x1bP1$r\x1b\\"))
	f.Fuzz(func(t *testing.T, p []byte) {
		got := StripReplies(slices.Clone(p))
		if len(got) > len(p) {
			t.Fatalf("grew: %q -> %q", p, got)
		}
		// Only whole sequences are removed, so plain text always survives.
		for _, b := range p {
			if b < 0x20 && b != esc && b != bel {
				if !slices.Contains(got, b) && !slices.Contains(p, esc) {
					t.Fatalf("control byte %#x lost from %q", b, p)
				}
			}
		}
		if !slices.Contains(p, esc) && !slices.Equal(got, p) {
			t.Fatalf("a chunk without escapes changed: %q -> %q", p, got)
		}
	})
}
