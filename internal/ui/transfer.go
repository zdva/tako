package ui

import (
	"context"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/settings"
	"github.com/wei-dong/tako/internal/transfer"
	"github.com/wei-dong/tako/internal/workspace"
)

// xferPane is one side of the transfer sheet: a host and a folder on it.
type xferPane struct {
	host model.ID // empty is this computer
	// choice is the host chooser's text.
	choice string
	path   string
	// input is the editable path field.
	input   string
	entries []transfer.Entry
	loading bool
	err     string
	// gen tells a listing that finished late that it is out of date.
	gen int
	// checked holds the selection of the source pane by entry name.
	checked map[string]*bool
	// dropped is set while a drop lands, for tests and the highlight.
	hot bool
}

// transferUI is the state of the transfer sheet.
type transferUI struct {
	open     bool
	src, dst xferPane
	// mode is 0 for relay, 1 for direct.
	mode   int
	resume bool
	// cancel stops listings still in flight.
	cancel context.CancelFunc
	ctx    context.Context //nolint:containedctx // the sheet's lifetime: every folder listing it starts ends with it
	// ready is set once the panes have their first folders.
	ready bool
}

// remote reports whether the pane shows a host's files.
func (p *xferPane) remote() bool { return p.host != "" }

// join appends a name to a folder in the style of the pane's file system.
func (p *xferPane) join(name string) string {
	if p.remote() {
		return path.Join(p.path, name)
	}
	return filepath.Join(p.path, name)
}

// openTransfer shows the transfer sheet. With a host, that host is the
// source; otherwise the focused block's machine is.
func (a *App) openTransfer(from model.ID) {
	x := &a.xfer
	if a.d.Files == nil {
		a.toast(a.T(kSoon))
		return
	}
	if x.cancel != nil {
		x.cancel()
	}
	x.ctx, x.cancel = context.WithCancel(context.Background())
	x.open, x.ready, x.resume = true, false, true
	x.mode = 0
	src, dst := from, model.ID("")
	focus := a.d.Model.Focused()
	if src == "" && focus != nil && focus.Kind == workspace.KindSSH {
		src = focus.HostID
	}
	// The target is another machine that has a block, else this computer.
	for _, b := range a.d.Model.Blocks() {
		if b.Kind == workspace.KindSSH && b.HostID != src {
			dst = b.HostID
			break
		}
	}
	a.setPane(&x.src, src, a.paneDir(src))
	a.setPane(&x.dst, dst, a.paneDir(dst))
}

// paneDir is the folder a shell on the machine last reported (OSC 7), or
// empty when no block is on it or none reported one. The focused block wins.
func (a *App) paneDir(h model.ID) string {
	dir := ""
	focus := a.d.Model.Focused()
	for _, b := range a.d.Model.Blocks() {
		if (h == "") != (b.Kind == workspace.KindLocal) || (h != "" && b.HostID != h) {
			continue
		}
		t := b.ActiveTab()
		if t == nil || t.Dir() == "" {
			continue
		}
		if dir = t.Dir(); b == focus {
			break
		}
	}
	return dir
}

// setPane points a pane at a host and a folder (the home folder when dir is
// empty) and lists it.
func (a *App) setPane(p *xferPane, host model.ID, dir string) {
	p.host, p.choice = host, a.hostName(host)
	p.checked = map[string]*bool{}
	p.path, p.input = dir, dir
	a.listPane(p)
}

// listPane reads the pane's folder in the background.
func (a *App) listPane(p *xferPane) {
	p.gen++
	gen, host, dir := p.gen, p.host, p.path
	p.loading, p.err = true, ""
	x := &a.xfer
	ctx := x.ctx
	files := a.d.Files
	go func() {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		loc := transfer.Location{Host: host}
		var (
			entries []transfer.Entry
			err     error
		)
		ep, err := files.Endpoint(ctx, loc)
		if err == nil {
			if dir == "" {
				dir, err = ep.FS().Home()
			}
		}
		if err == nil {
			entries, err = transfer.List(ctx, ep, dir)
		}
		if err != nil {
			files.Reset(loc)
		}
		a.Post(func() {
			if p.gen != gen {
				return
			}
			p.loading = false
			if err != nil {
				p.err = err.Error()
				return
			}
			p.path, p.input, p.entries = dir, dir, entries
			p.checked = map[string]*bool{}
		})
	}()
}

