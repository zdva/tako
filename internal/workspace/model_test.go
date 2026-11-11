package workspace_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"

	"github.com/wei-dong/tako/internal/inputbus"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/session"
	"github.com/wei-dong/tako/internal/testutil/memssh"
	"github.com/wei-dong/tako/internal/transport"
	tssh "github.com/wei-dong/tako/internal/transport/ssh"
	"github.com/wei-dong/tako/internal/workspace"
)

type env struct {
	t     *testing.T
	m     *workspace.Model
	hosts map[model.ID]model.Host
	srv   *memssh.Server
	dials int
	posts chan func()
	mu    sync.Mutex
}

func newEnv(t *testing.T) *env {
	t.Helper()
	srv, err := memssh.Start(t.Context(), memssh.Options{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	e := &env{t: t, srv: srv, posts: make(chan func(), 1024), hosts: map[model.ID]model.Host{}}
	store := secrets.NewMemory()
	_ = store.Set(t.Context(), "pw", []byte("pw"))
	dialer := &tssh.Dialer{
		KnownHosts: tssh.NewKnownHosts(t.TempDir() + "/known_hosts"),
		Secrets:    store,
		Prompter:   trustAll{},
	}
	bus := inputbus.New(t.Context())
	t.Cleanup(bus.Close)
	e.m = workspace.New(workspace.Deps{
		Ctx: t.Context(),
		Bus: bus,
		NewSession: func(h model.Host) *session.Session {
			e.mu.Lock()
			e.dials++
			e.mu.Unlock()
			return session.New(session.Options{Host: h, Dialer: dialer, Backoff: []time.Duration{50 * time.Millisecond}})
		},
		Post:  func(fn func()) { e.posts <- fn },
		Host:  func(id model.ID) (model.Host, bool) { h, ok := e.hosts[id]; return h, ok },
		Theme: func() *terminal.Theme { return terminal.LightTheme() },
	})
	t.Cleanup(e.m.Close)
	return e
}

type trustAll struct{}

func (trustAll) ConfirmHostKey(context.Context, transport.HostKeyRequest) (bool, error) {
	return true, nil
}
func (trustAll) Secret(context.Context, transport.SecretRequest) (transport.SecretReply, error) {
	return transport.SecretReply{}, transport.ErrCanceled
}
func (trustAll) Challenge(context.Context, transport.ChallengeRequest) ([]string, error) {
	return nil, transport.ErrCanceled
}

func (e *env) host(name string) model.Host {
	h, ps, _ := strings.Cut(e.srv.Addr(), ":")
	var port int
	for _, c := range ps {
		port = port*10 + int(c-'0')
	}
	host := model.Host{
		ID: model.NewID(), Name: name, Addr: h, Port: port, User: "tako",
		Auth: []model.Auth{{Kind: model.AuthPassword, SecretRef: "pw"}},
	}
	e.hosts[host.ID] = host
	return host
}

// pump runs the callbacks posted from other goroutines, on the test's.
func (e *env) pump() {
	for {
		select {
		case fn := <-e.posts:
			fn()
		default:
			return
		}
	}
}

func (e *env) waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for e.pump(); !cond(); e.pump() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOpenHost_ReusesBlockAndSharesSession(t *testing.T) {
	e := newEnv(t)
	h := e.host("web-01")

	b1, err := e.m.OpenHost(h, false)
	if err != nil {
		t.Fatal(err)
	}
	b1again, err := e.m.OpenHost(h, false)
	if err != nil || b1again != b1 || len(e.m.Blocks()) != 1 {
		t.Fatalf("second open: block %v err %v, blocks %d; want the same block", b1again, err, len(e.m.Blocks()))
	}
	b2, err := e.m.OpenHost(h, true)
	if err != nil || b2 == b1 || len(e.m.Blocks()) != 2 {
		t.Fatalf("new block: %v %v", b2, err)
	}
	if e.dials != 1 {
		t.Errorf("sessions created = %d, want 1 shared by both blocks", e.dials)
	}
	if e.m.Focused() != b2 {
		t.Error("the newest block should have the focus")
	}
	e.waitFor(t, "connected", func() bool { return b1.Link().State == session.StateConnected })

	// Closing one block keeps the session for the other.
	e.m.CloseBlock(b2)
	if b1.Link().State == session.StateClosed {
		t.Error("closing one block closed the shared session")
	}
	e.m.CloseBlock(b1)
	if len(e.m.Blocks()) != 0 || e.m.Focused() != nil {
		t.Error("blocks remain")
	}
}

func TestTabs(t *testing.T) {
	e := newEnv(t)
	b, err := e.m.OpenHost(e.host("a"), false)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := e.m.AddTab(b); err != nil {
			t.Fatal(err)
		}
	}
	if len(b.Tabs) != 3 || b.Active != 2 {
		t.Fatalf("tabs=%d active=%d", len(b.Tabs), b.Active)
	}
	e.m.SetActive(b, 0)
	e.m.CloseTab(b, 0) // closing the active first tab
	if len(b.Tabs) != 2 || b.Active != 0 {
		t.Errorf("after closing first: tabs=%d active=%d", len(b.Tabs), b.Active)
	}
	e.m.SetActive(b, 1)
	e.m.CloseTab(b, 0) // closing a tab before the active one keeps the active tab
	if len(b.Tabs) != 1 || b.Active != 0 {
		t.Errorf("after closing before active: tabs=%d active=%d", len(b.Tabs), b.Active)
	}
	e.m.CloseTab(b, 0)
	if len(e.m.Blocks()) != 0 {
		t.Error("the block should close with its last tab")
	}
}

func TestSyncMirrorsTypedInputToSyncedBlocks(t *testing.T) {
	e := newEnv(t)
	a, _ := e.m.OpenHost(e.host("a"), false)
	b, _ := e.m.OpenHost(e.host("b"), false)
	c, _ := e.m.OpenHost(e.host("c"), false)
	for _, blk := range []*workspace.Block{a, b, c} {
		blk := blk
		e.waitFor(t, "pane "+blk.Name, func() bool { return blk.ActiveTab().Pane.Connected() })
	}
	e.m.SetSynced(a, true)
	e.m.SetSynced(b, true)

	// What the terminal writes goes to a's session, and is mirrored to b only.
	a.ActiveTab().Term.Send([]byte("ls\n"))
	e.waitFor(t, "mirrored input", func() bool { return strings.Count(string(e.srv.Typed()), "ls\n") >= 2 })
	time.Sleep(100 * time.Millisecond)
	if n := strings.Count(string(e.srv.Typed()), "ls\n"); n != 2 {
		t.Errorf("'ls' reached %d shells, want 2 (a and b, not the unsynced c)", n)
	}
	if e.m.SyncedCount() != 2 {
		t.Errorf("SyncedCount = %d", e.m.SyncedCount())
	}
}

func TestMoveFocus(t *testing.T) {
	e := newEnv(t)
	var bs []*workspace.Block
	for _, n := range []string{"a", "b", "c", "d"} {
		b, err := e.m.OpenHost(e.host(n), false)
		if err != nil {
			t.Fatal(err)
		}
		bs = append(bs, b)
	}
	e.m.Focus(bs[0])
	e.m.MoveFocus(2, 1, 0)
	if e.m.Focused() != bs[1] {
		t.Error("right from a should be b")
	}
	e.m.MoveFocus(2, 1, 0)
	if e.m.Focused() != bs[1] {
		t.Error("right from the end of a row must not wrap")
	}
	e.m.MoveFocus(2, 0, 1)
	if e.m.Focused() != bs[3] {
		t.Error("down from b should be d")
	}
	e.m.MoveFocus(2, 0, 1)
	if e.m.Focused() != bs[3] {
		t.Error("down from the last row stays")
	}
	e.m.MoveFocus(2, -1, 0)
	e.m.MoveFocus(2, 0, -1)
	if e.m.Focused() != bs[0] {
		t.Error("up and left should reach a")
	}
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	e := newEnv(t)
	h1, h2 := e.host("a"), e.host("b")
	a, _ := e.m.OpenHost(h1, false)
	_ = e.m.AddTab(a)
	b, _ := e.m.OpenHost(h2, false)
	l, err := e.m.OpenLocal(t.TempDir())
	if err != nil {
		t.Skipf("local terminals unavailable here: %v", err)
	}
	e.m.SetSynced(b, true)
	e.m.SetActive(a, 0)
	e.m.Focus(b)
	_ = l

	snap := e.m.Snapshot()
	if len(snap.Blocks) != 3 || snap.Focus != 1 || len(snap.Blocks[0].Tabs) != 2 || !snap.Blocks[1].Synced {
		t.Fatalf("snapshot = %+v", snap)
	}
	if snap.Blocks[2].HostID != "" {
		t.Error("the local block must have no host id")
	}

	// A deleted host is skipped; the others come back.
	delete(e.hosts, h1.ID)
	e.m.Close()
	if n := e.m.Restore(snap); n != 2 {
		t.Fatalf("restored %d blocks, want 2 (host a was deleted)", n)
	}
	got := e.m.Blocks()
	if got[0].HostID != h2.ID || !got[0].Synced || e.m.Focused() != got[0] {
		t.Errorf("restored blocks wrong: %+v", got[0])
	}
	if !e.m.Bus().Synced(got[0].ID) {
		t.Error("the bus does not know the restored block is synced")
	}
}

func TestExitedShellClosesTab(t *testing.T) {
	e := newEnv(t)
	b, _ := e.m.OpenHost(e.host("a"), false)
	_ = e.m.AddTab(b)
	e.waitFor(t, "pane", func() bool { return b.Tabs[1].Pane.Connected() })
	// The remote shell exits: the pane ends, the terminal sees EOF, OnExit
	// posts tabExited. memssh's shell exits on Ctrl-D.
	b.Tabs[1].Term.Send([]byte{4})
	e.waitFor(t, "tab removal", func() bool { return len(b.Tabs) == 1 })
	if b.Active != 0 {
		t.Errorf("active = %d", b.Active)
	}
}
