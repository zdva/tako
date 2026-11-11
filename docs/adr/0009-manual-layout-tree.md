# 0009. Manual layout: a binary split tree

Status: Accepted (supersedes the "manual mode" note of [0008](0008-layout-model.md))

## Context

M1's grid is computed from the block count. M2 lets the user drag dividers,
with snapping, and must keep the result across restarts, while the minimap
draws exactly what the main area shows.

## Decision

- `internal/layout.Tree` is a binary tree. A node is a leaf (a block) or a
  split: an axis (`cols` or `rows`), a ratio, and two children. The ratio is
  side A's share of the room left after the gap, so a ratio survives a window
  resize unchanged.
- `Tree.Place(viewport, gap)` returns the block rectangles and the divider
  strips. The main area, the minimap and the tests all use it.
- `Tree.DragDivider(path, pos, opts)` moves one divider and returns the new
  tree and a *guide*: the divider snaps to ⅓, ½ or ⅔ of its span within a pixel
  threshold (⌥ turns snapping off). Blocks never get smaller than `Min`; a
  drag that would squeeze a nested split re-clamps the nested ratios
  (`Tree.fit`), so the invariant holds for every tree, not just flat ones.
- Both modes live in the workspace model: `manual=false` computes
  `layout.Auto` from block order (M1's grid, in tree form); the first divider
  drag converts it to a tree and sets `manual`. New blocks in manual mode are
  added beside the focused block; closed blocks are removed and the sibling
  takes the room. "Reset layout" and the presets go back to auto.
- Persistence: `session.json` gets `layout` (manual flag and tree with leaves
  named `b<index>`, because block IDs change between runs), `minimap` and the
  per-block sync flag. Restore validates the tree and falls back to auto if it
  does not match the restored blocks.
- The layout drag moves only the model; terminals are resized from the frame
  the layout produced, and `session.Pane` debounces the window-change request
  (100 ms of stillness, [0006](0006-session-pane-contract.md)), so a drag
  sends the server one resize, not one per frame.
- Spring animation: block frames animate to their targets over about 150 ms
  (`AnimateTo`), and jump in tests and with reduced motion.

## Consequences

Layout is table- and property-tested without a window (`internal/layout`:
no overlaps, no cell below the minimum, ratios in range, snapping, round trip
through JSON). The tree is the unit later features (edge insertion, presets,
saved layouts) build on. Each manual layout is tied to block order in
`session.json`; moving blocks in the minimap swaps leaves, so the tree stays
valid.
