# 0008. Layout model: auto grid now, split tree later

Status: Accepted (M1 grid). The manual split tree is decided in [0009](0009-manual-layout-tree.md).

## Context

The main area shows many blocks. Users want it to "just work", and later to
drag dividers. The minimap must draw exactly what the main area shows.

## Decision

- `internal/layout` is pure geometry with no dependencies. `Compute(n,
  viewport, minCell, gap)` returns one rectangle per block.
- Auto mode: 1 block fills the area, 2 are side by side, 3–4 form 2×2, 5–6
  3×2, more keep 3 columns and scroll vertically. A block never gets smaller
  than a minimum (80×24 characters by default; the "compact blocks" setting
  lowers it to about 48×16). When the width does not fit, columns are dropped
  and rows grow instead.
- Manual mode (M2) will be a binary split tree stored in `session.json`; the
  minimap and the main area both render the rectangles the layout package
  returns.

## Consequences

The grid is table-tested (no overlap, minimum sizes, column fallback).
`session.json` stores the blocks in grid order and no geometry, so a split tree
can be added later without migrating existing data.
