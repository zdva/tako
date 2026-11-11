package workspace

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"

	"github.com/wei-dong/tako/internal/inputbus"
	"github.com/wei-dong/tako/internal/layout"
	"github.com/wei-dong/tako/internal/model"
)

// StateVersion is the version of the persisted workspace.
const StateVersion = 1

// State is what is saved to session.json and restored on the next start.
// It never holds secrets: only host IDs, directories and layout.
type State struct {
	Version int          `json:"version"`
	Blocks  []BlockState `json:"blocks"`
	// Focus is the index of the focused block, -1 for none.
	Focus   int         `json:"focus"`
	Window  WindowState `json:"window"`
	Sidebar bool        `json:"sidebarCollapsed,omitempty"`
	// Layout is set once the user dragged a divider or chose a preset.
	Layout LayoutState `json:"layout,omitzero"`
	// Minimap is where the minimap sits and how opaque it is.
	Minimap MinimapState `json:"minimap,omitzero"`
}

// LayoutState is a manual layout. Blocks are named "b<N>" after their index in
// [State.Blocks], because block IDs change from one run to the next.
type LayoutState struct {
	Manual bool        `json:"manual,omitempty"`
	Tree   layout.Tree `json:"tree,omitzero"`
}

// MinimapState is the saved minimap preferences.
type MinimapState struct {
	Hidden  bool    `json:"hidden,omitempty"`
	Corner  string  `json:"corner,omitempty"`
	Opacity float32 `json:"opacity,omitempty"`
}

var cornerNames = [...]string{
	layout.TopLeft: "top-left", layout.TopRight: "top-right",
	layout.BottomLeft: "bottom-left", layout.BottomRight: "bottom-right",
}

func blockRef(i int) string { return "b" + strconv.Itoa(i) }

// BlockState is one saved block. Blocks appear in grid order, which is the
// automatic layout: the grid is recomputed from their number and window size.
type BlockState struct {
	// HostID is empty for a local shell.
	HostID model.ID   `json:"hostId,omitempty"`
	Name   string     `json:"name,omitempty"`
	Tabs   []TabState `json:"tabs"`
	Active int        `json:"active"`
	Synced bool       `json:"synced,omitempty"`
}

// TabState is one saved tab.
type TabState struct {
	Dir string `json:"dir,omitempty"`
}

// WindowState is the saved window geometry.
type WindowState struct {
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
}

// Snapshot returns the state to persist. It must run on the UI goroutine.
func (m *Model) Snapshot() State {
	s := State{Version: StateVersion, Focus: -1, Blocks: make([]BlockState, 0, len(m.blocks))}
	for i, b := range m.blocks {
		bs := BlockState{HostID: b.HostID, Active: b.Active, Synced: b.Synced}
		if b.Kind == KindLocal || b.Name != "" {
			bs.Name = b.Name
		}
		for _, t := range b.Tabs {
			bs.Tabs = append(bs.Tabs, TabState{Dir: t.Dir()})
		}
		s.Blocks = append(s.Blocks, bs)
		if b.ID == m.focus {
			s.Focus = i
		}
	}
	if m.manual {
		refs := map[string]string{}
		for i, b := range m.blocks {
			refs[string(b.ID)] = blockRef(i)
		}
		s.Layout = LayoutState{Manual: true, Tree: m.tree.MapIDs(func(id string) string { return refs[id] })}
	}
	s.Minimap = MinimapState{Hidden: m.mini.Hidden, Corner: cornerNames[m.mini.Corner], Opacity: m.mini.Opacity}
	return s
}

// Restore reopens the saved blocks and tabs. Blocks whose host was deleted
// are skipped. It returns how many blocks were restored; connections come up
// in the background, so the grid appears at once.
func (m *Model) Restore(s State) int {
	restored := 0
	var focus inputbus.ID
	refs := map[string]string{}
	m.restoring = true
	defer func() { m.restoring = false }()
	for i, bs := range s.Blocks {
		if len(bs.Tabs) == 0 {
			continue
		}
		b, err := m.restoreBlock(bs)
		if err != nil {
			slog.Warn("restoring a block failed", "host", bs.HostID, "err", err)
			continue
		}
		restored++
		refs[blockRef(i)] = string(b.ID)
		if i == s.Focus {
			focus = b.ID
		}
	}
	if focus != "" {
		m.focus = focus
	} else if len(m.blocks) > 0 {
		m.focus = m.blocks[0].ID
	}
	m.restoreLayout(s, refs)
	m.restoreMinimap(s.Minimap)
	m.changed(false)
	return restored
}

// restoreLayout brings back a saved manual layout, dropping blocks that did
// not restore and placing any that the file did not mention. A layout that is
// malformed is ignored and the grid is automatic.
func (m *Model) restoreLayout(s State, refs map[string]string) {
	if !s.Layout.Manual || len(m.blocks) == 0 {
		return
	}
	if err := s.Layout.Tree.Validate(); err != nil {
		slog.Warn("ignoring the saved layout", "err", err)
		return
	}
	t := s.Layout.Tree.MapIDs(func(ref string) string { return refs[ref] })
	// Leaves of blocks that did not restore are now "" and are removed here;
	// Reconcile keeps exactly the blocks that exist.
	for t.Has("") {
		t = t.Remove("")
	}
	m.tree = t.Reconcile(m.blockIDs(), m.view.Size, m.view.Gap)
	m.manual = true
}

func (m *Model) restoreMinimap(s MinimapState) {
	m.mini.Hidden = s.Hidden
	if i := slices.Index(cornerNames[:], s.Corner); i >= 0 {
		m.mini.Corner = layout.Corner(i)
	}
	if s.Opacity > 0 {
		m.mini.Opacity = min(max(s.Opacity, MinOpacity), 1)
	}
}

func (m *Model) restoreBlock(bs BlockState) (*Block, error) {
	b, err := m.reopenBlock(bs)
	if err != nil {
		return nil, err
	}
	for _, t := range bs.Tabs {
		if err := m.addTab(b, t.Dir, ""); err != nil {
			slog.Warn("restoring a tab failed", "block", b.Name, "err", err)
		}
	}
	if len(b.Tabs) == 0 {
		m.removeBlock(b)
		return nil, fmt.Errorf("no tab of %s could be restored", b.Name)
	}
	if bs.Active >= 0 && bs.Active < len(b.Tabs) {
		m.setActive(b, bs.Active)
	}
	if bs.Synced {
		b.Synced = true
		m.deps.Bus.SetSynced(b.ID, true)
	}
	return b, nil
}

// reopenBlock creates the block of a saved state, without its tabs.
func (m *Model) reopenBlock(bs BlockState) (*Block, error) {
	if bs.HostID == "" {
		m.nextID++
		b := &Block{ID: localID(m.nextID), Kind: KindLocal, Name: bs.Name}
		if b.Name == "" {
			b.Name = "local"
		}
		if err := m.attach(b); err != nil {
			return nil, err
		}
		return b, nil
	}
	h, ok := m.deps.Host(bs.HostID)
	if !ok {
		return nil, ErrNoSuchHost
	}
	b, err := m.addBlock(h)
	if err != nil {
		return nil, err
	}
	if bs.Name != "" {
		b.Name = bs.Name
	}
	return b, nil
}
