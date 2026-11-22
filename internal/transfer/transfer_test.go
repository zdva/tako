package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/transport"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// flaky is a "remote" endpoint over the local disk: it is not the local
// endpoint, so the copy takes the remote paths (relay, rewind), and it can
// lose its connection or stall on demand.
type flaky struct {
	localFS
	failAfter atomic.Int64 // bytes that may still be written before the link drops; <0 = never
	dead      atomic.Bool
	gate      atomic.Pointer[chan struct{}]
	written   atomic.Int64
}

func newFlaky() *flaky {
	f := &flaky{}
	f.failAfter.Store(-1)
	return f
}

func (f *flaky) FS() transport.FS { return f }
func (f *flaky) Hash(ctx context.Context, p string) (string, error) {
	return Local().Hash(ctx, p)
}

func (f *flaky) check() error {
	if f.dead.Load() {
		return fmt.Errorf("writing: %w", io.ErrUnexpectedEOF)
	}
	return nil
}

func (f *flaky) Open(name string) (transport.File, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	file, err := f.localFS.Open(name)
	if err != nil {
		return nil, err
	}
	return &flakyFile{File: file, f: f}, nil
}

func (f *flaky) OpenFile(name string, flag int) (transport.File, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	file, err := f.localFS.OpenFile(name, flag)
	if err != nil {
		return nil, err
	}
	return &flakyFile{File: file, f: f}, nil
}

func (f *flaky) Stat(name string) (os.FileInfo, error) {
	if err := f.check(); err != nil {
		return nil, err
	}
	return f.localFS.Stat(name)
}

type flakyFile struct {
	transport.File
	f *flaky
}

func (ff *flakyFile) Write(p []byte) (int, error) {
	if g := ff.f.gate.Load(); g != nil {
		<-*g
	}
	if err := ff.f.check(); err != nil {
		return 0, err
	}
	if left := ff.f.failAfter.Load(); left >= 0 {
		if int64(len(p)) > left {
			p = p[:left]
			n, _ := ff.File.Write(p)
			ff.f.written.Add(int64(n))
			ff.f.dead.Store(true)
			return n, fmt.Errorf("writing: %w", io.ErrUnexpectedEOF)
		}
		ff.f.failAfter.Add(-int64(len(p)))
	}
	n, err := ff.File.Write(p)
	ff.f.written.Add(int64(n))
	return n, err //nolint:wrapcheck // test double
}

func (ff *flakyFile) ReadFrom(r io.Reader) (int64, error) {
	return io.Copy(struct{ io.Writer }{ff}, r) //nolint:wrapcheck // test double
}

func (ff *flakyFile) WriteTo(w io.Writer) (int64, error) {
	return io.Copy(w, struct{ io.Reader }{ff.File}) //nolint:wrapcheck // test double
}

type provider struct {
	mu     sync.Mutex
	hosts  map[model.ID]Endpoint
	resets atomic.Int32
}

func (p *provider) Endpoint(_ context.Context, l Location) (Endpoint, error) {
	if l.Local() {
		return Local(), nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ep, ok := p.hosts[l.Host]
	if !ok {
		return nil, errors.New("unknown host")
	}
	return ep, nil
}

func (p *provider) Reset(Location) { p.resets.Add(1) }

func newQueue(t *testing.T, hosts map[model.ID]Endpoint, o Options) (*Queue, *provider) {
	t.Helper()
	p := &provider{hosts: hosts}
	o.Provider = p
	if o.RetryDelay == 0 {
		o.RetryDelay = 10 * time.Millisecond
	}
	o.Tick = 5 * time.Millisecond
	q := New(o)
	t.Cleanup(q.Close)
	return q, p
}

func wait(t *testing.T, q *Queue, id int, states ...State) JobInfo {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		info, ok := q.Job(id)
		if !ok {
			t.Fatalf("job %d vanished", id)
		}
		if slices.Contains(states, info.State) {
			return info
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %d is %v (%s), waiting for %v", id, info.State, info.Err, states)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func randomFile(t *testing.T, path string, size int) []byte {
	t.Helper()
	b := make([]byte, size)
	_, _ = rand.Read(b)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return b
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const hostB model.ID = "host-b"

func TestUploadFile(t *testing.T) {
	dir := t.TempDir()
	want := randomFile(t, filepath.Join(dir, "src", "a.bin"), 3<<20+17)
	q, _ := newQueue(t, map[model.ID]Endpoint{hostB: newFlaky()}, Options{})
	id, err := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "src", "a.bin")}, Dst: Location{Host: hostB, Path: filepath.Join(dir, "dst")}})
	if err != nil {
		t.Fatal(err)
	}
	info := wait(t, q, id, StateDone, StateFailed)
	if info.State != StateDone {
		t.Fatalf("%v: %s", info.State, info.Err)
	}
	if got := mustRead(t, filepath.Join(dir, "dst", "a.bin")); !bytes.Equal(got, want) {
		t.Fatal("content differs")
	}
	if _, err := os.Stat(filepath.Join(dir, "dst", "a.bin"+PartSuffix)); err == nil {
		t.Error("the part file is still there")
	}
	if info.Done != info.Total || info.FilesDone != 1 || info.FilesTotal != 1 {
		t.Errorf("progress %d/%d files %d/%d", info.Done, info.Total, info.FilesDone, info.FilesTotal)
	}
}

