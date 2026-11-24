//go:build e2e

package automation

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/zdva/tako/internal/store"
	"github.com/zdva/tako/internal/testutil/memssh"
)

// TestWindow_InputSyncLocalAndSSH types into real local shells and into a
// test SSH server on loopback. Synced blocks receive the line; a block left
// out of the group does not; an inactive tab does not; the SSH session is
// the test server, with a key minted here and no password.
func TestWindow_InputSyncLocalAndSSH(t *testing.T) {
	pw := make([]byte, 16)
	if _, err := rand.Read(pw); err != nil {
		t.Fatal(err)
	}
	keyPath, pub := writeClientKey(t, t.TempDir())
	srv, err := memssh.Start(t.Context(), memssh.Options{
		Name:           "e2e",
		User:           "tako",
		Password:       hex.EncodeToString(pw),
		AuthorizedKeys: []ssh.PublicKey{pub},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)

	s := startSession(t, launchOpts{
		size: "1200x800",
		prepare: func(dirs store.Dirs, _ string) {
			line := knownhosts.Line([]string{knownhosts.Normalize(srv.Addr())}, srv.HostKey()) + "\n"
			if err := os.WriteFile(dirs.KnownHosts(), []byte(line), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	})
	s.noKeychain()
	s.noDialog()

	alpha := s.openNamedLocal("alpha")
	beta := s.openNamedLocal("beta")
	gamma := s.openNamedLocal("gamma")
	pidA := s.shellPid(alpha)
	pidB := s.shellPid(beta)
	pidG := s.shellPid(gamma)

	s.onlyActiveTabReceives(gamma, pidG)

	s.toggleSync(alpha)
	s.toggleSync(beta)
	s.waitSyncCount(2)
	syncPath := filepath.Join(s.marks, "out-sync.txt")
	s.runLine(alpha, appendMark(syncPath, "SYNCIN"))
	body := waitContains(t, syncPath, "SYNCIN "+pidA, "SYNCIN "+pidB)
	if strings.Contains(body, "SYNCIN "+pidG) {
		t.Fatalf("the unsynced block received synced input:\n%s", body)
	}

	s.addSSHHost(srv.Addr(), keyPath)
	s.wait(25*time.Second, "the test ssh server to accept the session", func() bool {
		if srv.Logins() < 1 || len(srv.Sizes()) == 0 {
			s.rejectConnectDialog()
			return false
		}
		return true
	})
	s.wait(10*time.Second, "the ssh block", func() bool { return s.tree().hasText("e2e-ssh") })

	s.toggleSync(beta)
	s.toggleSync("e2e-ssh")
	s.waitSyncCount(2)
	crossPath := filepath.Join(s.marks, "out-cross.txt")
	s.runLine(alpha, appendMark(crossPath, "CROSSIN"))
	crossed := waitContains(t, crossPath, "CROSSIN "+pidA)
	if strings.Contains(crossed, "CROSSIN "+pidB) || strings.Contains(crossed, "CROSSIN "+pidG) {
		t.Fatalf("a block outside the sync group received the line:\n%s", crossed)
	}
	if strings.Count(crossed, "CROSSIN ") != 1 {
		t.Fatalf("the line reached %d shells, want the one synced local shell:\n%s", strings.Count(crossed, "CROSSIN "), crossed)
	}
	// The mirror to SSH is queued, so the local shell can finish first.
	var typed string
	s.wait(8*time.Second, "the ssh session to receive the line", func() bool {
		typed = string(srv.Typed())
		return strings.Count(typed, "CROSSIN") >= 1
	})
	if n := strings.Count(typed, "CROSSIN"); n != 1 {
		t.Fatalf("the ssh session saw the synced line %d times, want 1", n)
	}
	if strings.Contains(typed, "SYNCIN") {
		t.Fatal("the ssh session received input from before it joined the group")
	}
	s.noKeychain()
}

func writeClientKey(t *testing.T, dir string) (string, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "tako-e2e")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return path, signer.PublicKey()
}

func (s *session) openNamedLocal(name string) string {
	s.t.Helper()
	s.openLocal()
	s.renameFocused(name)
	return name
}

func (s *session) renameFocused(name string) {
	s.t.Helper()
	s.pressKey("searchbox")
	s.settle()
	s.must("act", map[string]any{"key": "pal:rename", "action": "scrollIntoView"})
	s.pressKey("pal:rename")
	s.settle()
	s.setValue("rename-name", name)
	s.pressKey("rename-done")
	s.settle()
	s.wait(5*time.Second, "the block to be renamed "+name, func() bool {
		return s.tree().hasText(name)
	})
}

func (s *session) shellPid(name string) string {
	s.t.Helper()
	path := filepath.Join(s.marks, name+".pid")
	s.runLine(name, writePid(path))
	body, ok := pollFile(path, 4*time.Second)
	if !ok {
		// A login shell can still be starting; the overwrite is the same pid.
		s.runLine(name, writePid(path))
		body, ok = pollFile(path, 8*time.Second)
	}
	pid := strings.TrimSpace(body)
	if !ok || pid == "" {
		s.t.Fatalf("shell %s did not write a pid (got %q)", name, body)
	}
	return pid
}

func (s *session) onlyActiveTabReceives(name, activePid string) {
	s.t.Helper()
	title := s.blockTitle(name)
	newer, ok := onBlock(s.tree().byKey("tab-new"), title)
	if !ok {
		s.t.Fatalf("no new-tab button on %s", name)
	}
	s.pressID(newer.ID)
	s.settle()
	s.wait(5*time.Second, "a second tab on "+name, func() bool {
		return len(tabsNear(s.tree(), title.Bounds.Y)) >= 2
	})
	tab2 := filepath.Join(s.marks, name+"-tab2.pid")
	s.runLine(name, writePid(tab2))
	body2, ok := pollFile(tab2, 4*time.Second)
	other := strings.TrimSpace(body2)
	if !ok || other == "" || other == activePid {
		s.runLine(name, writePid(tab2))
		body2, ok = pollFile(tab2, 8*time.Second)
		other = strings.TrimSpace(body2)
	}
	if !ok || other == "" || other == activePid {
		s.t.Fatalf("the second tab's shell pid is %q, active tab is %q", other, activePid)
	}
	tabs := tabsNear(s.tree(), title.Bounds.Y)
	slices.SortFunc(tabs, func(a, b node) int {
		switch {
		case a.Bounds.X < b.Bounds.X:
			return -1
		case a.Bounds.X > b.Bounds.X:
			return 1
		default:
			return 0
		}
	})
	first := tabs[0]
	s.click(first.Bounds.X+12, first.Bounds.cy())
	s.settle()
	who := filepath.Join(s.marks, name+"-who.txt")
	s.runLine(name, appendMark(who, "who"))
	body := waitContains(s.t, who, "who "+activePid)
	if strings.Contains(body, "who "+other) {
		s.t.Fatalf("the inactive tab received the line:\n%s", body)
	}
	if strings.Count(body, "who ") != 1 {
		s.t.Fatalf("the line reached %d tabs, want 1:\n%s", strings.Count(body, "who "), body)
	}
}

func (s *session) addSSHHost(addr, keyPath string) {
	s.t.Helper()
	host, port := atoiPort(addr)
	s.pressKey("add-host")
	s.settle()
	s.setValue("field-name", "e2e-ssh")
	s.setValue("field-addr", host)
	s.setValue("field-port", strconv.Itoa(port))
	s.setValue("field-user", "tako")
	s.pressSegment("field-auth", 1)
	s.settle()
	s.setValue("field-key", keyPath)
	s.pressKey("host-save")
	s.settle()
}

func (s *session) pressSegment(group string, index int) {
	s.t.Helper()
	tr := s.tree()
	g, ok := tr.one(group)
	if !ok {
		s.t.Fatalf("no control %s", group)
	}
	var radios []node
	for _, n := range tr.byRole("radio") {
		if g.Bounds.contains(n.Bounds.cx(), n.Bounds.cy()) {
			radios = append(radios, n)
		}
	}
	slices.SortFunc(radios, func(a, b node) int {
		switch {
		case a.Bounds.X < b.Bounds.X:
			return -1
		case a.Bounds.X > b.Bounds.X:
			return 1
		default:
			return 0
		}
	})
	if index >= len(radios) {
		s.t.Fatalf("%s has %d segments, want index %d", group, len(radios), index)
	}
	s.pressID(radios[index].ID)
}

func (s *session) rejectConnectDialog() {
	s.t.Helper()
	for _, n := range s.tree().Nodes {
		if n.Role == "dialog" || n.Role == "alertDialog" {
			s.t.Fatalf("connecting asked for %s %q; the test server's host key was seeded and no password is used", n.Role, n.Label)
		}
	}
}

func (s *session) toggleSync(name string) {
	s.t.Helper()
	chip := s.syncChip(name)
	s.click(chip.Bounds.cx(), chip.Bounds.cy())
	s.settle()
}

func (s *session) waitSyncCount(n int) {
	s.t.Helper()
	want := fmt.Sprintf("%d /", n)
	s.wait(5*time.Second, "sync count "+want, func() bool {
		for _, node := range s.tree().Nodes {
			if strings.Contains(node.Label, want) {
				return true
			}
		}
		return false
	})
}

func (s *session) runLine(name, cmd string) {
	s.t.Helper()
	term := s.terminalFor(name)
	s.click(term.Bounds.cx(), term.Bounds.cy())
	s.settle()
	s.typeLine(cmd)
	s.settle()
}

func (s *session) blockTitle(name string) node {
	s.t.Helper()
	tr := s.tree()
	chips := tr.byKey("sync")
	var best node
	found := false
	bestDy := 1e9
	for _, text := range tr.byLabel(name) {
		for _, chip := range chips {
			dy := absFloat(chip.Bounds.Y - text.Bounds.Y)
			if dy < 48 && dy < bestDy {
				best, bestDy, found = text, dy, true
			}
		}
	}
	if !found {
		s.t.Fatalf("no block titled %q", name)
	}
	return best
}

func (s *session) syncChip(name string) node {
	s.t.Helper()
	chip, ok := onBlock(s.tree().byKey("sync"), s.blockTitle(name))
	if !ok {
		s.t.Fatalf("no sync control on %s", name)
	}
	return chip
}

func (s *session) terminalFor(name string) node {
	s.t.Helper()
	title := s.blockTitle(name)
	var best node
	found := false
	bestDy := 1e9
	for _, term := range s.tree().byLabel("Terminal") {
		dy := term.Bounds.Y - title.Bounds.Y
		if dy < -5 || dy > 600 || !coversX(term.Bounds, title.Bounds.X) || dy >= bestDy {
			continue
		}
		best, bestDy, found = term, dy, true
	}
	if !found {
		s.t.Fatalf("no terminal for %s", name)
	}
	return best
}

// onBlock picks the control on title's header: same row, to the right of
// the name, and the nearest such control. Two blocks side by side share a
// Y, so Y alone matches both.
func onBlock(all []node, title node) (node, bool) {
	best := node{}
	found := false
	bestDx := 1e9
	for _, n := range all {
		if absFloat(n.Bounds.Y-title.Bounds.Y) > 48 {
			continue
		}
		dx := n.Bounds.X - title.Bounds.X
		if dx < -20 || dx >= bestDx {
			continue
		}
		best, bestDx, found = n, dx, true
	}
	return best, found
}

func coversX(r rect, x float64) bool {
	return x >= r.X-2 && x <= r.right()+2
}

func writePid(path string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("Set-Content -LiteralPath '%s' -Value $PID", path)
	}
	return fmt.Sprintf("printf '%%s\\n' $$ > '%s'", path)
}

func appendMark(path, mark string) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("Add-Content -LiteralPath '%s' -Value ('%s ' + $PID)", path, mark)
	}
	return fmt.Sprintf("printf '%s %%s\\n' $$ >> '%s'", mark, path)
}

func pollFile(path string, d time.Duration, parts ...string) (string, bool) {
	var body string
	deadline := time.Now().Add(d)
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			body = string(b)
			ok := strings.TrimSpace(body) != ""
			for _, part := range parts {
				if part != "" && !strings.Contains(body, part) {
					ok = false
				}
			}
			if ok {
				return body, true
			}
		}
		if time.Now().After(deadline) {
			return body, false
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func waitContains(t *testing.T, path string, parts ...string) string {
	t.Helper()
	body, ok := pollFile(path, 8*time.Second, parts...)
	if !ok {
		t.Fatalf("%s = %q, want %q", path, body, parts)
	}
	return body
}
