package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/monitor"
)

// monitorPage is the page opened from a block's footer: stat cards, charts of
// the last minutes, per-GPU status, disks and a short description of the
// machine (the layout of docs/design monitor.png).
func (a *App) monitorPage(c *ui.Context) {
	id := a.mon.host
	name := string(id)
	var os model.OSKind
	if h, ok := a.d.Library.Get(id); ok {
		name, os = h.Name, h.OS
	}
	var series *monitor.Series
	if a.d.Monitor != nil {
		series = a.d.Monitor.Series(id)
	}
	ui.Column(c).Key("monitor").Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(a.tk.Bg).Children(func() {
		a.monitorHeader(c, name, os, series)
		ui.Scroll(c).Key("monitor-scroll").Grow(1).MinHeight(0).Padding(0, 16).Children(func() {
			ui.Column(c).Gap(12).Padding(2, 0, 14).Children(func() {
				a.monitorBody(c, series, os)
			})
		})
		a.monitorFoot(c, name)
	})
}

func (a *App) monitorHeader(c *ui.Context, name string, os model.OSKind, s *monitor.Series) {
	ui.Row(c).Key("mon-head").Height(56).Padding(0, 16).Gap(12).AlignItems(ui.Center).Shrink(0).Children(func() {
		back := ui.Button(c, "‹  "+a.T(kMonBack)).Label(a.T(kMonBack)).Height(30).Padding(0, 12)
		if back.Clicked() {
			a.closeMonitor()
		}
		a.osTile(c, os, false, 30)
		ui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			ui.Text(c, name).Bold().FontSize(15).NoWrap()
			if s != nil {
				if info, ok := s.Info(); ok {
					a.caption(c, a.aboutLine(info)).NoWrap()
				}
			}
		})
		labels := []string{a.T(kMonRange1), a.T(kMonRange5), a.T(kMonRange15), a.T(kMonRange60)}
		ui.Segmented(c, &a.mon.rng, labels...).Label(a.T(kMonRange5))
	})
}

// aboutLine is the header's subtitle: system, cores, uptime.
func (a *App) aboutLine(i monitor.Info) string {
	if i.Uptime > 0 {
		return a.T(kMonSub, i.System, i.Cores, a.uptime(i.Uptime))
	}
	return a.T(kMonSubNoUp, i.System, i.Cores)
}

func (a *App) uptime(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return a.T(kMonDays, int(d/(24*time.Hour)))
	case d >= time.Hour:
		return a.T(kMonHours, int(d/time.Hour))
	}
	return a.T(kMonMinutes, max(int(d/time.Minute), 1))
}

func (a *App) monitorFoot(c *ui.Context, name string) {
	ui.Row(c).Key("mon-foot").Height(statusH).Padding(0, 16).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
		dot(c, a.tk.Green, 7)
		a.caption(c, a.T(kMonFoot, name, a.d.Settings.Values().Monitor.Interval()))
	})
}

