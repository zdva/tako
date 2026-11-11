// Package inputbus fans the keystrokes typed into one terminal block out to
// every other block that has sync turned on.
//
// The rule is the one in the product spec: input typed in a synced block goes
// to all synced blocks; input typed in an unsynced block stays there, and
// unsynced blocks never receive anything.
//
// Each destination has its own queue and writer goroutine, so one slow SSH
// session cannot stall typing in the others.
package inputbus

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
)

// defaultQueueSize is how many chunks may wait for one slow destination
// before new chunks for it are dropped (and counted) instead of blocking the
// typist.
const defaultQueueSize = 1024

// Option configures a Bus.
type Option func(*Bus)

// WithQueueSize sets the per-destination queue length.
func WithQueueSize(n int) Option {
	return func(b *Bus) { b.queueSize = max(n, 1) }
}

// ID names a block on the bus.
type ID string

// Stats counts what happened to the chunks sent to one destination.
type Stats struct {
	Delivered uint64
	Dropped   uint64
}

type sink struct {
	id        ID
	out       io.Writer
	queue     chan []byte
	synced    atomic.Bool
	delivered atomic.Uint64
	dropped   atomic.Uint64
	stop      context.CancelFunc
}

// Bus routes input between registered blocks. It is safe for concurrent use.
type Bus struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	queueSize int

	mu    sync.RWMutex
	sinks map[ID]*sink
}

// New returns a bus whose writer goroutines stop when ctx ends or Close is
// called.
func New(ctx context.Context, opts ...Option) *Bus {
	ctx, cancel := context.WithCancel(ctx)
	b := &Bus{ctx: ctx, cancel: cancel, queueSize: defaultQueueSize, sinks: map[ID]*sink{}}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Register adds a block. Chunks that reach it are written to out, which must
// not publish back to the bus (write to the session, not to the terminal).
// Registering an id twice replaces the earlier registration.
func (b *Bus) Register(id ID, out io.Writer) {
	ctx, stop := context.WithCancel(b.ctx)
	s := &sink{id: id, out: out, queue: make(chan []byte, b.queueSize), stop: stop}

	b.mu.Lock()
	if old, ok := b.sinks[id]; ok {
		old.stop()
	}
	b.sinks[id] = s
	b.mu.Unlock()

	b.wg.Go(func() { s.run(ctx) })
}

// Unregister removes a block and stops its writer.
func (b *Bus) Unregister(id ID) {
	b.mu.Lock()
	s, ok := b.sinks[id]
	delete(b.sinks, id)
	b.mu.Unlock()
	if ok {
		s.stop()
	}
}

// SetSynced turns sync on or off for a block.
func (b *Bus) SetSynced(id ID, on bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if s, ok := b.sinks[id]; ok {
		s.synced.Store(on)
	}
}

// Synced reports whether a block has sync turned on.
func (b *Bus) Synced(id ID) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s, ok := b.sinks[id]
	return ok && s.synced.Load()
}

// Publish sends input typed in block src to the other synced blocks and
// returns how many it queued for. It does nothing when src itself is not
// synced. What the terminal wrote on its own behalf (see StripReplies) is
// removed first. The caller still writes p to src's own session.
func (b *Bus) Publish(src ID, p []byte) int {
	p = StripReplies(p)
	if len(p) == 0 {
		return 0
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	from, ok := b.sinks[src]
	if !ok || !from.synced.Load() {
		return 0
	}
	queued := 0
	for id, s := range b.sinks {
		if id == src || !s.synced.Load() {
			continue
		}
		chunk := append([]byte(nil), p...)
		select {
		case s.queue <- chunk:
			queued++
		default:
			s.dropped.Add(1)
		}
	}
	return queued
}

// Stats returns the delivery counters of a block.
func (b *Bus) Stats(id ID) Stats {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s, ok := b.sinks[id]
	if !ok {
		return Stats{}
	}
	return Stats{Delivered: s.delivered.Load(), Dropped: s.dropped.Load()}
}

// Close stops every writer goroutine and waits for them.
func (b *Bus) Close() {
	b.cancel()
	b.wg.Wait()
}

func (s *sink) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-s.queue:
			if _, err := s.out.Write(p); err != nil {
				s.dropped.Add(1)
				continue
			}
			s.delivered.Add(1)
		}
	}
}
