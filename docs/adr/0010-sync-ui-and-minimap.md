# 0010. Sync UI and the floating minimap

Status: Accepted

## Context

The final design ([DESIGN.md](../DESIGN.md), "Final") has one rule: each
visible action appears exactly once. Sync is the app's signature feature, and
the first design repeated it in the toolbar, every block header, the sidebar
and the status bar.

## Decision

- A synced block shows a 1 px coral border and one small chip in its header;
  an unsynced block shows the chip as a grey outline. There is no big "syncing"
  button, no sidebar link icon, no toolbar button, no status-bar count.
- Sync is turned on and off in these places only: the chip, the minimap, the
  command palette, the shortcuts (`⌘⌥S`, `⌘⌥⇧S`) and the right-click menus.
  "All on / off" is a palette command, a shortcut and a menu item; it is not a
  toolbar button.
- The minimap is a floating panel, 176 px wide, translucent at rest and opaque
  while hovered or dragged (the panel's glass colour is scaled by an animated
  opacity; the user's slider sets the resting value, 35%–100%). It draws the
  real layout scaled down. Click toggles sync; a drag from empty space is a
  rubber band (⇧ adds, ⌥ inverts; either also starts one from a square); a drag
  from a square swaps places; the header drags the panel, which snaps to a
  corner. Gestures are one small state machine (`idle`, `pending`, `drag`,
  `box`) on the panel's input handler, with all geometry in `internal/layout`.
- Sync state is the model's: `SetSynced`, `SyncOnly`, `SyncAll`. A synced
  block whose link is not connected is *paused*: the InputBus skips it
  ([0007](0007-inputbus.md)), the chip shows "paused", the other blocks are
  unaffected, and it takes part again after the reconnect.
- The status bar shows only the focused host and the terminal's size and
  encoding. A dot appears (amber) only while a block reconnects.

## Consequences

The no-redundancy rule is a test (`TestFinalDesign_NoRedundantChrome`): the
removed labels, buttons and icons must not exist in the tree. Gestures that
`ui.Tester` cannot express (modifier keys on pointer events) use the
`HoldModifiers` test seam. Real-window proof of the translucent/opaque
difference is by screenshot comparison in the e2e suite.
