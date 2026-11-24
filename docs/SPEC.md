# Product specification (summary)

This is the product spec, including the milestone plan. How to use the app is
[MANUAL.md](MANUAL.md). Where passwords are stored today is
[ADR 0023](adr/0023-encrypted-secret-store.md). ADR 0003 (the system keychain)
is superseded.

The long-form design lives in [DESIGN.md](DESIGN.md) (interaction and visual
spec, Chinese) and [TECH-PLAN.md](TECH-PLAN.md) (architecture and milestones,
Chinese). This page is the short English version of *what Tako is for* and
*what each milestone delivers*.

## Who it is for

People who look after several to dozens of servers (developers, SREs, GPU
cluster users, small ops teams) and today juggle a terminal emulator, tmux,
an SSH config and a pile of tabs. They want one calm window that shows many
machines at once, survives flaky networks and is honest about credentials.

## Principles

1. Quiet, native look; the terminal content is the star.
2. Coral means "synchronized / primary action" and nothing else.
3. State is readable at a glance: synced, disconnected (amber), online (green),
   with the same colour in block header, sidebar, minimap and status bar.
4. The terminal theme follows the app (white terminal in light mode).
5. No hidden surprises: no replay of keystrokes typed during an outage, no
   silent acceptance of changed host keys, no plaintext secrets in hosts,
   session, logs or exports. Passwords live in Tako's encrypted file
   ([ADR 0023](adr/0023-encrypted-secret-store.md)).

## Concepts

- **Host**: a saved SSH target (address, user, auth methods, jump chain,
  proxy, tags, host-key policy). Stored in `hosts.json`; secrets only by reference.
- **Block**: one host (or the local machine) shown in the main grid, with one
  or more **tabs** (shells).
- **Workspace**: the set of open blocks, tabs, focus and window state; saved to
  `session.json` and restored on start.
- **Synchronized input**: keystrokes in the focused block are mirrored to the
  blocks marked as synced.

## Milestones

| | Scope | Status |
|---|---|---|
| **M1** | Connections (all auth methods, jump hosts, proxies), terminals, adaptive grid, host sidebar, tabs, palette, `~/.ssh/config` import, resilient panes, session restore, EN/zh, light/dark | **Done** (v0.1.0-m1) |
| **M2** | Sync UI (chip, all on/off, selection), floating minimap, draggable split layout with snapping, manual layout restore, full shortcuts, right-click menus, rename, custom title bar | **Done** (v0.2.0-m2) |
| **M3** | Agentless per-block metrics (CPU, memory, disk, GPU) for Linux/macOS/Windows, monitor page, file transfer (upload, download, server to server by relay or direct) with resume and a queue, OSC 7 folders | **Done** (v0.3.0-m3) |
| **M4** | Dark-mode polish and terminal color schemes, full EN / 简体中文 with a pseudo-locale test, settings window, export / import without secrets, Xshell / SecureCRT / Termius importers, accessibility and IME polish, packaging (dmg, NSIS, deb, rpm, tar.gz), signed auto-update from GitHub Releases, crash reports and diagnostics. UTF-8 only; no platform code signing | **Done** (v0.4.0, release candidate; needs the pre-release checks in [RELEASE.md](RELEASE.md)) |

## M1 acceptance criteria

1. Every auth × jump × proxy combination connects; `vim` and `top` are usable.
2. A 60 s network outage shows the reconnect banner, keeps scrollback,
   `Pane.Read` never reports EOF and input typed during the outage is not replayed.
3. A changed host key is refused with a clear message.
4. `~/.ssh/config` import handles 12+ hosts with `Include`, `ProxyJump`, `IdentityFile`.
5. Nine idle blocks: ~0% CPU, RSS ≤ 100 MB, cold start ≤ 1 s.
6. Quit and restart restores blocks and tabs and reconnects; after `kill -9`
   recovery takes ≤ 1 s.
7. Secrets are absent from `hosts.json`, `session.json`, logs and exports.
8. Race tests, lint and `govulncheck` are green; core coverage ≥ 80%.
9. GPU rendering baseline on real macOS and Windows hardware.

Measurements recorded for those tags are in
[history/acceptance.md](history/acceptance.md), not a promise about current
`main`. The test layout is [TESTING.md](TESTING.md).

## Non-goals

Code signing, notarization and Windows certificates (releases are unsigned; see docs/RELEASE.md), encodings other than UTF-8
([ADR 0005](adr/0005-utf8-only.md)), tmux-style remote session persistence,
a plugin system, cloud sync of hosts (a sync design exists for M2+ but is not committed to).
