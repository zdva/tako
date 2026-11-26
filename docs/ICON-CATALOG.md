# Tako Icon Catalog

This catalog records the icon assets currently used by the application and
defines a handoff format for a replacement SVG set.

## Inventory

There are 32 distinct hand-drawn UI SVG glyphs in `internal/ui/icons.go`: 28
general interface symbols, of which `iconTerminal` is shared as the local-host
mark, plus 4 additional host-system marks (`iconLinux`, `iconWindows`,
`iconApple`, `iconServer`). UI SVGs use a `24 × 24` viewBox.
General symbols use a 2 px rounded stroke with `currentColor`; system marks
are filled silhouettes whose foreground and tile colors are chosen by the UI.

### General UI symbols

| Go identifier | Suggested SVG filename | Meaning and current use |
|---|---|---|
| `iconPlus` | `plus.svg` | Add a host or tab; add buttons |
| `iconSearch` | `search.svg` | Host palette and search fields |
| `iconClose` | `close.svg` | Close tab, dialog, tag, hop, or transfer job |
| `iconChevronR` | `chevron-right.svg` | Breadcrumbs and nested navigation |
| `iconChevronD` | `chevron-down.svg` | Expand/collapse groups; find next |
| `iconChevronU` | `chevron-up.svg` | Find previous |
| `iconMore` | `more-horizontal.svg` | Block actions menu |
| `iconGrip` | `grip.svg` | Minimap drag handle |
| `iconSidebar` | `sidebar.svg` | Collapse or expand sidebar |
| `iconMinimap` | `minimap.svg` | Toggle minimap |
| `iconTerminal` | `terminal.svg` | Local terminal and terminal settings |
| `iconSettings` | `settings.svg` | Settings navigation and entry point |
| `iconSync` | `sync.svg` | Synchronized input state |
| `iconTransfer` | `transfer.svg` | Open transfer view and transfer heading |
| `iconUp` | `arrow-up.svg` | Upload action and transfer direction |
| `iconDown` | `arrow-down.svg` | Download direction |
| `iconSwap` | `swap.svg` | Reverse transfer endpoints; server-to-server direction |
| `iconRefresh` | `refresh.svg` | Retry or reconnect feedback |
| `iconPause` | `pause.svg` | Pause a transfer job |
| `iconPlay` | `play.svg` | Resume a transfer job |
| `iconEye` | `eye.svg` | Reveal password field |
| `iconLock` | `lock.svg` | Host-key/security indicator |
| `iconLink` | `link.svg` | Connection settings; jump/proxy relationships |
| `iconFolder` | `folder.svg` | Folder rows, open destination, data settings |
| `iconFile` | `file.svg` | File rows in transfer browser |
| `iconChip` | `chip.svg` | CPU/GPU/system information and monitor panels |
| `iconCheck` | `check.svg` | Successful connection test; About navigation |
| `iconAlert` | `alert.svg` | Error, warning, or disconnected feedback |

### Host-system marks

These are filled, single-color symbols. The 26 px background tile and light/dark
foreground colors are applied in `internal/ui/style.go`; the SVG should not
contain its own colored tile.

| Go identifier | Suggested SVG filename | Meaning |
|---|---|---|
| `iconLinux` | `linux.svg` | Linux host mark |
| `iconWindows` | `windows.svg` | Windows four-pane mark |
| `iconApple` | `apple.svg` | macOS host mark |
| `iconServer` | `server.svg` | Unknown/other remote host |
| `iconTerminal` | `terminal.svg` | Local machine mark; shared with terminal action (same asset, not an additional glyph) |

### Brand and non-SVG indicators

| Asset or drawing | Current location | Notes |
|---|---|---|
| Tako application icon | `resources/icon-source.png`, generated outputs under `resources/` and `resources/icons/` | Separate from the UI glyph set. macOS uses `tako.icns`; Windows output is `tako.ico`; Linux packages PNG/SVG. The PNG source drives raster exports, while the SVG output is generated as four-color vector paths traced from the source. |
| In-app brand image | `internal/ui/assets/brand.png`, `brand-light.png` | The dark and light PNG variants are generated from the app-icon source and selected by the current app appearance. |
| Online/disconnected/status dots | UI components in `internal/ui/` | Drawn as colored circles, not named SVG icons. |
| Monitor plots and meters | `internal/ui/chart.go`, `internal/ui/monitorview.go` | Drawn from chart geometry and colored bars. |
| Keyboard shortcut glyphs | `internal/ui/shortcuts.go` and labels | Text such as `⌘`, arrows, and `↵`, not icon assets. |
| Terminal prompt in the brand icon | `resources/icon-source.png` | Part of the application brand artwork; not `iconTerminal`. |

## Where Icons Are Used

| Area | Main source files | Typical symbols |
|---|---|---|
| Window chrome, sidebar, palette | `chrome.go`, `sidebar.go`, `palette.go` | Sidebar, minimap, transfer, search, plus, settings, terminal, chevrons |
| Blocks, tabs, connection feedback | `main.go`, `hostsheet.go` | Plus, close, more, sync, alert, refresh, check, eye |
| Settings navigation | `settingsview.go` | Settings, terminal, link, chip, transfer, folder, check |
| Minimap | `minimap.go` | Grip |
| Transfer browser and queue | `transfer.go` | Transfer, close, swap, up, down, pause, play, folder, file |
| Host OS tiles | `style.go` | Linux, Windows, Apple, server, terminal |
| Monitor panels | `monitorview.go` | Chip |

`internal/ui/icons_test.go` is the inventory smoke test: every glyph is parsed,
painted, and checked against light and dark current colors there. Add new
imported SVGs to that test so missing, invalid, or fixed-color assets are caught.

## SVG Handoff Contract

When a new icon set is designed, deliver one standalone SVG per identifier,
using the filenames above. Keep the existing semantic identifiers unless an
icon is intentionally removed or split into separate meanings.

- UI glyphs: `viewBox="0 0 24 24"`, transparent background, no embedded bitmap,
  scripts, external references, or fixed CSS color.
- General symbols: 2 px rounded stroke, `fill="none"`, `stroke="currentColor"`.
- System marks: single filled silhouette with `fill="currentColor"`; no tile,
  status dot, or OS-specific color baked into the SVG.
- Use simple SVG path/shape elements supported by MyGo's SVG parser.
- Preserve accessible naming at call sites; SVG filenames do not replace button
  labels or localized text.
- Put the delivered set in `internal/ui/assets/icons/`. The next import change
  can load and parse these files in one place in `icons.go`, while call sites
  continue to refer to semantic Go identifiers.

For the application brand, provide a separate square PNG source with the full
icon artwork and no external white page margin. Keep it separate from the
24 px interface icon family; small platform sizes are raster exports and should
be visually checked at 16 and 32 px.
