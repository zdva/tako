//go:build e2e && linux

package e2e

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// With MYGO_FRAME_STATS=all the engine logs one line per frame ("mygo: frame
// 12 took 3.4 ms: ..."). Tako's logger takes the standard log's output, so
// the lines land in tako.log. frameCount reads them, so a test can tell the
// frame rate and the mean frame time.

// frameCount returns how many frames the log has and how long they took.
func (f *fixture) frameCount() (int, time.Duration) {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dirs.Logs(), "tako.log"))
	if err != nil {
		f.t.Fatal(err)
	}
	n := 0
	var total time.Duration
	for line := range strings.SplitSeq(string(data), "\n") {
		_, rest, ok := strings.Cut(line, "mygo: frame ")
		if !ok {
			continue
		}
		n++
		if _, after, ok := strings.Cut(rest, " took "); ok {
			if ms, _, ok := strings.Cut(after, " ms"); ok {
				if v, err := strconv.ParseFloat(ms, 64); err == nil {
					total += time.Duration(v * float64(time.Millisecond))
				}
			}
		}
	}
	return n, total
}

// measureFrames returns the frame rate and the mean frame time over window.
func (f *fixture) measureFrames(window time.Duration) (fps float64, mean time.Duration) {
	n0, t0 := f.frameCount()
	time.Sleep(window)
	n1, t1 := f.frameCount()
	if n1 == n0 {
		return 0, 0
	}
	return float64(n1-n0) / window.Seconds(), (t1 - t0) / time.Duration(n1-n0)
}
