# 0016. The current folder comes from OSC 7

Status: Accepted

## Context

The transfer sheet should open on the folders the user is in, not on home.
There is no agent on the server, so the only report of the working directory is
the one a shell can print itself.

## Decision

- The terminal library tracks OSC 7 (`ESC ] 7 ; file://host/path BEL`) and
  exposes the last reported folder (`Tab.Dir`). The same value already opens a
  new tab in the folder of the old one (M1).
- When the transfer sheet opens, each pane starts at the folder reported by a
  block on that machine (the focused block wins); with none it starts in the
  home folder the file system reports. A folder that does not exist shows the
  server's error; the pane stays usable.
- Many shells do not send OSC 7 by default (macOS's do; most Linux ones need a
  hook). The command palette has "Copy the shell hook that reports the current
  folder" and [docs/OSC7.md](../OSC7.md) lists the snippets.

## Consequences

Without the hook the sheet opens at home, which is correct, just less
convenient. Nothing is installed on the server by Tako.
