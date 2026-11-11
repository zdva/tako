package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/i18n"
	"github.com/wei-dong/tako/internal/scheme"
	"github.com/wei-dong/tako/internal/settings"
	"github.com/wei-dong/tako/internal/theme"
)

// ImportSummary is what importing a host file did.
type ImportSummary struct {
	// Format names the file's format ("xshell", "securecrt", "termius", "tako").
	Format                    string
	Added, Skipped, Passwords int
	Warnings                  []string
}

// Settings pages.
const (
	pageGeneral    = "general"
	pageTerminal   = "terminal"
	pageConnection = "connection"
	pageMonitor    = "monitor"
	pageTransfer   = "transfer"
	pageData       = "data"
	pageAbout      = "about"
)

// settingsState is the settings window.
type settingsState struct {
	open bool
	page string
	// Paths typed or chosen on the Terminal and Data pages, and the line each
	// action answers with.
	colorsPath, importPath, exportPath string
	colorsMsg, importMsg, exportMsg    string
	importWarns                        []string
	diagMsg                            string
}

// setPref changes the saved settings and shows the error if saving failed.
func (a *App) setPref(fn func(*settings.Values)) {
	before := a.d.Settings.Values()
	if err := a.d.Settings.Update(fn); err != nil {
		a.toast(err.Error())
		return
	}
	a.applyTransferPrefs(before.Transfer, a.d.Settings.Values().Transfer)
}

// choice draws a segmented control over options and calls set with the new
// index when the user picks another one.
func (a *App) choice(c *ui.Context, label string, cur int, options []string, set func(int)) {
	idx := cur
	ui.Segmented(c, &idx, options...).Label(label)
	if idx != cur {
		set(idx)
	}
}

// toggle draws a labelled switch.
func (a *App) toggle(c *ui.Context, label string, cur bool, set func(bool)) {
	on := cur
	ui.Switch(c, &on).Label(label)
	if on != cur {
		set(on)
	}
}

// number draws a labelled stepper next to its caption.
func (a *App) number(c *ui.Context, label string, cur, lo, hi, step int, set func(int)) {
	v := float64(cur)
	a.caption(c, label)
	ui.Stepper(c, &v, float64(lo), float64(hi), float64(step)).Label(label).Width(90)
	if int(v) != cur {
		set(int(v))
	}
}

// pfield is a labelled group on a settings page. It never shrinks: the page
// scrolls instead of squeezing its groups into each other.
func (a *App) pfield(c *ui.Context, label string, body func()) {
	ui.Column(c).Gap(6).Shrink(0).Children(func() {
		ui.Text(c, label).FontSize(12).Bold().TextColor(a.tk.Text2)
		body()
	})
}

func (a *App) settingsView(c *ui.Context) {
	if !a.settings.open {
		return
	}
	st := &a.settings
	if st.page == "" {
		st.page = pageGeneral
	}
	open := true
	a.dialog(c, &open, func() {
		_, winH := c.Size()
		ui.Column(c).Key("settings").Width(760).Height(min(winH-48, 560)).Background(a.tk.Surface).Radius(16).
			Border(1, a.tk.BorderStrong).Shadow(0, 18, 48, 0, ui.RGBA(0, 0, 0, 0.2)).Clip().Children(func() {
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				a.settingsNav(c)
				ui.Column(c).Key("settings-page").Grow(1).MinWidth(0).Padding(20, 22, 12).Gap(14).Children(func() {
					ui.Text(c, a.pageTitle(st.page)).Bold().FontSize(17).Role(ui.RoleHeading).Level(1)
					ui.Scroll(c).Key("settings-scroll-" + st.page).Grow(1).MinHeight(0).Children(func() {
						ui.Column(c).Gap(16).Padding(0, 6, 0, 0).Children(func() { a.settingsPage(c, st.page) })
					})
					ui.Row(c).Justify(ui.End).Children(func() {
						if ui.PrimaryButton(c, a.T(kDone)).Clicked() {
							st.open = false
						}
					})
				})
			})
		})
	})
	if !open {
		st.open = false
	}
}

func (a *App) pageTitle(page string) string {
	return a.T(map[string]string{
		pageGeneral: kPageGeneral, pageTerminal: kPageTerminal, pageConnection: kPageConnection,
		pageMonitor: kSetMonitor, pageTransfer: kSetTransfer, pageData: kPageData, pageAbout: kPageAbout,
	}[page])
}

