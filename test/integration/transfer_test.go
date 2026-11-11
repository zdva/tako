//go:build integration && linux

package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/testutil/nettest"
	"github.com/wei-dong/tako/internal/transfer"
)

func makeFile(t testing.TB, path string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	r := rand.NewChaCha8([32]byte{1, 2, 3})
	w := bufio.NewWriterSize(f, 1<<20)
	buf := make([]byte, 1<<20)
	for left := size; left > 0; {
		n := min(left, int64(len(buf)))
		_, _ = r.Read(buf[:n])
		if _, err := w.Write(buf[:n]); err != nil {
			t.Fatal(err)
		}
		left -= n
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func sum(t testing.TB, path string) string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func newQueue(t *testing.T, f *fleet, o transfer.Options) *transfer.Queue {
	t.Helper()
	o.Provider = f
	if o.Tick == 0 {
		o.Tick = 5 * time.Millisecond
	}
	q := transfer.New(o)
	t.Cleanup(q.Close)
	return q
}

func waitJob(t *testing.T, q *transfer.Queue, id int, timeout time.Duration, states ...transfer.State) transfer.JobInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		info, _ := q.Job(id)
		for _, s := range states {
			if info.State == s {
				return info
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %d is %v (%s), waiting for %v", id, info.State, info.Err, states)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// mb reads an optional size override, so the 2.4 GB acceptance run can be
// requested without making every run slow.
func mb(def int64) int64 {
	if v, err := strconv.ParseInt(os.Getenv("TAKO_TRANSFER_MB"), 10, 64); err == nil && v > 0 {
		return v
	}
	return def
}

func TestTransferServerToServerThroughRelay(t *testing.T) {
	_, f, a, b := twoHosts(t)
	dir := t.TempDir()
	srcDir, dstDir := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	makeFile(t, filepath.Join(srcDir, "site", "index.html"), 5000)
	makeFile(t, filepath.Join(srcDir, "site", "assets", "app.js"), 3<<20)
	makeFile(t, filepath.Join(srcDir, "site", "assets", "empty"), 0)
	if err := os.MkdirAll(filepath.Join(srcDir, "site", "void"), 0o755); err != nil {
		t.Fatal(err)
	}
	q := newQueue(t, f, transfer.Options{})
	id, err := q.Add(transfer.Spec{
		Src: transfer.Location{Host: a.ID, Path: filepath.Join(srcDir, "site")},
		Dst: transfer.Location{Host: b.ID, Path: dstDir}, Verify: transfer.VerifyHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	info := waitJob(t, q, id, 30*time.Second, transfer.StateDone, transfer.StateFailed)
	if info.State != transfer.StateDone {
		t.Fatalf("%v: %s", info.State, info.Err)
	}
	for _, rel := range []string{"index.html", "assets/app.js", "assets/empty"} {
		if sum(t, filepath.Join(srcDir, "site", rel)) != sum(t, filepath.Join(dstDir, "site", rel)) {
			t.Errorf("%s differs", rel)
		}
	}
	if fi, err := os.Stat(filepath.Join(dstDir, "site", "void")); err != nil || !fi.IsDir() {
		t.Error("the empty directory is missing")
	}
}

// behindLink moves host id behind a relay of the given capacity (bytes per
// second, 0 for none) and returns the relay.
func behindLink(t *testing.T, e *env, id model.ID, rate float64) *nettest.Relay {
	t.Helper()
	e.mu.Lock()
	h := e.hosts[id]
	e.mu.Unlock()
	r := nettest.StartRelay(t, net.JoinHostPort(h.Addr, strconv.Itoa(h.Port)))
	r.SetRate(rate)
	h.Addr, h.Port = "127.0.0.1", r.PortInt(t)
	e.mu.Lock()
	e.hosts[id] = h
	e.mu.Unlock()
	return r
}

// The relay's speed is judged against what the links can do: raw SFTP reading
// the source and writing the destination at the same time over the same
// connections.
func TestTransferRelayThroughputAndMemory(t *testing.T) {
	linkMB := 100.0 // each server's link, MB/s
	if v, err := strconv.ParseFloat(os.Getenv("TAKO_LINK_MBPS"), 64); err == nil {
		linkMB = v
	}
	for _, tc := range []struct {
		name   string
		rate   float64
		strict bool
	}{
		{"links of 100 MB/s", linkMB * 1e6, true},
		// With no limit the two sshd processes and this process share the
		// machine's CPUs, so the figure is informational.
		{"unthrottled loopback", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			relayThroughput(t, tc.rate, tc.strict)
		})
	}
}

func relayThroughput(t *testing.T, rate float64, strict bool) {
	e, f, a, b := twoHosts(t)
	behindLink(t, e, a.ID, rate)
	behindLink(t, e, b.ID, rate)
	size := mb(256) << 20
	dir := t.TempDir()
	src := filepath.Join(dir, "a", "big.bin")
	makeFile(t, src, size)

	ctx := context.Background()
	epA, err := f.Endpoint(ctx, transfer.Location{Host: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	epB, err := f.Endpoint(ctx, transfer.Location{Host: b.ID})
	if err != nil {
		t.Fatal(err)
	}
	// Baseline: read the source and write the destination at the same time,
	// as the relay does.
	rf, err := epA.FS().Open(src)
	if err != nil {
		t.Fatal(err)
	}
	wf, err := epB.FS().OpenFile(filepath.Join(dir, "baseline.bin"), os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		t.Fatal(err)
	}
	var readTook, writeTook time.Duration
	var rn, wn int64
	var rerr, werr error
	var wg sync.WaitGroup
	start := time.Now()
	wg.Go(func() {
		rn, rerr = rf.(io.WriterTo).WriteTo(io.Discard)
		readTook = time.Since(start)
	})
	wg.Go(func() {
		wn, werr = wf.(io.ReaderFrom).ReadFrom(io.LimitReader(zeroReader{}, size))
		writeTook = time.Since(start)
	})
	wg.Wait()
	_ = rf.Close()
	_ = wf.Close()
	if rerr != nil || werr != nil || rn != size || wn != size {
		t.Fatalf("baseline: read %d %v, write %d %v", rn, rerr, wn, werr)
	}
	readRate, writeRate := float64(size)/readTook.Seconds(), float64(size)/writeTook.Seconds()
	_ = os.Remove(filepath.Join(dir, "baseline.bin"))
	limit := min(readRate, writeRate)

	// The relay run, with the process's own disk writes and heap watched.
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	diskBefore := procWriteBytes(t)
	var peak atomic.Uint64
	stop := make(chan struct{})
	watched := make(chan struct{})
	go func() {
		defer close(watched)
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			if m.HeapInuse > peak.Load() {
				peak.Store(m.HeapInuse)
			}
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()

	q := newQueue(t, f, transfer.Options{})
	id, _ := q.Add(transfer.Spec{
		Src: transfer.Location{Host: a.ID, Path: src},
		Dst: transfer.Location{Host: b.ID, Path: filepath.Join(dir, "b")},
	})
	began := time.Now()
	info := waitJob(t, q, id, 30*time.Minute, transfer.StateDone, transfer.StateFailed)
	took := time.Since(began)
	close(stop)
	<-watched
	if info.State != transfer.StateDone {
		t.Fatalf("%v: %s", info.State, info.Err)
	}
	got := float64(size) / took.Seconds()
	growth := int64(peak.Load()) - int64(base.HeapInuse)
	disk := procWriteBytes(t) - diskBefore
	t.Logf("%d MiB: relay %.0f MB/s, raw SFTP on the same links %.0f MB/s (read %.0f, write %.0f) = %.0f%%; heap growth %.1f MiB; this process wrote %d KiB to disk",
		size>>20, got/1e6, limit/1e6, readRate/1e6, writeRate/1e6, got/limit*100, float64(growth)/(1<<20), disk>>10)
	if strict && got < 0.8*limit {
		t.Errorf("relay %.0f MB/s is under 80%% of the link limit %.0f MB/s", got/1e6, limit/1e6)
	}
	if growth > 32<<20 {
		t.Errorf("heap grew %d MiB, limit 32", growth>>20)
	}
	if disk > 4<<20 {
		t.Errorf("the relay wrote %d KiB to this computer's disk", disk>>10)
	}
	if sum(t, src) != sum(t, filepath.Join(dir, "b", "big.bin")) {
		t.Error("content differs")
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

// procWriteBytes is how many bytes this process has caused to be written to
// storage.
func procWriteBytes(t testing.TB) int64 {
	t.Helper()
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Skipf("no /proc/self/io: %v", err)
	}
	for line := range strings.Lines(string(data)) {
		if v, ok := strings.CutPrefix(line, "write_bytes:"); ok {
			n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			return n
		}
	}
	return 0
}

// The network drops half way: the job pauses, continues from the bytes in
// place, and every file matches its source by SHA-256.
func TestTransferResumesAfterNetworkLoss(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]int64
		root  string
	}{
		{"file", map[string]int64{"big.bin": 400 << 20}, "big.bin"},
		{"directory", map[string]int64{"tree/a.bin": 60 << 20, "tree/sub/b.bin": 60 << 20, "tree/sub/c.bin": 60 << 20, "tree/d.bin": 60 << 20, "tree/e.bin": 60 << 20, "tree/z.txt": 100}, "tree"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, f, a, b := twoHosts(t)
			dir := t.TempDir()
			for rel, size := range tc.files {
				makeFile(t, filepath.Join(dir, "a", rel), size)
			}
			// Hosts are reached through relays so the link can be cut.
			relayB := nettest.StartRelay(t, fmt.Sprintf("%s:%d", b.Addr, b.Port))
			e.mu.Lock()
			bb := e.hosts[b.ID]
			bb.Addr, bb.Port = "127.0.0.1", relayB.PortInt(t)
			e.hosts[b.ID] = bb
			e.mu.Unlock()

			q := newQueue(t, f, transfer.Options{Retries: -1})
			var cut atomic.Bool
			q.Subscribe(func(j transfer.JobInfo) {
				if j.State == transfer.StateRunning && j.Done > j.Total*4/10 && cut.CompareAndSwap(false, true) {
					relayB.Cut()
				}
			})
			id, _ := q.Add(transfer.Spec{
				Src: transfer.Location{Host: a.ID, Path: filepath.Join(dir, "a", tc.root)},
				Dst: transfer.Location{Host: b.ID, Path: filepath.Join(dir, "b")},
			})
			info := waitJob(t, q, id, time.Minute, transfer.StatePaused, transfer.StateDone, transfer.StateFailed)
			if info.State != transfer.StatePaused || info.Reason != transfer.ReasonNetwork {
				t.Fatalf("%v (%v): %s", info.State, info.Reason, info.Err)
			}
			if info.Resumable <= 0 {
				t.Errorf("nothing to resume from: %+v", info)
			}
			t.Logf("paused at %d of %d MiB, resumable from %d MiB (%s)", info.Done>>20, info.Total>>20, info.Resumable>>20, info.Err)

			if err := q.Resume(id); err != nil {
				t.Fatal(err)
			}
			info = waitJob(t, q, id, 2*time.Minute, transfer.StateDone, transfer.StateFailed)
			if info.State != transfer.StateDone {
				t.Fatalf("%v: %s", info.State, info.Err)
			}
			for rel := range tc.files {
				if sum(t, filepath.Join(dir, "a", rel)) != sum(t, filepath.Join(dir, "b", rel)) {
					t.Errorf("%s differs after resuming", rel)
				}
			}
			var left []string
			_ = filepath.WalkDir(filepath.Join(dir, "b"), func(p string, _ os.DirEntry, _ error) error {
				if strings.HasSuffix(p, transfer.PartSuffix) {
					left = append(left, p)
				}
				return nil
			})
			if len(left) > 0 {
				t.Errorf("part files left: %v", left)
			}
		})
	}
}

func TestTransferSamePolicyOnRealSFTP(t *testing.T) {
	_, f, a, b := twoHosts(t)
	for _, tc := range []struct {
		name   string
		policy transfer.Policy
		answer *transfer.Decision
		kept   bool
	}{
		{"overwrite", transfer.PolicyOverwrite, nil, false},
		{"skip", transfer.PolicySkip, nil, true},
		{"ask-overwrite", transfer.PolicyAsk, &transfer.Decision{Overwrite: true}, false},
		{"ask-skip", transfer.PolicyAsk, &transfer.Decision{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "a", "f.txt")
			makeFile(t, src, 4096)
			makeFile(t, filepath.Join(dir, "b", "f.txt"), 10)
			existing := sum(t, filepath.Join(dir, "b", "f.txt"))
			q := newQueue(t, f, transfer.Options{})
			id, _ := q.Add(transfer.Spec{
				Src: transfer.Location{Host: a.ID, Path: src}, Dst: transfer.Location{Host: b.ID, Path: filepath.Join(dir, "b")},
				Policy: tc.policy,
			})
			if tc.answer != nil {
				waitJob(t, q, id, 20*time.Second, transfer.StateAsking)
				if err := q.Resolve(id, *tc.answer); err != nil {
					t.Fatal(err)
				}
			}
			if info := waitJob(t, q, id, 20*time.Second, transfer.StateDone, transfer.StateFailed); info.State != transfer.StateDone {
				t.Fatalf("%s", info.Err)
			}
			got := sum(t, filepath.Join(dir, "b", "f.txt"))
			if tc.kept && got != existing || !tc.kept && got != sum(t, src) {
				t.Errorf("wrong outcome for %s", tc.name)
			}
		})
	}
}

// Direct mode is refused unless it is switched on; when on, the copy runs on
// the destination server with the agent forwarded for that command only, and
// leaves nothing behind on it.
func TestTransferDirectMode(t *testing.T) {
	e, f, a, b := twoHosts(t)
	if _, err := newQueue(t, f, transfer.Options{}).Add(transfer.Spec{Mode: transfer.ModeDirect}); err == nil {
		t.Fatal("direct mode worked while disabled")
	}
	// Both servers must be trusted before B may be told about A.
	for _, h := range []model.Host{a, b} {
		if _, err := f.Endpoint(context.Background(), transfer.Location{Host: h.ID}); err != nil {
			t.Fatal(err)
		}
	}
	home, _ := os.UserHomeDir()
	marker := filepath.Join(t.TempDir(), "marker")
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	// Outside a transfer there is no agent on the server.
	out, err := f.Exec(context.Background(), b.ID, `echo "sock=${SSH_AUTH_SOCK:-none}"`)
	if err != nil || !bytes.Contains(out, []byte("sock=none")) {
		t.Fatalf("plain exec: %q %v", out, err)
	}
	// During an agent command there is.
	out, err = f.ExecAgent(context.Background(), b.ID, `echo "sock=${SSH_AUTH_SOCK:-none}"; ssh-add -l`)
	if err != nil || bytes.Contains(out, []byte("sock=none")) || !bytes.Contains(out, []byte("tako-test")) {
		t.Fatalf("agent exec: %q %v", out, err)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "a", "payload.bin")
	makeFile(t, src, 20<<20)
	makeFile(t, filepath.Join(dir, "a", "tree", "x", "y.txt"), 777)
	makeFile(t, filepath.Join(dir, "a", "tree", "z.bin"), 3<<20)

	q := newQueue(t, f, transfer.Options{Direct: &transfer.Direct{Env: f, PollEvery: 20 * time.Millisecond}})
	servedBefore := e.agent.Served.Load()
	for _, tc := range []struct{ name, path string }{{"file", src}, {"directory", filepath.Join(dir, "a", "tree")}} {
		id, err := q.Add(transfer.Spec{
			Mode: transfer.ModeDirect, Verify: transfer.VerifyHash,
			Src: transfer.Location{Host: a.ID, Path: tc.path}, Dst: transfer.Location{Host: b.ID, Path: filepath.Join(dir, "b", tc.name)},
		})
		if err != nil {
			t.Fatal(err)
		}
		info := waitJob(t, q, id, time.Minute, transfer.StateDone, transfer.StateFailed)
		if info.State != transfer.StateDone {
			t.Fatalf("%s: %v: %s", tc.name, info.State, info.Err)
		}
	}
	if sum(t, src) != sum(t, filepath.Join(dir, "b", "file", "payload.bin")) {
		t.Error("file differs")
	}
	if sum(t, filepath.Join(dir, "a", "tree", "z.bin")) != sum(t, filepath.Join(dir, "b", "directory", "tree", "z.bin")) {
		t.Error("directory content differs")
	}
	if e.agent.Served.Load() == servedBefore {
		t.Error("the agent was never used: the copy did not authenticate through it")
	}

	// Afterwards: no helper file, no key, no agent on the server.
	left, _ := f.Exec(context.Background(), b.ID, fmt.Sprintf(
		`ls -A %s | grep -c 'tako-kh' ; find %s -maxdepth 3 -type f -newer %s -not -path '*/.cache/*' 2>/dev/null | head -50 | xargs -r grep -l 'PRIVATE KEY' 2>/dev/null | wc -l; echo "sock=${SSH_AUTH_SOCK:-none}"`,
		home, home, marker))
	lines := strings.Fields(string(left))
	if len(lines) < 3 || lines[0] != "0" || lines[1] != "0" || lines[2] != "sock=none" {
		t.Errorf("traces left on the server: %q", left)
	}
	// The server can no longer reach the agent: a new agent channel is refused.
	out, err = f.Exec(context.Background(), b.ID, `ssh-add -l 2>&1; true`)
	if err != nil || bytes.Contains(out, []byte("tako-test")) {
		t.Errorf("agent still reachable: %q %v", out, err)
	}
}
