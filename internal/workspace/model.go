package workspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/plugins/terminal"

	"github.com/wei-dong/tako/internal/inputbus"
	"github.com/wei-dong/tako/internal/layout"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/session"
	"github.com/wei-dong/tako/internal/transport"
)

// initialSize is the size panes and terminals start with, before the view
// fits them.
var initialSize = transport.Size{Cols: 100, Rows: 30}

// Deps are what a Model needs from the rest of the app.
type Deps struct {
	// Ctx ends every session and terminal.
	Ctx context.Context //nolint:containedctx // a dependency bundle: Ctx is the lifetime of everything the model starts
	// Bus mirrors input between synced blocks.
	Bus *inputbus.Bus
	// NewSession creates the session of a host (one per host, shared by all
	// its blocks).
	NewSession func(model.Host) *session.Session
	// Host finds a host by ID.
	Host func(model.ID) (model.Host, bool)
	// NewTerminal creates a terminal; terminal.New when nil.
	NewTerminal func(terminal.Options) (*terminal.Terminal, error)
	// Theme returns the terminal colors for the current appearance.
	Theme func() *terminal.Theme
	// Post runs fn on the UI goroutine. Callbacks from other goroutines use
	// it; tests may run fn inline.
	Post func(fn func())
	// Changed is called after the model changed in a way the view shows.
	Changed func()
	// Save asks for the state to be persisted (debounced by the caller).
	Save func()
}

// Kind tells where a block's shell runs.
type Kind int

// Block kinds.
const (
	// KindSSH is a shell on a host from the library.
	KindSSH Kind = iota
	// KindLocal is a shell on this computer.
	KindLocal
)

// Tab is one terminal of a block.
type Tab struct {
	ID   string
	Term *terminal.Terminal
	// Pane is the SSH channel under the terminal; nil for local shells.
	Pane *session.Pane

	fallback string
	// DropAt is when input last went nowhere because the connection was
	// down; the view shows its "input is not sent" card for a while after.
	DropAt time.Time
	// dir is the directory to restore to: the terminal's last OSC 7 report,
	// or where it started.
	startDir string
}

// Title is the tab's label: the program's title when it set one.
func (t *Tab) Title() string {
	if title := strings.TrimSpace(t.Term.Title()); title != "" {
		return title
	}
	return t.fallback
}

// Dir returns the tab's current directory when the shell reports it (OSC 7),
// else where it started.
func (t *Tab) Dir() string {
	if d := t.Term.Dir(); d != "" {
		return d
	}
	return t.startDir
}

// write sends bytes to the tab's shell as input; it is the bus destination.
func (t *Tab) write(p []byte) (int, error) {
	if t.Pane != nil {
		return t.Pane.Write(p)
	}
	t.Term.Send(p)
	return len(p), nil
}

// Block is one machine in the grid.
type Block struct {
	ID     inputbus.ID
	Kind   Kind
	HostID model.ID
	// Name is shown in the block header.
	Name   string
	OS     model.OSKind
	Tabs   []*Tab
	Active int
	Synced bool

	sess *session.Session
	// shown mirrors Tabs[Active] for the bus goroutines, which must not read
	// the slice the UI goroutine edits.
	shown atomicTab
	unsub func()
}

// ActiveTab returns the tab being shown, or nil.
func (b *Block) ActiveTab() *Tab {
	if b.Active < 0 || b.Active >= len(b.Tabs) {
		return nil
	}
	return b.Tabs[b.Active]
}

// Write sends synced input to the block's active tab. It is the bus
// destination and is safe from any goroutine.
func (b *Block) Write(p []byte) (int, error) {
	tab := b.shown.Load()
	if tab == nil {
		return len(p), nil
	}
	return tab.write(p)
}

// Link returns the connection state of an SSH block; local blocks are always
// connected.
func (b *Block) Link() session.StateChange {
	if b.sess == nil {
		return session.StateChange{State: session.StateConnected}
	}
	return b.sess.State()
}

// Session returns the block's SSH session, nil for a local block. The monitor
// runs its commands on it.
func (b *Block) Session() *session.Session { return b.sess }

