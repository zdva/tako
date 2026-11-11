package ui

import (
	"errors"
	"fmt"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/layout"
	"github.com/wei-dong/tako/internal/session"
	"github.com/wei-dong/tako/internal/transport"
	"github.com/wei-dong/tako/internal/workspace"
)

func (a *App) toast(msg string) { a.pendingToast = msg }

func (a *App) writeClipboard(s string) {
	a.pendingClip = s
}

// block draws one block in the rectangle cell of the main area (in DIPs from
// its top-left corner). The block's frame glides to its place in about 150 ms
// when the layout changes, unless instant says the layout follows the pointer
// or the window.
func (a *App) block(c *ui.Context, b *workspace.Block, cell layout.Rect, instant bool) {
	m := a.d.Model
	focused := m.Focused() == b
	border, bw := a.tk.Border, float32(1)
	switch {
	case b.Synced:
		border = a.tk.SyncBorder
	case focused:
		border, bw = a.tk.BorderStrong, 1.5
	}
	link := b.Link()
	if link.State == session.StateReconnecting || link.State == session.StateFailed {
		border, bw = a.tk.Amber, 1.5
	}
	frame := ui.Column(c).Key(string(b.ID)).Absolute()
	anim := ui.Animation{Spring: ui.SpringOptions{Duration: layoutSpring}}
	if instant {
		anim = ui.Animation{Duration: time.Millisecond}
	}
	x, y := frame.AnimateTo("x", cell.X, anim), frame.AnimateTo("y", cell.Y, anim)
	w, h := frame.AnimateTo("w", cell.W, anim), frame.AnimateTo("h", cell.H, anim)
	frame.Left(x).Top(y).Size(w, h)
	frame.Radius(12).Background(a.tk.Surface).Border(bw, border).Clip()
	frame.Shadow(0, 1, 2, 0, ui.RGBA(24, 24, 32, 0.06))
	frame.Children(func() {
		a.blockHeader(c, b, link, cell.W)
		a.banner(c, b, link)
		a.terminalArea(c, b, link)
		a.blockFooter(c, b)
	})
	if frame.FocusWithin() {
		m.Focus(b)
	}
}

// layoutSpring is how long blocks take to glide to a new layout.
const layoutSpring = 150 * time.Millisecond

// headerChrome is the width the header keeps for the icon, name, status dot,
// new-tab, sync and more buttons; tab titles share what is left.
const headerChrome = 270

func (a *App) blockHeader(c *ui.Context, b *workspace.Block, link session.StateChange, cellW float32) {
	titleMax := min(max((cellW-headerChrome)/float32(max(len(b.Tabs), 1))-30, 36), 130)
	head := ui.Row(c).Key("head").Height(blockHead).Padding(0, 10).Gap(8).Background(a.tk.Surface2).Shrink(0)
	head.ContextMenu(func(m *ui.Menu) { a.blockMenu(m, b) })
	if head.DoubleClicked() {
		a.d.Model.ToggleZoom(b)
	}
	head.Children(func() {
		a.osTile(c, b.OS, b.Kind == workspace.KindLocal, 22)
		ui.Text(c, b.Name).Bold().FontSize(13).NoWrap()
		if col, show := a.blockDot(link); show {
			dot(c, col, 7)
		}
		ui.Row(c).Gap(4).Grow(1).MinWidth(0).Children(func() {
			for i, t := range b.Tabs {
				a.tabChip(c, b, i, t, titleMax)
			}
			if a.iconButton(c, iconPlus, a.T(kNewTab), 22).Clicked() {
				if err := a.d.Model.AddTab(b); err != nil {
					a.toast(a.T(kErrOpenLocal, err))
				}
				a.focusBlock(b)
			}
		})
		a.syncChip(c, b)
		more := a.iconButton(c, iconMore, a.T(kMoreActions), 24)
		more.PopoverMenuButton(func(m *ui.Menu) { a.blockMenu(m, b) })
	})
}

