package ui

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/monitor"
)

// line is one curve of a chart.
type line struct {
	color ui.Color
	// fill shades the area under the curve.
	fill bool
	at   func(monitor.Sample) float64
}

const (
	chartLeft   = 40 // room for the value labels
	chartBottom = 18 // room for the time labels
	chartTop    = 4
)

// chart draws curves over the window ending now. A percent chart is scaled
// 0–100; any other is a rate scaled to the largest value seen.
func (a *App) chart(c *ui.Context, percent bool, samples []monitor.Sample, window time.Duration, now time.Time, lines []line) {
	ui.Box(c).Grow(1).MinHeight(60).Draw(func(p *ui.Painter, r ui.Rect) {
		a.paintChart(p, r, percent, samples, window, now, lines)
	})
}

func (a *App) paintChart(p *ui.Painter, r ui.Rect, percent bool, samples []monitor.Sample, window time.Duration, now time.Time, lines []line) {
	plot := ui.Rect{X: r.X + chartLeft, Y: r.Y + chartTop, W: r.W - chartLeft - 4, H: r.H - chartTop - chartBottom}
	if plot.W < 20 || plot.H < 20 {
		return
	}
	top := 100.0
	if !percent {
		top = 1
		for _, s := range samples {
			for _, l := range lines {
				top = max(top, l.at(s))
			}
		}
		top = niceCeil(top * 1.1)
	}
	label := func(v float64) string {
		if percent {
			return fmt.Sprintf("%d%%", pct(v))
		}
		val, unit := a.splitBytes(v)
		return val + " " + unit[:1]
	}
	// Grid and value labels.
	for i := range 3 {
		frac := float32(i) / 2
		y := plot.Y + plot.H*(1-frac)
		p.Line(plot.X, y, plot.X+plot.W, y, 1, a.tk.Border)
		txt := label(top * float64(frac))
		w, h := p.MeasureText(0, ui.Span{Text: txt, Size: 10, Color: a.tk.Text3})
		p.Text(plot.X-6-w, min(max(y-h/2, r.Y), r.Y+r.H-chartBottom-h), txt, 10, a.tk.Text3)
	}
	// Time labels: the left edge is the start of the window.
	for i := range 5 {
		frac := float32(i) / 4
		txt := a.T(kMonNow)
		if i < 4 {
			txt = a.T(kMonAgo, windowLabel(time.Duration(float64(window)*(1-float64(frac)))))
		}
		w, _ := p.MeasureText(0, ui.Span{Text: txt, Size: 10, Color: a.tk.Text3})
		x := plot.X + plot.W*frac - w*frac
		p.Text(x, plot.Y+plot.H+4, txt, 10, a.tk.Text3)
	}
	if len(samples) < 2 {
		return
	}
	start := now.Add(-window)
	xOf := func(t time.Time) float32 {
		return plot.X + plot.W*float32(t.Sub(start))/float32(window)
	}
	yOf := func(v float64) float32 {
		return plot.Y + plot.H*float32(1-min(max(v/top, 0), 1))
	}
	p.Clip(plot, 0, func() {
		for _, l := range lines {
			pts := make([][2]float32, 0, len(samples))
			for _, s := range samples {
				pts = append(pts, [2]float32{xOf(s.At), yOf(l.at(s))})
			}
			if l.fill && len(pts) > 1 {
				area := new(ui.Path).MoveTo(pts[0][0], plot.Y+plot.H)
				for _, pt := range pts {
					area.LineTo(pt[0], pt[1])
				}
				area.LineTo(pts[len(pts)-1][0], plot.Y+plot.H).Close()
				p.FillPath(area, l.color.Alpha(0.16))
			}
			p.StrokePolyline(pts, 1.6, l.color)
		}
	})
}

// windowLabel writes an offset of the time axis: "5 min", "75 s".
func windowLabel(d time.Duration) string {
	if d < 90*time.Second {
		return fmt.Sprintf("%d s", int(d.Round(time.Second)/time.Second))
	}
	if d < 90*time.Minute {
		return fmt.Sprintf("%d min", int(d.Round(time.Minute)/time.Minute))
	}
	return fmt.Sprintf("%.1f h", d.Hours())
}

// niceCeil rounds up to 1, 2 or 5 times a power of 1024-ish ten, so axis
// labels are round numbers.
func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if v <= m*exp {
			return m * exp
		}
	}
	return 10 * exp
}
