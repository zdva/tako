# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/) (pre-release tags `-mN` mark milestones).

## [Unreleased]

### Changed

- License is now GPL-3.0-or-later (ADR 0022). The Go module path and public
  repository are `github.com/zdva/tako`. Third-party notices are in
  [`THIRD_PARTY_LICENSES.md`](THIRD_PARTY_LICENSES.md).

## [0.4.0] - 2026-11-11

First full release candidate. Local tag only; nothing has been published, and
the installers and updater have not been run on real macOS or Windows machines
(see [docs/RELEASE.md](docs/RELEASE.md)).

### Added

- Settings window (General, Terminal, Connection, Monitoring, File transfer,
  Data, About) replacing the old settings sheet. Language and appearance apply
  at once, with no restart; terminals recolor live (ADR 0017).
- Terminal color schemes: Tako (follows appearance), Solarized Dark / Light,
  Dracula, Nord, One Dark, Gruvbox Dark and Tokyo Night, plus import of
  iTerm2 `.itermcolors` files. Font size, copy on select, keep text readable,
  keepalive interval.
- Importers for Xshell (`.xsh`, `.xts`, folders; UTF-8 or UTF-16 files), SecureCRT (`.ini` folders and the XML export), Termius
  (CSV) and Tako's own export, all fuzzed and size-limited (ADR 0018). Saved
  passwords are never imported; the summary says how many were left out.
- Export of hosts, groups and tags without any secret or secret reference;
  importing it on another computer remaps IDs and jump chains.
- Pseudo-locale (`en-XA`) and region-aware number, date and clock formats; every
  user-visible string comes from the English or Simplified Chinese tables
  (ADR 0019).
- Crash reports (`internal/diag`): a panic on any goroutine writes a report; the
  next start says so in About. "Copy diagnostics" puts a scrubbed report on the
  clipboard (ADR 0020).
- Signed auto-update from GitHub Releases through MyGo's updater; packaging
  for tar.gz, .deb and PKGBUILD here, dmg and NSIS in CI, `.rpm` through nfpm
  in CI, SHA256SUMS; no Apple or Windows certificates (ADR 0021).
- Accessibility: roles, names and Tab stops for host rows, tabs, footer
  buttons and the search box; the settings window keeps focus inside; IME
  composition tests for text fields and terminals.
- Docs: [docs/RELEASE.md](docs/RELEASE.md), [docs/IMPORT.md](docs/IMPORT.md),
  [docs/MANUAL.md](docs/MANUAL.md), ADRs 0017–0021.

### Changed

- Terminals follow the appearance and the chosen scheme on the fly (before,
  open terminals kept their colors until restart).
- Footer meters shrink their bars instead of wrapping labels in narrow blocks.
- Encodings: UTF-8 only, for good (ADR 0005); GBK and other code pages are out
  of scope.

### M4 acceptance (Linux VM, Xvfb, Mesa software renderer; fresh clone of the tagged commit)

| # | Criterion | Result |
|---|---|---|
| 1 | Installers install, start, connect and update on clean systems of all three platforms | **Not met, needs people and machines.** On this Debian 13 box the `.deb` installed, started and uninstalled cleanly, and the `.tar.gz`, PKGBUILD and `.rpm` (nfpm) build; a signed update manifest is produced with a throw-away key. The dmg and NSIS installers are built by CI only (no `makensis` or macOS here) and the updater has never installed an update end to end |
| 2 | Light/dark switch without a restart; `theme` tokens equal `DESIGN.md` | Pass: `TestTokensMatchDesignDoc`, `TestSettings_DarkAppearanceChangesTerminalToo`, `TestSettings_SchemeRecolorsOpenTerminalsWithoutARestart` |
| 3 | English and Chinese complete; pseudo-locale shows no missing key, hard-coded text or text outside the window | Pass: `TestKeys_MatchLocales`, `TestPseudoLocale_EverySurfaceComesFromTheTables` (main window, host sheet, palette, every settings page, transfer sheet). Truncation is checked as "inside the window", not by pixel |
| 4 | An export imported on another computer works and contains no secret (grep) | Pass: `TestSettings_ExportHasNoSecretsAndImportsOnAnotherComputer`, `TestExportImportMovesHostsToAnotherComputerWithoutSecrets` (jump chain remapped) |
| 5 | Performance budgets; `goleak` and `-race` green | Pass here: nine idle blocks 0.40% of a core, 129 MB RSS (35 MB private), monitored 0.93% and 0.00 frames/s, flood 59 frames/s, cold start 0.14 s, recovery after `kill -9` 0.29 s; core coverage 83.3%; `make race` and `make vuln` green; five fuzz targets run 20 s each |
| 6 | Manual checklist on a real Mac and a real Windows | **Not done**: no such machines ([docs/RELEASE.md](docs/RELEASE.md)) |
| 7 | Linux smoke on Ubuntu 22.04/24.04 and Fedora | **Not done**: only Debian 13 was available |

### Known limitations

- Not run on real macOS or Windows hardware; installers for those platforms are
  unsigned, so Gatekeeper and SmartScreen will warn (docs/RELEASE.md explains how
  to add signing later).
