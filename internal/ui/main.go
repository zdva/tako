package ui

import (
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/layout"
	"github.com/wei-dong/tako/internal/session"
	"github.com/wei-dong/tako/internal/transport"
	"github.com/wei-dong/tako/internal/workspace"
)

// minCell is the smallest a block gets before the grid scrolls: about an 80x24
// terminal plus the header and footer.
var minCell = layout.Size{W: 520, H: 24*16 + blockHead + blockFoot + 8}

// primaryMod is the modifier of Tako's shortcuts: Command on macOS; Ctrl+Shift
// elsewhere, so plain Ctrl keys still reach the shell (DESIGN.md §4.6).
func primaryMod() ui.Modifiers {
	if runtime.GOOS == "darwin" {
		return ui.Super
	}
	return ui.Ctrl | ui.Shift
}

// secondMod adds the second modifier of two-modifier shortcuts.
func secondMod(extra ui.Modifiers) ui.Modifiers {
	if runtime.GOOS == "darwin" {
		return ui.Super | extra
	}
	return ui.Ctrl | ui.Shift | extra
}

// shortcutLabel renders a shortcut for display, as ⌘T or Ctrl+Shift+T.
func shortcutLabel(key string) string {
	if runtime.GOOS == "darwin" {
		return "⌘" + key
	}
	return "Ctrl+Shift+" + key
}

func (a *App) shortcuts(c *ui.Context) { //nolint:gocyclo // one case per shortcut
	m := a.d.Model
	mod := primaryMod()
	switch {
	case c.Shortcut(mod, ui.KeyP):
		a.palette.open = true
		a.palette.query = ""
		a.palette.sel = 0
	case c.Shortcut(mod, ui.KeyB):
		a.sidebarCollapsed = !a.sidebarCollapsed
	case c.Shortcut(mod, ui.KeyN):
		a.openAddHost()
	case c.Shortcut(mod, ui.KeyComma):
		a.settings.open = true
	case c.Shortcut(mod, ui.KeyT):
		if b := m.Focused(); b != nil {
			if err := m.AddTab(b); err != nil {
				a.toast(a.T(kErrOpenLocal, err))
			}
			a.focusBlock(b)
		} else {
			a.openLocal()
		}
	case c.Shortcut(mod, ui.KeyW):
		if b := m.Focused(); b != nil {
			m.CloseTab(b, b.Active)
		}
	case c.Shortcut(secondMod(ui.Alt), ui.KeyW):
		if b := m.Focused(); b != nil {
			m.CloseBlock(b)
		}
	case c.Shortcut(secondMod(ui.Alt), ui.KeyS):
		if b := m.Focused(); b != nil {
			m.SetSynced(b, !b.Synced)
		}
	}
	if b := m.Focused(); b != nil {
		for i, k := range []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
			if c.Shortcut(mod, k) {
				m.SetActive(b, i)
				a.focusBlock(b)
			}
		}
		cols, _ := layout.Dims(len(m.Blocks()))
		arrow := secondMod(ui.Alt)
		switch {
		case c.Shortcut(arrow, ui.KeyLeft):
			m.MoveFocus(cols, -1, 0)
			a.focusBlock(m.Focused())
		case c.Shortcut(arrow, ui.KeyRight):
			m.MoveFocus(cols, 1, 0)
			a.focusBlock(m.Focused())
		case c.Shortcut(arrow, ui.KeyUp):
			m.MoveFocus(cols, 0, -1)
			a.focusBlock(m.Focused())
		case c.Shortcut(arrow, ui.KeyDown):
			m.MoveFocus(cols, 0, 1)
			a.focusBlock(m.Focused())
		}
	}
	if a.pendingToast != "" {
		c.Toast(a.pendingToast)
		a.pendingToast = ""
	}
}

func (a *App) toast(msg string) { a.pendingToast = msg }

func (a *App) writeClipboard(s string) {
	a.pendingClip = s
}