// enter opens a folder of the pane.
func (a *App) enter(p *xferPane, dir string) {
	p.path = dir
	a.listPane(p)
}

// up goes to the parent folder.
func (a *App) up(p *xferPane) {
	if p.remote() {
		a.enter(p, path.Dir(p.path))
		return
	}
	a.enter(p, filepath.Dir(p.path))
}

// selected lists the checked entries of the source pane in order.
func (x *transferUI) selected() []transfer.Entry {
	var out []transfer.Entry
	for _, e := range x.src.entries {
		if v := x.src.checked[e.Name]; v != nil && *v {
			out = append(out, e)
		}
	}
	return out
}

// directOK reports whether the current panes can use direct mode.
func (a *App) directOK() (allowed bool, why string) {
	x := &a.xfer
	switch {
	case !a.d.Settings.Values().Transfer.AllowDirect:
		return false, a.T(kXferDirectOff)
	case x.src.host == "" || x.dst.host == "" || x.src.host == x.dst.host:
		return false, a.T(kXferDirectNeed)
	}
	return true, ""
}

// startTransfer queues one job per checked entry.
func (a *App) startTransfer() {
	x := &a.xfer
	prefs := a.d.Settings.Values().Transfer
	for _, e := range x.selected() {
		spec := transfer.Spec{
			Src:    transfer.Location{Host: x.src.host, Path: x.src.join(e.Name)},
			Dst:    transfer.Location{Host: x.dst.host, Path: x.dst.path},
			Policy: policyOf(prefs.Policy()),
			Verify: verifyOf(prefs.Verify),
			Fresh:  !x.resume,
		}
		if x.mode == 1 {
			spec.Mode = transfer.ModeDirect
		}
		if _, err := a.d.Transfers.Add(spec); err != nil {
			a.toast(err.Error())
			return
		}
	}
	x.src.checked = map[string]*bool{}
}

// upload sends files dropped on the target pane.
func (a *App) upload(paths []string) {
	x := &a.xfer
	prefs := a.d.Settings.Values().Transfer
	if x.dst.host == "" {
		return
	}
	for _, p := range paths {
		spec := transfer.Spec{
			Src:    transfer.Location{Path: p},
			Dst:    transfer.Location{Host: x.dst.host, Path: x.dst.path},
			Policy: policyOf(prefs.Policy()),
			Verify: verifyOf(prefs.Verify),
			Fresh:  !x.resume,
		}
		if _, err := a.d.Transfers.Add(spec); err != nil {
			a.toast(err.Error())
			return
		}
	}
}

// hostName names a machine for display.
func (a *App) hostName(h model.ID) string {
	if h == "" {
		return a.T(kXferLocal)
	}
	if host, ok := a.d.Library.Get(h); ok {
		return host.Name
	}
	return string(h)
}

// transferSheet is the file transfer dialog.
func (a *App) transferSheet(c *ui.Context) {
	x := &a.xfer
	a.xferOpen.Store(x.open)
	if !x.open {
		return
	}
	a.dialog(c, &x.open, func() { a.transferBody(c) })
	if !x.open && x.cancel != nil {
		x.cancel()
	}
}

