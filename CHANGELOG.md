# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[Semantic Versioning](https://semver.org/) (pre-release tags `-mN` mark milestones).

## [Unreleased]

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

