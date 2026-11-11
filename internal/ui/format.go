package ui

import (
	"math"
	"time"
)

// splitBytes writes a byte count as a number and a unit: "318", "GB", with
// the decimal mark of the system's region.
func (a *App) splitBytes(n float64) (value, unit string) {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	if i == 0 || n >= 10 {
		return a.loc.Number(n, 0), units[i]
	}
	return a.loc.Number(n, 1), units[i]
}

// humanBytes writes a byte count as "2.4 GB".
func (a *App) humanBytes(n float64) string {
	v, u := a.splitBytes(n)
	return v + " " + u
}

// humanRate writes bytes per second as "46.8 MB/s".
func (a *App) humanRate(n float64) string {
	v, u := a.splitBytes(n)
	return v + " " + u + "/s"
}

// humanDuration writes a short span: "15 s", "3 min", "2 h 5 min", in the
// interface language.
func (a *App) humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return a.T(kDurSec, int(d/time.Second))
	case d < time.Hour:
		return a.T(kDurMin, int(d/time.Minute))
	default:
		return a.T(kDurHourMin, int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
}

func pct(v float64) int { return int(math.Round(v)) }
