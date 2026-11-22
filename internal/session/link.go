package session

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/zdva/tako/internal/transport"
)

// link is one transport to the host plus the panes living on it, with its own
// connect/reconnect loop. The first link is the primary one whose state the
// session reports; extra links exist when a server's MaxSessions is reached.
type link struct {
	s       *Session
	primary bool
	kick    chan struct{}

	mu    sync.Mutex
	t     transport.Transport
	panes []*Pane
	full  bool
}

func newLink(s *Session, primary bool) *link {
	return &link{s: s, primary: primary, kick: make(chan struct{}, 1)}
}

func (l *link) isFull() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.full
}

func (l *link) current() transport.Transport {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.t
}

func (l *link) add(p *Pane) {
	l.mu.Lock()
	l.panes = append(l.panes, p)
	t := l.t
	l.mu.Unlock()
	if t != nil {
		l.s.wg.Go(func() { l.attachPane(t, p) })
	}
}

func (l *link) remove(p *Pane) {
	l.mu.Lock()
	l.panes = slices.DeleteFunc(l.panes, func(x *Pane) bool { return x == p })
	l.mu.Unlock()
}

func (l *link) kickNow() {
	select {
	case l.kick <- struct{}{}:
	default:
	}
}

func (l *link) report(st StateChange) {
	if l.primary {
		l.s.setState(l, st)
	}
}

// run connects, waits for the transport to die, and connects again, until the
// session is closed.
func (l *link) run(ctx context.Context) {
	attempt := 0
	first := true
	for ctx.Err() == nil {
		switch {
		case first:
			l.report(StateChange{State: StateConnecting})
		case attempt == 0:
			l.report(StateChange{State: StateReconnecting, Attempt: 0})
		}
		t, err := l.dial(ctx)
		if ctx.Err() != nil {
			if t != nil {
				_ = t.Close()
			}
			return
		}
		if err != nil {
			attempt++
			if !transport.Retryable(err) {
				l.report(StateChange{State: StateFailed, Attempt: attempt, Err: err})
				if !l.waitKick(ctx, 0) {
					return
				}
				attempt = 0
				first = true
				continue
			}
			d := l.s.delay(attempt)
			l.report(StateChange{State: StateReconnecting, Attempt: attempt, Err: err, NextRetry: l.s.opts.Now().Add(d)})
			if !l.waitKick(ctx, d) {
				return
			}
			continue
		}
		attempt = 0
		first = false
		l.connected(t)
		l.report(StateChange{State: StateConnected})
		select {
		case <-t.Done():
			l.lost(t)
			l.report(StateChange{State: StateReconnecting, Err: t.Err()})
		case <-l.kick:
			_ = t.Close()
			l.lost(t)
		case <-ctx.Done():
			_ = t.Close()
			l.lost(t)
			return
		}
	}
}

// waitKick waits d (or forever when d is 0) for a kick. It reports false when
// the session is closed.
func (l *link) waitKick(ctx context.Context, d time.Duration) bool {
	var timer <-chan time.Time
	if d > 0 {
		tm := time.NewTimer(d)
		defer tm.Stop()
		timer = tm.C
	}
	select {
	case <-ctx.Done():
		return false
	case <-l.kick:
	case <-timer:
	}
	return true
}

func (l *link) dial(ctx context.Context) (transport.Transport, error) {
	if lim := l.s.opts.Limit; lim != nil {
		select {
		case lim <- struct{}{}:
			defer func() { <-lim }()
		case <-ctx.Done():
			return nil, ctx.Err() //nolint:wrapcheck // plain cancellation
		}
	}
	// Drop a stale kick so it does not cancel the connection we just made.
	select {
	case <-l.kick:
	default:
	}
	return l.s.opts.Dialer.Dial(ctx, l.s.opts.Host) //nolint:wrapcheck // classified by transport.Retryable
}

func (l *link) connected(t transport.Transport) {
	l.mu.Lock()
	l.t = t
	panes := slices.Clone(l.panes)
	l.mu.Unlock()
	for _, p := range panes {
		l.s.wg.Go(func() { l.attachPane(t, p) })
	}
}

// lost detaches all panes from the dead transport.
func (l *link) lost(t transport.Transport) {
	l.mu.Lock()
	if l.t == t {
		l.t = nil
	}
	l.mu.Unlock()
}

// attachPane opens a shell for p on t and pumps until it ends. Panes outlive
// the shells: the link's next transport gets a fresh attach.
func (l *link) attachPane(t transport.Transport, p *Pane) {
	select {
	case <-p.Done():
		return
	default:
	}
	ctx, cancel := context.WithTimeout(l.s.ctx, 30*time.Second)
	defer cancel()
	size := p.Size()
	sh, err := t.OpenShell(ctx, size)
	if err != nil {
		if errors.Is(err, transport.ErrChannelLimit) {
			l.mu.Lock()
			l.full = true
			l.mu.Unlock()
			l.s.moveFromFull(l, p)
			return
		}
		// A transport that cannot open a shell is not usable: drop it so the
		// link reconnects.
		_ = t.Close()
		return
	}
	first := !p.started.Swap(true)
	if !first {
		if n := l.notice(EventRestored); n != nil {
			p.inject(n)
		}
	}
	ended := p.attach(sh)
	if first {
		l.runStart(p)
	}
	select {
	case <-ended:
	case <-p.Done():
		return
	}
	// The shell ended. If the transport died with it, this is a network
	// failure: keep the pane and wait for the reconnect. Otherwise the remote
	// shell exited on its own.
	select {
	case <-t.Done():
	case <-time.After(750 * time.Millisecond):
		p.markExited()
		return
	case <-p.Done():
		return
	}
	p.detach(sh)
	if n := l.notice(EventLost); n != nil {
		p.inject(n)
	}
}

func (l *link) notice(ev Event) []byte {
	if l.s.opts.Notice == nil {
		return nil
	}
	return l.s.opts.Notice(ev, l.s.opts.Now())
}

// runStart types the start directory and command into a pane's very first
// shell.
func (l *link) runStart(p *Pane) {
	if p.startDir != "" {
		_, _ = p.Write([]byte("cd " + shellQuote(p.startDir) + "\n"))
	}
	if p.startCmd != "" {
		_, _ = p.Write([]byte(p.startCmd + "\n"))
	}
}

// shellQuote single-quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