func TestDirectoryTree(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "proj")
	files := map[string][]byte{
		"a.txt":         randomFile(t, filepath.Join(root, "a.txt"), 10),
		"sub/b.bin":     randomFile(t, filepath.Join(root, "sub", "b.bin"), 1<<20),
		"sub/deep/c":    randomFile(t, filepath.Join(root, "sub", "deep", "c"), 1),
		"sub/empty.txt": randomFile(t, filepath.Join(root, "sub", "empty.txt"), 0),
	}
	if err := os.MkdirAll(filepath.Join(root, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	q, _ := newQueue(t, map[model.ID]Endpoint{hostB: newFlaky()}, Options{})
	id, _ := q.Add(Spec{Src: Location{Path: root}, Dst: Location{Host: hostB, Path: filepath.Join(dir, "out")}})
	info := wait(t, q, id, StateDone, StateFailed)
	if info.State != StateDone {
		t.Fatalf("%v: %s", info.State, info.Err)
	}
	for rel, want := range files {
		if got := mustRead(t, filepath.Join(dir, "out", "proj", filepath.FromSlash(rel))); !bytes.Equal(got, want) {
			t.Errorf("%s differs", rel)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, "out", "proj", "emptydir")); err != nil || !fi.IsDir() {
		t.Error("the empty directory was not created")
	}
	if _, err := os.Lstat(filepath.Join(dir, "out", "proj", "link")); err == nil {
		t.Error("a link was copied")
	}
	if info.FilesTotal != 4 || info.Skipped != 1 {
		t.Errorf("files %d, skipped %d", info.FilesTotal, info.Skipped)
	}
}

func TestDownloadAndLocalCopy(t *testing.T) {
	dir := t.TempDir()
	want := randomFile(t, filepath.Join(dir, "r", "x.bin"), 700_000)
	q, _ := newQueue(t, map[model.ID]Endpoint{hostB: newFlaky()}, Options{})
	id, _ := q.Add(Spec{Src: Location{Host: hostB, Path: filepath.Join(dir, "r", "x.bin")}, Dst: Location{Path: filepath.Join(dir, "d")}, Verify: VerifyHash})
	if info := wait(t, q, id, StateDone, StateFailed); info.State != StateDone {
		t.Fatalf("download: %s", info.Err)
	}
	id, _ = q.Add(Spec{Src: Location{Path: filepath.Join(dir, "d", "x.bin")}, Dst: Location{Path: filepath.Join(dir, "e")}})
	if info := wait(t, q, id, StateDone, StateFailed); info.State != StateDone {
		t.Fatalf("local copy: %s", info.Err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(dir, "e", "x.bin")), want) {
		t.Fatal("content differs")
	}
}

func TestPolicies(t *testing.T) {
	setup := func(t *testing.T) (string, string, Spec) {
		t.Helper()
		dir := t.TempDir()
		src := filepath.Join(dir, "src", "f.txt")
		randomFile(t, src, 1000)
		dst := filepath.Join(dir, "dst")
		randomFile(t, filepath.Join(dst, "f.txt"), 50)
		return src, dst, Spec{Src: Location{Path: src}, Dst: Location{Host: hostB, Path: dst}}
	}
	t.Run("overwrite", func(t *testing.T) {
		src, dst, spec := setup(t)
		spec.Policy = PolicyOverwrite
		q, _ := newQueue(t, map[model.ID]Endpoint{hostB: newFlaky()}, Options{})
		id, _ := q.Add(spec)
		wait(t, q, id, StateDone)
		if !bytes.Equal(mustRead(t, filepath.Join(dst, "f.txt")), mustRead(t, src)) {
			t.Error("not overwritten")
		}
	})
	t.Run("skip", func(t *testing.T) {
		_, dst, spec := setup(t)
		before := mustRead(t, filepath.Join(dst, "f.txt"))
		spec.Policy = PolicySkip
		q, _ := newQueue(t, map[model.ID]Endpoint{hostB: newFlaky()}, Options{})
		id, _ := q.Add(spec)
		info := wait(t, q, id, StateDone)
		if !bytes.Equal(mustRead(t, filepath.Join(dst, "f.txt")), before) {
			t.Error("overwritten despite skip")
		}
		if info.Skipped != 1 || info.Done != info.Total {
			t.Errorf("skipped %d, %d/%d", info.Skipped, info.Done, info.Total)
		}
	})
	t.Run("ask overwrite", func(t *testing.T) {
		src, dst, spec := setup(t)
		q, _ := newQueue(t, map[model.ID]Endpoint{hostB: newFlaky()}, Options{})
		id, _ := q.Add(spec)
		info := wait(t, q, id, StateAsking)
		if info.Conflict == nil || info.Conflict.SrcSize != 1000 || info.Conflict.DstSize != 50 {
			t.Fatalf("conflict %+v", info.Conflict)
		}
		if err := q.Resolve(id, Decision{Overwrite: true}); err != nil {
			t.Fatal(err)
		}
		wait(t, q, id, StateDone)
		if !bytes.Equal(mustRead(t, filepath.Join(dst, "f.txt")), mustRead(t, src)) {
			t.Error("not overwritten")
		}
	})
	t.Run("ask skip for all", func(t *testing.T) {
		dir := t.TempDir()
		for _, n := range []string{"a", "b", "c"} {
			randomFile(t, filepath.Join(dir, "src", "tree", n), 100)
			randomFile(t, filepath.Join(dir, "dst", "tree", n), 5)
		}
		q, _ := newQueue(t, map[model.ID]Endpoint{hostB: newFlaky()}, Options{})
		id, _ := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "src", "tree")}, Dst: Location{Host: hostB, Path: filepath.Join(dir, "dst")}})
		wait(t, q, id, StateAsking)
		_ = q.Resolve(id, Decision{All: true})
		info := wait(t, q, id, StateDone)
		if info.Skipped != 3 {
			t.Errorf("skipped %d, want 3 from one answer", info.Skipped)
		}
	})
	t.Run("resolve when not asking", func(t *testing.T) {
		q, _ := newQueue(t, nil, Options{})
		if err := q.Resolve(99, Decision{}); !errors.Is(err, ErrNoSuchJob) {
			t.Errorf("got %v", err)
		}
	})
	t.Run("a directory in the way", func(t *testing.T) {
		dir := t.TempDir()
		randomFile(t, filepath.Join(dir, "s", "f"), 10)
		if err := os.MkdirAll(filepath.Join(dir, "d", "f"), 0o755); err != nil {
			t.Fatal(err)
		}
		q, _ := newQueue(t, nil, Options{})
		id, _ := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "s", "f")}, Dst: Location{Path: filepath.Join(dir, "d")}, Policy: PolicyOverwrite})
		if info := wait(t, q, id, StateFailed); info.Err == "" {
			t.Error("no reason given")
		}
	})
}

