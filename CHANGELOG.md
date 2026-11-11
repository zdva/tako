# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/) (pre-release tags `-mN` mark milestones).

## [Unreleased]

## [0.2.0-m2] - 2026-11-11

Second milestone: synchronized input UI, minimap, draggable layouts. Local
tag only; nothing has been published.

### Added

- Sync UI: a synced block has a 1 px coral border and one small chip; sync is
  switched from the chip, minimap, command palette, shortcuts and menus; all
  on/off (`⌘⌥⇧S`, `Ctrl+Shift+Alt+A`); a disconnected synced block is paused and
  resumes after reconnect without affecting the others.
- Floating minimap: 176 px, translucent at rest and opaque on hover or drag;
  opacity slider; click toggles sync, box select (⇧ adds, ⌥ inverts), drag to
  reorder, corner snapping, show/hide; position and opacity are remembered.
- Manual layout: binary split tree (`internal/layout.Tree`), draggable
  dividers snapping to ½, ⅓, ⅔ with a guide line (⌥ disables), minimum block
  sizes, about 150 ms spring animation, zoom a block (`⌘⇧↵`, double-click),
  layout presets; manual layout, sync flags and minimap prefs survive restarts.
- Full shortcut set per DESIGN §4.6 ([docs/SHORTCUTS.md](docs/SHORTCUTS.md)),
  right-click menus (block header, tab, sidebar host and tag, minimap), block
  rename, find bar, move block to…, monitor and transfer entries (notice until M3).
- Hidden title bar with the brand row and toolbar as drag handles.
- `session.Options.ResizeDelay` (tests); ADRs 0009–0011; e2e frame-rate,
  restart and screenshot tests; `make screenshots MILESTONE=m2`.

### Changed

- UI aligned with the final design's no-redundancy rule: removed the
  "auto layout 3×2" toolbar button, the big "syncing" buttons, "session saved"
  and "connected" texts, the always-green status dot, the sidebar sync link
  icon, status-bar counts and latency, and the minimap's title, close button
  and hint row. Toolbar: search, minimap toggle, transfer icon.
- The status bar shows the focused host and the terminal's size and encoding.

### M2 acceptance (Linux VM, Xvfb, Mesa software renderer; fresh clone)

| # | Criterion | Result |
|---|---|---|
| 1 | 9 synced blocks get identical bytes, no cross-replies; a 10 KB/s target does not delay the other 8 | Pass (`inputbus` acceptance tests, StripReplies fuzz; real window: one command ran in all 9 blocks) |
| 2 | Minimap click, box (⇧/⌥), reorder, corner snap, opacity in `ui.Tester`; translucent vs opaque by screenshot | Pass (Tester tests; e2e compares real-window pixels: 94% of the header pixels change between rest and hover) |
| 3 | Divider snapping ½/⅓/⅔ with guide; layout property tests | Pass |
| 4 | Disconnected block's sync pauses, others unaffected, resumes | Pass |
| 5 | Resize and layout drag debounce terminal resizes | Pass: 60 pointer moves, 2 blocks, 2 window-size changes, none during the drag |
| 6 | Restart restores manual layout and sync state | Pass (Tester; real window with `kill -9`) |
| 7 | Shortcuts match the DESIGN table | Pass (test parses DESIGN.md §4.6) |
| 8 | 9-block flood ≥ 18 fps | Pass: 59 frames/s, mean frame 3.5 ms (1440×900, software renderer) |

Also: idle with 9 blocks CPU 0.00%, private memory 24 MB (RSS 71 MB), cold
start 0.15 s, `kill -9` recovery 0.21 s; core coverage 85.0%; `govulncheck`
clean; lint clean.

### Known issues and limits

- The terminal's own right-click menu (Copy, Paste, Select All, Clear) cannot
  be extended, so "search" and "open current directory" are not in it
  (ADR 0011).
- Hover uses the toolkit's own opacity animation; frosted-glass blur was not
  attempted, so the minimap falls back to plain translucency.
- Windows and macOS builds compile but have never been run, so the hidden
  title bar is only checked under X11.
- Monitor and transfer entries show a notice until M3; footer metrics are
  placeholders.
- Module path and licence are still placeholders.

## [0.1.0-m1] - 2026-11-11

First milestone: connect, terminal, grid, sidebar. Local tag only; nothing
has been published.

### Added

- SSH transport: password, public key (OpenSSH, PEM, PuTTY PPK, plain and
  encrypted), SSH agent and keyboard-interactive auth; jump-host chains;
  SOCKS5 and HTTP CONNECT proxies; keep-alives; extra connection when the
  server's MaxSessions is reached.
