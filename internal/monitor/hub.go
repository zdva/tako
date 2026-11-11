package monitor

import (
	"context"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/wei-dong/tako/internal/model"
)

// Target is a machine the hub can measure.
type Target struct {
	Host model.ID
	// OS picks the collector; empty means probe the machine first.
	OS   model.OSKind
	Exec Exec
	Ping func(ctx context.Context) (time.Duration, error)
}

// HubOptions configure a [Hub].
type HubOptions struct {
	// Interval is the time between samples; zero is [DefaultInterval].
	Interval time.Duration
	// OnChange is told when what a host's footer shows has changed, or its
	// failure state has, and on every sample of the focused host. It is
	// called from sampler goroutines and must only post work elsewhere.
	OnChange func(model.ID)
	// Sampler hooks for tests.
	Jitter float64
}

// Hub measures many machines and keeps their history. It tells the window
// about a host only when something visible changed, so an idle fleet costs no
// redraws.
type Hub struct {
	opts HubOptions
	ctx  context.Context
	stop context.CancelFunc
	wg   sync.WaitGroup

	mu       sync.Mutex
	hosts    map[model.ID]*watched
	interval time.Duration
	paused   bool
	focus    model.ID
}

type watched struct {
	target  Target
	series  *Series
	sampler *Sampler
	last    shown
	cancel  context.CancelFunc
}

// shown is what the footer displays, rounded the way it is displayed.
type shown struct {
	cpu, mem, disk, gpu int
	failing             bool
	known               bool
}

// NewHub returns a hub that measures until Close.
func NewHub(o HubOptions) *Hub {
	ctx, stop := context.WithCancel(context.Background())
	return &Hub{
		opts: o, ctx: ctx, stop: stop, hosts: map[model.ID]*watched{},
		interval: cmpOr(o.Interval, DefaultInterval),
	}
}

func cmpOr[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// Watch starts measuring a machine; watching one already watched replaces its
// connection (the session was recreated) and keeps its history.
func (h *Hub) Watch(t Target) {
	h.mu.Lock()
	defer h.mu.Unlock()
	w := h.hosts[t.Host]
	if w == nil {
		w = &watched{series: &Series{}}
		h.hosts[t.Host] = w
	} else if w.cancel != nil {
		w.cancel()
	}
	w.target = t
	ctx, cancel := context.WithCancel(h.ctx)
	w.cancel = cancel
	h.wg.Go(func() { h.measure(ctx, w, t) })
}

// measure probes the OS if needed, then samples until ctx ends.
func (h *Hub) measure(ctx context.Context, w *watched, t Target) {
	os := t.OS
	if os == "" {
		os = h.probe(ctx, t)
		if ctx.Err() != nil {
			return
		}
	}
	h.mu.Lock()
	interval, paused := h.interval, h.paused
	h.mu.Unlock()
	s := StartSampler(ctx, SamplerOptions{
		Collector: ForOS(os), Exec: t.Exec, Ping: t.Ping, Interval: interval, Jitter: h.opts.Jitter,
		OnSample: func(p Sample) { w.series.Add(p); h.notify(t.Host, w, false) },
		OnInfo:   func(i Info) { w.series.SetInfo(i); h.notify(t.Host, w, true) },
		OnFail:   func(err error) { w.series.Fail(err); h.notify(t.Host, w, true) },
	})
	h.mu.Lock()
	w.sampler = s
	h.mu.Unlock()
	if paused {
		s.Pause()
	}
	<-ctx.Done()
	s.Close()
}

// probe finds out whether the machine is Unix-like or Windows, retrying until
// the connection is up.
func (h *Hub) probe(ctx context.Context, t Target) model.OSKind {
	for delay := time.Second; ; delay = min(delay*2, 15*time.Second) {
		pctx, cancel := context.WithTimeout(ctx, defaultTimeout)
		out, err := t.Exec(pctx, ProbeCommand)
		cancel()
		if ctx.Err() != nil {
			return ""
		}
		// A command that ran and failed ("uname" is not a command) is a
		// Windows machine; a command that could not run is a connection
		// that is not up yet.
		if err == nil || len(out) > 0 || strings.Contains(err.Error(), "exited with status") {
			return DetectOS(out, err)
		}
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(delay):
		}
	}
}

// notify tells the window when the footer's numbers moved.
func (h *Hub) notify(id model.ID, w *watched, force bool) {
	cur := h.display(w)
	h.mu.Lock()
	changed := force || cur != w.last || id == h.focus
	w.last = cur
	cb := h.opts.OnChange
	h.mu.Unlock()
	if changed && cb != nil {
		cb(id)
	}
}

func (h *Hub) display(w *watched) shown {
	s := shown{}
	s.failing, _ = w.series.Failing()
	if p, ok := w.series.Latest(); ok {
		s.known = true
		s.cpu, s.mem, s.disk = int(math.Round(p.CPU)), int(math.Round(p.MemPercent())), int(math.Round(p.DiskMax()))
		s.gpu = int(math.Round(p.GPUUtil()))
	}
	return s
}

// Series returns a host's history, or nil if it is not watched.
func (h *Hub) Series(id model.ID) *Series {
	h.mu.Lock()
	defer h.mu.Unlock()
	if w := h.hosts[id]; w != nil {
		return w.series
	}
	return nil
}

// Watching reports whether a host is being measured.
func (h *Hub) Watching(id model.ID) bool { return h.Series(id) != nil }

// Forget stops measuring a host and drops its history.
func (h *Hub) Forget(id model.ID) {
	h.mu.Lock()
	w := h.hosts[id]
	delete(h.hosts, id)
	h.mu.Unlock()
	if w != nil && w.cancel != nil {
		w.cancel()
	}
}

// SetInterval changes how often every host is sampled.
func (h *Hub) SetInterval(d time.Duration) {
	h.mu.Lock()
	h.interval = cmpOr(d, DefaultInterval)
	var all []*Sampler
	for _, w := range h.hosts {
		if w.sampler != nil {
			all = append(all, w.sampler)
		}
	}
	d = h.interval
	h.mu.Unlock()
	for _, s := range all {
		s.SetInterval(d)
	}
}

// SetPaused stops (or restarts) all sampling, for a window nobody can see.
func (h *Hub) SetPaused(p bool) {
	h.mu.Lock()
	h.paused = p
	var all []*Sampler
	for _, w := range h.hosts {
		if w.sampler != nil {
			all = append(all, w.sampler)
		}
	}
	h.mu.Unlock()
	for _, s := range all {
		if p {
			s.Pause()
		} else {
			s.Resume()
		}
	}
}

// SetFocus names the host whose page is open: it is reported on every sample,
// not only when the footer's numbers change. An empty ID clears it.
func (h *Hub) SetFocus(id model.ID) {
	h.mu.Lock()
	h.focus = id
	h.mu.Unlock()
}

// Close stops every sampler and waits for them.
func (h *Hub) Close() {
	h.stop()
	h.wg.Wait()
}