func (a *App) transferBody(c *ui.Context) {
	x := &a.xfer
	winW, winH := c.Size()
	ui.Column(c).Key("xfer").Width(min(920, winW-48)).MaxHeight(winH-48).Background(a.tk.Surface).Radius(18).
		Border(1, a.tk.BorderStrong).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.18)).Clip().Children(func() {
		ui.Row(c).Padding(16, 20).Gap(12).Shrink(0).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(36, 36).Radius(10).Background(a.tk.Amber).Center().Children(func() {
				ui.Icon(c, iconTransfer).FontSize(20).TextColor(ui.RGB(255, 255, 255))
			})
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, a.T(kXferTitle)).Bold().FontSize(17)
				a.caption(c, a.T(kXferSub))
			})
			if a.iconButton(c, iconClose, a.T(kPalClose), 28).Clicked() {
				x.open = false
			}
		})
		ui.Divider(c)
		scroll := ui.Scroll(c).Key("xfer-scroll").Grow(1).MinHeight(0)
		scroll.Children(func() {
			ui.Column(c).Padding(14, 20, 16).Gap(12).Children(func() {
				ui.Row(c).Gap(14).Children(func() {
					a.pane(c, &x.src, true)
					if a.iconButton(c, iconSwap, a.T(kXferSwap), 30).AlignSelf(ui.Center).Clicked() {
						a.swapPanes()
					}
					a.pane(c, &x.dst, false)
				})
				a.transferActions(c)
				a.queuePanel(c)
			})
		})
	})
}

func (a *App) swapPanes() {
	x := &a.xfer
	x.src, x.dst = x.dst, x.src
	x.src.gen++
	x.dst.gen++
	x.src.checked = map[string]*bool{}
	a.listPane(&x.src)
	a.listPane(&x.dst)
}

// pane draws one side: the host chooser, the path and the file table.
func (a *App) pane(c *ui.Context, p *xferPane, source bool) {
	key, title := "xfer-dst", a.T(kXferDst)
	if source {
		key, title = "xfer-src", a.T(kXferSrc)
	}
	// A chooser change moves the pane to that host.
	opts := []string{a.T(kXferLocal)}
	ids := []model.ID{""}
	for _, h := range a.d.Library.Hosts() {
		opts = append(opts, h.Name)
		ids = append(ids, h.ID)
	}
	box := ui.Column(c).Key(key).Grow(1).BasisPercent(50).MinWidth(0).Gap(8)
	box.Children(func() {
		a.caption(c, title)
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Select(c, &p.choice, opts).Label(title).Width(170)
			if p.choice != a.hostName(p.host) {
				if i := slices.Index(opts, p.choice); i >= 0 {
					p.host = ids[i]
					p.path, p.input = "", ""
					p.checked = map[string]*bool{}
					a.listPane(p)
				}
			}
			if a.iconButton(c, iconUp, a.T(kXferUp), 28).Clicked() {
				a.up(p)
			}
			in := ui.TextInput(c, &p.input).Label(a.T(kXferName)).Grow(1).MinWidth(0).FontSize(12)
			if in.Submitted() && p.input != "" {
				a.enter(p, p.input)
			}
		})
		a.fileTable(c, p, source)
	})
	if !source {
		if files := box.DroppedFiles(); len(files) > 0 {
			a.upload(files)
		}
		p.hot = box.FileDragOver()
	}
}

// fileTable lists a folder.
func (a *App) fileTable(c *ui.Context, p *xferPane, source bool) {
	ui.Column(c).Height(232).Background(a.tk.Surface2).Radius(10).Border(1, a.tk.Border).Clip().Children(func() {
		ui.Row(c).Height(26).Padding(0, 10).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
			if source {
				ui.Box(c).Width(18)
			}
			a.caption(c, a.T(kXferName)).Grow(1)
			a.caption(c, a.T(kXferSize)).Width(64)
			a.caption(c, a.T(kXferModified)).Width(96)
		})
		ui.Divider(c)
		body := ui.Scroll(c).Key("files-" + strconv.FormatBool(source)).Grow(1).MinHeight(0)
		body.Children(func() {
			switch {
			case p.loading:
				ui.Text(c, a.T(kXferLoading)).Padding(12).TextColor(a.tk.Text3).FontSize(12)
			case p.err != "":
				ui.Text(c, a.T(kXferListErr, p.err)).Padding(12).TextColor(a.tk.Red).FontSize(12)
			default:
				a.fileRows(c, p, source)
			}
		})
		if !source && p.hot {
			a.dropHint(c, p)
		}
	})
}