- `session.Pane` contract: `Read` never reaches EOF during an outage, `Write`
  drops and counts, debounced resize, reconnect backoff 1/2/5/10/30 s with
  jitter, bounded parallel dials, localized "connection lost" notice.
- Host-key policy: Tako's own `known_hosts`, ask/strict policies, changed keys
  always refused with old and new fingerprints, every jump hop verified.
- Host library (`hosts.json`) with tags, tag colours, OS detection, import of
  `~/.ssh/config` (`Include`, `ProxyJump`, `IdentityFile`).
- Secrets only in the system keychain (session-only fallback); redacting
  structured logs; atomic JSON storage with locking, backups and migration hooks.
- Workspace: blocks, tabs, focus, local terminals, sync toggle mechanism
  (InputBus with reply filtering), session save/restore (1 s debounce,
  crash-safe).
- Adaptive grid (1, 2, 2×2, 3×2, scrolling) and the "compact blocks" setting.
- UI in MyGo: window, toolbar, sidebar with groups and search, block headers
  and footers, reconnect banner, add/edit host sheet with connection test,
  command palette, settings, prompts (host key, password, challenge), empty
  state with import cards, light and dark themes, English and 简体中文.
- Tests: unit and `ui.Tester` suites, integration suite against a real OpenSSH
  (`make integration`), live-window e2e suite under Xvfb (`make e2e`),
  fuzz targets, goleak; core coverage 83.5%.
- Tooling: golangci-lint v2 config with dependency-direction rules, Makefile,
  cross-compile for Windows/macOS/Linux (`make build-all`), GitHub Actions CI,
  dependabot, issue and PR templates.
- Docs: README (en, zh-CN), AGENTS.md, CONTRIBUTING, SECURITY, Code of Conduct,
  spec summary, design, tech plan, testing guide, ADRs 0001–0008, M1 screenshots.

### Fixed

- Terminals created before the window's first frame were drawn black under
  Mesa's software GL; the session is now restored after the first frame.
- `Shell.Close` could race between a reconnect and closing a tab.

### Security

- Built with Go 1.27.4 (`toolchain` directive) to include the `net/http` fix
  GO-2026-6859, and `golang.org/x/net` v0.62.0. `govulncheck`: no reachable
  vulnerabilities. (`golang.org/x/crypto` v0.58.0 has an open advisory for the
  unused `openpgp` package; Tako does not import it.)

### M1 acceptance (measured on a Linux VM, Xvfb, Mesa software renderer)

| # | Criterion | Result |
|---|---|---|
| 1 | Auth × jump × proxy matrix; vim/top usable | Pass for key, agent and PPK against a real OpenSSH; password and keyboard-interactive against the in-process server (an unprivileged sshd cannot do PAM) |
| 2 | Outage: banner, scrollback kept, no EOF, input not replayed | Pass in the integration test (relay paused, then cut; the outage is shorter than 60 s and the backoff is shortened, the mechanism is the same) |
| 3 | Changed host key refused with a clear message | Pass |
| 4 | `~/.ssh/config` import ≥ 12 hosts | Pass (14-host fixture with Include, ProxyJump, IdentityFile) |
| 5 | 9 idle blocks: 0% CPU, RSS ≤ 100 MB, cold start ≤ 1 s | CPU 0.00% over 10 s; cold start: window in 0.13–0.15 s, 9 blocks connected in 0.19–0.22 s (0.5–0.6 s on a first run with an empty fontconfig cache). **RSS depends on the Xvfb setup:** 63.8 MB on a plain long-running Xvfb, 116.5 MB under `xvfb-run` where Mesa loads LLVM (87 MB of that is shared libraries; private memory is 21–30 MB). The e2e test asserts private memory ≤ 100 MB and logs total RSS. Total RSS on a real GPU is unmeasured (see 9) |
| 6 | Restore after quit; `kill -9` recovers ≤ 1 s | Pass; window 0.14 s, 9 reconnected 0.20 s |
| 7 | Secrets absent from files, logs, export | Pass (grep test) |
| 8 | Race, lint, govulncheck green; coverage ≥ 80% | Pass; core coverage 83.5% |
| 9 | GPU baseline on real macOS/Windows | **Not done** (no such hardware here) |

### Known issues and limits

- Windows and macOS builds compile but have never been run. CI runs their unit
  tests as non-blocking until seen green.
- Not code-signed.
- Local terminals use MyGo's built-in PTY.
- Jump chains are flat (one list); one auth method per host in the sheet.
- Block footer metrics are placeholders (M3); no minimap, rename or
  sync UI (M2); no SFTP (M3); only OpenSSH config import (M4 adds others).
- Default OS title bar instead of the designed custom one.
- A plain `go build` binary downloads `libghostty-vt` on first use.
- Module path and licence are placeholders.

