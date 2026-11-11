package ui

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/monitor"
	"github.com/wei-dong/tako/internal/workspace"
)

// monitorRanges are the time windows of the monitor page's charts.
var monitorRanges = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// monitorUI is the state of monitoring in the window.
type monitorUI struct {
	// host is the machine whose page is open; empty shows the blocks.
	host model.ID
	// rng indexes monitorRanges.
	rng int
	// blocks records which block's session feeds each watched host.
	blocks map[model.ID]*workspace.Block
	// paused and interval mirror what the hub was last told.
	paused   bool
	interval int
}

// syncMonitors makes the hub watch exactly the hosts that have a block, over
// the session of one of their blocks, and applies the settings. It runs every
// frame and only does map lookups when nothing changed.
func (a *App) syncMonitors() {
	hub := a.d.Monitor
	if hub == nil {
		return
	}
	m := &a.mon
	prefs := a.d.Settings.Values().Monitor
	if prefs.Off != m.paused {
		m.paused = prefs.Off
		hub.SetPaused(prefs.Off)
	}
	if iv := prefs.Interval(); iv != m.interval {
		m.interval = iv
		hub.SetInterval(time.Duration(iv) * time.Second)
	}
	if m.blocks == nil {
		m.blocks = map[model.ID]*workspace.Block{}
	}
	alive := map[model.ID]bool{}
	for _, b := range a.d.Model.Blocks() {
		if b.Kind != workspace.KindSSH {
			continue
		}
		alive[b.HostID] = true
		if cur := m.blocks[b.HostID]; cur != nil && a.d.Model.IndexOf(cur) >= 0 && hub.Watching(b.HostID) {
			continue
		}
		sess := b.Session()
		if sess == nil {
			continue
		}
		m.blocks[b.HostID] = b
		hub.Watch(monitor.Target{Host: b.HostID, OS: b.OS, Exec: sess.Exec, Ping: sess.Ping})
	}
	for id := range m.blocks {
		if !alive[id] {
			hub.Forget(id)
			delete(m.blocks, id)
			if m.host == id {
				a.closeMonitor()
			}
		}
	}
}

// openMonitor shows a host's monitor page.
func (a *App) openMonitor(id model.ID) {
	a.mon.host = id
	if a.d.Monitor != nil {
		a.d.Monitor.SetFocus(id)
	}
}

// closeMonitor returns to the blocks.
func (a *App) closeMonitor() {
	a.mon.host = ""
	if a.d.Monitor != nil {
		a.d.Monitor.SetFocus("")
	}
}

// level colors a percentage by the user's thresholds.
func (a *App) level(p float64) ui.Color {
	prefs := a.d.Settings.Values().Monitor
	switch {
	case p >= float64(prefs.Crit()):
		return a.tk.Red
	case p >= float64(prefs.Warn()):
		return a.tk.Amber
	}
	return a.tk.Green
}

// blockFooter holds the block's resource readings: CPU, memory, disk and, when
// the machine has them, GPUs, each as a small bar with its percentage. Clicking
// it opens the monitor page.
func (a *App) blockFooter(c *ui.Context, b *workspace.Block) {
	row := ui.Row(c).Key("foot").Height(blockFoot).Padding(0, 12).Gap(14).Background(a.tk.Surface2).Shrink(0)
	row.AlignItems(ui.Center)
	if b.Kind == workspace.KindSSH {
		row.Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(a.T(kMonOpen, b.Name))
	}
	var s *monitor.Series
	if a.d.Monitor != nil && b.Kind == workspace.KindSSH {
		s = a.d.Monitor.Series(b.HostID)
	}
	var p monitor.Sample
	have := false
	failing := false
	if s != nil {
		p, have = s.Latest()
		failing, _ = s.Failing()
	}
	row.Children(func() {
		type meter struct {
			label string
			value float64
		}
		meters := []meter{{a.T(kMetricCPU), p.CPU}, {a.T(kMetricMem), p.MemPercent()}, {a.T(kMetricDisk), p.DiskMax()}}
		if len(p.GPUs) > 0 {
			meters = append(meters, meter{a.T(kMetricGPU), p.GPUUtil()})
		}
		if !have {
			meters = meters[:3]
		}
		for _, m := range meters {
			a.meter(c, m.label, m.value, have && !failing)
		}
		if have && p.Latency > 0 {
			ui.Spacer(c)
			a.caption(c, fmt.Sprintf("%d ms", max(int(p.Latency.Round(time.Millisecond)/time.Millisecond), 1)))
		}
	})
	if b.Kind == workspace.KindSSH && row.Clicked() {
		a.openMonitor(b.HostID)
	}
}

// meter is one labelled bar of the footer; without a reading it is a dash.
func (a *App) meter(c *ui.Context, label string, value float64, ok bool) {
	ui.Row(c).Key("m-" + label).Gap(5).AlignItems(ui.Center).Children(func() {
		a.caption(c, label)
		if !ok {
			ui.Text(c, "—").FontSize(11).Bold().TextColor(a.tk.Text3)
			return
		}
		ui.Box(c).Width(40).Height(4).Radius(2).Background(a.tk.Surface4).Clip().Children(func() {
			ui.Box(c).Width(max(40*float32(value)/100, 2)).Height(4).Background(a.level(value))
		})
		ui.Text(c, fmt.Sprintf("%d%%", pct(value))).FontSize(11).Bold().TextColor(a.tk.Text).Width(30)
	})
}

// overlayOpen reports whether a dialog, the palette or a sheet is showing,
// which then owns Escape.
func (a *App) overlayOpen() bool {
	return a.palette.open || a.settings.open || a.sheet.open || a.xfer.open
}

// toggleMonitor opens the focused SSH block's monitor, or closes the page.
func (a *App) toggleMonitor(b *workspace.Block) {
	if a.mon.host != "" {
		a.closeMonitor()
		return
	}
	if b == nil || b.Kind != workspace.KindSSH {
		a.toast(a.T(kMonNoHost))
		return
	}
	a.openMonitor(b.HostID)
}
