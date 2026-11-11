package workspace

import (
	"fmt"
	"log/slog"

	"github.com/wei-dong/tako/internal/inputbus"
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
}

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
	return s
}

// Restore reopens the saved blocks and tabs. Blocks whose host was deleted
// are skipped. It returns how many blocks were restored; connections come up
// in the background, so the grid appears at once.
func (m *Model) Restore(s State) int {
	restored := 0
	var focus inputbus.ID
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
		if i == s.Focus {
			focus = b.ID
		}
	}
	if focus != "" {
		m.focus = focus
	} else if len(m.blocks) > 0 {
		m.focus = m.blocks[0].ID
	}
	m.changed(false)
	return restored
}

func (m *Model) restoreBlock(bs BlockState) (*Block, error) {
	var b *Block
	if bs.HostID == "" {
		m.nextID++
		b = &Block{ID: localID(m.nextID), Kind: KindLocal, Name: bs.Name}
		if b.Name == "" {
			b.Name = "local"
		}
		if err := m.attach(b); err != nil {
			return nil, err
		}
	} else {
		h, ok := m.deps.Host(bs.HostID)
		if !ok {
			return nil, ErrNoSuchHost
		}
		var err error
		if b, err = m.addBlock(h); err != nil {
			return nil, err
		}
		if bs.Name != "" {
			b.Name = bs.Name
		}
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
