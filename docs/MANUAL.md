# Tako user manual

Tako shows many servers at once. Each server (or a local shell) is a **block**
in a grid; a block can hold several **tabs**.

## First start

The welcome screen offers *Add host*, *New local terminal* and, when
`~/.ssh/config` exists, an import of it. Passwords and key passphrases are
encrypted in Tako's own file (`secrets.enc` in the config directory). Tako does
not ask the system keychain to unlock. The first connection to a host asks you
to trust its fingerprint; a **changed** key is always refused.

## Hosts

*Add host* (`Ctrl+Shift+N`, `⌘N` on macOS) takes name, tags, address, port,
user, system, sign-in methods (password, key, SSH agent, keyboard-interactive),
a jump-host chain and a proxy. Tags become groups in the sidebar. Right-click a
host, a tag, a block, a tab or the terminal for menus; `Ctrl+Shift+P` opens the
palette.

## Blocks, sync and the minimap

Open several hosts and they tile the window. Drag a divider to resize; it snaps
to ½, ⅓, ⅔ (hold `⌥` / `Alt` to move freely). *Sync* on a block mirrors your
typing to the other synced blocks; the minimap (bottom right) shows which blocks
are synced, lets you select by dragging, and reorders by dragging squares. A
block that lost its connection is paused and rejoins when it is back.

## Monitoring and files

Each SSH block's footer shows CPU, memory and disk (and GPU where `nvidia-smi` /
`rocm-smi` exist); click it for the monitor page. Nothing is installed on the
server. *Transfer files…* (`Ctrl+Shift+Alt+T`) copies between this computer and
servers, or between two servers, with a queue, resume and verification.

## Settings (`Ctrl+Shift+,`)

| Page | What it holds |
|---|---|
| General | Language (system, English, 简体中文), appearance (system, light, dark), compact blocks. Applied at once. |
| Terminal | Color scheme (Tako follows the appearance; Solarized Dark / Light, Dracula, Nord, One Dark, Gruvbox Dark, Tokyo Night; import an iTerm2 `.itermcolors`; right-click an imported scheme to remove it), font size, copy on select, keep text readable. Terminals recolor live. |
| Connection | Keepalive interval for hosts that do not set their own. |
| Monitoring | On / off, sample interval, amber and red thresholds. |
| File transfer | Parallel transfers, same-name policy, verification, direct server-to-server copies. |
| Data | Export hosts (no secrets), import hosts from Xshell, SecureCRT, Termius or Tako ([IMPORT.md](IMPORT.md)), where the files live. |
| About | Version, updates, crash notice, **Copy diagnostics**. |

## Moving to another computer

*Data → Export hosts* writes a file with hosts, tags and jump chains and no
password or key passphrase. Import it on the other computer and enter each
password once.

## Updates

Tako checks GitHub Releases and verifies the signature before installing
(*About → Check for updates…*). Installers are **not** code-signed: on macOS
right-click the app and choose Open the first time; on Windows choose *More info
→ Run anyway*.

## When something goes wrong

- A crash leaves a report in the logs folder; the next start says so in
  *About*. *Copy diagnostics* puts a scrubbed report (version, system, recent
  log lines, no passwords, keys, host names or addresses) on the clipboard. Read
  it before pasting it into an issue: the scrubbing is a safety net, not a
  guarantee.
- Folders: *Data → Folders* shows where settings, hosts and logs are.
- Encoding is UTF-8 only. If a server prints garbage, set its locale to a UTF-8
  one (`LANG=en_US.UTF-8`).

## Keyboard and accessibility

Every control can be reached with `Tab` / `Shift+Tab` and activated with
`Enter` or `Space`; `Esc` closes the window or palette on top. Controls expose
roles and names to screen readers. Input methods (Pinyin, Japanese, Korean)
compose in text fields and in the terminal, with the candidate window at the
cursor. All shortcuts: [SHORTCUTS.md](SHORTCUTS.md).