// main is the area under the toolbar: the grid of blocks, or the empty state.
func (a *App) main(c *ui.Context) {
	blocks := a.d.Model.Blocks()
	if a.pendingClip != "" {
		c.WriteClipboard(a.pendingClip)
		a.pendingClip = ""
	}
	if len(blocks) == 0 {
		a.emptyState(c)
		return
	}
	g := a.grid(c, len(blocks))
	scroll := ui.Scroll(c).Key("grid").Grow(1).Padding(gridGap)
	scroll.Children(func() {
		ui.Box(c).Size(g.Content.W, g.Content.H).Children(func() {
			for i, b := range blocks {
				a.block(c, b, g.Cells[i])
			}
		})
	})
}

// grid places n blocks in the area under the toolbar.
func (a *App) grid(c *ui.Context, n int) layout.Grid {
	w, h := c.Size()
	if !a.sidebarCollapsed {
		w -= sidebarW + 1
	}
	viewport := layout.Size{W: w - 2*gridGap, H: h - toolbarH - statusH - 2*gridGap}
	return layout.Compute(n, viewport, minCell, gridGap)
}

func (a *App) block(c *ui.Context, b *workspace.Block, cell layout.Rect) {
	focused := a.d.Model.Focused() == b
	border, bw := a.tk.Border, float32(1)
	switch {
	case b.Synced:
		border, bw = a.tk.Accent, 1.5
	case focused:
		border, bw = a.tk.BorderStrong, 1.5
	}
	link := b.Link()
	if link.State == session.StateReconnecting || link.State == session.StateFailed {
		border, bw = a.tk.Amber, 1.5
	}
	frame := ui.Column(c).Key(string(b.ID)).Absolute().Left(cell.X).Top(cell.Y).Size(cell.W, cell.H)
	frame.Radius(12).Background(a.tk.Surface).Border(bw, border).Clip()
	frame.Shadow(0, 1, 2, 0, ui.RGBA(24, 24, 32, 0.06))
	if b.Synced {
		frame.Shadow(0, 0, 0, 3, a.tk.AccentSoft)
	}
	frame.Children(func() {
		a.blockHeader(c, b, link)
		a.banner(c, b, link)
		a.terminalArea(c, b, link)
		a.blockFooter(c, b, link)
	})
	if frame.FocusWithin() {
		a.d.Model.Focus(b)
	}
}

func (a *App) blockHeader(c *ui.Context, b *workspace.Block, link session.StateChange) {
	head := ui.Row(c).Key("head").Height(blockHead).Padding(0, 10).Gap(8).Background(a.tk.Surface2).Shrink(0)
	head.ContextMenu(func(m *ui.Menu) { a.blockMenu(m, b) })
	head.Children(func() {
		a.osTile(c, b.OS, b.Kind == workspace.KindLocal, 22)
		ui.Text(c, b.Name).Bold().FontSize(13).NoWrap()
		col := a.blockDot(link)
		dot(c, col, 7)
		ui.Row(c).Gap(4).Grow(1).MinWidth(0).Children(func() {
			for i, t := range b.Tabs {
				a.tabChip(c, b, i, t)
			}
			if a.iconButton(c, iconPlus, a.T(kNewTab), 22).Clicked() {
				if err := a.d.Model.AddTab(b); err != nil {
					a.toast(a.T(kErrOpenLocal, err))
				}
				a.focusBlock(b)
			}
		})
		a.syncChip(c, b, link)
		more := a.iconButton(c, iconMore, a.T(kMoreActions), 24)
		more.PopoverMenuButton(func(m *ui.Menu) { a.blockMenu(m, b) })
	})
}

func (a *App) blockDot(link session.StateChange) ui.Color {
	switch link.State {
	case session.StateConnected:
		return a.tk.Green
	case session.StateFailed:
		return a.tk.Red
	case session.StateConnecting, session.StateReconnecting:
		return a.tk.Amber
	case session.StateClosed:
	}
	return a.tk.Text3
}