// dropHint covers the target while files are dragged over it.
func (a *App) dropHint(c *ui.Context, p *xferPane) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(a.tk.AccentSoft).Border(2, a.tk.Accent).Radius(10).Center().Children(func() {
		ui.Text(c, a.T(kXferDrop, a.hostName(p.host)+":"+p.path)).Bold().FontSize(13).TextColor(a.tk.Accent)
	})
}

func (a *App) fileRows(c *ui.Context, p *xferPane, source bool) {
	a.fileRow(c, p, transfer.Entry{Name: "..", Dir: true}, source, true)
	for _, e := range p.entries {
		a.fileRow(c, p, e, source, false)
	}
	if len(p.entries) == 0 {
		ui.Text(c, a.T(kXferEmpty)).Padding(12).TextColor(a.tk.Text3).FontSize(12)
	}
}

func (a *App) fileRow(c *ui.Context, p *xferPane, e transfer.Entry, source, parent bool) {
	row := ui.Row(c).Key("f:"+e.Name).Height(28).Padding(0, 10).Gap(8).AlignItems(ui.Center).Shrink(0).Cursor(ui.CursorPointer)
	row.Children(func() {
		if source {
			if parent {
				ui.Box(c).Width(18)
			} else {
				v := p.checked[e.Name]
				if v == nil {
					v = new(bool)
					p.checked[e.Name] = v
				}
				ui.Checkbox(c, v, "").Label(e.Name)
			}
		}
		ic, col := iconFile, a.tk.Text3
		if e.Dir {
			ic, col = iconFolder, a.tk.Blue
		}
		ui.Icon(c, ic).FontSize(14).TextColor(col)
		ui.Text(c, e.Name).FontSize(12.5).Grow(1).MinWidth(0).Ellipsis("…").SingleLine()
		if !parent {
			size := ""
			if !e.Dir {
				size = a.humanBytes(float64(e.Size))
			}
			a.caption(c, size).Width(64)
			a.caption(c, a.when(e.ModTime)).Width(96)
		}
	})
	if e.Dir && row.DoubleClicked() {
		if parent {
			a.up(p)
		} else {
			a.enter(p, p.join(e.Name))
		}
	}
}

// when formats a modification time the short way: the time today, else the
// date.
func (a *App) when(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	now := time.Now()
	switch {
	case sameDay(t, now):
		return a.T(kXferToday, a.loc.Clock(t))
	case sameDay(t, now.AddDate(0, 0, -1)):
		return a.T(kXferYesterday, a.loc.Clock(t))
	}
	return a.loc.Date(t)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

// transferActions is the summary line, the method and the start button.
func (a *App) transferActions(c *ui.Context) {
	x := &a.xfer
	sel := x.selected()
	var total int64
	for _, e := range sel {
		total += e.Size
	}
	ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
		summary := a.T(kXferNoneSelected)
		if len(sel) > 0 {
			summary = a.T(kXferSelected, len(sel), a.humanBytes(float64(total)))
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, summary).Bold().FontSize(13).Ellipsis("…").SingleLine().Key("xfer-summary")
			a.caption(c, a.hostName(x.src.host)+" → "+a.hostName(x.dst.host))
		})
		a.caption(c, a.T(kXferMethod))
		mode := x.mode
		ui.Segmented(c, &x.mode, a.T(kXferRelay), a.T(kXferDirect)).Label(a.T(kXferMethod))
		if x.mode != mode && x.mode == 1 {
			if ok, why := a.directOK(); !ok {
				x.mode = 0
				a.toast(why)
			}
		}
		ui.Switch(c, &x.resume).Label(a.T(kXferResume))
		a.caption(c, a.T(kXferResume))
		start := ui.PrimaryButton(c, a.T(kXferStart)+"  ↵").Disabled(len(sel) == 0 || a.d.Transfers == nil)
		if start.Clicked() {
			a.startTransfer()
		}
	})
}