// Reconnect retries an SSH block's connection at once.
func (b *Block) Reconnect() {
	if b.sess != nil {
		b.sess.Reconnect()
	}
}

// Model is the whole workspace.
type Model struct {
	deps   Deps
	blocks []*Block
	focus  inputbus.ID
	nextID int
	// sessions are shared by the blocks of one host.
	sessions map[model.ID]*sharedSession

	view      Viewport
	manual    bool
	tree      layout.Tree
	mini      Minimap
	zoom      inputbus.ID
	restoring bool
}

type sharedSession struct {
	s    *session.Session
	refs int
}

// New returns an empty workspace.
func New(deps Deps) *Model {
	if deps.NewTerminal == nil {
		deps.NewTerminal = terminal.New
	}
	if deps.Post == nil {
		deps.Post = func(fn func()) { fn() }
	}
	if deps.Changed == nil {
		deps.Changed = func() {}
	}
	if deps.Save == nil {
		deps.Save = func() {}
	}
	return &Model{
		deps: deps, sessions: map[model.ID]*sharedSession{},
		mini: Minimap{Corner: layout.BottomRight, Opacity: DefaultOpacity},
		view: Viewport{
			Size: layout.Size{W: 1200, H: 800}, Gap: 8, Min: layout.Size{W: 240, H: 150},
		},
	}
}

// Blocks returns the blocks in display order. Do not modify the slice.
func (m *Model) Blocks() []*Block { return m.blocks }

// Bus returns the input bus.
func (m *Model) Bus() *inputbus.Bus { return m.deps.Bus }

// Focused returns the block with the keyboard focus, or nil.
func (m *Model) Focused() *Block { return m.find(m.focus) }

