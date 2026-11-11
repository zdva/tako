package ui

import (
	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/hostlib"
	"github.com/wei-dong/tako/internal/layout"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/workspace"
)

// blockMenu is the menu of a block's header: right-click on it, or its ⋯
// button (DESIGN.md §4.7).
func (a *App) blockMenu(m *ui.Menu, b *workspace.Block) {
	mod := a.d.Model
	if m.Item(a.T(kMenuRename)).Chosen() {
		a.startRename(b)
	}
	if m.Item(a.T(kMenuDuplicateTab)).Chosen() {
		a.duplicateTab(b)
	}
	if menuKey(m.Item(a.T(kMenuToggleSync)).Checked(b.Synced), actSyncBlock).Chosen() {
		mod.SetSynced(b, !b.Synced)
	}
	zoom := a.T(kMenuZoom)
	if mod.Zoomed() == b {
		zoom = a.T(kMenuUnzoom)
	}
	if menuKey(m.Item(zoom), actZoom).Chosen() {
		mod.ToggleZoom(b)
	}
	a.moveToMenu(m, b)
	if b.Kind == workspace.KindSSH {
		m.Separator()
		if menuKey(m.Item(a.T(kCmdMonitor)), actMonitor).Chosen() {
			a.openMonitor(b.HostID)
		}
		if menuKey(m.Item(a.T(kXferMenu)), actTransfer).Chosen() {
			a.openTransfer(b.HostID)
		}
		if m.Item(a.T(kReconnectNow)).Chosen() {
			b.Reconnect()
		}
		if h, ok := a.d.Library.Get(b.HostID); ok && m.Item(a.T(kMenuEdit)).Chosen() {
			a.openEditHost(h)
		}
	}
	m.Separator()
	if menuKey(m.Item(a.T(kMenuCloseBlock)).Danger(), actCloseBlock).Chosen() {
		mod.CloseBlock(b)
	}
}

// moveToMenu lists the other blocks, each with the ways to place b beside it.
func (a *App) moveToMenu(m *ui.Menu, b *workspace.Block) {
	others := a.otherBlocks(b)
	if len(others) == 0 {
		return
	}
	m.Submenu(a.T(kMenuMoveTo), func(sub *ui.Menu) {
		for _, o := range others {
			sub.Submenu(o.Name, func(at *ui.Menu) {
				for _, d := range []struct {
					key  string
					edge layout.Edge
				}{
					{kMenuDockLeft, layout.Left},
					{kMenuDockRight, layout.Right},
					{kMenuDockAbove, layout.Top},
					{kMenuDockBelow, layout.Bottom},
				} {
					if at.Item(a.T(d.key)).Chosen() {
						a.d.Model.Dock(b, o, d.edge)
					}
				}
				at.Separator()
				if at.Item(a.T(kMenuSwap)).Chosen() {
					a.d.Model.Reposition(b, o)
				}
			})
		}
	})
}

func (a *App) otherBlocks(b *workspace.Block) []*workspace.Block {
	var out []*workspace.Block
	for _, o := range a.d.Model.Blocks() {
		if o != b {
			out = append(out, o)
		}
	}
	return out
}

func (a *App) duplicateTab(b *workspace.Block) {
	if err := a.d.Model.DuplicateTab(b); err != nil {
		a.toast(err.Error())
		return
	}
	a.focusBlock(b)
}

// tabMenu is the menu of one tab chip.
func (a *App) tabMenu(m *ui.Menu, b *workspace.Block, i int) {
	if m.Item(a.T(kMenuDuplicateTab)).Chosen() {
		a.d.Model.SetActive(b, i)
		a.duplicateTab(b)
	}
	m.Separator()
	if m.Item(a.T(kCloseTab)).Chosen() {
		a.d.Model.CloseTab(b, i)
	}
}

// tagMenu is the menu of a tag heading in the sidebar.
func (a *App) tagMenu(m *ui.Menu, g hostlib.Group, collapsed bool) {
	key := g.Tag.Name
	if m.Item(a.T(kMenuOpenTag, len(g.Hosts))).Disabled(len(g.Hosts) == 0).Chosen() {
		for _, h := range g.Hosts {
			a.openHost(h, false)
		}
	}
	open := a.openBlocksOf(g.Hosts)
	allSynced := len(open) > 0
	for _, b := range open {
		allSynced = allSynced && b.Synced
	}
	label := a.T(kMenuSyncTag)
	if allSynced {
		label = a.T(kMenuUnsyncTag)
	}
	if m.Item(label).Disabled(len(open) == 0).Chosen() {
		a.d.Model.SetSyncedMany(open, !allSynced)
	}
	m.Separator()
	toggle := a.T(kMenuCollapse)
	if collapsed {
		toggle = a.T(kMenuExpand)
	}
	if m.Item(toggle).Chosen() {
		a.collapsedGroups[key] = !collapsed
	}
}

