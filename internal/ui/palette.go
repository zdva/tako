package ui

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/i18n"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/settings"
	"github.com/wei-dong/tako/internal/workspace"
)

func contextBackground() context.Context { return context.Background() }

type paletteState struct {
	open  bool
	query string
	sel   int
}

// paletteItem is one row of the command palette.
type paletteItem struct {
	section string
	title   string
	sub     string
	hint    string
	run     func()
}

const maxPaletteHosts = 8

// paletteItems lists what the query matches: hosts, tags, then commands.
func (a *App) paletteItems() []paletteItem {
	q := strings.TrimSpace(a.palette.query)
	var items []paletteItem
	hosts := a.d.Library.Search(q)
	if len(hosts) > maxPaletteHosts {
		hosts = hosts[:maxPaletteHosts]
	}
	for _, h := range hosts {
		items = append(items, paletteItem{
			section: a.T(kPalHosts), title: h.Name, sub: h.User + "@" + h.Addr,
			hint: a.T(kPalOpenInBlock),
			run:  func() { a.openHost(h, true) },
		})
	}
	if q != "" {
		for _, t := range a.d.Library.Tags() {
			if !strings.Contains(strings.ToLower(t.Name), strings.ToLower(strings.TrimPrefix(q, "#"))) {
				continue
			}
			members := a.d.Library.Search("#" + t.Name)
			items = append(items, paletteItem{
				section: a.T(kPalTags), title: t.Name, sub: a.T(kPalTagSub, len(members)),
				hint: a.T(kPalOpenAll),
				run: func() {
					for _, h := range members {
						a.openHost(h, true)
					}
				},
			})
		}
	}
	for _, cmd := range a.commands() {
		if q == "" || strings.Contains(strings.ToLower(cmd.title), strings.ToLower(q)) {
			items = append(items, cmd)
		}
	}
	return items
}

func (a *App) commands() []paletteItem {
	sec := a.T(kPalCommands)
	do := func(act actID) func() { return func() { a.do(act, 0) } }
	cmds := []paletteItem{
		{section: sec, title: a.T(kAddHost), hint: hint(actAddHost), run: do(actAddHost)},
		{section: sec, title: a.T(kNewLocal), hint: hint(actNewTab), run: a.openLocal},
		{section: sec, title: a.T(kToggleSidebar), hint: hint(actSidebar), run: do(actSidebar)},
		{section: sec, title: a.T(kCmdMinimap), hint: hint(actMinimap), run: do(actMinimap)},
		{section: sec, title: a.T(kCmdSyncAll), hint: hint(actSyncAll), run: do(actSyncAll)},
		{section: sec, title: a.T(kCmdLayoutAuto), run: func() { a.d.Model.ApplyPreset(workspace.PresetAuto) }},
		{section: sec, title: a.T(kCmdLayoutRow), run: func() { a.d.Model.ApplyPreset(workspace.PresetRow) }},
		{section: sec, title: a.T(kCmdLayoutColumn), run: func() { a.d.Model.ApplyPreset(workspace.PresetColumn) }},
		{section: sec, title: a.T(kImportSSHConfig), run: a.importSSHConfig},
		{section: sec, title: a.T(kSettings), hint: hint(actSettings), run: do(actSettings)},
		{section: sec, title: a.T(kCmdMonitor), hint: hint(actMonitor), run: do(actMonitor)},
		{section: sec, title: a.T(kCmdTransfer), hint: hint(actTransfer), run: do(actTransfer)},
	}
	cmds = append(cmds, paletteItem{section: sec, title: a.T(kCmdOSC7), run: a.copyOSC7Hook})
	seen := map[model.ID]bool{}
	for _, b := range a.d.Model.Blocks() {
		if b.Kind != workspace.KindSSH || seen[b.HostID] {
			continue
		}
		seen[b.HostID] = true
		id := b.HostID
		cmds = append(cmds, paletteItem{section: sec, title: a.T(kCmdMonitorHost, b.Name), run: func() { a.openMonitor(id) }})
	}
	if b := a.d.Model.Focused(); b != nil {
		cmds = append(cmds,
			paletteItem{section: sec, title: a.T(kPalToggleSync, b.Name), hint: hint(actSyncBlock), run: do(actSyncBlock)},
			paletteItem{section: sec, title: a.T(kCmdRename), run: func() { a.startRename(b) }},
			paletteItem{section: sec, title: a.T(kCmdZoom), hint: hint(actZoom), run: do(actZoom)},
			paletteItem{section: sec, title: a.T(kCmdSearch), hint: hint(actFind), run: do(actFind)},
			paletteItem{section: sec, title: a.T(kCmdCloseTab), hint: hint(actCloseTab), run: do(actCloseTab)},
			paletteItem{section: sec, title: a.T(kCloseBlock), hint: hint(actCloseBlock), run: do(actCloseBlock)},
		)
	}
	return cmds
}