// A lost connection pauses the job; continuing resumes from the bytes in
// place and the checksum agrees.
func TestNetworkLossPausesAndResumes(t *testing.T) {
	dir := t.TempDir()
	want := randomFile(t, filepath.Join(dir, "s", "big.bin"), 48<<20)
	remote := newFlaky()
	remote.failAfter.Store(30 << 20)
	q, p := newQueue(t, map[model.ID]Endpoint{hostB: remote}, Options{Retries: -1})
	id, _ := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "s", "big.bin")}, Dst: Location{Host: hostB, Path: filepath.Join(dir, "d")}})
	info := wait(t, q, id, StatePaused)
	if info.Reason != ReasonNetwork {
		t.Fatalf("reason %v (%s)", info.Reason, info.Err)
	}
	if info.Resumable < 10<<20 || info.Resumable > 30<<20 {
		t.Errorf("resumable %d bytes", info.Resumable)
	}
	if p.resets.Load() == 0 {
		t.Error("the broken endpoint was not reset")
	}

	remote.dead.Store(false)
	remote.failAfter.Store(-1)
	remote.written.Store(0)
	if err := q.Resume(id); err != nil {
		t.Fatal(err)
	}
	info = wait(t, q, id, StateDone, StateFailed)
	if info.State != StateDone {
		t.Fatalf("%v: %s", info.State, info.Err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(dir, "d", "big.bin")), want) {
		t.Fatal("content differs after resume")
	}
	if w := remote.written.Load(); w >= 48<<20 {
		t.Errorf("rewrote the whole file (%d bytes) instead of continuing", w)
	}
}