// counts splits the jobs by what the header shows.
func counts(jobs []transfer.JobInfo) (running, paused, done int) {
	for _, j := range jobs {
		switch j.State {
		case transfer.StateQueued, transfer.StateRunning, transfer.StateAsking:
			running++
		case transfer.StatePaused:
			paused++
		case transfer.StateDone:
			done++
		case transfer.StateFailed, transfer.StateCanceled:
		}
	}
	return running, paused, done
}

// orderJobs puts running jobs first, then paused, then finished, newest
// first within each.
func orderJobs(jobs []transfer.JobInfo) []transfer.JobInfo {
	rank := func(s transfer.State) int {
		switch s {
		case transfer.StateQueued, transfer.StateRunning, transfer.StateAsking:
			return 0
		case transfer.StatePaused, transfer.StateFailed:
			return 1
		case transfer.StateDone, transfer.StateCanceled:
		}
		return 2
	}
	out := slices.Clone(jobs)
	slices.SortStableFunc(out, func(x, y transfer.JobInfo) int {
		if d := rank(x.State) - rank(y.State); d != 0 {
			return d
		}
		return y.ID - x.ID
	})
	return out
}

// queuePanel lists the jobs.
func (a *App) queuePanel(c *ui.Context) {
	var jobs []transfer.JobInfo
	if a.d.Transfers != nil {
		jobs = orderJobs(a.d.Transfers.Snapshot())
	}
	running, paused, done := counts(jobs)
	ui.Column(c).Key("xfer-queue").Gap(8).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.T(kXferQueue)).Bold().FontSize(13)
			a.caption(c, a.T(kXferRunning, running))
			a.caption(c, a.T(kXferPaused, paused))
			a.caption(c, a.T(kXferDone, done))
			ui.Spacer(c)
			clearBtn := ui.Button(c, a.T(kXferClear)).FontSize(12).Height(26).Padding(0, 10).Disabled(done == 0)
			if clearBtn.Clicked() {
				a.d.Transfers.ClearFinished()
			}
		})
		if len(jobs) == 0 {
			ui.Text(c, a.T(kXferNoJobs)).FontSize(12).TextColor(a.tk.Text3).Padding(8, 0)
		}
		for _, j := range jobs {
			a.jobRow(c, j)
		}
	})
}

// route writes where a job goes: "web-01:/var/db → db-01:/data · via this computer".
func (a *App) route(j transfer.JobInfo) string {
	via := a.T(kXferViaRelay)
	if j.Spec.Mode == transfer.ModeDirect {
		via = a.T(kXferViaDirect)
	}
	src := a.hostName(j.Spec.Src.Host) + ":" + j.Spec.Src.Path
	dst := a.hostName(j.Spec.Dst.Host) + ":" + j.Spec.Dst.Path
	return src + " → " + dst + " · " + via
}

// kindOf is the tag of a job: server to server, upload or download.
func (a *App) kindOf(j transfer.JobInfo) (label string, col ui.Color, ic *ui.SVG) {
	switch {
	case j.Spec.Src.Local() && !j.Spec.Dst.Local():
		return a.T(kXferTagUp), a.tk.Blue, iconUp
	case !j.Spec.Src.Local() && j.Spec.Dst.Local():
		return a.T(kXferTagDown), a.tk.Green, iconDown
	}
	return a.T(kXferTagS2S), a.tk.Purple, iconSwap
}

func (a *App) jobRow(c *ui.Context, j transfer.JobInfo) {
	tag, col, ic := a.kindOf(j)
	ui.Row(c).Key(fmt.Sprintf("job%d", j.ID)).Gap(12).Padding(10, 12).Radius(10).Background(a.tk.Surface2).
		Border(1, a.tk.Border).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c).Size(30, 30).Radius(8).Background(col.Alpha(0.14)).Center().Children(func() {
			ui.Icon(c, ic).FontSize(16).TextColor(col)
		})
		ui.Column(c).Grow(1).BasisPercent(46).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, j.Name).Bold().FontSize(13).Ellipsis("…").SingleLine().MinWidth(0)
				if j.Total > 0 {
					a.caption(c, a.humanBytes(float64(j.Total)))
				}
				a.pill(c, tag, col, col.Alpha(0.14))
				if j.Resumed {
					a.pill(c, a.T(kXferTagResumed), a.tk.Amber, a.tk.Amber.Alpha(0.16))
				}
			})
			a.caption(c, a.route(j)).Ellipsis("…").SingleLine()
		})
		ui.Column(c).Grow(1).BasisPercent(54).MinWidth(0).Gap(4).Children(func() {
			a.jobStatus(c, j)
		})
		a.jobButtons(c, j)
	})
}