// blockDot is the status dot of a block's header. A healthy connection shows
// none: only trouble does (DESIGN.md, Final: no redundant status).
func (a *App) blockDot(link session.StateChange) (col ui.Color, show bool) {
	switch link.State {
	case session.StateFailed:
		return a.tk.Red, true
	case session.StateConnecting, session.StateReconnecting:
		return a.tk.Amber, true
	case session.StateConnected, session.StateClosed:
	}
	return ui.Color{}, false
}

func (a *App) tabChip(c *ui.Context, b *workspace.Block, i int, t *workspace.Tab, titleMax float32) {
	active := i == b.Active
	chip := ui.Row(c).Key(t.ID).Height(24).Padding(0, 4, 0, 10).Gap(4).Radius(12).Cursor(ui.CursorPointer)
	if active {
		chip.Background(a.tk.Surface).Border(1, a.tk.BorderStrong)
	}
	chip.Children(func() {
		col := a.tk.Text2
		if active {
			col = a.tk.Text
		}
		ui.Text(c, t.Title()).FontSize(11.5).Bold().TextColor(col).SingleLine().MaxWidth(titleMax).Ellipsis("…")
		if a.iconButton(c, iconClose, a.T(kCloseTab), 16).Clicked() {
			a.d.Model.CloseTab(b, i)
		}
	})
	if chip.Clicked() {
		a.d.Model.SetActive(b, i)
		a.focusBlock(b)
	}
	chip.ContextMenu(func(m *ui.Menu) { a.tabMenu(m, b, i) })
}

// syncChip is the one small control that shows and toggles a block's sync:
// coral when on, a gray outline when off, a dashed coral outline while the
// connection is down and sync waits.
func (a *App) syncChip(c *ui.Context, b *workspace.Block) {
	label, fg, bg, border := a.T(kSync), a.tk.Text3, ui.Transparent, a.tk.BorderStrong
	paused := a.d.Model.SyncPaused(b)
	switch {
	case paused:
		label, fg, bg, border = a.T(kSyncPaused), a.tk.Accent, a.tk.AccentSoft, a.tk.SyncBorder
	case b.Synced:
		fg, bg, border = a.tk.Accent, a.tk.AccentSoft, a.tk.SyncBorder
	}
	chip := ui.Row(c).Key("sync").Height(24).Padding(0, 9).Gap(4).Radius(8).Background(bg).Border(1, border).
		Cursor(ui.CursorPointer).Label(a.T(kToggleSync))
	if paused {
		chip.BorderStyle(ui.BorderDashed)
	}
	chip.Children(func() {
		ui.Icon(c, iconLink).FontSize(12).TextColor(fg)
		ui.Text(c, label).FontSize(11).Bold().TextColor(fg).NoWrap()
	})
	if chip.Clicked() {
		a.d.Model.SetSynced(b, !b.Synced)
	}
}

// banner is the strip under the header that says the connection is not up.
func (a *App) banner(c *ui.Context, b *workspace.Block, link session.StateChange) {
	if link.State == session.StateConnected || link.State == session.StateClosed {
		return
	}
	fg, bg := a.tk.Amber, a.tk.Amber.Alpha(0.14)
	var msg string
	var extra func()
	switch link.State {
	case session.StateConnecting:
		fg, bg = a.tk.Text2, a.tk.Surface3
		msg = a.T(kBannerConnecting)
	case session.StateReconnecting:
		msg = a.T(kBannerReconnecting, max(link.Attempt, 1))
		if !link.NextRetry.IsZero() {
			secs := max(int(time.Until(link.NextRetry).Round(time.Second)/time.Second), 0)
			extra = func() { ui.Text(c, a.T(kSecondsShort, secs)).FontSize(11.5).TextColor(a.tk.Text2) }
			c.After(500 * time.Millisecond)
		}
	case session.StateFailed:
		fg, bg = a.tk.Red, a.tk.Red.Alpha(0.10)
		msg = a.explain(link.Err)
	case session.StateConnected, session.StateClosed:
	}
	ui.Row(c).Key("banner").MinHeight(34).Padding(5, 12).Gap(8).Background(bg).Shrink(0).Children(func() {
		ic := iconAlert
		if link.State == session.StateConnecting {
			ic = iconRefresh
		}
		ui.Icon(c, ic).FontSize(14).TextColor(fg)
		ui.Text(c, msg).FontSize(12).Bold().TextColor(fg).Grow(1).MinWidth(0)
		if extra != nil {
			extra()
		}
		a.bannerButtons(c, b, link)
	})
}

