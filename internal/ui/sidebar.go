package ui

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/hostlib"
	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/session"
	"github.com/zdva/tako/internal/workspace"
)

func (a *App) sidebar(c *ui.Context) {
	ui.Column(c).Key("sidebar").Width(sidebarW).Background(a.tk.Sidebar).
		BorderWidth(0).Children(func() {
		a.brandRow(c)
		a.filterRow(c)
		groups := a.d.Library.Groups(a.filter)
		scroll := ui.Scroll(c).Grow(1).Padding(0, 10)
		scroll.Children(func() {
			ui.Column(c).Gap(2).Children(func() {
				if len(groups) == 0 && a.filter != "" {
					ui.Text(c, a.T(kSidebarNoMatch)).FontSize(12).TextColor(a.tk.Text3).Padding(16, 8)
				}
				for _, g := range groups {
					a.group(c, g)
				}
			})
		})
		a.sidebarFooter(c)
	})
	ui.Box(c).Width(1).Background(a.tk.Border)
}

func (a *App) brandRow(c *ui.Context) {
	// The window controls sit over the page. When a platform puts controls on
	// the left, move the brand below their bottom edge instead of squeezing it
	// into the same line. This covers macOS traffic lights and left-side Linux
	// decorations; Windows and right-side Linux decorations keep the first row.
	bar := c.TitleBar()
	brandTop, brandHeight, brandLeft := float32(10), float32(54), 16+bar.Left
	if bar.Left > 0 {
		brandTop = max(bar.Bottom, bar.Height) + 6
		brandHeight = brandTop + 42
		brandLeft = 16
	}
	row := ui.Row(c).Key("brand").Height(brandHeight).Padding(brandTop, 16, 0, brandLeft).Gap(9)
	row.DragWindow().Children(func() {
		if img := brand(a.tk.Dark); img != nil {
			ui.Image(c, img).Size(34, 34).Radius(11)
		}
		ui.Text(c, "Tako").Bold().FontSize(15)
		ui.Spacer(c)
		if a.iconButton(c, iconSidebar, a.T(kToggleSidebar), 26).Clicked() {
			a.sidebarCollapsed = !a.sidebarCollapsed
		}
	})
}

func (a *App) filterRow(c *ui.Context) {
	ui.Row(c).Padding(4, 12, 10).Gap(8).Children(func() {
		ui.SearchField(c, &a.filter).Placeholder(a.T(kSidebarFilter)).Label(a.T(kSidebarFilter)).Grow(1)
		add := ui.PrimaryButton(c, "").Label(a.T(kAddHost)).Tooltip(a.T(kAddHost)).Size(34, 34).Padding(0)
		add.Children(func() { ui.Icon(c, iconPlus).FontSize(18).TextColor(ui.RGB(255, 255, 255)) })
		if add.Clicked() {
			a.openAddHost()
		}
	})
}

func (a *App) group(c *ui.Context, g hostlib.Group) {
	key := g.Tag.Name
	collapsed := a.collapsedGroups[key]
	name := key
	if key == hostlib.Untagged {
		name = a.T(kUntagged)
	}
	head := ui.Row(c).Key("group:"+key).Height(26).Padding(0, 6).Gap(6).Cursor(ui.CursorPointer).
		Focusable().Role(ui.RoleButton).Label(name)
	head.ContextMenu(func(m *ui.Menu) { a.tagMenu(m, g, collapsed) })
	head.Children(func() {
		ic := iconChevronD
		if collapsed {
			ic = iconChevronR
		}
		ui.Icon(c, ic).FontSize(11).TextColor(a.tk.Text3)
		dot(c, a.tagColor(key), 7)
		ui.Text(c, name).FontSize(11.5).Bold().TextColor(a.tk.Text2)
		ui.Spacer(c)
		ui.Text(c, strconv.Itoa(len(g.Hosts))).FontSize(11).TextColor(a.tk.Text3)
	})
	if head.Clicked() {
		a.collapsedGroups[key] = !collapsed
	}
	if collapsed {
		return
	}
	for _, h := range g.Hosts {
		a.hostRow(c, h)
	}
}

// hostState summarizes the open blocks of a host for its row.
type hostState struct {
	open    bool
	focused bool
	synced  bool
	state   session.State
}

func (a *App) stateOf(h model.Host) hostState {
	var st hostState
	focus := a.d.Model.Focused()
	for _, b := range a.d.Model.BlocksOf(h.ID) {
		if !st.open {
			st.state = b.Link().State
		}
		st.open = true
		st.focused = st.focused || b == focus
		st.synced = st.synced || b.Synced
	}
	return st
}

func (a *App) statusColor(st hostState) (col ui.Color, label string) {
	if !st.open {
		return a.tk.Text3.Alpha(0.5), a.T(kStatusIdle)
	}
	switch st.state {
	case session.StateConnected:
		return a.tk.Green, a.T(kStatusConnected)
	case session.StateFailed:
		return a.tk.Red, a.T(kStatusFailed)
	case session.StateConnecting, session.StateReconnecting:
		return a.tk.Amber, a.T(kStatusReconnecting)
	case session.StateClosed:
	}
	return a.tk.Text3, ""
}