func (a *App) monitorBody(c *ui.Context, s *monitor.Series, os model.OSKind) {
	prefs := a.d.Settings.Values().Monitor
	var latest monitor.Sample
	have := false
	if s != nil {
		latest, have = s.Latest()
	}
	if !have {
		msg := a.T(kMonWaiting)
		switch {
		case prefs.Off:
			msg = a.T(kMonOff)
		case s == nil:
			msg = a.T(kMonNoHost)
		default:
			if failing, err := s.Failing(); failing {
				msg = a.T(kMonFailing, err)
			}
		}
		ui.Column(c).Key("mon-empty").Height(240).Center().Children(func() {
			ui.Text(c, msg).FontSize(13).TextColor(a.tk.Text2)
		})
		return
	}
	now := time.Now()
	window := monitorRanges[a.mon.rng]
	samples := s.Window(window, now)
	info, _ := s.Info()
	gpus := len(latest.GPUs) > 0

	a.statCards(c, latest, info, gpus)
	if gpus {
		ui.Row(c).Key("mon-gpu").Gap(12).Height(280).Shrink(0).AlignItems(ui.Stretch).Children(func() {
			a.panel(c, "gpu-chart", iconChip, a.T(kMonCardGPU), a.note(c, gpuHeader(latest, info)), 3, func() {
				a.chart(c, true, samples, window, now, a.gpuLines(samples))
			})
			a.panel(c, "gpu-cards", iconChip, a.T(kMonPerGPU), a.note(c, a.T(kMonPerGPUHint)), 2, func() { a.gpuTable(c, latest) })
		})
	}
	ui.Row(c).Key("mon-charts").Gap(12).Height(220).Shrink(0).AlignItems(ui.Stretch).Children(func() {
		for _, ch := range a.charts() {
			a.panel(c, ch.key, iconChip, a.T(ch.title), a.legend(c, ch.legend...), 1, func() {
				a.chart(c, ch.percent, samples, window, now, ch.lines)
			})
		}
	})
	ui.Row(c).Key("mon-bottom").Gap(12).Shrink(0).AlignItems(ui.Stretch).Children(func() {
		a.panel(c, "disks", iconChip, a.T(kMonDisks), nil, 3, func() { a.diskBars(c, latest) })
		a.panel(c, "about", iconChip, a.T(kMonAbout), nil, 2, func() { a.aboutRows(c, info, latest, os) })
	})
}

func percentOf(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return min(part/whole*100, 100)
}

func gpuHeader(p monitor.Sample, i monitor.Info) string {
	name := ""
	if len(p.GPUs) > 0 {
		name = fmt.Sprintf("%s × %d", p.GPUs[0].Name, len(p.GPUs))
	}
	if i.GPUDriver != "" {
		name += " · " + i.GPUDriver
	}
	return name
}

// statCards is the row of headline numbers.
func (a *App) statCards(c *ui.Context, p monitor.Sample, info monitor.Info, gpus bool) {
	cores := len(p.Cores)
	if cores == 0 {
		cores = info.Cores
	}
	ui.Row(c).Key("mon-cards").Gap(12).Children(func() {
		a.card(c, "cpu", a.tk.Blue, a.T(kMonCardCPU), strconv.Itoa(pct(p.CPU)), "%", a.T(kMonLoad, p.Load[0], cores))
		memV, memU := a.splitBytes(float64(p.MemUsed))
		a.card(c, "mem", a.tk.Purple, a.T(kMonCardMem), memV, memU,
			a.T(kMonMemOf, a.humanBytes(float64(p.MemTotal)), pct(p.MemPercent())))
		if gpus {
			peak := 0.0
			for _, g := range p.GPUs {
				peak = max(peak, g.Util)
			}
			a.card(c, "gpu", a.tk.Teal, a.T(kMonCardGPU), strconv.Itoa(pct(p.GPUUtil())), "%",
				a.T(kMonGPUAvg, len(p.GPUs), pct(peak)))
			used, total := p.GPUMem()
			a.card(c, "vram", a.tk.Red, a.T(kMonCardVRAM), strconv.Itoa(pct(percentOf(float64(used), float64(total)))), "%",
				a.humanBytes(float64(used))+" / "+a.humanBytes(float64(total)))
		}
		ioV, ioU := a.splitBytes(p.DiskRead + p.DiskWrite)
		sub := a.T(kMonIOTotal)
		if p.DiskSplit {
			sub = a.T(kMonRW, a.humanBytes(p.DiskRead), a.humanBytes(p.DiskWrite))
		}
		a.card(c, "io", a.tk.Amber, a.T(kMonCardDisk), ioV, ioU+"/s", sub)
		netV, netU := a.splitBytes(p.NetRx + p.NetTx)
		a.card(c, "net", a.tk.Green, a.T(kMonCardNet), netV, netU+"/s", a.T(kMonDownUp, a.humanRate(p.NetRx), a.humanRate(p.NetTx)))
	})
}

