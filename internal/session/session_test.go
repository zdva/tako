package session_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/session"
	"github.com/wei-dong/tako/internal/transport"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// fakeShell is a shell whose output the test controls.
type fakeShell struct {
	in      chan []byte
	out     *io.PipeReader
	outW    *io.PipeWriter
	mu      sync.Mutex
	written []byte
	rows    int
	cols    int
}

func newFakeShell(size transport.Size) *fakeShell {
	r, w := io.Pipe()
	return &fakeShell{out: r, outW: w, cols: size.Cols, rows: size.Rows}
}

func (s *fakeShell) Read(b []byte) (int, error) { return s.out.Read(b) } //nolint:wrapcheck // test fake
func (s *fakeShell) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.written = append(s.written, b...)
	return len(b), nil
}
func (s *fakeShell) Close() error { _ = s.outW.Close(); return s.out.Close() } //nolint:wrapcheck // test fake
func (s *fakeShell) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = cols, rows
	return nil
}
func (s *fakeShell) typed() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.written)
}

type fakeTransport struct {
	done      chan struct{}
	once      sync.Once
	err       error
	maxShells int // 0 = unlimited

	mu     sync.Mutex
	shells []*fakeShell
}

func newFakeTransport(maxShells int) *fakeTransport {
	return &fakeTransport{done: make(chan struct{}), maxShells: maxShells}
}

func (t *fakeTransport) OpenShell(_ context.Context, size transport.Size) (transport.Shell, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.maxShells > 0 && len(t.shells) >= t.maxShells {
		return nil, transport.ErrChannelLimit
	}
	sh := newFakeShell(size)
	t.shells = append(t.shells, sh)
	return sh, nil
}
func (t *fakeTransport) Exec(context.Context, string) ([]byte, error) { return []byte("ok"), nil }
func (t *fakeTransport) Close() error                                 { t.kill(nil); return nil }
func (t *fakeTransport) Done() <-chan struct{}                        { return t.done }
func (t *fakeTransport) Err() error                                   { return t.err }

// kill simulates the connection dying: shells end, Done closes.
func (t *fakeTransport) kill(err error) {
	t.once.Do(func() {
		t.err = err
		t.mu.Lock()
		for _, sh := range t.shells {
			_ = sh.outW.Close()
		}
		t.mu.Unlock()
		close(t.done)
	})
}

func (t *fakeTransport) shell(i int) *fakeShell {
	t.mu.Lock()
	defer t.mu.Unlock()
	if i < len(t.shells) {
		return t.shells[i]
	}
	return nil
}

// scriptDialer returns the queued results in order, then fails.
type scriptDialer struct {
	mu      sync.Mutex
	results []any // transport.Transport or error
	calls   []time.Time
}

func (d *scriptDialer) Dial(ctx context.Context, _ model.Host) (transport.Transport, error) {
	d.mu.Lock()
	d.calls = append(d.calls, time.Now())
	var r any = errors.New("script exhausted")
	if len(d.results) > 0 {
		r, d.results = d.results[0], d.results[1:]
	}
	d.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err //nolint:wrapcheck // test fake
	}
	if err, ok := r.(error); ok {
		return nil, err
	}
	return r.(transport.Transport), nil //nolint:forcetypeassert // test script holds only these two kinds
}

