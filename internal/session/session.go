package session

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/transport"
)

// State is where a connection is in its life.
type State int

// Connection states.
const (
	// StateConnecting is the first attempt.
	StateConnecting State = iota
	// StateConnected means a transport is up.
	StateConnected
	// StateReconnecting means the connection was lost or an attempt failed
	// with an error worth retrying; another attempt is scheduled.
	StateReconnecting
	// StateFailed means retrying cannot help (see StateChange.Err) until the
	// user fixes something and calls Reconnect.
	StateFailed
	// StateClosed means the session was closed.
	StateClosed
)

func (s State) String() string {
	switch s {
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateReconnecting:
		return "reconnecting"
	case StateFailed:
		return "failed"
	case StateClosed:
		return "closed"
	default:
		return fmt.Sprintf("state(%d)", int(s))
	}
}

// StateChange describes the session's current state.
type StateChange struct {
	State State
	// Attempt counts failed attempts since the last connection (1-based while
	// reconnecting).
	Attempt int
	// NextRetry is when the next attempt starts (Reconnecting only).
	NextRetry time.Time
	// Since is when the session entered this state.
	Since time.Time
	// Err is the last error (Reconnecting and Failed).
	Err error
}

// Event tells a [Options.Notice] function what happened to a pane's link.
type Event int

// Notice events.
const (
	// EventLost: the connection dropped.
	EventLost Event = iota
	// EventRestored: a new shell is attached after a drop.
	EventRestored
)

// DefaultBackoff is the retry schedule; the last value repeats.
var DefaultBackoff = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second}

// Options configure a [Session].
type Options struct {
	Host   model.Host
	Dialer transport.Dialer
	// Backoff overrides [DefaultBackoff].
	Backoff []time.Duration
	// Jitter is the random spread applied to each delay (0.2 means ±20%);
	// negative disables it, zero means 0.2.
	Jitter float64
	// Limit, when not nil, bounds concurrent dial attempts across sessions:
	// an attempt takes a token first. It stops a restart from opening dozens
	// of connections at the same moment.
	Limit chan struct{}
	// Notice returns text to show in a pane when its link changes, or nil.
	Notice func(ev Event, at time.Time) []byte
	// StartDir and StartCmd run in the first shell of each pane only.
	StartDir string
	StartCmd string
	// ResizeDelay overrides [DefaultResizeDelay].
	ResizeDelay time.Duration
	// Now and Rand are test hooks.
	Now  func() time.Time
	Rand func() float64
}

// Session manages the connection(s) to one host.
type Session struct {
	opts   Options
	ctx    context.Context //nolint:containedctx // owns the lifetime of the goroutines this type starts
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	links    []*link
	subs     map[int]func(StateChange)
	nextSub  int
	state    StateChange
	closed   bool
	panesAll []*Pane
}

// New creates a session and starts connecting to the host in the background.
func New(opts Options) *Session {
	if len(opts.Backoff) == 0 {
		opts.Backoff = DefaultBackoff
	}
	if opts.Jitter == 0 {
		opts.Jitter = 0.2
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Rand == nil {
		opts.Rand = rand.Float64
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{opts: opts, ctx: ctx, cancel: cancel, subs: map[int]func(StateChange){}}
	s.state = StateChange{State: StateConnecting, Since: opts.Now()}
	s.addLinkLocked()
	return s
}

// ErrNotConnected is returned by Exec when no connection is up.
var ErrNotConnected = errors.New("not connected")

// ErrClosed is returned after Close.
var ErrClosed = errors.New("session closed")

// State returns the primary connection's state.
func (s *Session) State() StateChange {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Host returns the host this session connects to.
func (s *Session) Host() model.Host { return s.opts.Host }

// Subscribe registers fn to be called on every state change of the primary
// connection. The returned function unsubscribes.
func (s *Session) Subscribe(fn func(StateChange)) func() {
	s.mu.Lock()
	id := s.nextSub
	s.nextSub++
	s.subs[id] = fn
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		delete(s.subs, id)
		s.mu.Unlock()
	}
}

func (s *Session) setState(l *link, st StateChange) {
	s.mu.Lock()
	if l != nil && (s.closed || (len(s.links) > 0 && s.links[0] != l)) {
		s.mu.Unlock()
		return
	}
	st.Since = s.opts.Now()
	s.state = st
	subs := make([]func(StateChange), 0, len(s.subs))
	for _, fn := range s.subs {
		subs = append(subs, fn)
	}
	s.mu.Unlock()
	for _, fn := range subs {
		fn(st)
	}
}

// OpenPane opens a terminal channel. It returns at once; the pane connects
// when the transport is up, so a session being restored can show its panes
// while they connect.
func (s *Session) OpenPane(size transport.Size) (*Pane, error) {
	return s.OpenPaneAt(size, s.opts.StartDir, s.opts.StartCmd)
}

// OpenPaneAt is OpenPane with its own start directory and command, which run
// in the pane's first shell only (a restored tab returns to where it was).
func (s *Session) OpenPaneAt(size transport.Size, dir, cmd string) (*Pane, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	p := newPane(size, s.opts.ResizeDelay)
	p.startDir, p.startCmd = dir, cmd
	s.panesAll = append(s.panesAll, p)
	s.assignLocked(p)
	return p, nil
}

// assignLocked gives a pane to the first link with room.
func (s *Session) assignLocked(p *Pane) {
	for _, l := range s.links {
		if !l.isFull() {
			l.add(p)
			return
		}
	}
	l := s.addLinkLocked()
	l.add(p)
}

// moveFromFull is called by a link whose server refused another channel.
func (s *Session) moveFromFull(from *link, p *Pane) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	from.remove(p)
	s.assignLocked(p)
}

func (s *Session) addLinkLocked() *link {
	l := newLink(s, len(s.links) == 0)
	s.links = append(s.links, l)
	s.wg.Go(func() { l.run(s.ctx) })
	return l
}

// Exec runs a command on the primary connection.
func (s *Session) Exec(ctx context.Context, cmd string) ([]byte, error) {
	s.mu.Lock()
	var t transport.Transport
	if len(s.links) > 0 {
		t = s.links[0].current()
	}
	s.mu.Unlock()
	if t == nil {
		return nil, ErrNotConnected
	}
	return t.Exec(ctx, cmd) //nolint:wrapcheck // the transport's error already names the failure
}

// Reconnect restarts connecting now: it skips a pending backoff wait, retries
// after a Failed state, and replaces a live connection.
func (s *Session) Reconnect() {
	s.mu.Lock()
	links := append([]*link(nil), s.links...)
	s.mu.Unlock()
	for _, l := range links {
		l.kickNow()
	}
}

// Close ends the session and all its panes.
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	panes := s.panesAll
	s.mu.Unlock()
	for _, p := range panes {
		_ = p.Close()
	}
	s.cancel()
	s.wg.Wait()
	s.setState(nil, StateChange{State: StateClosed})
}

func (s *Session) delay(attempt int) time.Duration {
	b := s.opts.Backoff
	d := b[min(attempt-1, len(b)-1)]
	if s.opts.Jitter > 0 {
		d = time.Duration(float64(d) * (1 + s.opts.Jitter*(2*s.opts.Rand()-1)))
	}
	return d
}