func (a *App) tabChip(c *ui.Context, b *workspace.Block, i int, t *workspace.Tab) {
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
		ui.Text(c, t.Title()).FontSize(11.5).Bold().TextColor(col).NoWrap().MaxWidth(130).Ellipsis("…")
		if a.iconButton(c, iconClose, a.T(kCloseTab), 16).Clicked() {
			a.d.Model.CloseTab(b, i)
		}
	})
	if chip.Clicked() {
		a.d.Model.SetActive(b, i)
		a.focusBlock(b)
	}
	chip.ContextMenu(func(m *ui.Menu) {
		if m.Item(a.T(kMenuDuplicateTab)).Chosen() {
			a.d.Model.SetActive(b, i)
			if err := a.d.Model.DuplicateTab(b); err != nil {
				a.toast(err.Error())
			}
		}
		if m.Item(a.T(kCloseTab)).Chosen() {
			a.d.Model.CloseTab(b, i)
		}
	})
}

func (a *App) syncChip(c *ui.Context, b *workspace.Block, link session.StateChange) {
	label, fg, bg, border := a.T(kSync), a.tk.Text2, ui.Transparent, a.tk.BorderStrong
	switch {
	case b.Synced && link.State == session.StateConnected:
		label, fg, bg, border = a.T(kSyncOn), ui.RGB(255, 255, 255), a.tk.Accent, a.tk.Accent
	case b.Synced:
		label, fg, bg, border = a.T(kSyncPaused), a.tk.Accent, a.tk.AccentSoft, a.tk.Accent
	}
	chip := ui.Row(c).Key("sync").Height(24).Padding(0, 9).Gap(4).Radius(8).Background(bg).Border(1, border).
		Cursor(ui.CursorPointer).Label(a.T(kToggleSync))
	if b.Synced && link.State != session.StateConnected {
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

func (a *App) blockMenu(m *ui.Menu, b *workspace.Block) {
	if m.Item(a.T(kMenuDuplicateTab)).Chosen() {
		if err := a.d.Model.DuplicateTab(b); err != nil {
			a.toast(err.Error())
		}
	}
	if m.Item(a.T(kMenuToggleSync)).Checked(b.Synced).Chosen() {
		a.d.Model.SetSynced(b, !b.Synced)
	}
	if b.Kind == workspace.KindSSH {
		if m.Item(a.T(kReconnectNow)).Chosen() {
			b.Reconnect()
		}
		if h, ok := a.d.Library.Get(b.HostID); ok {
			if m.Item(a.T(kMenuEdit)).Chosen() {
				a.openEditHost(h)
			}
		}
	}
	m.Separator()
	if m.Item(a.T(kMenuCloseBlock)).Chosen() {
		a.d.Model.CloseBlock(b)
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

func (a *App) blockFooter(c *ui.Context, b *workspace.Block, link session.StateChange) {
	ui.Row(c).Key("foot").Height(blockFoot).Padding(0, 12).Gap(14).Background(a.tk.Surface2).Shrink(0).Children(func() {
		for _, m := range []string{a.T(kMetricCPU), a.T(kMetricMem), a.T(kMetricDisk)} {
			ui.Row(c).Gap(4).Children(func() {
				a.caption(c, m)
				ui.Text(c, "—").FontSize(11).Bold().TextColor(a.tk.Text3)
			})
		}
		ui.Spacer(c)
		state := a.T(kLocalShell)
		if b.Kind == workspace.KindSSH {
			state = a.linkLabel(link)
		}
		a.caption(c, state)
	})
}

func (a *App) linkLabel(link session.StateChange) string {
	switch link.State {
	case session.StateConnected:
		return a.T(kStatusConnected)
	case session.StateFailed:
		return a.T(kStatusFailed)
	case session.StateConnecting, session.StateReconnecting:
		return a.T(kStatusReconnecting)
	case session.StateClosed:
	}
	return ""
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
