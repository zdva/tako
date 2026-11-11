package monitor

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"
)

// Defaults.
const (
	DefaultInterval = 2 * time.Second
	DefaultJitter   = 0.1
	defaultTimeout  = 15 * time.Second
	// primeGap is the wait between the first two samples, which makes the
	// first speeds appear after under a second instead of a whole interval.
	primeGap = 700 * time.Millisecond
	// maxBackoff bounds the wait between failed attempts.
	maxBackoff = 30 * time.Second
)

// SamplerOptions configure a [Sampler].
type SamplerOptions struct {
	Collector Collector
	Exec      Exec
	// Ping measures the connection's round trip; optional.
	Ping func(ctx context.Context) (time.Duration, error)
	// Interval is the time between samples; zero is [DefaultInterval].
	Interval time.Duration
	// Jitter is the random spread of the interval (0.1 is ±10%) that keeps
	// many hosts from all being asked at the same moment; negative disables it
	// and zero is [DefaultJitter].
	Jitter float64
	// Timeout bounds one command; zero is 15 s.
	Timeout time.Duration
	// OnSample, OnInfo and OnFail are called from the sampler's goroutine.
	OnSample func(Sample)
	OnInfo   func(Info)
	// OnFail is called once when sampling starts failing.
	OnFail func(error)
	// Now and Rand are test hooks.
	Now  func() time.Time
	Rand func() float64
}

// Sampler measures one machine on a timer. A command that fails (the
// connection is down) does not stop it: it tries again with growing waits and
// forgets the previous counters, since speeds across a gap would be wrong.
type Sampler struct {
	opts SamplerOptions

	mu       sync.Mutex
	paused   bool
	interval time.Duration
	wake     chan struct{}

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// StartSampler starts measuring in the background until ctx ends or Close.
func StartSampler(ctx context.Context, o SamplerOptions) *Sampler {
	if o.Interval <= 0 {
		o.Interval = DefaultInterval
	}
	switch {
	case o.Jitter == 0:
		o.Jitter = DefaultJitter
	case o.Jitter < 0:
		o.Jitter = 0
	}
	if o.Timeout <= 0 {
		o.Timeout = defaultTimeout
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = rand.Float64
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Sampler{opts: o, interval: o.Interval, wake: make(chan struct{}, 1), cancel: cancel}
	s.wg.Go(func() { s.run(ctx) })
	return s
}

// Pause stops sampling until Resume; the history stays.
func (s *Sampler) Pause() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
}

// Resume continues after Pause, sampling at once.
func (s *Sampler) Resume() {
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
	s.kick()
}

// SetInterval changes the time between samples.
func (s *Sampler) SetInterval(d time.Duration) {
	if d <= 0 {
		d = DefaultInterval
	}
	s.mu.Lock()
	s.interval = d
	s.mu.Unlock()
	s.kick()
}

func (s *Sampler) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Close stops the sampler and waits for it.
func (s *Sampler) Close() {
	s.cancel()
	s.wg.Wait()
}

func (s *Sampler) state() (paused bool, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused, s.interval
}

// wait sleeps for d, or until woken by Resume or SetInterval. It reports
// whether ctx is still alive.
func (s *Sampler) wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-s.wake:
	case <-t.C:
	}
	return ctx.Err() == nil
}

func (s *Sampler) run(ctx context.Context) {
	var prev Raw
	gotInfo, failures := false, 0
	for ctx.Err() == nil {
		if paused, _ := s.state(); paused {
			if !s.wait(ctx, time.Hour) {
				return
			}
			prev = Raw{} // the gap makes the old counters useless
			continue
		}
		if !gotInfo {
			gotInfo = s.readInfo(ctx)
		}
		next, err := s.sample(ctx, &prev)
		_, interval := s.state()
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			prev = Raw{}
			failures++
			if failures == 1 && s.opts.OnFail != nil {
				s.opts.OnFail(err)
			}
			next = min(interval<<min(failures, 5), maxBackoff)
		default:
			failures = 0
		}
		if !s.wait(ctx, s.jittered(next)) {
			return
		}
	}
}

// sample takes one measurement and returns how long to wait for the next.
func (s *Sampler) sample(ctx context.Context, prev *Raw) (time.Duration, error) {
	cctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	out, err := s.opts.Exec(cctx, s.opts.Collector.Command())
	if err != nil && len(out) == 0 {
		return 0, err //nolint:wrapcheck // the transport's error says what failed
	}
	first := !prev.valid
	smp, raw, perr := s.opts.Collector.Parse(out, *prev, s.opts.Now())
	if perr != nil {
		return 0, perr //nolint:wrapcheck // the collector's error says what failed
	}
	*prev = raw
	if s.opts.Ping != nil {
		if d, perr := s.opts.Ping(cctx); perr == nil {
			smp.Latency = d
		}
	}
	if s.opts.OnSample != nil {
		s.opts.OnSample(smp)
	}
	if first && !smp.Rates {
		return primeGap, nil
	}
	_, interval := s.state()
	return interval, nil
}

func (s *Sampler) readInfo(ctx context.Context) bool {
	if s.opts.OnInfo == nil {
		return true
	}
	cctx, cancel := context.WithTimeout(ctx, s.opts.Timeout)
	defer cancel()
	out, err := s.opts.Exec(cctx, s.opts.Collector.InfoCommand())
	if err != nil && len(out) == 0 {
		return false
	}
	in, perr := s.opts.Collector.ParseInfo(out)
	if perr != nil {
		return false
	}
	s.opts.OnInfo(in)
	return true
}

func (s *Sampler) jittered(d time.Duration) time.Duration {
	if s.opts.Jitter <= 0 {
		return d
	}
	return time.Duration(float64(d) * (1 + s.opts.Jitter*(2*s.opts.Rand()-1)))
}
