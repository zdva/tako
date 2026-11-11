package monitor

import (
	"slices"
	"sync"
	"time"
)

// maxPoints bounds a series: an hour at one sample a second.
const maxPoints = 3600

// Series is the history of one machine's samples, newest last. It is safe for
// concurrent use: the sampler adds, the UI reads.
type Series struct {
	mu      sync.RWMutex
	points  []Sample
	info    Info
	hasInfo bool
	failing bool
	err     error
}

// Add appends a sample and clears a failure.
func (s *Series) Add(p Sample) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.points) == maxPoints {
		copy(s.points, s.points[1:])
		s.points = s.points[:maxPoints-1]
	}
	s.points = append(s.points, p)
	s.failing, s.err = false, nil
}

// SetInfo records what the machine is.
func (s *Series) SetInfo(in Info) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info, s.hasInfo = in, true
}

// Info returns what the machine is, and whether it is known yet.
func (s *Series) Info() (Info, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.info, s.hasInfo
}

// Fail records that sampling is failing.
func (s *Series) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failing, s.err = true, err
}

// Failing reports whether the last attempt failed, and why.
func (s *Series) Failing() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.failing, s.err
}

// Latest returns the newest sample that has rates, falling back to the newest
// of any kind.
func (s *Series) Latest() (Sample, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range slices.Backward(s.points) {
		if p.Rates {
			return p, true
		}
	}
	if n := len(s.points); n > 0 {
		return s.points[n-1], true
	}
	return Sample{}, false
}

// Window returns a copy of the samples of the last d before now, oldest first,
// without the sample that has no rates.
func (s *Series) Window(d time.Duration, now time.Time) []Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	from := now.Add(-d)
	var out []Sample
	for _, p := range s.points {
		if p.Rates && !p.At.Before(from) {
			out = append(out, p)
		}
	}
	return out
}

// Len is the number of samples held.
func (s *Series) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.points)
}