func (a *App) settingsNav(c *ui.Context) {
	st := &a.settings
	page := st.page
	ui.Column(c).Width(188).Padding(14, 8).Background(a.tk.Sidebar).Shrink(0).Children(func() {
		ui.Text(c, a.T(kSettings)).Bold().FontSize(13).TextColor(a.tk.Text2).Padding(0, 10, 8)
		ui.Sidebar(c, &page, func() {
			for _, it := range []struct {
				id   string
				icon *ui.SVG
			}{
				{pageGeneral, iconSettings}, {pageTerminal, iconTerminal}, {pageConnection, iconLink},
				{pageMonitor, iconChip}, {pageTransfer, iconTransfer}, {pageData, iconFolder}, {pageAbout, iconCheck},
			} {
				ui.SidebarItem(c, it.id, it.icon, a.pageTitle(it.id))
			}
		}).Label(a.T(kSettings)).Grow(1).Background(a.tk.Sidebar)
	})
	st.page = page
}

func (a *App) settingsPage(c *ui.Context, page string) {
	v := a.d.Settings.Values()
	switch page {
	case pageGeneral:
		a.generalPage(c, v)
	case pageTerminal:
		a.terminalPage(c, v)
	case pageConnection:
		a.connectionPage(c, v)
	case pageMonitor:
		a.monitorSettings(c, v.Monitor)
	case pageTransfer:
		a.transferSettings(c, v.Transfer)
	case pageData:
		a.dataPage(c)
	case pageAbout:
		a.aboutPage(c)
	}
}

func (a *App) generalPage(c *ui.Context, v settings.Values) {
	langs := []i18n.Language{"", i18n.English, i18n.Chinese}
	apps := []settings.Appearance{settings.AppearanceSystem, settings.AppearanceLight, settings.AppearanceDark}
	a.pfield(c, a.T(kSetLanguage), func() {
		a.choice(c, a.T(kSetLanguage), slices.Index(langs, v.Language), []string{a.T(kLangSystem), "English", "简体中文"},
			func(i int) { a.setPref(func(x *settings.Values) { x.Language = langs[i] }) })
	})
	a.pfield(c, a.T(kSetAppearance), func() {
		a.choice(c, a.T(kSetAppearance), slices.Index(apps, v.ResolveAppearance()), []string{a.T(kAppSystem), a.T(kAppLight), a.T(kAppDark)},
			func(i int) { a.setPref(func(x *settings.Values) { x.Appearance = apps[i] }) })
		a.caption(c, a.T(kSetAppearanceHint))
	})
	a.pfield(c, a.T(kSetCompact), func() {
		a.toggle(c, a.T(kSetCompact), v.CompactBlocks, func(on bool) { a.setPref(func(x *settings.Values) { x.CompactBlocks = on }) })
		a.caption(c, a.T(kSetCompactHint))
	})
}

func (a *App) terminalPage(c *ui.Context, v settings.Values) {
	t := v.Terminal
	a.pfield(c, a.T(kSetScheme), func() {
		a.schemeGrid(c, t)
	})
	a.pfield(c, a.T(kSetImportColors), func() {
		a.pathRow(c, "colors", &a.settings.colorsPath, a.T(kSetColorsPlaceholder), a.T(kSetImport), false, func(path string) {
			a.settings.colorsMsg = a.importColors(path)
		})
		if a.settings.colorsMsg != "" {
			a.caption(c, a.settings.colorsMsg).Role(ui.RoleStatus)
		}
	})
	a.pfield(c, a.T(kSetFont), func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.number(c, a.T(kSetFontSize), t.Font(), settings.MinFontSize, settings.MaxFontSize, 1,
				func(n int) { a.setPref(func(x *settings.Values) { x.Terminal.FontSize = n }) })
		})
	})
	a.pfield(c, a.T(kSetBehavior), func() {
		a.toggle(c, a.T(kSetCopyOnSelect), t.CopyOnSelect, func(on bool) { a.setPref(func(x *settings.Values) { x.Terminal.CopyOnSelect = on }) })
		a.toggle(c, a.T(kSetKeepContrast), t.KeepContrast, func(on bool) { a.setPref(func(x *settings.Values) { x.Terminal.KeepContrast = on }) })
		a.caption(c, a.T(kSetKeepContrastHint))
	})
}

