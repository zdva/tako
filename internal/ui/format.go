package ui

import (
	"fmt"
	"math"
	"time"
)

// splitBytes writes a byte count as a number and a unit: "318", "GB".
func splitBytes(n float64) (value, unit string) {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	if i == 0 || n >= 10 {
		return fmt.Sprintf("%.0f", n), units[i]
	}
	return fmt.Sprintf("%.1f", n), units[i]
}

// humanBytes writes a byte count as "2.4 GB".
func humanBytes(n float64) string {
	v, u := splitBytes(n)
	return v + " " + u
}

// humanRate writes bytes per second as "46.8 MB/s".
func humanRate(n float64) string {
	v, u := splitBytes(n)
	return v + " " + u + "/s"
}

// humanDuration writes a short span: "15 s", "3 min", "2 h 5 min".
func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d/time.Minute))
	default:
		return fmt.Sprintf("%d h %d min", int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
}

func pct(v float64) int { return int(math.Round(v)) }
