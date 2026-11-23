package ui

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/workspace"
)

// toolbar is the top strip: the search field that opens the command palette,
// the minimap switch and the transfer icon. It is also the window's drag
// handle when the title bar is hidden, and keeps clear of the window controls.
func (a *App) toolbar(c *ui.Context) {
	bar := c.TitleBar()
	row := ui.Row(c).Key("toolbar").Height(max(toolbarH, bar.Height)).Padding(0, 14+bar.Right, 0, 14).Gap(8).Shrink(0)
	row.DragWindow().Children(func() {
		if a.sidebarCollapsed {
			ui.Box(c).Width(bar.Left)
			if a.iconButton(c, iconSidebar, a.T(kToggleSidebar), 28).Clicked() {
				a.sidebarCollapsed = false
			}
		}
		ui.Spacer(c)
		a.searchBox(c)
		ui.Spacer(c)
		prefs := a.d.Model.MinimapPrefs()
		if a.toolIcon(c, iconMinimap, a.T(kMinimapToggle)+" "+hint(actMinimap), !prefs.Hidden).Clicked() {
			a.do(actMinimap, 0)
		}
		n := a.transferBadge()
		ui.Box(c).Key("xfer-tool").Size(30, 30).Children(func() {
			if a.toolIcon(c, iconTransfer, a.T(kTransfers)+" "+hint(actTransfer), a.xfer.open || n > 0).Clicked() {
				a.do(actTransfer, 0)
			}
			if n > 0 {
				ui.Box(c).Key("xfer-badge").Absolute().Top(-2).Right(-2).MinWidth(15).Height(15).Padding(0, 4).Radius(8).Background(a.tk.Accent).Center().Children(func() {
					ui.Text(c, strconv.Itoa(n)).FontSize(9.5).Bold().TextColor(ui.RGB(255, 255, 255))
				})
			}
		})
	})
}

// toolIcon is a toolbar icon button; on shows it as switched on.
func (a *App) toolIcon(c *ui.Context, ic *ui.SVG, label string, on bool) ui.Element {
	b := ui.Button(c, "").Label(label).Tooltip(label)
	b.Size(30, 30).Padding(0).Radius(8).Border(1, ui.Transparent).Background(ui.Transparent)
	fg := a.tk.Text2
	if on {
		b.Background(a.tk.AccentSoft).Border(1, a.tk.SyncBorder)
		fg = a.tk.Accent
	}
	b.Children(func() { ui.Icon(c, ic).FontSize(16).TextColor(fg) })
	return b
}

// searchBox is the field in the toolbar that opens the command palette.
func (a *App) searchBox(c *ui.Context) {
	box := ui.Row(c).Key("searchbox").Width(340).Height(36).Padding(0, 12).Gap(8).Radius(12).
		Background(a.tk.Surface).Border(1, a.tk.BorderStrong).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(a.T(kSearchPlaceholder))
	box.Children(func() {
		ui.Icon(c, iconSearch).FontSize(16).TextColor(a.tk.Text3)
		ui.Text(c, a.T(kSearchPlaceholder)).FontSize(12).TextColor(a.tk.Text3).Grow(1).MinWidth(0).NoWrap().Ellipsis("…")
		ui.Box(c).Padding(1, 6).Radius(8).Background(a.tk.Surface3).Children(func() {
			ui.Text(c, hint(actPalette)).FontSize(10.5).TextColor(a.tk.Text2)
		})
	})
	if box.Clicked() {
		a.openPalette()
	}
}

// statusBar shows the focused block's machine on the left, and its terminal's
// size and encoding on the right. Nothing else: counts and latency live on the
// blocks themselves (DESIGN.md, Final).
func (a *App) statusBar(c *ui.Context) {
	ui.Row(c).Key("status").Height(statusH).Padding(0, 14).Gap(10).Shrink(0).Children(func() {
		b := a.d.Model.Focused()
		if b == nil {
			return
		}
		ui.Text(c, b.Name).FontSize(11).Bold().TextColor(a.tk.Text2)
		if b.Kind == workspace.KindSSH {
			if h, ok := a.d.Library.Get(b.HostID); ok {
				a.caption(c, h.Target())
			}
		}
		ui.Spacer(c)
		if t := b.ActiveTab(); t != nil {
			cols, rows := t.Term.Size()
			a.caption(c, fmt.Sprintf("%d×%d · UTF-8", cols, rows))
		}
	})
}