func (a *App) closePalette() { a.palette = paletteState{} }

func (a *App) paletteView(c *ui.Context) {
	p := &a.palette
	if !p.open {
		return
	}
	items := a.paletteItems()
	if p.sel >= len(items) {
		p.sel = len(items) - 1
	}
	p.sel = max(p.sel, 0)
	open := true
	a.dialog(c, &open, func() {
		_, winH := c.Size()
		ui.Column(c).Key("palette").Width(560).MaxHeight(winH*0.7).Background(a.tk.Surface).Radius(16).
			Border(1, a.tk.BorderStrong).Shadow(0, 20, 60, 0, ui.RGBA(0, 0, 0, 0.28)).Clip().Children(func() {
			ui.Row(c).Padding(12, 16).Gap(10).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Icon(c, iconSearch).FontSize(16).TextColor(a.tk.Accent)
				in := ui.TextInput(c, &p.query).Placeholder(a.T(kSearchPlaceholder)).Label(a.T(kSearchPlaceholder)).AutoFocus().Grow(1)
				if in.Changed() {
					p.sel = 0
				}
				if in.Submitted() && len(items) > 0 {
					a.runPalette(items[p.sel])
				}
			})
			ui.Divider(c)
			ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Padding(6).Children(func() {
					last := ""
					for i, it := range items {
						if it.section != last {
							last = it.section
							ui.Text(c, it.section).FontSize(11).Bold().TextColor(a.tk.Text3).Padding(8, 10, 4)
						}
						a.paletteRow(c, i, it)
					}
					if len(items) == 0 {
						ui.Text(c, a.T(kPalNone)).FontSize(12.5).TextColor(a.tk.Text3).Padding(16)
					}
				})
			})
			ui.Divider(c)
			ui.Row(c).Padding(8, 14).Gap(14).Shrink(0).Background(a.tk.Surface2).Children(func() {
				a.caption(c, "↑↓ "+a.T(kPalSelect))
				a.caption(c, "↵ "+a.T(kPalOpen))
				a.caption(c, "esc "+a.T(kPalClose))
			})
		})
	})
	if c.Shortcut(0, ui.KeyDown) {
		p.sel = min(p.sel+1, len(items)-1)
	}
	if c.Shortcut(0, ui.KeyUp) {
		p.sel = max(p.sel-1, 0)
	}
	if !open {
		a.closePalette()
	}
}

func (a *App) paletteRow(c *ui.Context, i int, it paletteItem) {
	sel := i == a.palette.sel
	row := ui.Row(c).Key("pal:"+it.section+":"+it.title).Height(48).Padding(0, 10).Gap(12).Radius(10).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if sel {
		row.Background(a.tk.AccentSoft)
	}
	row.Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, it.title).Bold().FontSize(13).NoWrap()
			if it.sub != "" {
				a.caption(c, it.sub)
			}
		})
		if it.hint != "" {
			a.caption(c, it.hint)
		}
	})
	if row.Clicked() {
		a.runPalette(it)
	}
}

func (a *App) runPalette(it paletteItem) {
	a.closePalette()
	it.run()
}

// settingsState is the settings sheet.
type settingsState struct{ open bool }