// A partial file whose first bytes are wrong is caught by the hash check and
// the copy starts over instead of delivering a corrupt file.
func TestResumeDetectsCorruptPrefix(t *testing.T) {
	dir := t.TempDir()
	want := randomFile(t, filepath.Join(dir, "s", "f.bin"), 4<<20)
	bad := append([]byte(nil), want[:2<<20]...)
	bad[1000] ^= 0xff
	if err := os.MkdirAll(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "d", "f.bin"+PartSuffix), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	q, _ := newQueue(t, nil, Options{})
	id, _ := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "s", "f.bin")}, Dst: Location{Path: filepath.Join(dir, "d")}})
	if info := wait(t, q, id, StateDone, StateFailed); info.State != StateDone {
		t.Fatalf("%s", info.Err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(dir, "d", "f.bin")), want) {
		t.Fatal("a corrupt file was delivered")
	}
}

func TestAutomaticRetry(t *testing.T) {
	dir := t.TempDir()
	want := randomFile(t, filepath.Join(dir, "s", "f.bin"), 8<<20)
	remote := newFlaky()
	remote.failAfter.Store(3 << 20)
	q, _ := newQueue(t, map[model.ID]Endpoint{hostB: remote}, Options{Retries: 3})
	var sawAttempt atomic.Bool
	q.Subscribe(func(j JobInfo) {
		if j.State == StatePaused && j.Reason == ReasonNetwork && j.Attempt == 1 {
			remote.dead.Store(false) // the network comes back while it waits
			remote.failAfter.Store(-1)
			sawAttempt.Store(true)
		}
	})
	id, _ := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "s", "f.bin")}, Dst: Location{Host: hostB, Path: filepath.Join(dir, "d")}})
	if info := wait(t, q, id, StateDone, StateFailed); info.State != StateDone {
		t.Fatalf("%v: %s", info.State, info.Err)
	}
	if !sawAttempt.Load() {
		t.Error("it never paused")
	}
	if !bytes.Equal(mustRead(t, filepath.Join(dir, "d", "f.bin")), want) {
		t.Fatal("content differs")
	}
}

func TestRetriesRunOut(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "s", "f.bin"), 1<<20)
	remote := newFlaky()
	remote.dead.Store(true)
	q, _ := newQueue(t, map[model.ID]Endpoint{hostB: remote}, Options{Retries: 2})
	id, _ := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "s", "f.bin")}, Dst: Location{Host: hostB, Path: filepath.Join(dir, "d")}})
	deadline := time.Now().Add(5 * time.Second)
	for {
		info, _ := q.Job(id)
		if info.State == StatePaused && info.Attempt == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("state %v attempt %d", info.State, info.Attempt)
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if info, _ := q.Job(id); info.State != StatePaused {
		t.Errorf("a job out of retries must wait for the user, is %v", info.State)
	}
}

func TestPermissionErrorFails(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "s", "f"), 10)
	if err := os.WriteFile(filepath.Join(dir, "blocker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	q, _ := newQueue(t, nil, Options{})
	id, _ := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "s", "f")}, Dst: Location{Path: filepath.Join(dir, "blocker", "x")}})
	info := wait(t, q, id, StateFailed)
	if info.Err == "" {
		t.Error("no reason")
	}
	// A failed job can be retried; it fails the same way here.
	if err := q.Resume(id); err != nil {
		t.Fatal(err)
	}
	wait(t, q, id, StateFailed)
}

