package workspace_test

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/zdva/tako/internal/workspace"
)

// Input sync with local terminals (user feedback: typing in a synced local
// terminal reached no other terminal). The commands print $((40+2)) so the
// output, "42", only shows when a shell ran the command: an echo of the
// typed line shows "$((40+2))" instead, and a count of the output tells how
// many times it ran.

var markN atomic.Int64

// mark returns a fresh marker and the command that prints it once.
func mark(name string) (out, cmd string) {
	m := fmt.Sprintf("TAKO_%s_%d_", name, markN.Add(1))
	return m + "42", "echo " + m + "$((40+2))\r"
}

func localShell(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the commands are POSIX shell")
	}
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "") // no interactive startup file
}

func (e *env) local(t *testing.T) *workspace.Block {
	t.Helper()
	b, err := e.m.OpenLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e.waitForShell(t, b)
	return b
}

// waitForShell waits until the active tab's shell runs commands, typed into
// it alone (the block is not synced yet).
func (e *env) waitForShell(t *testing.T, b *workspace.Block) {
	t.Helper()
	out, cmd := mark("ready")
	b.ActiveTab().Term.Send([]byte(cmd))
	e.waitFor(t, "a shell in "+string(b.ID), func() bool { return strings.Contains(text(b), out) })
}

func (e *env) ssh(t *testing.T, name string) *workspace.Block {
	t.Helper()
	b, err := e.m.OpenHost(e.host(name), false)
	if err != nil {
		t.Fatal(err)
	}
	e.waitFor(t, "pane "+name, func() bool { return b.ActiveTab().Pane.Connected() })
	return b
}

func text(b *workspace.Block) string { return b.ActiveTab().Term.Text() }

// ranOnce waits until out shows exactly once in b's active tab, and checks
// it stays once.
func (e *env) ranOnce(t *testing.T, b *workspace.Block, out string) {
	t.Helper()
	e.waitFor(t, out+" in "+string(b.ID), func() bool { return strings.Contains(text(b), out) })
	time.Sleep(150 * time.Millisecond)
	e.pump()
	if n := strings.Count(text(b), out); n != 1 {
		t.Errorf("%s ran %d times in %s, want once:\n%s", out, n, b.ID, text(b))
	}
}

func (e *env) never(t *testing.T, b *workspace.Block, out string) {
	t.Helper()
	time.Sleep(200 * time.Millisecond)
	e.pump()
	if strings.Contains(text(b), out) {
		t.Errorf("%s reached %s, which should not get it:\n%s", out, b.ID, text(b))
	}
}

func TestSync_LocalToLocal(t *testing.T) {
	localShell(t)
	e := newEnv(t)
	a, b, off := e.local(t), e.local(t), e.local(t)
	e.m.SetSynced(a, true)
	e.m.SetSynced(b, true)

	out, cmd := mark("l2l")
	a.ActiveTab().Term.Send([]byte(cmd))
	e.ranOnce(t, a, out) // the source's own shell got it once, not echoed back
	e.ranOnce(t, b, out)
	e.never(t, off, out)
	// The typed line shows once in the source: no double echo.
	if n := strings.Count(text(a), strings.TrimSuffix(cmd, "\r")); n != 1 {
		t.Errorf("the typed line shows %d times in the source:\n%s", n, text(a))
	}

	// Either side can be the source.
	out, cmd = mark("back")
	b.ActiveTab().Term.Send([]byte(cmd))
	e.ranOnce(t, b, out)
	e.ranOnce(t, a, out)
	e.never(t, off, out)
}

func TestSync_LocalToSSH(t *testing.T) {
	localShell(t)
	e := newEnv(t)
	l, s, off := e.local(t), e.ssh(t, "s"), e.ssh(t, "off")
	_ = off
	e.m.SetSynced(l, true)
	e.m.SetSynced(s, true)

	out, cmd := mark("l2s")
	l.ActiveTab().Term.Send([]byte(cmd))
	e.ranOnce(t, l, out)
	line := strings.TrimSuffix(cmd, "\r")
	e.waitFor(t, "the SSH shell got the local input", func() bool {
		return strings.Contains(string(e.srv.Typed()), line)
	})
	time.Sleep(150 * time.Millisecond)
	// memssh records the typing of both of its shells: only s's may have it.
	if n := strings.Count(string(e.srv.Typed()), line); n != 1 {
		t.Errorf("the local line reached %d SSH shells, want 1 (not the unsynced one)", n)
	}
}