func (a *App) hostRow(c *ui.Context, h model.Host) {
	st := a.stateOf(h)
	row := ui.Row(c).Key("host:"+string(h.ID)).Height(hostRowH).Padding(0, 8).Gap(9).Radius(10).Cursor(ui.CursorPointer).
		Focusable().Role(ui.RoleButton).Label(h.Name + ", " + h.Target())
	switch {
	case st.focused:
		row.Background(a.tk.Surface).Border(1.5, a.tk.Accent)
	case st.open:
		row.Background(a.tk.Surface).Border(1, a.tk.Border)
	}
	row.Children(func() {
		a.osTile(c, h.OS, false, 26)
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, h.Name).Bold().FontSize(13).Ellipsis("…").SingleLine()
			a.caption(c, h.Target()).Ellipsis("…").SingleLine()
		})
		col, label := a.statusColor(st)
		ui.Box(c).Children(func() { dot(c, col, 7) }).Label(label)
	})
	if row.Clicked() {
		a.openHost(h, false)
	}
	row.ContextMenu(func(m *ui.Menu) { a.hostMenu(m, h, st) })
}

func (a *App) hostMenu(m *ui.Menu, h model.Host, st hostState) {
	if m.Item(a.T(kMenuOpen)).Chosen() {
		a.openHost(h, false)
	}
	if m.Item(a.T(kMenuOpenNew)).Chosen() {
		a.openHost(h, true)
	}
	if st.open {
		if m.Item(a.T(kMenuToggleSync)).Checked(st.synced).Chosen() {
			a.d.Model.SetSyncedMany(a.d.Model.BlocksOf(h.ID), !st.synced)
		}
	}
	m.Separator()
	if m.Item(a.T(kXferMenu)).Chosen() {
		a.openTransfer(h.ID)
	}
	if st.open && m.Item(a.T(kCmdMonitorHost, h.Name)).Chosen() {
		a.openMonitor(h.ID)
	}
	m.Separator()
	if m.Item(a.T(kMenuEdit)).Chosen() {
		a.openEditHost(h)
	}
	if m.Item(a.T(kMenuCopyAddr)).Chosen() {
		a.writeClipboard(h.HostPort())
	}
	if m.Item(a.T(kMenuCopySSH)).Chosen() {
		a.writeClipboard(sshCommand(h))
	}
	m.Separator()
	if m.Item(a.T(kMenuDelete)).Chosen() {
		hc := h
		a.confirmDelete = &hc
	}
}

func sshCommand(h model.Host) string {
	cmd := "ssh " + h.User + "@" + h.Addr
	if h.EffectivePort() != model.DefaultPort {
		cmd += fmt.Sprintf(" -p %d", h.EffectivePort())
	}
	return cmd
}

func (a *App) sidebarFooter(c *ui.Context) {
	ui.Column(c).Padding(8, 10, 10).Gap(2).Children(func() {
		a.footerButton(c, iconTerminal, a.T(kNewLocalShort), hint(actNewTab), func() { a.openLocal() })
		a.footerButton(c, iconSettings, a.T(kSettings), hint(actSettings), func() { a.settings.open = true })
	})
}

func (a *App) footerButton(c *ui.Context, ic *ui.SVG, label, key string, fn func()) {
	row := ui.Row(c).Key("footer:"+label).Height(34).Padding(0, 8).Gap(10).Radius(8).Cursor(ui.CursorPointer).
		Focusable().Role(ui.RoleButton).Label(label)
	row.Children(func() {
		ui.Icon(c, ic).FontSize(15).TextColor(a.tk.Text2)
		ui.Text(c, label).FontSize(12.5).Grow(1).MinWidth(0).NoWrap()
		ui.Box(c).Padding(1, 6).Shrink(0).Radius(5).Background(a.tk.Surface3).Children(func() {
			ui.Text(c, key).FontSize(10.5).TextColor(a.tk.Text2).NoWrap()
		})
	})
	if row.Clicked() {
		fn()
	}
}

// openHost shows a host in the grid and gives its terminal the keyboard.
func (a *App) openHost(h model.Host, newBlock bool) {
	b, err := a.d.Model.OpenHost(h, newBlock)
	if err != nil {
		a.toast(a.T(kErrOpenHost, h.Name, err))
		return
	}
	a.focusBlock(b)
}

func (a *App) openLocal() {
	b, err := a.d.Model.OpenLocal("")
	if err != nil {
		a.toast(a.T(kErrOpenLocal, err))
		return
	}
	a.focusBlock(b)
}

func (a *App) focusBlock(b *workspace.Block) {
	if t := b.ActiveTab(); t != nil {
		a.focusTab = t
	}
}
