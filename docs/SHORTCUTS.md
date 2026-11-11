# Keyboard shortcuts

The table follows the operation → key table of [DESIGN.md](DESIGN.md) §4.6; a
test (`TestShortcuts_MatchTheDesignTable`) fails when the two drift apart.

On macOS the primary modifier is `⌘`. On Linux and Windows it is `Ctrl+Shift`,
so a plain `Ctrl+C`, `Ctrl+D` or `Ctrl+W` still reaches the shell. Where macOS
adds `⌥` or `⇧` as a second modifier, the other systems add `Alt`.

| macOS | Linux / Windows | Action |
|---|---|---|
| `⌘P` | `Ctrl+Shift+P` | Command palette |
| `⌘B` | `Ctrl+Shift+B` | Show / hide the sidebar |
| `⌘N` | `Ctrl+Shift+N` | Add host |
| `⌘T` | `Ctrl+Shift+T` | New tab in the focused block (no block: a local terminal) |
| `⌘W` | `Ctrl+Shift+W` | Close the tab |
| `⌘⇧W` | `Ctrl+Shift+Alt+W` | Close the block |
| `⌘1…9` | `Ctrl+Shift+1…9` | Go to tab 1…9 |
| `⌘⌥← ↑ → ↓` | `Ctrl+Shift+Alt+← ↑ → ↓` | Move focus to the neighbouring block (by position on screen) |
| `⌘⌥S` | `Ctrl+Shift+Alt+S` | Turn input sync on / off for the focused block |
| `⌘⌥⇧S` | `Ctrl+Shift+Alt+A` | Sync every block on, or all off when all were on |
| `⌘⌥M` | `Ctrl+Shift+Alt+M` | Open monitoring (arrives in M3: shows a notice) |
| `⌘⇧T` | `Ctrl+Shift+Alt+T` | Open the transfer panel (arrives in M3: shows a notice) |
| `⌘⌥G` | `Ctrl+Shift+Alt+G` | Show / hide the minimap |
| `⌘F` | `Ctrl+Shift+F` | Search in the terminal |
| `⌘,` | `Ctrl+Shift+,` | Settings |
| `⌘⇧↵` | `Ctrl+Shift+↵` | Zoom the focused block to the whole area, and back (not in the design table) |

Two of the macOS combinations have no direct equivalent: `⌘⌥⇧S` would be the
same keys as `⌘⌥S` with Ctrl+Shift+Alt, so sync-all is on `A` elsewhere; and
`⌘⇧T` would be new-tab plus Shift, so transfer is on Alt.

## Mouse

| Where | Gesture | Result |
|---|---|---|
| Minimap square | click | toggle that block's sync |
| Minimap background | drag a box | select (sync those, un-sync the rest) |
| Minimap | `⇧`-drag / `⌥`-drag a box | add to / invert the sync set (also from a square) |
| Minimap square | drag onto another square | swap their places |
| Minimap header | drag | move the panel; it snaps to the nearest corner |
| Minimap | right-click | square menu, or panel menu on the background |
| Divider between blocks | drag | resize; snaps to ½, ⅓, ⅔ within 8 px, guide line shown |
| Divider | drag with `⌥` held | no snapping |
| Block header | double-click | zoom the block |
| Anything with a menu | right-click | block header and tab, sidebar host, sidebar tag, minimap |
