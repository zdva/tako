package workspace

import (
	"slices"

	"github.com/zdva/tako/internal/inputbus"
	"github.com/zdva/tako/internal/layout"
	"github.com/zdva/tako/internal/session"
)

// Minimap holds the preferences of the floating minimap.
type Minimap struct {
	// Hidden is true while the minimap is switched off.
	Hidden bool
	// Corner is the corner of the main area it sits in.
	Corner layout.Corner
	// Opacity is how opaque it is at rest, from MinOpacity to 1.
	Opacity float32
}

// Opacity limits of the minimap at rest (the slider's range).
const (
	MinOpacity     float32 = 0.35
	DefaultOpacity float32 = 0.5
)

// Viewport tells the model how much room the blocks have. The view reports it
// every frame; the model needs it to place new blocks and to drag dividers.
type Viewport struct {
	Size layout.Size
	Gap  float32
	// Min is the smallest a block may get when a divider is dragged.
	Min layout.Size
}

// SetViewport records the room the blocks are laid out in.
func (m *Model) SetViewport(v Viewport) { m.view = v }

// Manual reports whether the layout was changed by hand. Until then the
// blocks follow the automatic grid.
func (m *Model) Manual() bool { return m.manual }

// Tree returns the layout of the blocks: the manual tree, or the automatic
// grid of the blocks in order.
func (m *Model) Tree() layout.Tree {
	if m.manual {
		return m.tree
	}
	return layout.Auto(m.blockIDs(), m.view.Size, m.view.Gap)
}

func (m *Model) blockIDs() []string {
	ids := make([]string, len(m.blocks))
	for i, b := range m.blocks {
		ids[i] = string(b.ID)
	}
	return ids
}

// Placement returns where every block is in the current viewport.
func (m *Model) Placement() layout.Placement { return m.Tree().Place(m.view.Size, m.view.Gap) }

// DragDivider moves the divider at path to pos (a coordinate in the viewport)
// and returns the guide line to draw. The first drag turns the automatic grid
// into a manual layout. snap is false while ⌥ is held.
func (m *Model) DragDivider(path string, pos float32, snap bool) layout.Guide {
	next, guide := m.Tree().DragDivider(path, pos, layout.DragOptions{
		Viewport: m.view.Size, Gap: m.view.Gap, Min: m.view.Min, Snap: snap, Threshold: snapThreshold,
	})
	m.tree, m.manual = next, true
	m.changed(true)
	return guide
}

// snapThreshold is how close, in DIPs, a divider must come to 1/3, 1/2 or
// 2/3 to snap onto it.
const snapThreshold = 8

// ResetLayout goes back to the automatic grid.
func (m *Model) ResetLayout() {
	m.tree, m.manual = layout.Tree{}, false
	m.changed(true)
}

// Preset names a ready-made arrangement.
type Preset int

// The presets of the layout menu.
const (
	// PresetAuto is the automatic grid.
	PresetAuto Preset = iota
	// PresetRow puts every block side by side.
	PresetRow
	// PresetColumn stacks every block.
	PresetColumn
)

// ApplyPreset arranges the blocks.
func (m *Model) ApplyPreset(p Preset) {
	ids := m.blockIDs()
	switch p {
	case PresetRow:
		m.tree, m.manual = layout.Row(ids, m.view.Size, m.view.Gap), true
	case PresetColumn:
		m.tree, m.manual = layout.Column(ids, m.view.Size, m.view.Gap), true
	case PresetAuto:
		m.tree, m.manual = layout.Tree{}, false
	}
	m.changed(true)
}

// Reposition puts block b where target is: the two swap places in a manual
// layout, and b moves to target's slot in the automatic grid.
func (m *Model) Reposition(b, target *Block) {
	if b == nil || target == nil || b == target {
		return
	}
	if m.manual {
		m.tree = m.tree.Swap(string(b.ID), string(target.ID))
	} else {
		m.blocks = layout.Move(m.blocks, m.IndexOf(b), m.IndexOf(target))
	}
	m.changed(true)
}