func (m *Model) find(id inputbus.ID) *Block {
	for _, b := range m.blocks {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// IndexOf returns a block's position, or -1.
func (m *Model) IndexOf(b *Block) int { return slices.Index(m.blocks, b) }

// Focus gives a block the focus.
func (m *Model) Focus(b *Block) {
	if b == nil || m.focus == b.ID {
		return
	}
	m.focus = b.ID
	m.changed(false)
}

// BlocksOf returns the open blocks of a host.
func (m *Model) BlocksOf(id model.ID) []*Block {
	var out []*Block
	for _, b := range m.blocks {
		if b.HostID == id && b.Kind == KindSSH {
			out = append(out, b)
		}
	}
	return out
}

func (m *Model) changed(save bool) {
	m.deps.Changed()
	if save {
		m.deps.Save()
	}
}

// OpenHost shows a host in the grid. When the host is already open and
// newBlock is false, its block gets the focus instead.
func (m *Model) OpenHost(h model.Host, newBlock bool) (*Block, error) {
	if !newBlock {
		if open := m.BlocksOf(h.ID); len(open) > 0 {
			m.Focus(open[0])
			return open[0], nil
		}
	}
	b, err := m.addBlock(h)
	if err != nil {
		return nil, err
	}
	if err := m.addTab(b, "", ""); err != nil {
		m.removeBlock(b)
		return nil, err
	}
	m.focus = b.ID
	m.changed(true)
	return b, nil
}

// OpenLocal opens a shell on this computer in a new block.
func (m *Model) OpenLocal(dir string) (*Block, error) {
	m.nextID++
	b := &Block{ID: localID(m.nextID), Kind: KindLocal, Name: "local"}
	if err := m.attach(b); err != nil {
		return nil, err
	}
	if err := m.addTab(b, dir, ""); err != nil {
		m.removeBlock(b)
		return nil, err
	}
	m.focus = b.ID
	m.changed(true)
	return b, nil
}

func (m *Model) addBlock(h model.Host) (*Block, error) {
	m.nextID++
	b := &Block{
		ID: inputbus.ID(string(h.ID) + "#" + strconv.Itoa(m.nextID)), Kind: KindSSH, HostID: h.ID,
		Name: h.Name, OS: h.OS,
	}
	sh := m.sessions[h.ID]
	if sh == nil {
		sh = &sharedSession{s: m.deps.NewSession(h)}
		m.sessions[h.ID] = sh
	}
	sh.refs++
	b.sess = sh.s
	b.unsub = sh.s.Subscribe(func(session.StateChange) {
		m.deps.Post(func() { m.changed(false) })
	})
	if err := m.attach(b); err != nil {
		m.release(b)
		return nil, err
	}
	return b, nil
}

// attach adds b to the grid and the bus. In a manual layout the new block
// takes half of the focused one.
func (m *Model) attach(b *Block) error {
	if m.manual && !m.restoring {
		id := string(b.ID)
		if target := m.Focused(); target != nil {
			m.tree = m.tree.AddBeside(id, string(target.ID), m.view.Size, m.view.Gap)
		} else {
			m.tree = m.tree.Reconcile(append(m.blockIDs(), id), m.view.Size, m.view.Gap)
		}
	}
	m.blocks = append(m.blocks, b)
	m.deps.Bus.Register(b.ID, b)
	return nil
}

// release drops a block's hold on its shared session, closing it with the last.
func (m *Model) release(b *Block) {
	if b.unsub != nil {
		b.unsub()
		b.unsub = nil
	}
	if b.sess == nil {
		return
	}
	if sh := m.sessions[b.HostID]; sh != nil {
		sh.refs--
		if sh.refs <= 0 {
			sh.s.Close()
			delete(m.sessions, b.HostID)
		}
	}
	b.sess = nil
}

// AddTab opens another terminal on the block's machine and shows it.
func (m *Model) AddTab(b *Block) error {
	dir := ""
	if t := b.ActiveTab(); t != nil && b.Kind == KindLocal {
		dir = t.Dir()
	}
	err := m.addTab(b, dir, "")
	if err == nil {
		m.changed(true)
	}
	return err
}

func (m *Model) addTab(b *Block, dir, cmd string) error {
	tab := &Tab{ID: fmt.Sprintf("%s/t%d", b.ID, m.bumpTab()), startDir: dir}
	tab.fallback = "shell " + strconv.Itoa(len(b.Tabs)+1)
	opts := terminal.Options{
		Theme:      m.deps.Theme(),
		NoBlink:    true,
		Font:       terminal.Font{Size: 12},
		Scrollback: 4 << 20,
		OnExit: func(int) {
			m.deps.Post(func() { m.tabExited(b, tab) })
		},
		OnTitle: func(string) { m.deps.Post(func() { m.changed(false) }) },
	}
	if b.Kind == KindLocal {
		opts.Dir = dir
	} else {
		pane, err := b.sess.OpenPaneAt(initialSize, dir, cmd)
		if err != nil {
			return fmt.Errorf("opening a tab on %s: %w", b.Name, err)
		}
		tab.Pane = pane
		var lastDrop atomic.Int64
		pane.OnDrop(func() {
			now := time.Now().UnixNano()
			if prev := lastDrop.Swap(now); now-prev < int64(500*time.Millisecond) {
				return // one notice per burst of typing
			}
			m.deps.Post(func() {
				tab.DropAt = time.Now()
				m.changed(false)
			})
		})
		opts.Conn = &inputbus.SyncConn{Raw: pane, Bus: m.deps.Bus, ID: b.ID}
	}
	term, err := m.deps.NewTerminal(opts)
	if err != nil {
		if tab.Pane != nil {
			_ = tab.Pane.Close()
		}
		return fmt.Errorf("creating the terminal for %s: %w", b.Name, err)
	}
	tab.Term = term
	b.Tabs = append(b.Tabs, tab)
	m.setActive(b, len(b.Tabs)-1)
	return nil
}

func (m *Model) bumpTab() int { m.nextID++; return m.nextID }

// tabExited closes a tab whose shell ended on its own (the user typed exit).
func (m *Model) tabExited(b *Block, t *Tab) {
	if t.Pane != nil && !t.Pane.ShellExited() {
		// The network dropped; the session reconnects and keeps the tab.
		select {
		case <-t.Pane.Done():
		default:
			return
		}
		if !t.Pane.ShellExited() {
			return // closed locally
		}
	}
	if i := slices.Index(b.Tabs, t); i >= 0 {
		m.CloseTab(b, i)
	}
}

// SetActive shows the tab at index i.
func (m *Model) SetActive(b *Block, i int) {
	if i < 0 || i >= len(b.Tabs) || b.Active == i {
		return
	}
	m.setActive(b, i)
	m.changed(true)
}

func (m *Model) setActive(b *Block, i int) {
	b.Active = i
	b.shown.Store(b.Tabs[i])
}

// CloseTab closes one tab; the block goes with its last tab.
func (m *Model) CloseTab(b *Block, i int) {
	if i < 0 || i >= len(b.Tabs) {
		return
	}
	t := b.Tabs[i]
	b.Tabs = slices.Delete(b.Tabs, i, i+1)
	closeTab(t)
	if len(b.Tabs) == 0 {
		m.removeBlock(b)
		m.changed(true)
		return
	}
	active := b.Active
	if i < active {
		active--
	}
	m.setActive(b, min(max(active, 0), len(b.Tabs)-1))
	m.changed(true)
}

func closeTab(t *Tab) {
	if err := t.Term.Close(); err != nil {
		slog.Debug("closing terminal", "err", err)
	}
	if t.Pane != nil {
		_ = t.Pane.Close()
	}
}

// CloseBlock closes a block and all its tabs.
func (m *Model) CloseBlock(b *Block) {
	for _, t := range b.Tabs {
		closeTab(t)
	}
	b.Tabs = nil
	m.removeBlock(b)
	m.changed(true)
}

func (m *Model) removeBlock(b *Block) {
	m.deps.Bus.Unregister(b.ID)
	m.release(b)
	i := slices.Index(m.blocks, b)
	if i < 0 {
		return
	}
	m.blocks = slices.Delete(m.blocks, i, i+1)
	if m.manual {
		m.tree = m.tree.Remove(string(b.ID))
		if m.tree.Empty() {
			m.manual = false
		}
	}
	if m.zoom == b.ID {
		m.zoom = ""
	}
	if m.focus == b.ID {
		m.focus = ""
		if len(m.blocks) > 0 {
			m.focus = m.blocks[min(i, len(m.blocks)-1)].ID
		}
	}
}

// SetSynced turns input sync on or off for one block.
func (m *Model) SetSynced(b *Block, on bool) {
	b.Synced = on
	m.deps.Bus.SetSynced(b.ID, on)
	m.changed(true)
}

// SyncedCount returns how many blocks have sync on.
func (m *Model) SyncedCount() int {
	n := 0
	for _, b := range m.blocks {
		if b.Synced {
			n++
		}
	}
	return n
}

// DuplicateTab opens a new tab like the active one (same host, same
// directory).
func (m *Model) DuplicateTab(b *Block) error {
	dir := ""
	if t := b.ActiveTab(); t != nil {
		dir = t.Dir()
	}
	err := m.addTab(b, dir, "")
	if err == nil {
		m.changed(true)
	}
	return err
}

// Rename sets a block's header name.
func (m *Model) Rename(b *Block, name string) {
	if name = strings.TrimSpace(name); name != "" {
		b.Name = name
		m.changed(true)
	}
}

// Counts returns how many SSH blocks are connected and reconnecting.
func (m *Model) Counts() (connected, reconnecting int) {
	for _, b := range m.blocks {
		if b.Kind != KindSSH {
			continue
		}
		switch b.Link().State {
		case session.StateConnected:
			connected++
		case session.StateReconnecting, session.StateConnecting, session.StateFailed:
			reconnecting++
		case session.StateClosed:
		}
	}
	return connected, reconnecting
}

// Close ends every terminal and session.
func (m *Model) Close() {
	for _, b := range slices.Clone(m.blocks) {
		for _, t := range b.Tabs {
			closeTab(t)
		}
		b.Tabs = nil
		m.removeBlock(b)
	}
}

// ErrNoSuchHost is returned when restoring state that mentions a deleted host.
var ErrNoSuchHost = errors.New("host no longer exists")

var _ io.Writer = (*Block)(nil)

func localID(n int) inputbus.ID { return inputbus.ID("local#" + strconv.Itoa(n)) }
