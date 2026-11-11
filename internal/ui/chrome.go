package ui

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/model"
)

func (a *App) toolbar(c *ui.Context) {
	ui.Row(c).Key("toolbar").Height(toolbarH).Padding(0, 14).Gap(10).Shrink(0).Children(func() {
		if a.sidebarCollapsed {
			if a.iconButton(c, iconSidebar, a.T(kToggleSidebar), 28).Clicked() {
				a.sidebarCollapsed = false
			}
		}
		n := len(a.d.Model.Blocks())
		if n > 0 {
			g := a.grid(c, n)
			a.layoutPill(c, a.T(kLayoutAuto, g.Cols, g.Rows))
		}
		ui.Spacer(c)
		a.searchBox(c)
		ui.Spacer(c)
		if n > 0 && a.d.Model.SyncedCount() > 0 {
			a.pill(c, a.T(kSyncCount, a.d.Model.SyncedCount(), n), a.tk.Accent, a.tk.AccentSoft)
		}
	})
}

func (a *App) layoutPill(c *ui.Context, text string) {
	ui.Row(c).Height(28).Padding(0, 10).Radius(8).Background(a.tk.Surface).Border(1, a.tk.Border).Children(func() {
		ui.Text(c, text).FontSize(12).TextColor(a.tk.Text2).NoWrap()
	})
}

// searchBox is the field in the toolbar that opens the command palette.
func (a *App) searchBox(c *ui.Context) {
	box := ui.Row(c).Key("searchbox").Width(340).Height(30).Padding(0, 10).Gap(8).Radius(9).
		Background(a.tk.Surface).Border(1, a.tk.BorderStrong).Cursor(ui.CursorPointer).Label(a.T(kSearchPlaceholder))
	box.Children(func() {
		ui.Icon(c, iconSearch).FontSize(14).TextColor(a.tk.Text3)
		ui.Text(c, a.T(kSearchPlaceholder)).FontSize(12).TextColor(a.tk.Text3).Grow(1)
		ui.Box(c).Padding(1, 6).Radius(5).Background(a.tk.Surface3).Children(func() {
			ui.Text(c, shortcutLabel("P")).FontSize(10.5).TextColor(a.tk.Text2)
		})
	})
	if box.Clicked() {
		a.palette.open, a.palette.query, a.palette.sel = true, "", 0
	}
}

func (a *App) statusBar(c *ui.Context) {
	connected, re := a.d.Model.Counts()
	ui.Row(c).Key("status").Height(statusH).Padding(0, 14).Gap(14).Shrink(0).Children(func() {
		ui.Row(c).Gap(5).Children(func() {
			dot(c, a.tk.Green, 6)
			a.caption(c, a.T(kStatusCount, connected))
		})
		if re > 0 {
			ui.Row(c).Gap(5).Children(func() {
				dot(c, a.tk.Amber, 6)
				a.caption(c, a.T(kStatusReconnectingCount, re))
			})
		}
		if b := a.d.Model.Focused(); b != nil {
			ui.Text(c, b.Name).FontSize(11).Bold().TextColor(a.tk.Text2)
		}
		a.caption(c, a.T(kSessionSaved))
		ui.Spacer(c)
		if b := a.d.Model.Focused(); b != nil {
			if t := b.ActiveTab(); t != nil {
				cols, rows := t.Term.Size()
				a.caption(c, fmt.Sprintf("%d×%d · UTF-8", cols, rows))
			}
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
		ui.Text(c, a.T(kWelcome)).Bold().FontSize(24)
		ui.Column(c).Gap(2).Center().Children(func() {
			ui.Text(c, a.T(kWelcomeLine1)).FontSize(12.5).TextColor(a.tk.Text2)
			ui.Text(c, a.T(kWelcomeLine2)).FontSize(12.5).TextColor(a.tk.Text2)
		})
		ui.Row(c).Gap(10).Padding(6, 0, 10).Children(func() {
			add := ui.PrimaryButton(c, a.T(kAddHost)+"  "+shortcutLabel("N")).Height(34).Padding(0, 16)
			if add.Clicked() {
				a.openAddHost()
			}
			if ui.Button(c, a.T(kNewLocal)+"  "+shortcutLabel("T")).Height(34).Padding(0, 16).Clicked() {
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
			Background(a.tk.Surface).Border(1, a.tk.Border).Cursor(ui.CursorPointer).Label(a.T(kImportSSHConfig))
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