// jobStatus is the progress bar and its text, or the question of a conflict.
func (a *App) jobStatus(c *ui.Context, j transfer.JobInfo) {
	q := a.d.Transfers
	if j.State == transfer.StateAsking && j.Conflict != nil {
		a.caption(c, a.T(kXferAsk, path.Base(j.Conflict.Path), a.humanBytes(float64(j.Conflict.SrcSize)), a.humanBytes(float64(j.Conflict.DstSize)))).
			Ellipsis("…").SingleLine().TextColor(a.tk.Amber)
		ui.Row(c).Gap(6).Children(func() {
			for _, o := range []struct {
				key string
				d   transfer.Decision
			}{
				{kXferOverwrite, transfer.Decision{Overwrite: true}},
				{kXferSkip, transfer.Decision{}},
				{kXferOverwriteAll, transfer.Decision{Overwrite: true, All: true}},
				{kXferSkipAll, transfer.Decision{All: true}},
			} {
				if ui.Button(c, a.T(o.key)).FontSize(11.5).Height(24).Padding(0, 8).Clicked() {
					_ = q.Resolve(j.ID, o.d)
				}
			}
		})
		return
	}
	frac := 0.0
	if j.Total > 0 {
		frac = min(float64(j.Done)/float64(j.Total), 1)
	}
	barCol := a.tk.Accent
	text := ""
	switch j.State {
	case transfer.StateQueued:
		text = a.T(kXferQueued)
	case transfer.StateRunning:
		text = a.T(kXferProgress, a.humanBytes(float64(j.Done)), a.humanBytes(float64(j.Total)))
		if eta := j.ETA(); eta > 0 {
			text += " · " + a.T(kXferLeft, a.humanDuration(eta))
		}
		if j.Speed > 0 {
			text += " · " + a.humanRate(j.Speed)
		}
		if j.Attempt > 0 {
			text = a.T(kXferRetrying, j.Attempt)
			barCol = a.tk.Amber
		}
	case transfer.StatePaused:
		barCol = a.tk.Amber
		key := kXferPausedAt
		if j.Reason == transfer.ReasonNetwork {
			key = kXferNetLost
		}
		text = a.T(key, a.humanBytes(float64(j.Resumable)))
	case transfer.StateDone:
		barCol, frac = a.tk.Green, 1
		text = a.T(kXferDoneJustNow)
		if ago := time.Since(j.Finished); ago >= 10*time.Second {
			text = a.T(kXferDoneAgo, a.humanDuration(ago))
		}
		text += " · " + a.verifiedText(j)
		if j.Skipped > 0 {
			text += " · " + a.T(kXferSkipped, j.Skipped)
		}
	case transfer.StateFailed:
		barCol = a.tk.Red
		text = a.T(kXferFailed, j.Err)
	case transfer.StateCanceled:
		barCol = a.tk.Text3
		text = a.T(kXferCanceled)
	case transfer.StateAsking:
	}
	ui.Row(c).Height(5).Radius(3).Background(a.tk.Surface3).Clip().Children(func() {
		ui.Box(c).BasisPercent(float32(frac * 100)).Height(5).Background(barCol)
	})
	a.caption(c, text).Ellipsis("…").SingleLine()
}

func (a *App) verifiedText(j transfer.JobInfo) string {
	if j.Spec.Verify == transfer.VerifyHash || j.Resumed {
		return a.T(kXferVerified)
	}
	return a.T(kXferComplete)
}

