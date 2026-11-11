package store

import (
	"sync"
	"time"
)

// Saver coalesces many "something changed" signals into one write: the
// change is applied to memory immediately and Request schedules the disk
// write after a quiet period. Writes never overlap.
//
// The save function is called from a timer goroutine, never concurrently with
// itself, and must be safe to run alongside the code that calls Request.
type Saver struct {
	delay time.Duration
	save  func() error
	onErr func(error)

	mu      sync.Mutex // guards timer, pending, closed
	timer   *time.Timer
	pending bool
	closed  bool
	running sync.Mutex // serializes save calls
}

// NewSaver returns a Saver that calls save delay after the last Request.
// onErr, if non-nil, receives save errors.
func NewSaver(delay time.Duration, save func() error, onErr func(error)) *Saver {
	return &Saver{delay: delay, save: save, onErr: onErr}
}

// Request schedules a save. Repeated requests within the delay share one
// write.
func (s *Saver) Request() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.pending = true
	if s.timer == nil {
		s.timer = time.AfterFunc(s.delay, s.fire)
		return
	}
	s.timer.Reset(s.delay)
}

func (s *Saver) fire() {
	s.mu.Lock()
	if !s.pending {
		s.mu.Unlock()
		return
	}
	s.pending = false
	s.mu.Unlock()
	s.run()
}

func (s *Saver) run() {
	s.running.Lock()
	defer s.running.Unlock()
	if err := s.save(); err != nil && s.onErr != nil {
		s.onErr(err)
	}
}

// Flush writes now if a save is pending. Call it before exit.
func (s *Saver) Flush() {
	s.mu.Lock()
	if s.timer != nil {
		s.timer.Stop()
	}
	pending := s.pending
	s.pending = false
	s.mu.Unlock()
	if pending {
		s.run()
	}
}

// Close flushes and stops the saver; later Requests are ignored.
func (s *Saver) Close() {
	s.Flush()
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}