// Dock moves block b to an edge of target (dragging a block onto the side of
// another). It turns the layout manual.
func (m *Model) Dock(b, target *Block, edge layout.Edge) {
	if b == nil || target == nil || b == target {
		return
	}
	m.tree = m.Tree().Move(string(b.ID), string(target.ID), edge)
	m.manual = true
	m.changed(true)
}

// MoveFocus moves the keyboard focus to the nearest block in a direction;
// dx and dy are -1, 0 or 1.
func (m *Model) MoveFocus(dx, dy int) {
	cur := m.Focused()
	if cur == nil {
		return
	}
	p := m.Placement()
	from, ok := p.Rect(string(cur.ID))
	if !ok {
		return
	}
	var best *Block
	bestScore := float32(-1)
	for _, c := range p.Cells {
		if c.ID == string(cur.ID) {
			continue
		}
		primary, perp := offsets(from, c.Rect, dx, dy)
		if primary < -0.5 {
			continue
		}
		score := primary + 2*perp
		if b := m.find(inputbus.ID(c.ID)); b != nil && (bestScore < 0 || score < bestScore) {
			best, bestScore = b, score
		}
	}
	if best != nil {
		m.Focus(best)
	}
}

// offsets returns how far to is beyond from in the direction (primary,
// negative when it is behind) and how far off the direction's line it is.
func offsets(from, to layout.Rect, dx, dy int) (primary, perp float32) {
	switch {
	case dx > 0:
		primary = to.X - from.Right()
	case dx < 0:
		primary = from.X - to.Right()
	case dy > 0:
		primary = to.Y - from.Bottom()
	default:
		primary = from.Y - to.Bottom()
	}
	if dx != 0 {
		perp = abs32(to.Y + to.H/2 - from.Y - from.H/2)
	} else {
		perp = abs32(to.X + to.W/2 - from.X - from.W/2)
	}
	return primary, perp
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// Zoomed returns the block that fills the whole area, or nil.
func (m *Model) Zoomed() *Block { return m.find(m.zoom) }

// ToggleZoom makes b fill the area, or brings the layout back.
func (m *Model) ToggleZoom(b *Block) {
	if b == nil || m.zoom == b.ID {
		m.zoom = ""
	} else {
		m.zoom = b.ID
		m.focus = b.ID
	}
	m.changed(false)
}

// MinimapPrefs returns the minimap preferences.
func (m *Model) MinimapPrefs() Minimap { return m.mini }

// SetMinimap changes the minimap preferences; Opacity is clamped.
func (m *Model) SetMinimap(p Minimap) {
	p.Opacity = min(max(p.Opacity, MinOpacity), 1)
	if p == m.mini {
		return
	}
	m.mini = p
	m.changed(true)
}

// SetSyncedMany sets sync on or off for several blocks at once.
func (m *Model) SetSyncedMany(blocks []*Block, on bool) {
	for _, b := range blocks {
		b.Synced = on
		m.deps.Bus.SetSynced(b.ID, on)
	}
	m.changed(true)
}

// SyncOnly turns sync on for exactly the given blocks and off for the rest.
func (m *Model) SyncOnly(blocks []*Block) {
	for _, b := range m.blocks {
		on := slices.Contains(blocks, b)
		b.Synced = on
		m.deps.Bus.SetSynced(b.ID, on)
	}
	m.changed(true)
}

// SyncAll turns sync on or off for every block.
func (m *Model) SyncAll(on bool) { m.SetSyncedMany(m.blocks, on) }

// ToggleSyncAll turns sync off when every block has it on, and on otherwise.
func (m *Model) ToggleSyncAll() { m.SyncAll(m.SyncedCount() != len(m.blocks)) }

// SyncPaused reports whether a block has sync on but cannot take input now
// because its connection is down. The bus keeps skipping it, and it takes
// part again by itself once the connection is back.
func (m *Model) SyncPaused(b *Block) bool {
	return b.Synced && b.Link().State != session.StateConnected
}