- The importers were written from public format documentation and fixtures
  composed here, not from real exports.

## [0.3.0-m3] - 2026-11-11

Third milestone: monitoring and file transfer. Local tag only; nothing has been
published.

### Added

- Agentless monitoring (`internal/monitor`, ADR 0012): one command per sample
  over the block's SSH session, nothing installed on the server. Collectors for
  Linux (`/proc`, `df`, `nvidia-smi` / `rocm-smi` when present), macOS (`top`,
  `vm_stat`, `iostat`, `netstat`) and Windows (PowerShell `-EncodedCommand`
  over CIM). `Sampler` (jitter, pause, backoff), `Series` history (1 h), and a
  `Hub` that wakes the window only when a footer would look different.
- Footer resource bars on every block (CPU, memory, disk or GPU, latency;
  yellow ≥ 70%, red ≥ 90%, configurable) and the monitor page opened from the
  footer, `⌘⌥M` or the block menu: overview cards, GPU chart and per-card
  status (only on hosts with a GPU), CPU, memory and network charts, disks,
  about; 1 / 5 / 15 / 60 minute windows and pause.
- File transfer (`internal/transfer`, ADRs 0013–0015): upload, download and
  server-to-server through this computer (streamed, nothing written to disk),
  or direct `scp` / `rsync` with a temporary agent forward (opt-in; no private
  key is written to the server). Two-pane SFTP browser, folders, multi-select,
  drag and drop, queue with pause / resume / cancel, speed and time left,
  resume from the byte where the link broke, retry, size and optional SHA-256
  verification, overwrite / skip / ask policies, concurrency setting. Toolbar
  icon with a badge, `⌘⇧T`, host menu "Transfer files…".
- Transfers use their own connection per host so a copy never starves a shell
  (ADR 0014); `transport.FS` (SFTP via `github.com/pkg/sftp`), `Ping` and
  agent-forwarded exec on the SSH transport.
- OSC 7 current folder (ADR 0016, [docs/OSC7.md](docs/OSC7.md)): new tabs and
  the transfer sheet open in the folder the shell reports; without a report
  Tako asks; the palette copies the bash/zsh/fish hook.
- Tests: collector parsers on recorded (Linux) and composed (macOS, Windows,
  NVIDIA, ROCm) fixtures, Linux collector against the real local sshd,
  transfer queue/resume/policies/direct unit tests with goleak, integration
  tests for relay, resume after a cut link, policies and direct mode, e2e for
  nine monitored hosts; `ui.Tester` tests of the monitor page and transfer sheet.

### Changed

- The nine-terminal idle e2e budget is measured with monitoring off;
  monitoring has its own test.
- Monitor and transfer shortcuts and entries are no longer notices.

### M3 acceptance (Linux VM, Xvfb, Mesa software renderer; fresh clone)

| # | Criterion | Result |
|---|---|---|
| 1 | Readings within 5 points of the system tools; parser tests for all three systems | Linux: pass against the real sshd (CPU 13.6% vs `/proc/stat` 13.5%, 47.7% vs 47.7% under load; memory equal). macOS, Windows, NVIDIA, ROCm: parsers pass on **hand-composed** fixtures only; **not validated on real machines or GPUs** |
| 2 | Remote cost < 1%; client idle ≈ 0%; no whole-window redraw every second | Remote: 4.2 ms CPU per sample = 0.21% of a core at 2 s. Client, nine hosts at 2 s: 0.6–0.7% of one core (0.00 frames/s); monitoring off: 0.00% |
| 3 | 2.4 GB relay: ≥ 80% of the link, nothing on local disk, memory growth < 32 MB | Pass: 2400 MiB over 100 MB/s links: 100 MB/s vs 99 MB/s raw SFTP = 100%; heap growth 12.7 MiB; 0 KiB written by the client. Unthrottled loopback (informational): 69% on a shared CPU |
| 4 | Link cut mid-transfer: Paused, resumes from the break, verified; also for folders | Pass (file: paused at 160 of 400 MiB, resumed from 144 MiB; folder likewise) |
| 5 | Direct mode only when enabled; no key left on the server; forwarding off afterwards | Pass for `scp` against a real sshd; the `rsync` branch is covered with a fake environment only (no rsync on the box) |
| 6 | Overwrite / skip / ask policies | Pass (unit and integration on a real SFTP server) |
| 7 | Monitor page and transfer sheet match `monitor.png` / `transfer.png` in structure | Pass in `ui.Tester` structure tests; screenshots in `docs/screenshots/m3/` |

### Known issues and limits

- macOS and Windows collectors and the GPU parsers have never seen a real
  machine; replace the composed fixtures with recordings before a release
  (`internal/monitor/testdata/README.md`).
- Monitoring costs the client about 0.6% of a core with nine hosts at 2 s
  (software SSH crypto and parsing); it is not exactly zero.
- Direct directory copies cannot resume part-way; direct mode needs an SSH
  agent that holds the source's key.
- The shape of the 2.4 GB test depends on the loopback; real links are
  unmeasured. GPU-backed rendering is still unmeasured on macOS and Windows.
- Module path and licence are still placeholders.

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