// card is one stat card: a tinted tile, its title, the number and a note.
func (a *App) card(c *ui.Context, key string, tint ui.Color, title, value, unit, note string) {
	ui.Column(c).Key("card-"+key).Grow(1).Basis(0).MinWidth(0).Padding(12, 14).Gap(6).
		Background(a.tk.Surface).Radius(16).Border(1, a.tk.Border).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(18, 18).Radius(5).Background(tint.Alpha(0.18)).Center().Children(func() {
				ui.Icon(c, iconChip).FontSize(11).TextColor(tint)
			})
			ui.Text(c, title).FontSize(11.5).Bold().TextColor(a.tk.Text2).NoWrap()
		})
		ui.Row(c).Gap(4).AlignItems(ui.Baseline).Children(func() {
			ui.Text(c, value).Bold().FontSize(28).LetterSpacing(-0.56).NoWrap()
			ui.Text(c, unit).FontSize(12).TextColor(a.tk.Text2).NoWrap()
		})
		a.caption(c, note).NoWrap()
	})
}

// panel is a titled box that holds a chart or a list; weight is its share of
// the row.
func (a *App) panel(c *ui.Context, key string, ic *ui.SVG, title string, right func(), weight float32, body func()) {
	ui.Column(c).Key("panel-"+key).Grow(weight).Basis(0).MinWidth(0).Padding(12, 14).Gap(8).
		Background(a.tk.Surface).Radius(16).Border(1, a.tk.Border).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Icon(c, ic).FontSize(14).TextColor(a.tk.Text2)
			ui.Text(c, title).Bold().FontSize(12.5).NoWrap()
			ui.Spacer(c)
			if right != nil {
				right()
			}
		})
		ui.Column(c).Grow(1).MinHeight(0).Children(body)
	})
}

// legendItem is one entry of a chart legend.
type legendItem struct {
	key string
	col ui.Color
}

// legend draws colored dots with names, the right side of a chart's title.
func (a *App) legend(c *ui.Context, items ...legendItem) func() {
	return func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			for _, it := range items {
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					dot(c, it.col, 7)
					a.caption(c, a.T(it.key)).NoWrap()
				})
			}
		})
	}
}

// note is plain small text on the right of a panel's title.
func (a *App) note(c *ui.Context, text string) func() {
	return func() { a.caption(c, text).NoWrap().Ellipsis("…") }
}

func (a *App) diskBars(c *ui.Context, p monitor.Sample) {
	if len(p.Disks) == 0 {
		return
	}
	ui.Column(c).Gap(10).Children(func() {
		for _, d := range p.Disks {
			ui.Row(c).Key("disk-" + d.Mount).Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Text(c, d.Mount).FontSize(12).Bold().Width(110).NoWrap().Ellipsis("…")
				ui.Row(c).Grow(1).Height(6).Radius(3).Background(a.tk.Surface3).Clip().Children(func() {
					ui.Box(c).BasisPercent(float32(d.Percent())).Height(6).Background(a.level(d.Percent()))
				})
				ui.Text(c, fmt.Sprintf("%s / %s · %d%%", a.humanBytes(float64(d.Used)), a.humanBytes(float64(d.Total)), pct(d.Percent()))).
					FontSize(11.5).TextColor(a.tk.Text2).Width(190).NoWrap()
			})
		}
	})
}

func (a *App) aboutRows(c *ui.Context, info monitor.Info, p monitor.Sample, os model.OSKind) {
	via := a.T(kMonViaLinux)
	switch {
	case os == model.OSMacOS:
		via = a.T(kMonViaMac)
	case os == model.OSWindows:
		via = a.T(kMonViaWin)
	case len(p.GPUs) > 0 && strings.Contains(strings.ToLower(info.GPUDriver+p.GPUs[0].Name), "radeon"):
		via = a.T(kMonViaROCm)
	case len(p.GPUs) > 0:
		via = a.T(kMonViaNvidia)
	}
	rows := [][2]string{
		{a.T(kMonAboutSystem), info.System},
		{a.T(kMonAboutKernel), info.Kernel},
		{a.T(kMonAboutCPU), info.CPU},
		{a.T(kMonAboutMethod), via},
	}
	ui.Column(c).Gap(7).Children(func() {
		for _, r := range rows {
			if r[1] == "" {
				continue
			}
			ui.Row(c).Key("about-" + r[0]).Gap(10).Children(func() {
				a.caption(c, r[0]).Width(64).NoWrap()
				ui.Text(c, r[1]).FontSize(12).TextColor(a.tk.Text).Grow(1).MinWidth(0).NoWrap().Ellipsis("…")
			})
		}
	})
}