// jobButtons are the icon buttons at the right of a row.
func (a *App) jobButtons(c *ui.Context, j transfer.JobInfo) {
	q := a.d.Transfers
	key := strconv.Itoa(j.ID)
	ui.Row(c).Gap(2).Shrink(0).Children(func() {
		switch j.State {
		case transfer.StateRunning, transfer.StateQueued:
			if a.iconButton(c, iconPause, a.T(kXferPause)+" "+key, 28).Clicked() {
				_ = q.Pause(j.ID)
			}
		case transfer.StatePaused, transfer.StateFailed:
			lbl := a.T(kXferContinue)
			if j.State == transfer.StateFailed {
				lbl = a.T(kXferRetry)
			}
			if a.iconButton(c, iconPlay, lbl+" "+key, 28).Clicked() {
				_ = q.Resume(j.ID)
			}
		case transfer.StateDone:
			if a.iconButton(c, iconFolder, a.T(kXferShow)+" "+key, 28).Clicked() {
				a.showInTarget(j)
			}
		case transfer.StateAsking, transfer.StateCanceled:
		}
		switch j.State {
		case transfer.StateDone, transfer.StateCanceled:
			if a.iconButton(c, iconClose, a.T(kXferRemove)+" "+key, 28).Clicked() {
				q.Remove(j.ID)
			}
		case transfer.StateQueued, transfer.StateRunning, transfer.StatePaused, transfer.StateAsking, transfer.StateFailed:
			if a.iconButton(c, iconClose, a.T(kXferCancel)+" "+key, 28).Clicked() {
				_ = q.Cancel(j.ID)
			}
		}
	})
}

// showInTarget points the target pane at a finished job's folder.
func (a *App) showInTarget(j transfer.JobInfo) {
	a.setPane(&a.xfer.dst, j.Spec.Dst.Host, j.Spec.Dst.Path)
}

// transferBadge is the number of unfinished jobs, for the toolbar.
func (a *App) transferBadge() int {
	if a.d.Transfers == nil {
		return 0
	}
	return a.d.Transfers.ActiveCount()
}

// osc7Hook is the shell snippet that makes a shell report its folder.
const osc7Hook = `# Tako: report the current folder (OSC 7)
__tako_osc7() { printf '\033]7;file://%s%s\033\\' "${HOSTNAME:-$(hostname)}" "$PWD"; }
if [ -n "$BASH_VERSION" ]; then PROMPT_COMMAND="__tako_osc7${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
elif [ -n "$ZSH_VERSION" ]; then precmd_functions+=(__tako_osc7); fi
`

// copyOSC7Hook puts the snippet on the clipboard.
func (a *App) copyOSC7Hook() {
	a.writeClipboard(strings.TrimSpace(osc7Hook) + "\n")
	a.toast(a.T(kOSC7Copied))
}

// policyOf maps the saved same-name choice to the queue's.
func policyOf(c settings.Conflict) transfer.Policy {
	switch c {
	case settings.ConflictOverwrite:
		return transfer.PolicyOverwrite
	case settings.ConflictSkip:
		return transfer.PolicySkip
	case settings.ConflictAsk:
	}
	return transfer.PolicyAsk
}

// verifyOf maps the saved check choice to the queue's.
func verifyOf(s string) transfer.VerifyMode {
	switch s {
	case "hash":
		return transfer.VerifyHash
	case "size":
		return transfer.VerifySize
	}
	return transfer.VerifyAuto
}

// OnJob is the queue's subscription: it wakes the window when a job changes
// state, and for progress ticks only while the transfer sheet is open, so a
// copy running in the background costs the idle window nothing.
func (a *App) OnJob(j transfer.JobInfo) {
	a.jobMu.Lock()
	if a.jobState == nil {
		a.jobState = map[int]transfer.State{}
	}
	prev, seen := a.jobState[j.ID]
	a.jobState[j.ID] = j.State
	a.jobMu.Unlock()
	if !seen || prev != j.State || a.xferOpen.Load() {
		a.Post(func() {})
	}
}