// explain turns a connection error into the sentence the banner shows.
func (a *App) explain(err error) string {
	if err == nil {
		return a.T(kErrUnknown)
	}
	if ch, ok := errors.AsType[*transport.HostKeyChangedError](err); ok {
		return a.T(kErrHostKeyChanged, ch.Host, ch.Old, ch.New)
	}
	if un, ok := errors.AsType[*transport.HostKeyUnknownError](err); ok {
		return a.T(kErrHostKeyUnknown, un.Host, un.Fingerprint)
	}
	switch {
	case errors.Is(err, transport.ErrHostKeyRejected):
		return a.T(kErrHostKeyRejected)
	case errors.Is(err, transport.ErrAuthFailed):
		return a.T(kErrAuthFailed)
	case errors.Is(err, transport.ErrCanceled):
		return a.T(kErrCanceled)
	case errors.Is(err, transport.ErrProxyFailed):
		return a.T(kErrProxy, err)
	default:
		return fmt.Sprint(err)
	}
}

func (a *App) terminalArea(c *ui.Context, b *workspace.Block, link session.StateChange) {
	tab := b.ActiveTab()
	area := ui.Box(c).Key("term").Grow(1).MinHeight(0).Background(a.tk.TermBg)
	area.Children(func() {
		if tab == nil {
			return
		}
		col := ui.Column(c).Fill()
		if link.State == session.StateReconnecting || link.State == session.StateFailed {
			col.Opacity(0.55)
		}
		col.Children(func() {
			v := terminal.View(c, tab.Term).Key(tab.ID).Grow(1)
			if a.focusTab == tab {
				v.Focus()
				a.focusTab = nil
			}
		})
		a.findBar(c, b)
		if !tab.DropAt.IsZero() && time.Since(tab.DropAt) < 6*time.Second {
			c.After(time.Second)
			a.dropCard(c, b)
		}
	})
}

func (a *App) dropCard(c *ui.Context, b *workspace.Block) {
	card := ui.Row(c).Key("dropcard").Absolute().Left(16).Right(16).Bottom(16).Padding(12, 14).Gap(10)
	card.Radius(12).Background(a.tk.Surface).Border(1, a.tk.BorderStrong).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.18))
	card.Children(func() {
		ui.Icon(c, iconAlert).FontSize(16).TextColor(a.tk.Amber)
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, a.T(kDropTitle)).Bold().FontSize(12.5)
			ui.Text(c, a.T(kDropBody, b.Name, max(a.d.Model.SyncedCount()-1, 0))).FontSize(11.5).TextColor(a.tk.Text2)
		})
	})
}

// bannerButtons are the actions of the disconnect banner.
func (a *App) bannerButtons(c *ui.Context, b *workspace.Block, link session.StateChange) {
	if link.State == session.StateConnecting {
		return
	}
	label := a.T(kReconnectNow)
	if link.State == session.StateFailed {
		label = a.T(kRetry)
	}
	if ui.Button(c, label).FontSize(11.5).Height(24).Padding(0, 10).Clicked() {
		b.Reconnect()
	}
	if link.State != session.StateFailed {
		return
	}
	if h, ok := a.d.Library.Get(b.HostID); ok {
		if ui.Button(c, a.T(kMenuEdit)).FontSize(11.5).Height(24).Padding(0, 10).Clicked() {
			a.openEditHost(h)
		}
	}
}