var gpuPalette = func(a *App) []ui.Color {
	t := a.tk
	return []ui.Color{t.Red, t.Amber, t.Green, t.Teal, t.Blue, t.Purple, t.Accent, t.Text2}
}

func (a *App) gpuTable(c *ui.Context, p monitor.Sample) {
	pal := gpuPalette(a)
	ui.Column(c).Gap(7).Children(func() {
		for i, g := range p.GPUs {
			col := pal[i%len(pal)]
			ui.Row(c).Key(fmt.Sprintf("gpu-%d", g.Index)).Gap(9).AlignItems(ui.Center).Children(func() {
				dot(c, col, 7)
				ui.Text(c, fmt.Sprintf("GPU %d", g.Index)).FontSize(11.5).TextColor(a.tk.Text2).Width(46).NoWrap()
				ui.Row(c).Grow(1).Height(5).Radius(3).Background(a.tk.Surface3).Clip().Children(func() {
					ui.Box(c).BasisPercent(float32(g.Util)).Height(5).Background(a.tk.Teal)
				})
				ui.Text(c, fmt.Sprintf("%d%%", pct(g.Util))).FontSize(11.5).Bold().Width(34).NoWrap()
				temp := ""
				tempCol := a.tk.Text2
				if g.TempC > 0 {
					temp = fmt.Sprintf("%d°C", pct(g.TempC))
					if g.TempC >= 80 {
						tempCol = a.tk.Amber
					}
				}
				ui.Text(c, temp).FontSize(11.5).Bold().TextColor(tempCol).Width(42).NoWrap()
				ui.Text(c, a.humanBytes(float64(g.MemUsed))+" / "+a.humanBytes(float64(g.MemTotal))).
					FontSize(11).TextColor(a.tk.Text2).Width(108).NoWrap()
			})
		}
	})
}

func (a *App) gpuLines(samples []monitor.Sample) []line {
	pal := gpuPalette(a)
	n := 0
	for _, s := range samples {
		n = max(n, len(s.GPUs))
	}
	lines := make([]line, n)
	for i := range n {
		lines[i] = line{color: pal[i%len(pal)], at: func(p monitor.Sample) float64 {
			if i < len(p.GPUs) {
				return p.GPUs[i].Util
			}
			return 0
		}}
	}
	return lines
}

// chartSpec is one of the three small charts.
type chartSpec struct {
	key, title string
	percent    bool
	legend     []legendItem
	lines      []line
}

// charts lists the CPU, memory and network charts.
func (a *App) charts() []chartSpec {
	return []chartSpec{
		{
			key: "cpu", title: kMonCardCPU, percent: true,
			legend: []legendItem{{kMonUser, a.tk.Blue}, {kMonSystem, a.tk.Purple}},
			lines: []line{
				{color: a.tk.Blue, fill: true, at: func(p monitor.Sample) float64 { return p.CPU }},
				{color: a.tk.Purple, at: func(p monitor.Sample) float64 { return p.CPUSystem }},
			},
		},
		{
			key: "mem", title: kMonCardMem, percent: true,
			legend: []legendItem{{kMonUsed, a.tk.Purple}, {kMonCached, a.tk.Text3}},
			lines: []line{
				{color: a.tk.Purple, fill: true, at: func(p monitor.Sample) float64 { return p.MemPercent() }},
				{color: a.tk.Text3, at: func(p monitor.Sample) float64 {
					return percentOf(float64(p.MemUsed+p.MemCached), float64(p.MemTotal))
				}},
			},
		},
		{
			key: "net", title: kMonCardNet,
			legend: []legendItem{{kMonDown, a.tk.Green}, {kMonUp, a.tk.Blue}},
			lines: []line{
				{color: a.tk.Green, fill: true, at: func(p monitor.Sample) float64 { return p.NetRx }},
				{color: a.tk.Blue, at: func(p monitor.Sample) float64 { return p.NetTx }},
			},
		},
	}
}