func TestPauseResumeCancel(t *testing.T) {
	dir := t.TempDir()
	want := randomFile(t, filepath.Join(dir, "s", "f.bin"), 6<<20)
	remote := newFlaky()
	gate := make(chan struct{})
	remote.gate.Store(&gate)
	q, prov := newQueue(t, map[model.ID]Endpoint{hostB: remote}, Options{})
	spec := Spec{Src: Location{Path: filepath.Join(dir, "s", "f.bin")}, Dst: Location{Host: hostB, Path: filepath.Join(dir, "d")}}
	id, _ := q.Add(spec)
	wait(t, q, id, StateRunning)
	if err := q.Pause(id); err != nil {
		t.Fatal(err)
	}
	close(gate) // let the blocked write finish so the copy can notice
	info := wait(t, q, id, StatePaused)
	if info.Reason != ReasonUser {
		t.Errorf("reason %v", info.Reason)
	}
	remote.gate.Store(nil)
	_ = q.Resume(id)
	if info := wait(t, q, id, StateDone, StateFailed); info.State != StateDone {
		t.Fatal(info.Err)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(dir, "d", "f.bin")), want) {
		t.Fatal("content differs")
	}

	// Cancel mid-copy removes the half file.
	gate2 := make(chan struct{})
	remote.gate.Store(&gate2)
	spec.Dst.Path = filepath.Join(dir, "d2")
	id, _ = q.Add(spec)
	wait(t, q, id, StateRunning)
	time.Sleep(20 * time.Millisecond)
	_ = q.Cancel(id)
	close(gate2)
	wait(t, q, id, StateCanceled)
	entries, _ := os.ReadDir(filepath.Join(dir, "d2"))
	if len(entries) != 0 {
		t.Errorf("cancel left %d files behind", len(entries))
	}

	// Pausing a queued job and cancelling it.
	remote.gate.Store(nil)
	q.SetConcurrency(1)
	slow := newFlaky()
	g3 := make(chan struct{})
	slow.gate.Store(&g3)
	prov.hosts["slow"] = slow
	a, _ := q.Add(Spec{Src: spec.Src, Dst: Location{Host: "slow", Path: filepath.Join(dir, "d3")}})
	wait(t, q, a, StateRunning)
	b, _ := q.Add(spec)
	if info, _ := q.Job(b); info.State != StateQueued {
		t.Errorf("second job is %v with concurrency 1", info.State)
	}
	_ = q.Pause(b)
	if info, _ := q.Job(b); info.State != StatePaused {
		t.Errorf("paused queued job is %v", info.State)
	}
	_ = q.Cancel(b)
	if info, _ := q.Job(b); info.State != StateCanceled {
		t.Errorf("cancelled paused job is %v", info.State)
	}
	_ = q.Cancel(a)
	close(g3)
	wait(t, q, a, StateCanceled)
	if n := q.ActiveCount(); n != 0 {
		t.Errorf("%d active", n)
	}
	q.ClearFinished()
	if got := len(q.Snapshot()); got != 0 {
		t.Errorf("%d jobs left after clearing", got)
	}
	for _, f := range []func(int) error{q.Pause, q.Resume, q.Cancel} {
		if err := f(12345); !errors.Is(err, ErrNoSuchJob) {
			t.Errorf("unknown job: %v", err)
		}
	}
}

func TestConcurrencyLimit(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "s", "f.bin"), 1<<20)
	remote := newFlaky()
	gate := make(chan struct{})
	remote.gate.Store(&gate)
	q, _ := newQueue(t, map[model.ID]Endpoint{hostB: remote}, Options{Concurrency: 2})
	var ids []int
	for i := range 5 {
		id, _ := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "s", "f.bin")}, Dst: Location{Host: hostB, Path: filepath.Join(dir, fmt.Sprintf("d%d", i))}})
		ids = append(ids, id)
	}
	time.Sleep(60 * time.Millisecond)
	running := 0
	for _, j := range q.Snapshot() {
		if j.State == StateRunning {
			running++
		}
	}
	if running != 2 {
		t.Errorf("%d running, want 2", running)
	}
	close(gate)
	for _, id := range ids {
		wait(t, q, id, StateDone)
	}
	q.Remove(ids[0])
	if len(q.Snapshot()) != 4 {
		t.Error("Remove did not drop the finished job")
	}
}