func TestSync_SSHToLocal(t *testing.T) {
	localShell(t)
	e := newEnv(t)
	s, l, off := e.ssh(t, "s"), e.local(t), e.local(t)
	e.m.SetSynced(s, true)
	e.m.SetSynced(l, true)

	out, cmd := mark("s2l")
	s.ActiveTab().Term.Send([]byte(cmd))
	e.ranOnce(t, l, out)
	e.never(t, off, out)
	if n := strings.Count(string(e.srv.Typed()), strings.TrimSuffix(cmd, "\r")); n != 1 {
		t.Errorf("the SSH source got its own line %d times, want once", n)
	}
}

func TestSync_SSHToSSH(t *testing.T) {
	e := newEnv(t)
	a, b, off := e.ssh(t, "a"), e.ssh(t, "b"), e.ssh(t, "off")
	_ = off
	e.m.SetSynced(a, true)
	e.m.SetSynced(b, true)
	_, cmd := mark("s2s")
	line := strings.TrimSuffix(cmd, "\r")
	a.ActiveTab().Term.Send([]byte(cmd))
	e.waitFor(t, "both SSH shells", func() bool { return strings.Count(string(e.srv.Typed()), line) >= 2 })
	time.Sleep(150 * time.Millisecond)
	if n := strings.Count(string(e.srv.Typed()), line); n != 2 {
		t.Errorf("the line reached %d SSH shells, want 2", n)
	}
	_ = b
}

// TestSync_OnlyTheActiveTabReceives: a block takes synced input in the tab
// it shows, and follows a switch of tabs; a hidden tab gets nothing.
func TestSync_OnlyTheActiveTabReceives(t *testing.T) {
	localShell(t)
	e := newEnv(t)
	src, dst := e.local(t), e.local(t)
	if err := e.m.AddTab(dst); err != nil {
		t.Fatal(err)
	}
	e.waitForShell(t, dst)
	e.m.SetSynced(src, true)
	e.m.SetSynced(dst, true)
	first, second := dst.Tabs[0], dst.Tabs[1]

	out, cmd := mark("tab2")
	src.ActiveTab().Term.Send([]byte(cmd))
	e.ranOnce(t, dst, out) // dst shows its second tab
	if strings.Contains(first.Term.Text(), out) {
		t.Errorf("the hidden first tab got synced input")
	}

	e.m.SetActive(dst, 0)
	out, cmd = mark("tab1")
	src.ActiveTab().Term.Send([]byte(cmd))
	e.ranOnce(t, dst, out)
	if strings.Contains(second.Term.Text(), out) {
		t.Errorf("the hidden second tab got synced input")
	}

	// A local tab of the source that is not shown does not publish: only
	// what is typed in a synced block counts, whichever tab shows.
	if err := e.m.AddTab(src); err != nil {
		t.Fatal(err)
	}
	e.waitForShell(t, src)
	out, cmd = mark("srctab")
	src.ActiveTab().Term.Send([]byte(cmd))
	e.ranOnce(t, dst, out)
}

// TestSync_ClosingLocalTabsAndBlocksLeaksNothing: the local terminals and
// their sync plumbing stop with their tabs and blocks.
func TestSync_ClosingLocalTabsAndBlocksLeaksNothing(t *testing.T) {
	localShell(t)
	e := newEnv(t)
	before := goleak.IgnoreCurrent()

	a, b := e.local(t), e.local(t)
	s := e.ssh(t, "s")
	if err := e.m.AddTab(a); err != nil {
		t.Fatal(err)
	}
	e.waitForShell(t, a)
	e.m.SyncAll(true)
	out, cmd := mark("leak")
	a.ActiveTab().Term.Send([]byte(cmd))
	e.ranOnce(t, b, out)

	e.m.CloseTab(a, 1)
	e.m.CloseBlock(a)
	e.m.CloseBlock(b)
	e.m.CloseBlock(s)
	if n := len(e.m.Blocks()); n != 0 {
		t.Fatalf("%d blocks left", n)
	}
	e.pump()
	goleak.VerifyNone(t, before,
		// memssh's connection handlers end as the client's close reaches them.
		goleak.IgnoreAnyFunction("github.com/zdva/tako/internal/testutil/memssh.(*Server).serveConn"),
	)
}