func (a *App) emptyState(c *ui.Context) {
	if !a.sshConfigLoaded {
		a.sshConfigLoaded = true
		if a.d.ImportSSHConfig != nil {
			a.sshConfigHosts, _, _ = a.d.ImportSSHConfig()
		}
	}
	ui.Column(c).Key("empty").Grow(1).Center().Gap(14).Children(func() {
		if img := brand(); img != nil {
			ui.Image(c, img).Size(94, 94)
		}
		ui.Text(c, a.T(kWelcome)).Bold().FontSize(28)
		ui.Column(c).Gap(2).Center().Children(func() {
			ui.Text(c, a.T(kWelcomeLine1)).FontSize(12.5).TextColor(a.tk.Text2)
			ui.Text(c, a.T(kWelcomeLine2)).FontSize(12.5).TextColor(a.tk.Text2)
		})
		ui.Row(c).Gap(10).Padding(6, 0, 10).Children(func() {
			add := ui.PrimaryButton(c, a.T(kAddHost)+"  "+hint(actAddHost)).Height(34).Padding(0, 16)
			if add.Clicked() {
				a.openAddHost()
			}
			if ui.Button(c, a.T(kNewLocal)+"  "+hint(actNewTab)).Height(34).Padding(0, 16).Clicked() {
				a.openLocal()
			}
		})
		a.importCard(c)
		ui.Row(c).Gap(6).Padding(8, 0, 0).Children(func() {
			ui.Icon(c, iconLock).FontSize(12).TextColor(a.tk.Text3)
			a.caption(c, a.T(kSecretsNote))
		})
	})
}

func (a *App) importCard(c *ui.Context) {
	n := len(a.sshConfigHosts)
	ui.Column(c).Width(500).Gap(8).Children(func() {
		ui.Text(c, a.T(kImportFrom)).FontSize(11.5).Bold().TextColor(a.tk.Text3)
		row := ui.Row(c).Key("import-ssh").Height(52).Padding(0, 14).Gap(12).Radius(12).
			Background(a.tk.Surface).Border(1, a.tk.Border).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleButton).Label(a.T(kImportSSHConfig))
		row.Children(func() {
			ui.Box(c).Size(30, 30).Radius(9).Background(a.tk.Text2).Center().Children(func() {
				ui.Text(c, "~").Bold().FontSize(14).TextColor(ui.RGB(255, 255, 255))
			})
			ui.Column(c).Grow(1).Gap(1).Children(func() {
				ui.Text(c, a.T(kImportSSHConfig)).Bold().FontSize(13)
				a.caption(c, a.T(kImportSSHPath))
			})
			if n > 0 {
				a.pill(c, a.T(kHostsCount, n), a.tk.Accent, a.tk.AccentSoft)
			}
			ui.Icon(c, iconChevronR).FontSize(14).TextColor(a.tk.Text3)
		})
		if row.Clicked() {
			a.importSSHConfig()
		}
	})
}

// deleteView asks before removing a host.
func (a *App) deleteView(c *ui.Context) {
	if a.confirmDelete == nil {
		return
	}
	h := *a.confirmDelete
	open := true
	a.dialog(c, &open, func() {
		ui.Column(c).Width(380).Padding(20).Gap(12).Background(a.tk.Surface).Radius(16).Border(1, a.tk.BorderStrong).Children(func() {
			ui.Text(c, a.T(kDeleteTitle, h.Name)).Bold().FontSize(15)
			ui.Text(c, a.T(kDeleteBody)).FontSize(12.5).TextColor(a.tk.Text2)
			if a.deleteErr != "" {
				ui.Text(c, a.deleteErr).FontSize(12).TextColor(a.tk.Red)
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, a.T(kCancel)).Clicked() {
					a.confirmDelete, a.deleteErr = nil, ""
				}
				del := ui.PrimaryButton(c, a.T(kMenuDelete)).Background(a.tk.Red)
				if del.Clicked() {
					a.doDelete(h)
				}
			})
		})
	})
	if !open {
		a.confirmDelete, a.deleteErr = nil, ""
	}
}

func (a *App) doDelete(h model.Host) {
	for _, b := range a.d.Model.BlocksOf(h.ID) {
		a.d.Model.CloseBlock(b)
	}
	if err := a.d.Library.Delete(contextBackground(), h.ID); err != nil {
		a.deleteErr = a.formError(err)
		if err == nil {
			return
		}
		return
	}
	a.confirmDelete, a.deleteErr = nil, ""
}