func TestSpeedAndSubscribe(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "s", "f.bin"), 4<<20)
	remote := newFlaky()
	gate := make(chan struct{})
	remote.gate.Store(&gate)
	q, _ := newQueue(t, map[model.ID]Endpoint{hostB: remote}, Options{})
	var events atomic.Int32
	var maxSpeed atomic.Int64
	unsub := q.Subscribe(func(j JobInfo) {
		events.Add(1)
		maxSpeed.Store(max(maxSpeed.Load(), int64(j.Speed)))
	})
	defer unsub()
	id, _ := q.Add(Spec{Src: Location{Path: filepath.Join(dir, "s", "f.bin")}, Dst: Location{Host: hostB, Path: filepath.Join(dir, "d")}})
	wait(t, q, id, StateRunning)
	time.Sleep(30 * time.Millisecond)
	close(gate)
	wait(t, q, id, StateDone)
	if events.Load() < 3 {
		t.Errorf("only %d events", events.Load())
	}
}

func TestDirectDisabled(t *testing.T) {
	q, _ := newQueue(t, nil, Options{})
	if _, err := q.Add(Spec{Mode: ModeDirect}); !errors.Is(err, ErrDirectDisabled) {
		t.Errorf("got %v", err)
	}
	q.Close()
	if _, err := q.Add(Spec{}); !errors.Is(err, ErrQueueClosed) {
		t.Errorf("got %v", err)
	}
}

type fakeDirect struct{ ran chan Policy }

func (f fakeDirect) Run(_ context.Context, _ Spec, h DirectHooks) error {
	h.Total(100)
	h.Done(100)
	if h.Policy() == PolicyAsk {
		if _, err := h.Ask(Conflict{Path: "x"}); err != nil {
			return err
		}
	}
	f.ran <- h.Policy()
	return nil
}

func TestDirectJobs(t *testing.T) {
	d := fakeDirect{ran: make(chan Policy, 1)}
	q, _ := newQueue(t, nil, Options{Direct: d})
	id, err := q.Add(Spec{Mode: ModeDirect, Src: Location{Host: "a", Path: "/x"}, Dst: Location{Host: "b", Path: "/y"}})
	if err != nil {
		t.Fatal(err)
	}
	wait(t, q, id, StateAsking)
	_ = q.Resolve(id, Decision{Overwrite: true, All: true})
	info := wait(t, q, id, StateDone)
	if got := <-d.ran; got != PolicyOverwrite {
		t.Errorf("policy %v", got)
	}
	if info.Total != 100 || info.FilesDone != 1 {
		t.Errorf("%+v", info)
	}
}

func TestHelpers(t *testing.T) {
	if got := ShellQuote("it's here"); got != `'it'\''s here'` {
		t.Errorf("quote %s", got)
	}
	sum := "a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a3b2"
	if got, ok := parseSum([]byte(sum + "  file\n")); !ok || got != sum {
		t.Errorf("parseSum %q %v", got, ok)
	}
	if _, ok := parseSum([]byte("sha256sum: no such file")); ok {
		t.Error("parsed an error message")
	}
	for in, want := range map[string]string{"/a/b/": "b", "c": "c", "/": ""} {
		if got := baseName(in, true); got != want {
			t.Errorf("baseName(%q) = %q", in, got)
		}
	}
	if got := baseName(`C:\Users\me\f.txt`, false); got != "f.txt" {
		t.Errorf("windows base %q", got)
	}
	for err, want := range map[error]bool{
		io.EOF: true, io.ErrUnexpectedEOF: true, errors.New("sftp: connection lost"): true,
		errors.New("permission denied"): false, syscall.ENOTDIR: false, syscall.ECONNRESET: true, nil: false, ErrVerify: false,
		fmt.Errorf("wrapped: %w", &netError{errors.New("x")}): true,
	} {
		if got := isNetwork(err); got != want {
			t.Errorf("isNetwork(%v) = %v", err, got)
		}
	}
	j := JobInfo{State: StateRunning, Total: 1000, Done: 400, Speed: 100}
	if j.ETA() != 6*time.Second {
		t.Errorf("eta %v", j.ETA())
	}
	if (JobInfo{State: StateDone}).ETA() != 0 || StateAsking.String() != "asking" || !StatePaused.Active() || StateDone.Active() {
		t.Error("state helpers")
	}
	if !(Location{}).Local() {
		t.Error("empty location is local")
	}
}