func (a *App) openBlocksOf(hosts []model.Host) []*workspace.Block {
	var out []*workspace.Block
	for _, h := range hosts {
		out = append(out, a.d.Model.BlocksOf(h.ID)...)
	}
	return out
}

// minimapSquareMenu is the menu of a minimap square.
func (a *App) minimapSquareMenu(m *ui.Menu, b *workspace.Block) {
	if m.Item(a.T(kMenuToggleSync)).Checked(b.Synced).Chosen() {
		a.d.Model.SetSynced(b, !b.Synced)
	}
	if m.Item(a.T(kMenuFocus)).Chosen() {
		a.d.Model.Focus(b)
		a.focusBlock(b)
	}
	m.Separator()
	if m.Item(a.T(kMenuCloseBlock)).Danger().Chosen() {
		a.d.Model.CloseBlock(b)
	}
}

// minimapMenu is the menu of the minimap's header and empty space.
func (a *App) minimapMenu(m *ui.Menu) {
	if m.Item(a.T(kMMSyncAll)).Chosen() {
		a.d.Model.SyncAll(true)
	}
	if m.Item(a.T(kMMSyncNone)).Chosen() {
		a.d.Model.SyncAll(false)
	}
	m.Separator()
	if menuKey(m.Item(a.T(kMMHide)), actMinimap).Chosen() {
		a.do(actMinimap, 0)
	}
}

// renameState is the "rename block" dialog.
type renameState struct {
	b    *workspace.Block
	text string
}

func (a *App) startRename(b *workspace.Block) {
	a.rename = renameState{b: b, text: b.Name}
}

func (a *App) renameView(c *ui.Context) {
	r := &a.rename
	if r.b == nil {
		return
	}
	open := true
	a.dialog(c, &open, func() {
		ui.Column(c).Key("rename").Width(360).Padding(20).Gap(12).Background(a.tk.Surface).Radius(16).
			Border(1, a.tk.BorderStrong).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.2)).Children(func() {
			ui.Text(c, a.T(kRenameTitle)).Bold().FontSize(15)
			in := ui.TextInput(c, &r.text).Placeholder(a.T(kRenameHint)).Label(a.T(kRenameHint)).AutoFocus()
			done := func() {
				a.d.Model.Rename(r.b, r.text)
				*r = renameState{}
			}
			if in.Submitted() {
				done()
				return
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, a.T(kCancel)).Clicked() {
					*r = renameState{}
				}
				if ui.PrimaryButton(c, a.T(kDone)).Clicked() {
					done()
				}
			})
		})
	})
	if !open {
		*r = renameState{}
	}
}

// findState is the terminal search bar of one block.
type findState struct {
	b     *workspace.Block
	query string
}

func (a *App) openFind(b *workspace.Block) {
	if b == nil || b.ActiveTab() == nil {
		return
	}
	a.closeFind()
	a.find = findState{b: b}
}

func (a *App) closeFind() {
	if a.find.b != nil {
		if t := a.find.b.ActiveTab(); t != nil {
			t.Term.EndSearch()
		}
		a.focusBlock(a.find.b)
	}
	a.find = findState{}
}

// findBar floats over the top-right corner of the block's terminal.
func (a *App) findBar(c *ui.Context, b *workspace.Block) {
	f := &a.find
	tab := b.ActiveTab()
	if f.b != b || tab == nil {
		return
	}
	if c.Shortcut(0, ui.KeyEscape) {
		a.closeFind()
		return
	}
	bar := ui.Row(c).Key("find").Absolute().Top(10).Right(14).Padding(5, 8).Gap(6).AlignItems(ui.Center)
	bar.Radius(10).Background(a.tk.Surface).Border(1, a.tk.BorderStrong).Shadow(0, 6, 18, 0, ui.RGBA(0, 0, 0, 0.18))
	bar.Children(func() {
		in := ui.TextInput(c, &f.query).Placeholder(a.T(kFindPlaceholder)).Label(a.T(kFindPlaceholder)).AutoFocus().Width(180)
		if in.Changed() {
			tab.Term.Search(f.query)
		}
		if in.Submitted() {
			tab.Term.SearchNext()
		}
		st := tab.Term.SearchStatus()
		switch {
		case f.query == "":
		case st.Total == 0 && !st.Searching:
			a.caption(c, a.T(kFindNone))
		default:
			a.caption(c, a.T(kFindCount, st.Selected, st.Total))
		}
		if a.iconButton(c, iconChevronD, a.T(kFindNext), 22).Clicked() {
			tab.Term.SearchNext()
		}
		if a.iconButton(c, iconChevronU, a.T(kFindPrev), 22).Clicked() {
			tab.Term.SearchPrevious()
		}
		if a.iconButton(c, iconClose, a.T(kFindClose), 22).Clicked() {
			a.closeFind()
		}
	})
}