// schemeGrid shows the color schemes as swatches: what the terminal would
// look like, the name below, the chosen one ringed.
func (a *App) schemeGrid(c *ui.Context, t settings.Terminal) {
	type card struct {
		id, name string
		sc       scheme.Scheme
		custom   bool
	}
	def := a.tk
	def.Scheme = nil
	defScheme := scheme.Scheme{
		ID: scheme.Default, Name: a.T(kSchemeDefault), Foreground: hexOf(def.TermFg), Background: hexOf(def.TermBg),
	}
	cards := []card{{id: scheme.Default, name: a.T(kSchemeDefault), sc: defScheme}}
	for _, s := range scheme.Builtin {
		cards = append(cards, card{id: s.ID, name: s.Name, sc: s})
	}
	for _, s := range t.Custom {
		cards = append(cards, card{id: s.ID, name: s.Name, sc: s, custom: true})
	}
	const perRow = 3
	ui.Column(c).Key("schemes").Gap(10).Children(func() {
		for row := range slices.Chunk(cards, perRow) {
			ui.Row(c).Key("scheme-row-" + row[0].id).Gap(10).Children(func() {
				for _, cd := range row {
					a.schemeCard(c, t, cd.id, cd.name, cd.sc, cd.custom)
				}
			})
		}
	})
	a.caption(c, a.T(kSchemeHint))
}

// schemeCard is one swatch of the grid.
func (a *App) schemeCard(c *ui.Context, t settings.Terminal, id, name string, sc scheme.Scheme, custom bool) {
	sel := id == t.Scheme
	border, bw := a.tk.Border, float32(1)
	if sel {
		border, bw = a.tk.Accent, 2
	}
	box := ui.Column(c).Key("scheme-"+id).Width(164).Gap(6).Padding(6).Radius(10).Background(a.tk.Surface2).
		Border(bw, border).Cursor(ui.CursorPointer).Focusable().Role(ui.RoleRadio).AccessSelected(sel).Label(name)
	box.Children(func() {
		a.schemePreview(c, sc, id == scheme.Default)
		ui.Text(c, name).FontSize(12).Bold().NoWrap().Ellipsis("…")
	})
	if box.Clicked() {
		a.setPref(func(x *settings.Values) { x.Terminal.Scheme = id })
	}
	if custom {
		box.ContextMenu(func(m *ui.Menu) {
			if m.Item(a.T(kSchemeRemove)).Chosen() {
				a.setPref(func(x *settings.Values) {
					x.Terminal.Custom = slices.DeleteFunc(slices.Clone(x.Terminal.Custom), func(s scheme.Scheme) bool { return s.ID == id })
					if x.Terminal.Scheme == id {
						x.Terminal.Scheme = scheme.Default
					}
				})
			}
		})
	}
}

func hexOf(c ui.Color) string {
	return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
}

// schemePreview is a miniature terminal in the scheme's colors.
func (a *App) schemePreview(c *ui.Context, s scheme.Scheme, follows bool) {
	bg, fg := theme.Hex(s.Background), theme.Hex(s.Foreground)
	ui.Column(c).Height(58).Padding(7, 9).Gap(4).Radius(6).Background(bg).Border(1, a.tk.Border).Children(func() {
		ui.Text(c, "~ ❯ ls -la").FontSize(10.5).TextColor(fg).NoWrap()
		ui.Row(c).Gap(4).Children(func() {
			for i := 1; i <= 6; i++ {
				col := fg
				if !follows && scheme.Valid(s.Palette[i]) {
					col = theme.Hex(s.Palette[i])
				} else if follows {
					col = []ui.Color{a.tk.TermRed, a.tk.TermGreen, a.tk.TermYellow, a.tk.TermBlue, a.tk.TermMagenta, a.tk.TermCyan}[i-1]
				}
				ui.Box(c).Size(14, 8).Radius(3).Background(col)
			}
		})
	})
}

