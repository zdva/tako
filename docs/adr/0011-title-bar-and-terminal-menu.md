# 0011. Hidden title bar; terminal-area right-click menu

Status: Accepted

## Context

The design has a custom title bar: brand and sidebar toggle on the left, the
search field and two icons in the toolbar. MyGo offers `TitleBarHidden`, which
gives the page the whole window and keeps the system's window controls on top.

## Decision

- The window is created with `TitleBarStyle: TitleBarHidden`. The brand row
  and the toolbar are the drag handles (`Element.DragWindow`) and keep clear of
  the controls using `Context.TitleBar()` (`Left` on macOS, `Right` elsewhere).
  This works under X11 (checked under Xvfb: the controls draw top right and
  the toolbar leaves room for them). Windows and macOS builds use the same
  code but have not been run (no such hardware here).
- The terminal's right-click menu stays MyGo's own (Copy, Paste, Select All,
  Clear). The terminal view does not let an application add items, and it has
  no public selection API, so replacing the menu would lose Copy. The rest of
  the design's "in the terminal" items are reachable elsewhere: search
  (`⌘F`, find bar), block and tab actions (block header menu, shortcuts).
  "Open the current directory in a new tab" waits for OSC 7 (M3).

## Consequences

One documented deviation from DESIGN §4.7 (the terminal-area menu has four
items instead of six). If MyGo adds menu extension or a selection getter, the
menu is a small change in `internal/ui/menus.go`.
