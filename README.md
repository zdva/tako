# Tako (章鱼终端)

[English](README.md) · [简体中文](README.zh-CN.md)

Tako is a desktop terminal for a fleet of servers. It keeps many SSH (and local)
terminals on one screen as a grid of *blocks*, reconnects the ones whose network
dropped, and remembers what you had open. It is written in Go with the
[MyGo](https://github.com/egoist/mygo) native UI toolkit, for macOS, Windows and Linux.

> **Status: milestone M1 (v0.1.0-m1), pre-release.** Connections, terminals,
> the adaptive grid, the host sidebar, session restore and `~/.ssh/config`
> import work. Synchronized input UI and the minimap (M2), monitoring and file
> transfer (M3) and packaging polish (M4) are not done yet. See
> [docs/SPEC.md](docs/SPEC.md) and [docs/TECH-PLAN.md](docs/TECH-PLAN.md).

![Main window](docs/screenshots/m1/main-light.png)

| Dark | Connection lost | Add host |
|---|---|---|
| ![dark](docs/screenshots/m1/main-dark.png) | ![disconnected](docs/screenshots/m1/disconnected.png) | ![add host](docs/screenshots/m1/add-host.png) |

More in [docs/screenshots/m1](docs/screenshots/m1).

## What works in M1

- **SSH**: password, public key (OpenSSH, PEM, PuTTY `.ppk`), SSH agent and
  keyboard-interactive authentication; jump-host chains; SOCKS5 and HTTP
  proxies; keep-alives; MaxSessions fallback (opens an extra connection).
- **Resilient panes**: when the link drops, the block shows a reconnect banner,
  keeps its scrollback, retries with backoff (1, 2, 5, 10, 30 s) and never
  replays what you typed during the outage. See [ADR 0006](docs/adr/0006-session-pane-contract.md).
- **Host keys**: Tako's own `known_hosts`; a changed key is always refused
  with both fingerprints shown. See [ADR 0004](docs/adr/0004-host-key-policy.md).
- **Secrets** only in the system keychain, never in files or logs.
  See [ADR 0003](docs/adr/0003-secrets-in-keychain.md).
- **Grid of blocks**: 1 / 2 / 2×2 / 3×2 / scrolling, tabs per block, local
  shells, right-click menus, command palette, English and 简体中文, light and dark.
- **Import** hosts from `~/.ssh/config` (`Include`, `ProxyJump`, `IdentityFile`).
- **Session restore**: quit or crash (even `kill -9`) and the blocks and tabs
  come back and reconnect, usually well under a second.

## Install and run

Release binaries are not published yet. Build from source (Go 1.27.3 or newer):

```sh
git clone <repository-url> tako && cd tako
make run            # go run ./cmd/tako
make build          # ./tako (a plain build downloads libghostty-vt on first use)
make package-linux  # packaged app in build/linux-amd64 (libghostty-vt bundled)
make build-all      # Windows, macOS (universal) and Linux, amd64 and arm64
```

Packaged builds (`mygo build`) ship the terminal engine next to the executable
and work offline. A plain `go build` binary fetches `libghostty-vt` from the
MyGo releases into your user cache the first time a terminal opens, and checks
its SHA-256.

Binaries are **not code-signed** yet: macOS Gatekeeper and Windows SmartScreen
will warn.

### Keyboard

The primary modifier is `Ctrl+Shift` on Linux and Windows (so plain `Ctrl+C`
still reaches the shell) and `⌘` on macOS.

| Keys | Action |
|---|---|
| `Ctrl+Shift+P` | Command palette (hosts, commands) |
| `Ctrl+Shift+N` | Add host |
| `Ctrl+Shift+T` | New tab / local terminal |
| `Ctrl+Shift+W` | Close tab · `Ctrl+Shift+Alt+W` close block |
| `Ctrl+Shift+1…9` | Switch tab |
| `Ctrl+Shift+Alt+←↑→↓` | Move focus between blocks |
| `Ctrl+Shift+Alt+S` | Toggle synchronized input for the focused block |
| `Ctrl+Shift+B` | Toggle the sidebar |
| `Ctrl+Shift+,` | Settings |

### Where data lives

| | Linux | macOS | Windows |
|---|---|---|---|
| Config (`hosts.json`, `known_hosts`, `settings.json`) | `~/.config/tako` | `~/Library/Application Support/Tako` | `%AppData%\Tako` |
| State (`session.json`, logs, lock) | `~/.local/state/tako` | same as config | `%LocalAppData%\Tako` |

Override with `TAKO_CONFIG_DIR` and `TAKO_STATE_DIR`. On a machine without a
credential store (a headless Linux box without Secret Service), "remember
password" lasts for the running session only, and Tako says so.

## Develop

```sh
make check          # gofmt, vet, golangci-lint, go test -race, coverage gate (80% core)
make integration    # against a real OpenSSH (scripts/fetch-sshd.sh downloads a portable one)
make e2e            # live window under Xvfb: idle CPU/RSS, cold start, kill -9 recovery
make screenshots    # real-window screenshots into docs/screenshots/m1
make vuln           # govulncheck
```

Read [AGENTS.md](AGENTS.md) for the layout and conventions and
[CONTRIBUTING.md](CONTRIBUTING.md) for how to send a change.
[docs/TESTING.md](docs/TESTING.md) explains the test environments.

## Notes for maintainers

- The Go module path `github.com/wei-dong/tako` is a **placeholder** until the
  repository has a home; it will be changed in one commit.
- The [MIT license](LICENSE) is a **placeholder pending the owner's confirmation**.

## License

MIT, see [LICENSE](LICENSE) (pending confirmation, see above).
