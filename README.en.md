# Tako

[简体中文](README.md) · [English](README.en.md)

[![License: GPL-3.0-or-later](https://img.shields.io/badge/license-GPL--3.0--or--later-blue.svg)](LICENSE)
[![CI](https://github.com/zdva/tako/actions/workflows/ci.yml/badge.svg)](https://github.com/zdva/tako/actions/workflows/ci.yml)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8.svg)](go.mod)

Tako is a desktop SSH terminal for a fleet of servers. It keeps many machines on one screen (each cell is a *block*), keeps the scrollback and reconnects when a link drops, and restores the blocks and tabs you had open. The UI is [MyGo](https://github.com/egoist/mygo). The languages are English and 简体中文.

The GitHub landing page is the [Chinese README](README.md). This file is the same guide in English. The documentation map is [docs/README.md](docs/README.md) (Chinese, with the language of each page marked).

## What it does

- **Many machines on one screen.** A block holds tabs, and you can open a local terminal. Drag a divider to resize; it snaps to ½, ⅓ and ⅔.
- **Synchronized input.** Keystrokes in one synced block go to the others. A block whose link is down is paused, and input typed during the outage is not replayed.
- **Minimap.** See which blocks are synced, click or drag a box to select, and drag squares to reorder.
- **SSH.** Password, public key (OpenSSH, PEM, PuTTY `.ppk`), SSH agent and keyboard-interactive authentication; jump-host chains; SOCKS5 and HTTP proxies.
- **Host keys.** Tako keeps its own `known_hosts`. A changed key is refused, and both fingerprints are shown.
- **Monitoring**, with nothing installed on the server. Each SSH block's footer shows CPU, memory and disk, and GPU when `nvidia-smi` or `rocm-smi` is present. Click the footer for the monitor page.
- **File transfer.** Upload, download, and copy between two servers (through this computer, or directly with `scp` / `rsync` if you allow that in Settings). The queue can pause and resume.
- **Host library.** Import from `~/.ssh/config`, Xshell, SecureCRT and Termius. An export contains no passwords.
- **Settings apply at once.** Language, light/dark, terminal color scheme (including an iTerm2 `.itermcolors` import), font, keepalive, monitoring and transfers.

UTF-8 only.

## Status

The version in the source is **0.4.0**. The repository has the tag [`v0.4.0`](https://github.com/zdva/tako/tree/v0.4.0). **There is no GitHub Release and no downloadable installer.** Build from source.

`main` is newer than the `v0.4.0` tag. Changes that are merged and not yet released on their own are under Unreleased in [CHANGELOG.md](CHANGELOG.md), including passwords moving into Tako's own encrypted file.

The build targets are macOS, Windows and Linux. The packaging config sets a macOS minimum of 13.0. Release checks on record were done on Linux. There is no record of installing on a real Mac or Windows machine and finishing that checklist. Continuous integration results are whatever [Actions](https://github.com/zdva/tako/actions/workflows/ci.yml) shows.

## Build and run

Go 1.27.3 or newer (`go.mod` sets the toolchain to `go1.27.4`) and a display.

```sh
git clone https://github.com/zdva/tako.git
cd tako
make run
```

`make build` writes `./tako` for this machine. That plain build downloads `libghostty-vt` from the MyGo releases into the user cache the first time a terminal opens, and checks its SHA-256.

`make package-linux` writes `build/linux-amd64` and places `libghostty-vt` next to the executable, so a terminal can open offline afterwards. `make build-all` cross-compiles Windows, macOS (universal) and Linux binaries. It does not build installers and does not sign them.

Pushing a version tag runs the release workflow, which uploads installer drafts (tar.gz, deb, rpm, PKGBUILD, dmg, NSIS). A person reviews the draft before it is public. The binaries are not Apple or Windows code-signed, so Gatekeeper and SmartScreen warn. How to open the app is in [docs/MANUAL.md](docs/MANUAL.md); how to add signing later is in [docs/RELEASE.md](docs/RELEASE.md).

A window on Linux needs a graphical session. The RPM recipe in this repository declares a dependency on `gtk3`. The deb's system dependencies are not listed in this repository.

## First session

1. The welcome screen offers adding a host, a local terminal, and an import when `~/.ssh/config` is present.
2. The first connection asks you to trust the host fingerprint. A later change of that key is refused.
3. Passwords, key passphrases and proxy credentials go into `secrets.enc` in the config directory (mode 0600). Tako does not use the system keychain, and there is no master password.
4. Settings: `Ctrl+Shift+,` on Linux and Windows, `⌘,` on macOS.

The primary modifier is `Ctrl+Shift` on Linux and Windows, so a plain `Ctrl+C` still reaches the shell, and `⌘` on macOS. The full table is [docs/SHORTCUTS.md](docs/SHORTCUTS.md). The user manual is [docs/MANUAL.md](docs/MANUAL.md).

### Where data lives

| | Linux | macOS | Windows |
|---|---|---|---|
| Config (`hosts.json`, `secrets.enc`, `known_hosts`, `settings.json`) | `~/.config/tako` | `~/Library/Application Support/Tako` | `%AppData%\Tako` |
| State (`session.json`, logs) | `~/.local/state/tako` | same directory as config | `%LocalAppData%\Tako` |

Override either with `TAKO_CONFIG_DIR` and `TAKO_STATE_DIR`.

The encryption key is bound to this machine. When the machine id cannot be read, the key is the sibling file `secrets.key`. Copying `secrets.enc` alone, without that machine id or `secrets.key`, does not decrypt on another computer. The threat model is in [SECURITY.md](SECURITY.md).

An earlier build stored passwords in the system credential store under the service `dev.tako.app`. That decision ([ADR 0003](docs/adr/0003-secrets-in-keychain.md)) is superseded. Startup does not read those items. Type the password the next time you connect and choose to remember it, or quit and run `tako --migrate-keychain` once (the system may ask per item). The current decision is [ADR 0023](docs/adr/0023-encrypted-secret-store.md).

## Limits to know before you rely on it

- Monitoring has been compared with the local OpenSSH on Linux. The macOS, Windows and GPU parsers are checked against fixtures written from the tools' documented formats, not against recordings from those machines.
- The Xshell, SecureCRT and Termius importers were written from public format documentation. The fixtures in the repository are not real exports from those programs.
- A direct server-to-server copy of a directory cannot resume part-way. Direct mode needs the source key loaded in the local SSH agent.
- Crash reports and "Copy diagnostics" are not uploaded. Read the text before pasting it into an issue.

## Contributing and security

How to send a change: [CONTRIBUTING.md](CONTRIBUTING.md). Repository layout: [AGENTS.md](AGENTS.md). Test levels: [docs/TESTING.md](docs/TESTING.md). Conduct: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). Do not open a public issue for a vulnerability; see [SECURITY.md](SECURITY.md).

## License

Tako is distributed under the [GNU General Public License](LICENSE), version 3 or (at your option) any later version (**GPL-3.0-or-later**), without warranty. Third-party licenses are in [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