func (d *scriptDialer) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func fastOpts(d transport.Dialer) session.Options {
	return session.Options{
		Host:    model.Host{ID: "h_1", Name: "x", Addr: "x", User: "u"},
		Dialer:  d,
		Backoff: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond},
		Jitter:  -1,
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func readN(t *testing.T, r io.Reader, n int) string {
	t.Helper()
	buf := make([]byte, 0, n)
	tmp := make([]byte, 256)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for len(buf) < n {
			m, err := r.Read(tmp)
			buf = append(buf, tmp[:m]...)
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out reading %d bytes, got %q", n, buf)
	}
	return string(buf)
}

func TestSession_ConnectsAndEchoes(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(0)
	s := session.New(fastOpts(&scriptDialer{results: []any{tr}}))
	defer s.Close()

	p, err := s.OpenPane(transport.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "shell", func() bool { return tr.shell(0) != nil && p.Connected() })

	if _, err := p.Write([]byte("ls\n")); err != nil {
		t.Fatal(err)
	}
	if got := tr.shell(0).typed(); got != "ls\n" {
		t.Errorf("typed = %q, want %q", got, "ls\n")
	}
	_, _ = tr.shell(0).outW.Write([]byte("hello"))
	if got := readN(t, p, 5); got != "hello" {
		t.Errorf("read = %q", got)
	}
	if st := s.State(); st.State != session.StateConnected {
		t.Errorf("state = %v, want connected", st.State)
	}
}

func TestSession_StartDirAndCmdOnFirstShellOnly(t *testing.T) {
	t.Parallel()
	t1, t2 := newFakeTransport(0), newFakeTransport(0)
	opts := fastOpts(&scriptDialer{results: []any{t1, t2}})
	opts.StartDir = "/srv/it's"
	opts.StartCmd = "htop"
	s := session.New(opts)
	defer s.Close()
	p, _ := s.OpenPane(transport.Size{Cols: 80, Rows: 24})

	waitFor(t, "start input", func() bool { sh := t1.shell(0); return sh != nil && len(sh.typed()) > 0 })
	want := "cd '/srv/it'\\''s'\nhtop\n"
	waitFor(t, "full start input", func() bool { return t1.shell(0).typed() == want })

	t1.kill(errors.New("drop"))
	waitFor(t, "second shell", func() bool { return t2.shell(0) != nil && p.Connected() })
	time.Sleep(50 * time.Millisecond)
	if got := t2.shell(0).typed(); got != "" {
		t.Errorf("reconnected shell got start input %q", got)
	}
}

func TestSession_ReconnectKeepsPaneAlive(t *testing.T) {
	t.Parallel()
	t1, t2 := newFakeTransport(0), newFakeTransport(0)
	d := &scriptDialer{results: []any{t1, errors.New("still down"), t2}}
	opts := fastOpts(d)
	opts.Backoff = []time.Duration{150 * time.Millisecond}
	opts.Notice = func(ev session.Event, _ time.Time) []byte {
		if ev == session.EventLost {
			return []byte("<lost>")
		}
		return []byte("<back>")
	}
	s := session.New(opts)
	defer s.Close()

	var states []session.State
	var smu sync.Mutex
	s.Subscribe(func(c session.StateChange) { smu.Lock(); states = append(states, c.State); smu.Unlock() })

	p, _ := s.OpenPane(transport.Size{Cols: 100, Rows: 30})
	waitFor(t, "first shell", func() bool { return p.Connected() })
	_, _ = t1.shell(0).outW.Write([]byte("before"))
	if got := readN(t, p, 6); got != "before" {
		t.Fatalf("got %q", got)
	}

	// Drop the connection.
	t1.kill(errors.New("network down"))
	waitFor(t, "lost notice", func() bool { return !p.Connected() })
	if got := readN(t, p, 6); got != "<lost>" {
		t.Fatalf("notice = %q", got)
	}

	// Read must block, not return EOF, while the connection is down.
	type res struct {
		got string
		err error
	}
	rc := make(chan res, 1)
	go func() {
		b := make([]byte, 16)
		n, err := p.Read(b)
		rc <- res{string(b[:n]), err}
	}()
	select {
	case r := <-rc:
		t.Fatalf("Read returned %q, %v during the outage; it must block", r.got, r.err)
	case <-time.After(30 * time.Millisecond):
	}

	// Input during the outage is dropped and counted, with no error.
	var dropCalls atomic.Int32
	p.OnDrop(func() { dropCalls.Add(1) })
	for _, in := range []string{"rm -rf /\n", "x"} {
		if n, err := p.Write([]byte(in)); err != nil || n != len(in) {
			t.Fatalf("Write during outage = %d, %v", n, err)
		}
	}
	if got := p.Dropped(); got != 10 {
		t.Errorf("Dropped = %d, want 10", got)
	}
	if dropCalls.Load() != 2 {
		t.Errorf("OnDrop calls = %d, want 2", dropCalls.Load())
	}

	// Size changes while down are applied to the new shell.
	_ = p.Resize(120, 40)

	waitFor(t, "new shell", func() bool { return t2.shell(0) != nil && p.Connected() })
	if sh := t2.shell(0); sh.cols != 120 || sh.rows != 40 {
		t.Errorf("new shell size = %dx%d, want 120x40", sh.cols, sh.rows)
	}
	if got := t2.shell(0).typed(); got != "" {
		t.Errorf("input from the outage was replayed: %q", got)
	}
	if d.count() != 3 {
		t.Errorf("dial attempts = %d, want 3", d.count())
	}
	select {
	case r := <-rc:
		if r.err != nil || r.got != "<back>" {
			t.Errorf("first read after reconnect = %q, %v; want the restore notice", r.got, r.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("blocked Read never woke after the reconnect")
	}
	_, _ = t2.shell(0).outW.Write([]byte("after"))
	if got := readN(t, p, 5); got != "after" {
		t.Errorf("post-reconnect output = %q", got)
	}

	smu.Lock()
	defer smu.Unlock()
	if len(states) < 2 || states[len(states)-1] != session.StateConnected {
		t.Errorf("states = %v", states)
	}
	sawRe := false
	for _, st := range states {
		sawRe = sawRe || st == session.StateReconnecting
	}
	if !sawRe {
		t.Errorf("never reported Reconnecting: %v", states)
	}
}

func TestSession_NonRetryableStopsUntilReconnect(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(0)
	authErr := errors.Join(transport.ErrAuthFailed, errors.New("bad password"))
	d := &scriptDialer{results: []any{authErr, tr}}
	s := session.New(fastOpts(d))
	defer s.Close()

	waitFor(t, "failed state", func() bool { return s.State().State == session.StateFailed })
	if !errors.Is(s.State().Err, transport.ErrAuthFailed) {
		t.Errorf("Err = %v", s.State().Err)
	}
	time.Sleep(80 * time.Millisecond) // several backoff periods
	if d.count() != 1 {
		t.Fatalf("dialed %d times after a non-retryable error, want 1", d.count())
	}
	s.Reconnect()
	waitFor(t, "connected", func() bool { return s.State().State == session.StateConnected })
}

func TestSession_BackoffSchedule(t *testing.T) {
	t.Parallel()
	d := &scriptDialer{}
	opts := fastOpts(d)
	opts.Backoff = []time.Duration{40 * time.Millisecond, 80 * time.Millisecond}
	s := session.New(opts)
	defer s.Close()

	waitFor(t, "5 attempts", func() bool { return d.count() >= 5 })
	d.mu.Lock()
	defer d.mu.Unlock()
	gaps := []time.Duration{40, 80, 80, 80}
	for i, want := range gaps {
		got := d.calls[i+1].Sub(d.calls[i])
		want *= time.Millisecond
		if got < want-5*time.Millisecond || got > want+60*time.Millisecond {
			t.Errorf("gap %d = %v, want about %v", i, got, want)
		}
	}
}

func TestSession_JitterBounds(t *testing.T) {
	t.Parallel()
	for _, r := range []float64{0, 0.5, 1} {
		d := &scriptDialer{}
		opts := fastOpts(d)
		opts.Backoff = []time.Duration{100 * time.Millisecond}
		opts.Jitter = 0.2
		opts.Rand = func() float64 { return r }
		s := session.New(opts)
		waitFor(t, "failure", func() bool { return s.State().State == session.StateReconnecting && s.State().Attempt == 1 })
		st := s.State()
		delay := time.Until(st.NextRetry)
		lo, hi := 75*time.Millisecond, 125*time.Millisecond
		if delay > hi || delay < lo-60*time.Millisecond {
			t.Errorf("rand=%v: delay %v outside ±20%% of 100ms", r, delay)
		}
		s.Close()
	}
}

func TestSession_ExtraLinkWhenServerLimitsChannels(t *testing.T) {
	t.Parallel()
	t1, t2 := newFakeTransport(2), newFakeTransport(0)
	d := &scriptDialer{results: []any{t1, t2}}
	s := session.New(fastOpts(d))
	defer s.Close()

	panes := make([]*session.Pane, 3)
	for i := range panes {
		p, err := s.OpenPane(transport.Size{Cols: 80, Rows: 24})
		if err != nil {
			t.Fatal(err)
		}
		panes[i] = p
		waitFor(t, "pane connected", func() bool { return p.Connected() || i == 2 })
	}
	waitFor(t, "third pane on a second connection", func() bool { return panes[2].Connected() })
	if d.count() != 2 {
		t.Errorf("dials = %d, want 2", d.count())
	}
	if t1.shell(2) != nil || t2.shell(0) == nil {
		t.Error("third shell should be on the second transport")
	}
}

func TestSession_RemoteShellExitClosesPane(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(0)
	s := session.New(fastOpts(&scriptDialer{results: []any{tr}}))
	defer s.Close()
	p, _ := s.OpenPane(transport.Size{Cols: 80, Rows: 24})
	waitFor(t, "shell", func() bool { return p.Connected() })

	_ = tr.shell(0).outW.Close() // the shell exits; the connection stays up
	select {
	case <-p.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("pane did not end after the remote shell exited")
	}
	if !p.ShellExited() {
		t.Error("ShellExited = false")
	}
	if _, err := p.Read(make([]byte, 4)); !errors.Is(err, io.EOF) {
		t.Errorf("Read = %v, want EOF", err)
	}
}

func TestSession_CloseEndsPanesAndNeverDialsAgain(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(0)
	d := &scriptDialer{results: []any{tr}}
	s := session.New(fastOpts(d))
	p, _ := s.OpenPane(transport.Size{Cols: 80, Rows: 24})
	waitFor(t, "shell", func() bool { return p.Connected() })

	s.Close()
	if _, err := p.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Errorf("Read after Close = %v", err)
	}
	if _, err := s.OpenPane(transport.Size{}); !errors.Is(err, session.ErrClosed) {
		t.Errorf("OpenPane after Close = %v", err)
	}
	if st := s.State().State; st != session.StateClosed {
		t.Errorf("state = %v", st)
	}
	n := d.count()
	time.Sleep(50 * time.Millisecond)
	if d.count() != n {
		t.Error("dialed after Close")
	}
}

func TestSession_Exec(t *testing.T) {
	t.Parallel()
	tr := newFakeTransport(0)
	s := session.New(fastOpts(&scriptDialer{results: []any{tr}}))
	defer s.Close()
	waitFor(t, "connected", func() bool { return s.State().State == session.StateConnected })
	out, err := s.Exec(t.Context(), "uname")
	if err != nil || string(out) != "ok" {
		t.Errorf("Exec = %q, %v", out, err)
	}
}

func TestSession_LimitBoundsConcurrentDials(t *testing.T) {
	t.Parallel()
	limit := make(chan struct{}, 2)
	var cur, peak atomic.Int32
	d := dialerFunc(func(ctx context.Context, _ model.Host) (transport.Transport, error) {
		n := cur.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		cur.Add(-1)
		return newFakeTransport(0), nil
	})
	var ss []*session.Session
	for range 6 {
		o := fastOpts(d)
		o.Limit = limit
		ss = append(ss, session.New(o))
	}
	for _, s := range ss {
		waitFor(t, "connected", func() bool { return s.State().State == session.StateConnected })
	}
	for _, s := range ss {
		s.Close()
	}
	if peak.Load() > 2 {
		t.Errorf("peak concurrent dials = %d, want ≤ 2", peak.Load())
	}
}

type dialerFunc func(context.Context, model.Host) (transport.Transport, error)

func (f dialerFunc) Dial(ctx context.Context, h model.Host) (transport.Transport, error) {
	return f(ctx, h)
}
