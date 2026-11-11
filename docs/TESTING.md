# Testing

Tako is tested at four levels. `make check` runs the first; the others have
their own targets.

| Level | Command | What it covers | Needs |
|---|---|---|---|
| Unit + UI | `make race` (`go test -race ./...`) | Core packages, `ui.Tester` tests of every screen and flow (no window), i18n key alignment, secrets grep test | libghostty-vt (auto-downloaded; tests skip if unavailable) |
| Coverage gate | `make cover` | Core packages (`model`, `hostlib`, `session`, `layout`, `inputbus`, `workspace`, `transport/ssh`, `store`, `secrets`, `importer/sshconfig`, `settings`) ≥ 80% in total | — |
| Integration | `make integration` (`-tags integration`) | Auth × jump × proxy matrix, `vim`/`top`, MaxSessions, host-key change, outage and reconnect — against a **real OpenSSH** | Linux; `scripts/fetch-sshd.sh` or `/usr/sbin/sshd` |
| End to end | `make e2e` (`-tags e2e`) | The packaged app in a real window under Xvfb: nine idle blocks (CPU, memory), cold start, `kill -9` recovery | Linux, `xvfb-run`/`DISPLAY`, `xdotool`, Python PIL (screenshots only) |

Also: `make fuzz` (PPK parser, ssh-config parser), `make vuln` (govulncheck),
`make lint`.

## Test servers

- `internal/testutil/memssh`: an in-process SSH server with a
  scripted line-echo "shell" (so tests can prove which bytes reached which
  machine), password, keyboard-interactive and public-key auth, the ability to
  drop every connection and to refuse extra sessions (MaxSessions). Fast and
  deterministic; used by unit and UI tests.
- `internal/testutil/realsshd`: a real OpenSSH `sshd` started as the current
  user on a loopback port. `scripts/fetch-sshd.sh` unpacks a portable build
  into `.cache/openssh` without root. **Limit:** an unprivileged sshd cannot
  check passwords or PAM, so the real-sshd tests cover public key, agent and
  PPK; password and keyboard-interactive are covered with memssh.
- `internal/testutil/nettest`: SOCKS5 and HTTP CONNECT proxies (with or without
  credentials), a TCP relay that can be paused or cut to simulate an outage, and
  an in-process SSH agent.

## Manual and not covered

- GPU rendering and real macOS/Windows runs cannot be exercised on a CI box
  (M1 criterion 9 is **not done**; see the changelog).
- The e2e RSS figure is measured under Mesa's software renderer in Xvfb; a
  real GPU driver changes it.
- Real PAM/keyboard-interactive servers and exotic jump setups are checked by
  hand before a release.

## Rules

- Tests use `t.Context()`, `t.TempDir()`, `-race`, and `goleak` where goroutines
  are started. Never sleep to wait for a condition when it can be polled.
- No test touches the user's real home, keychain or `~/.ssh`: the app takes its
  directories and keychain as options.
- Never put real credentials in fixtures.