func (a *App) settingsView(c *ui.Context) {
	if !a.settings.open {
		return
	}
	v := a.d.Settings.Values()
	lang := map[i18n.Language]int{"": 0, i18n.English: 1, i18n.Chinese: 2}[v.Language]
	app := map[settings.Appearance]int{"": 0, settings.AppearanceSystem: 0, settings.AppearanceLight: 1, settings.AppearanceDark: 2}[v.Appearance]
	compact := v.CompactBlocks
	mon, xf := v.Monitor, v.Transfer
	monOn := !mon.Off
	every, warn, crit := float64(mon.Interval()), float64(mon.Warn()), float64(mon.Crit())
	par := float64(xf.Parallel())
	conflict := map[settings.Conflict]int{settings.ConflictAsk: 0, settings.ConflictOverwrite: 1, settings.ConflictSkip: 2}[xf.Policy()]
	verify := map[string]int{"": 0, "auto": 0, "hash": 1, "size": 2}[xf.Verify]
	open := true
	a.dialog(c, &open, func() {
		_, winH := c.Size()
		ui.Column(c).Key("settings").Width(460).MaxHeight(winH-48).Padding(20).Gap(14).Background(a.tk.Surface).Radius(16).
			Border(1, a.tk.BorderStrong).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.2)).Children(func() {
			ui.Text(c, a.T(kSettings)).Bold().FontSize(17)
			a.field(c, a.T(kSetLanguage), func() {
				ui.Segmented(c, &lang, a.T(kLangSystem), "English", "简体中文").Label(a.T(kSetLanguage))
			})
			a.field(c, a.T(kSetAppearance), func() {
				ui.Segmented(c, &app, a.T(kAppSystem), a.T(kAppLight), a.T(kAppDark)).Label(a.T(kSetAppearance))
			})
			a.field(c, a.T(kSetCompact), func() {
				ui.Switch(c, &compact).Label(a.T(kSetCompact))
				a.caption(c, a.T(kSetCompactHint))
			})
			ui.Scroll(c).Key("settings-scroll").Grow(1).MinHeight(0).Children(func() {
				ui.Column(c).Gap(14).Children(func() {
					a.monitorSettings(c, &monOn, &every, &warn, &crit)
					a.transferSettings(c, &par, &conflict, &verify, &xf.AllowDirect)
				})
			})
			a.caption(c, a.T(kSetVersion, a.d.Version))
			ui.Row(c).Justify(ui.End).Children(func() {
				if ui.PrimaryButton(c, a.T(kDone)).Clicked() {
					a.settings.open = false
				}
			})
		})
	})
	langs := []i18n.Language{"", i18n.English, i18n.Chinese}
	apps := []settings.Appearance{settings.AppearanceSystem, settings.AppearanceLight, settings.AppearanceDark}
	mon = settings.Monitor{Off: !monOn, IntervalSec: int(every), WarnPct: int(warn), CritPct: int(max(crit, warn))}
	xf.Concurrency, xf.Conflict = int(par), []settings.Conflict{settings.ConflictAsk, settings.ConflictOverwrite, settings.ConflictSkip}[conflict]
	xf.Verify = []string{"", "hash", "size"}[verify]
	if langs[lang] != v.Language || apps[app] != v.ResolveAppearance() || compact != v.CompactBlocks ||
		mon != v.Monitor || xf != v.Transfer {
		if err := a.d.Settings.Update(func(x *settings.Values) {
			x.Language, x.Appearance, x.CompactBlocks = langs[lang], apps[app], compact
			x.Monitor, x.Transfer = mon, xf
		}); err != nil {
			a.toast(err.Error())
		}
		a.applyTransferPrefs(v.Transfer, xf)
	}
	if !open {
		a.settings.open = false
	}
}

var _ = model.Host{}

// monitorSettings is the monitoring part of the settings dialog.
func (a *App) monitorSettings(c *ui.Context, on *bool, every, warn, crit *float64) {
	a.field(c, a.T(kSetMonitor), func() {
		ui.Switch(c, on).Label(a.T(kSetMonitorOn))
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.caption(c, a.T(kSetMonitorEvery))
			ui.Stepper(c, every, 1, 60, 1).Label(a.T(kSetMonitorEvery)).Width(90)
			a.caption(c, a.T(kSetMonitorWarn))
			ui.Stepper(c, warn, 1, 99, 5).Label(a.T(kSetMonitorWarn)).Width(90)
			a.caption(c, a.T(kSetMonitorCrit))
			ui.Stepper(c, crit, 1, 100, 5).Label(a.T(kSetMonitorCrit)).Width(90)
		})
	})
}

// transferSettings is the file transfer part of the settings dialog.
func (a *App) transferSettings(c *ui.Context, par *float64, conflict, verify *int, direct *bool) {
	a.field(c, a.T(kSetTransfer), func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.caption(c, a.T(kSetParallel))
			ui.Stepper(c, par, 1, 8, 1).Label(a.T(kSetParallel)).Width(90)
		})
		ui.Segmented(c, conflict, a.T(kSetConflictAsk), a.T(kSetConflictOver), a.T(kSetConflictSkip)).Label(a.T(kSetConflict))
		ui.Segmented(c, verify, a.T(kSetVerifyAuto), a.T(kSetVerifyHash), a.T(kSetVerifySize)).Label(a.T(kSetVerify))
		ui.Switch(c, direct).Label(a.T(kSetDirect))
		a.caption(c, a.T(kSetDirectHint))
	})
}

// applyTransferPrefs hands changed transfer settings to the running queue.
func (a *App) applyTransferPrefs(old, now settings.Transfer) {
	if a.d.Transfers != nil && old.Parallel() != now.Parallel() {
		a.d.Transfers.SetConcurrency(now.Parallel())
	}
	if a.d.SetDirect != nil && old.AllowDirect != now.AllowDirect {
		a.d.SetDirect(now.AllowDirect)
	}
}