func TestRemoteEndpointHashFallsBackToReading(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "f"), 5000)
	want, _ := Local().Hash(t.Context(), filepath.Join(dir, "f"))
	viaExec := Remote(localFS{}, func(context.Context, string) ([]byte, error) {
		return []byte(want + "  " + filepath.Join(dir, "f") + "\n"), nil
	})
	if got, err := viaExec.Hash(t.Context(), filepath.Join(dir, "f")); err != nil || got != want {
		t.Errorf("exec hash %q %v", got, err)
	}
	broken := Remote(localFS{}, func(context.Context, string) ([]byte, error) { return nil, errors.New("no sha256sum") })
	if got, err := broken.Hash(t.Context(), filepath.Join(dir, "f")); err != nil || got != want {
		t.Errorf("fallback hash %q %v", got, err)
	}
	noExec := Remote(localFS{}, nil)
	if got, err := noExec.Hash(t.Context(), filepath.Join(dir, "f")); err != nil || got != want {
		t.Errorf("no-exec hash %q %v", got, err)
	}
	if _, err := noExec.Hash(t.Context(), filepath.Join(dir, "missing")); err == nil {
		t.Error("hashed a missing file")
	}
}

func TestLocalFS(t *testing.T) {
	dir := t.TempDir()
	var l localFS
	if err := l.MkdirAll(filepath.Join(dir, "a", "b")); err != nil {
		t.Fatal(err)
	}
	f, err := l.OpenFile(filepath.Join(dir, "a", "x"), os.O_WRONLY|os.O_CREATE)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.Write([]byte("hi"))
	_ = f.Close()
	if err := l.Rename(filepath.Join(dir, "a", "x"), filepath.Join(dir, "a", "y")); err != nil {
		t.Fatal(err)
	}
	list, err := l.ReadDir(filepath.Join(dir, "a"))
	if err != nil || len(list) != 2 {
		t.Fatalf("%v %v", list, err)
	}
	if err := l.Remove(filepath.Join(dir, "a", "y")); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Stat(filepath.Join(dir, "a", "y")); err == nil {
		t.Error("still there")
	}
	if _, err := l.Open(filepath.Join(dir, "none")); err == nil {
		t.Error("opened a missing file")
	}
	if h, err := l.Home(); err != nil || h == "" {
		t.Errorf("home %q %v", h, err)
	}
	if l.Join("a", "b") != filepath.Join("a", "b") || l.Close() != nil {
		t.Error("join/close")
	}
}

func TestListAndParent(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "b.txt"), 5)
	randomFile(t, filepath.Join(dir, "A.txt"), 7)
	if err := os.MkdirAll(filepath.Join(dir, "zdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("zdir", filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	got, err := List(t.Context(), Local(), dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range got {
		names = append(names, e.Name)
	}
	if want := []string{"linkdir", "zdir", "A.txt", "b.txt"}; !slices.Equal(names, want) {
		t.Errorf("order %v, want %v", names, want)
	}
	if !got[0].Dir || !got[0].Link {
		t.Errorf("a link to a folder shows as %+v", got[0])
	}
	if Parent(Local(), dir) != filepath.Dir(dir) {
		t.Errorf("parent %q", Parent(Local(), dir))
	}
	if _, err := List(t.Context(), Local(), filepath.Join(dir, "none")); err == nil {
		t.Error("listed a missing directory")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := List(ctx, Local(), dir); err == nil {
		t.Error("listed after cancel")
	}
}

func TestFreshIgnoresAPartFromBefore(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		t.Run(fmt.Sprint("fresh=", fresh), func(t *testing.T) {
			dir := t.TempDir()
			want := randomFile(t, filepath.Join(dir, "src", "a.bin"), 2<<20)
			// An earlier run left the first megabyte behind.
			part := filepath.Join(dir, "dst", "a.bin"+PartSuffix)
			if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(part, want[:1<<20], 0o644); err != nil {
				t.Fatal(err)
			}
			q, _ := newQueue(t, nil, Options{})
			id, err := q.Add(Spec{
				Src: Location{Path: filepath.Join(dir, "src", "a.bin")},
				Dst: Location{Path: filepath.Join(dir, "dst")}, Fresh: fresh,
			})
			if err != nil {
				t.Fatal(err)
			}
			info := wait(t, q, id, StateDone, StateFailed)
			if info.State != StateDone {
				t.Fatalf("%v: %s", info.State, info.Err)
			}
			if info.Resumed == fresh {
				t.Errorf("Resumed = %v with Fresh = %v", info.Resumed, fresh)
			}
			if !bytes.Equal(mustRead(t, filepath.Join(dir, "dst", "a.bin")), want) {
				t.Error("content differs")
			}
		})
	}
}