// importColors reads an iTerm2 preset and adds it as a custom scheme.
func (a *App) importColors(path string) string {
	if a.d.ImportColors == nil {
		return a.T(kSetUnavailable)
	}
	s, err := a.d.ImportColors(path)
	if err != nil {
		return a.T(kSetColorsFailed, err)
	}
	a.setPref(func(x *settings.Values) {
		x.Terminal.Custom = scheme.Merge(x.Terminal.Custom, s)
		x.Terminal.Scheme = s.ID
	})
	return a.T(kSetColorsDone, s.Name)
}

// pathRow is a path field with a Choose button and an action button.
func (a *App) pathRow(c *ui.Context, key string, path *string, placeholder, action string, save bool, run func(string)) {
	ui.Row(c).Key("path-" + key).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c, path).Label(placeholder).Placeholder(placeholder).Grow(1).MinWidth(0)
		pick := a.d.PickFile
		if save && a.d.PickSave != nil {
			pick = func(title string) (string, error) { return a.d.PickSave(title, filepath.Base(*path)) }
		}
		if pick != nil && ui.Button(c, a.T(kChoose)).Clicked() {
			if p, err := pick(placeholder); err == nil && p != "" {
				*path = p
			}
		}
		if ui.Button(c, action).Clicked() && strings.TrimSpace(*path) != "" {
			run(strings.TrimSpace(*path))
		}
	})
}

func (a *App) connectionPage(c *ui.Context, v settings.Values) {
	a.pfield(c, a.T(kSetKeepAlive), func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.number(c, a.T(kSetKeepAliveEvery), v.Connection.KeepAlive(), 5, 300, 5,
				func(n int) { a.setPref(func(x *settings.Values) { x.Connection.KeepAliveSec = n }) })
		})
		a.caption(c, a.T(kSetKeepAliveHint))
	})
	a.pfield(c, a.T(kSetHostKeys), func() {
		a.caption(c, a.T(kSetHostKeysHint))
	})
}

func (a *App) monitorSettings(c *ui.Context, m settings.Monitor) {
	a.pfield(c, a.T(kSetMonitor), func() {
		a.toggle(c, a.T(kSetMonitorOn), !m.Off, func(on bool) { a.setPref(func(x *settings.Values) { x.Monitor.Off = !on }) })
		ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
			a.number(c, a.T(kSetMonitorEvery), m.Interval(), 1, 60, 1, func(n int) { a.setPref(func(x *settings.Values) { x.Monitor.IntervalSec = n }) })
			a.number(c, a.T(kSetMonitorWarn), m.Warn(), 1, 99, 5, func(n int) {
				a.setPref(func(x *settings.Values) { x.Monitor.WarnPct = n; x.Monitor.CritPct = max(x.Monitor.Crit(), n) })
			})
			a.number(c, a.T(kSetMonitorCrit), m.Crit(), 1, 100, 5, func(n int) {
				a.setPref(func(x *settings.Values) { x.Monitor.CritPct = n; x.Monitor.WarnPct = min(x.Monitor.Warn(), n) })
			})
		})
	})
}

func (a *App) transferSettings(c *ui.Context, t settings.Transfer) {
	conflicts := []settings.Conflict{settings.ConflictAsk, settings.ConflictOverwrite, settings.ConflictSkip}
	verifies := []string{"auto", "hash", "size"}
	a.pfield(c, a.T(kSetTransfer), func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			a.number(c, a.T(kSetParallel), t.Parallel(), 1, 8, 1, func(n int) { a.setPref(func(x *settings.Values) { x.Transfer.Concurrency = n }) })
		})
		a.choice(c, a.T(kSetConflict), slices.Index(conflicts, t.Policy()),
			[]string{a.T(kSetConflictAsk), a.T(kSetConflictOver), a.T(kSetConflictSkip)},
			func(i int) { a.setPref(func(x *settings.Values) { x.Transfer.Conflict = conflicts[i] }) })
		verify := max(slices.Index(verifies, cmpOr(t.Verify, "auto")), 0)
		a.choice(c, a.T(kSetVerify), verify, []string{a.T(kSetVerifyAuto), a.T(kSetVerifyHash), a.T(kSetVerifySize)},
			func(i int) { a.setPref(func(x *settings.Values) { x.Transfer.Verify = verifies[i] }) })
		a.toggle(c, a.T(kSetDirect), t.AllowDirect, func(on bool) { a.setPref(func(x *settings.Values) { x.Transfer.AllowDirect = on }) })
		a.caption(c, a.T(kSetDirectHint))
	})
}

func cmpOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (a *App) dataPage(c *ui.Context) {
	st := &a.settings
	if st.exportPath == "" {
		st.exportPath = a.d.DefaultExport
	}
	a.pfield(c, a.T(kDataExport), func() {
		a.pathRow(c, "export", &st.exportPath, a.T(kDataExportPlaceholder), a.T(kDataExportAction), true, func(p string) {
			st.exportMsg = a.exportHosts(p)
		})
		a.caption(c, a.T(kDataExportHint))
		if st.exportMsg != "" {
			a.caption(c, st.exportMsg).Role(ui.RoleStatus)
		}
	})
	a.pfield(c, a.T(kDataImport), func() {
		a.pathRow(c, "import", &st.importPath, a.T(kDataImportPlaceholder), a.T(kSetImport), false, func(p string) {
			st.importMsg, st.importWarns = a.importHosts(p)
		})
		a.caption(c, a.T(kDataImportHint))
		if st.importMsg != "" {
			a.caption(c, st.importMsg).Role(ui.RoleStatus)
			for _, w := range st.importWarns[:min(len(st.importWarns), 5)] {
				a.caption(c, "• "+w).TextColor(a.tk.Amber)
			}
			if n := len(st.importWarns) - 5; n > 0 {
				a.caption(c, a.T(kDataMoreWarnings, n))
			}
		}
	})
	a.pfield(c, a.T(kDataFolders), func() {
		a.caption(c, a.T(kDataConfigDir, a.d.Paths.Config)).Selectable()
		a.caption(c, a.T(kDataLogDir, a.d.Paths.Logs)).Selectable()
	})
}

func (a *App) exportHosts(path string) string {
	if a.d.ExportHosts == nil {
		return a.T(kSetUnavailable)
	}
	if err := a.d.ExportHosts(path); err != nil {
		return a.T(kDataExportFailed, err)
	}
	return a.T(kDataExportDone, a.d.Library.Len(), path)
}

func (a *App) importHosts(path string) (string, []string) {
	if a.d.ImportHosts == nil {
		return a.T(kSetUnavailable), nil
	}
	sum, err := a.d.ImportHosts(path)
	if err != nil {
		return a.T(kImportFailed, err), sum.Warnings
	}
	msg := a.T(kImportDone, sum.Added, sum.Skipped)
	if sum.Passwords > 0 {
		msg += " " + a.T(kDataPasswordsNotImported, sum.Passwords)
	}
	return msg, sum.Warnings
}

func (a *App) aboutPage(c *ui.Context) {
	st := &a.settings
	a.pfield(c, "Tako", func() {
		a.caption(c, a.T(kSetVersion, a.d.Version)).Selectable()
		a.caption(c, a.T(kAboutLicense))
	})
	if a.d.CrashNotice != nil {
		if note := a.d.CrashNotice(); note != "" {
			ui.Row(c).Gap(8).Padding(10, 12).Radius(10).Background(a.tk.Amber.Alpha(0.14)).Children(func() {
				ui.Icon(c, iconAlert).FontSize(14).TextColor(a.tk.Amber)
				ui.Text(c, a.T(kAboutCrashed, note)).FontSize(12).Grow(1).MinWidth(0).Selectable()
			})
		}
	}
	a.pfield(c, a.T(kAboutUpdates), func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			if a.d.CheckUpdates != nil && ui.Button(c, a.T(kAboutCheck)).Clicked() {
				a.d.CheckUpdates()
			}
		})
		if a.d.UpdateAvailable != nil {
			if v := a.d.UpdateAvailable(); v != "" {
				a.caption(c, a.T(kAboutAvailable, v)).TextColor(a.tk.Accent).Role(ui.RoleStatus)
			}
		}
		a.caption(c, a.T(kAboutUpdatesHint))
	})
	a.pfield(c, a.T(kAboutDiagnostics), func() {
		if a.d.Diagnostics != nil && ui.Button(c, a.T(kAboutCopyDiag)).Clicked() {
			a.writeClipboard(a.d.Diagnostics())
			st.diagMsg = a.T(kAboutCopied, time.Now().Format("15:04:05"))
		}
		a.caption(c, a.T(kAboutDiagHint))
		if st.diagMsg != "" {
			a.caption(c, st.diagMsg).Role(ui.RoleStatus)
		}
	})
}
