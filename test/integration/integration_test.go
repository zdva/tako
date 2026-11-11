//go:build integration && linux

// Package integration tests Tako's SSH stack against a real OpenSSH server.
//
// Run them with `make test-integration`. They need an sshd: scripts/fetch-sshd.sh
// unpacks a portable one into .cache/openssh, and the tests skip without it.
package integration

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/session"
	"github.com/wei-dong/tako/internal/testutil/nettest"
	"github.com/wei-dong/tako/internal/testutil/realsshd"
	"github.com/wei-dong/tako/internal/transport"
	tssh "github.com/wei-dong/tako/internal/transport/ssh"
)

const ppkPassphrase = "secret"

// prompter trusts every host key and answers secrets from fixed values.
type prompter struct{ password string }

func (p prompter) ConfirmHostKey(context.Context, transport.HostKeyRequest) (bool, error) {
	return true, nil
}

func (p prompter) Secret(_ context.Context, _ transport.SecretRequest) (transport.SecretReply, error) {
	return transport.SecretReply{Value: []byte(p.password)}, nil
}

func (p prompter) Challenge(_ context.Context, r transport.ChallengeRequest) ([]string, error) {
	out := make([]string, len(r.Questions))
	for i := range out {
		out[i] = p.password
	}
	return out, nil
}

// env is a set of servers and the dialer that reaches them.
type env struct {
	t      *testing.T
	dialer *tssh.Dialer
	mu     sync.Mutex
	hosts  map[model.ID]model.Host
	agent  *nettest.Agent
}

func newEnv(t *testing.T, password string) *env {
	t.Helper()
	e := &env{t: t, hosts: map[model.ID]model.Host{}, agent: nettest.StartAgent(t)}
	e.dialer = &tssh.Dialer{
		KnownHosts: tssh.NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts")),
		Secrets:    secrets.NewMemory(),
		Prompter:   prompter{password: password},
		Lookup: func(id model.ID) (model.Host, bool) {
			e.mu.Lock()
			defer e.mu.Unlock()
			h, ok := e.hosts[id]
			return h, ok
		},
		AgentConn:        e.agent.Dial,
		DialTimeout:      10 * time.Second,
		KeepAliveTimeout: 2 * time.Second,
	}
	return e
}

// host registers a server as a host entry that logs in with its client key.
func (e *env) host(s *realsshd.Server, name string, auth ...model.Auth) model.Host {
	if len(auth) == 0 {
		auth = []model.Auth{{Kind: model.AuthKey, KeyPath: s.ClientKeyPath}}
	}
	h := model.Host{ID: model.NewID(), Name: name, Addr: s.Host, Port: s.Port, User: s.User, Auth: auth}
	e.mu.Lock()
	e.hosts[h.ID] = h
	e.mu.Unlock()
	return h
}

// shellRun types a command in a shell and returns the output between two
// markers, so prompts and echo do not matter.
func shellRun(t *testing.T, sh transport.Shell, command string) string {
	t.Helper()
	const begin, end = "<<<", ">>>"
	// The markers are built by the shell, so the echoed command line does not
	// contain them in final form.
	line := fmt.Sprintf("printf '%%s%%s\\n' '<<' '<'; %s; printf '%%s%%s\\n' '>>' '>'\n", command)
	if _, err := sh.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	chunk := make([]byte, 4096)
	deadline := time.Now().Add(20 * time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for time.Now().Before(deadline) {
			n, err := sh.Read(chunk)
			buf.Write(chunk[:n])
			s := buf.String()
			if before, _, ok := strings.CutLast(s, end); ok && strings.Contains(before, begin) {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(25 * time.Second):
		t.Fatalf("timed out running %q, output so far: %q", command, buf.String())
	}
	s := buf.String()
	i := strings.LastIndex(s, begin)
	j := strings.LastIndex(s, end)
	if i < 0 || j < i {
		t.Fatalf("no markers in output of %q: %q", command, s)
	}
	return strings.ReplaceAll(strings.TrimSpace(s[i+len(begin):j]), "\r", "")
}

// ppkAuthorizedKey converts a PuTTY key file's public part to an ssh key.
func ppkPublicKey(t *testing.T, path string) gossh.PublicKey {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, l := range lines {
		if rest, ok := strings.CutPrefix(l, "Public-Lines:"); ok {
			n, err := strconv.Atoi(strings.TrimSpace(rest))
			if err != nil {
				t.Fatal(err)
			}
			blob, err := base64.StdEncoding.DecodeString(strings.Join(lines[i+1:i+1+n], ""))
			if err != nil {
				t.Fatal(err)
			}
			pub, err := gossh.ParsePublicKey(blob)
			if err != nil {
				t.Fatal(err)
			}
			return pub
		}
	}
	t.Fatalf("no Public-Lines in %s", path)
	return nil
}

func ppk(name string) string {
	return filepath.Join("..", "..", "internal", "transport", "ssh", "testdata", "ppk", name+".ppk")
}

func encryptedKey(t *testing.T, s *realsshd.Server) string {
	t.Helper()
	// Re-encrypt the server's client key with a passphrase.
	raw, err := os.ReadFile(s.ClientKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	priv, err := gossh.ParseRawPrivateKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	block, err := gossh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("open sesame"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "enc_key")
	if err := os.WriteFile(path, pemEncode(block.Type, block.Bytes), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

type authCase struct {
	name string
	auth func(s *realsshd.Server) model.Auth
	pass string
}

func TestAuthAndRouteMatrix(t *testing.T) {
	if !realsshd.Available() {
		t.Skip("no sshd: run scripts/fetch-sshd.sh")
	}
	e := newEnv(t, "")
	target := realsshd.Start(t, realsshd.Options{})
	bastion1 := realsshd.Start(t, realsshd.Options{})
	bastion2 := realsshd.Start(t, realsshd.Options{})
	target.AuthorizeKey(t, e.agent.Signer.PublicKey())
	for _, n := range []string{"ed25519-plain", "rsa-plain", "ecdsa-plain", "ed25519-enc", "rsa-enc2", "ecdsa-enc"} {
		target.AuthorizeKey(t, ppkPublicKey(t, ppk(n)))
	}
	encPath := encryptedKey(t, target)

	auths := []authCase{
		{"key", func(s *realsshd.Server) model.Auth { return model.Auth{Kind: model.AuthKey, KeyPath: s.ClientKeyPath} }, ""},
		{"key-passphrase", func(*realsshd.Server) model.Auth { return model.Auth{Kind: model.AuthKey, KeyPath: encPath} }, "open sesame"},
		{"agent", func(*realsshd.Server) model.Auth { return model.Auth{Kind: model.AuthAgent} }, ""},
		{"ppk-ed25519", func(*realsshd.Server) model.Auth {
			return model.Auth{Kind: model.AuthKey, KeyPath: ppk("ed25519-plain")}
		}, ""},
		{"ppk-rsa", func(*realsshd.Server) model.Auth { return model.Auth{Kind: model.AuthKey, KeyPath: ppk("rsa-plain")} }, ""},
		{"ppk-ecdsa", func(*realsshd.Server) model.Auth { return model.Auth{Kind: model.AuthKey, KeyPath: ppk("ecdsa-plain")} }, ""},
		{"ppk-ed25519-encrypted-v3", func(*realsshd.Server) model.Auth { return model.Auth{Kind: model.AuthKey, KeyPath: ppk("ed25519-enc")} }, ppkPassphrase},
		{"ppk-rsa-encrypted-v2", func(*realsshd.Server) model.Auth { return model.Auth{Kind: model.AuthKey, KeyPath: ppk("rsa-enc2")} }, ppkPassphrase},
		{"ppk-ecdsa-encrypted-v3", func(*realsshd.Server) model.Auth { return model.Auth{Kind: model.AuthKey, KeyPath: ppk("ecdsa-enc")} }, ppkPassphrase},
	}

	socks := nettest.StartSOCKS5(t, "", "")
	httpProxy := nettest.StartHTTPProxy(t, "u", "p")
	b1 := e.host(bastion1, "bastion1")
	b2 := e.host(bastion2, "bastion2")

	routes := []struct {
		name  string
		apply func(h *model.Host)
	}{
		{"direct", func(*model.Host) {}},
		{"socks5", func(h *model.Host) { h.Proxy = &model.Proxy{Kind: model.ProxySOCKS5, Addr: socks.Addr} }},
		{"http-proxy", func(h *model.Host) {
			_ = e.dialer.Secrets.Set(t.Context(), "proxy", []byte("p"))
			h.Proxy = &model.Proxy{Kind: model.ProxyHTTP, Addr: httpProxy.Addr, User: "u", SecretRef: "proxy"}
		}},
		{"jump1", func(h *model.Host) { h.Jump = []model.Hop{{HostID: b1.ID}} }},
		{"jump2", func(h *model.Host) { h.Jump = []model.Hop{{HostID: b1.ID}, {HostID: b2.ID}} }},
		{"jump1-over-socks5", func(h *model.Host) {
			h.Jump = []model.Hop{{HostID: b1.ID}}
			h.Proxy = &model.Proxy{Kind: model.ProxySOCKS5, Addr: socks.Addr}
		}},
	}

	for _, rt := range routes {
		for _, ac := range auths {
			t.Run(rt.name+"/"+ac.name, func(t *testing.T) {
				t.Parallel()
				d := *e.dialer
				d.Prompter = prompter{password: ac.pass}
				h := e.host(target, "target", ac.auth(target))
				rt.apply(&h)
				tr, err := d.Dial(t.Context(), h)
				if err != nil {
					t.Fatalf("Dial: %v", err)
				}
				defer func() { _ = tr.Close() }()
				sh, err := tr.OpenShell(t.Context(), transport.Size{Cols: 100, Rows: 30})
				if err != nil {
					t.Fatalf("OpenShell: %v", err)
				}
				defer func() { _ = sh.Close() }()
				if got := shellRun(t, sh, "echo TERM=$TERM; stty size"); got != "TERM=xterm-256color\n30 100" {
					t.Errorf("shell environment = %q", got)
				}
				if err := sh.Resize(132, 43); err != nil {
					t.Fatal(err)
				}
				// The server applies window-change asynchronously.
				deadline := time.Now().Add(3 * time.Second)
				var size string
				for time.Now().Before(deadline) {
					if size = shellRun(t, sh, "stty size"); size == "43 132" {
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				if size != "43 132" {
					t.Errorf("size after Resize = %q, want 43 132", size)
				}
			})
		}
	}
}

func TestInteractiveProgramsWork(t *testing.T) {
	if !realsshd.Available() {
		t.Skip("no sshd")
	}
	e := newEnv(t, "")
	srv := realsshd.Start(t, realsshd.Options{})
	tr, err := e.dialer.Dial(t.Context(), e.host(srv, "t"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.Close() }()
	sh, err := tr.OpenShell(t.Context(), transport.Size{Cols: 120, Rows: 40})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sh.Close() }()

	if _, err := exec.LookPath("top"); err == nil {
		out := shellRun(t, sh, "top -b -n 1 | head -n 3")
		if !strings.Contains(out, "top -") {
			t.Errorf("top output = %q", out)
		}
	} else {
		t.Log("top is not installed here")
	}
	if vim, err := exec.LookPath("vim"); err == nil {
		// Full-screen mode: start vim, quit it, and see the shell responds.
		_, _ = sh.Write([]byte(vim + " -u NONE -N\n"))
		time.Sleep(500 * time.Millisecond)
		_, _ = sh.Write([]byte(":q\r"))
		if out := shellRun(t, sh, "echo back-from-vim"); !strings.Contains(out, "back-from-vim") {
			t.Errorf("shell did not come back after vim: %q", out)
		}
	} else {
		t.Log("vim is not installed here")
	}
	// Binary-safe and large output passes through unchanged.
	out := shellRun(t, sh, "seq 1 20000 | md5sum | cut -c1-8; echo done")
	if !strings.HasSuffix(out, "done") {
		t.Errorf("large output run = %q", out)
	}
}

func TestHostKeyChangeRefusedAndExplained(t *testing.T) {
	if !realsshd.Available() {
		t.Skip("no sshd")
	}
	e := newEnv(t, "")
	srv := realsshd.Start(t, realsshd.Options{})
	h := e.host(srv, "prod")
	tr, err := e.dialer.Dial(t.Context(), h)
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Close()
	oldFP := gossh.FingerprintSHA256(srv.HostKey)
	port := srv.Port
	srv.Stop()

	// Another machine appears on the same address.
	srv2 := realsshd.Start(t, realsshd.Options{Port: port})
	srv2.AuthorizeKey(t, srv.ClientKey.PublicKey())
	_, err = e.dialer.Dial(t.Context(), h)
	changed, ok := errors.AsType[*transport.HostKeyChangedError](err)
	if !ok {
		t.Fatalf("err = %v, want HostKeyChangedError", err)
	}
	if changed.Old != oldFP || changed.New != gossh.FingerprintSHA256(srv2.HostKey) {
		t.Errorf("fingerprints: old=%q new=%q", changed.Old, changed.New)
	}
	msg := err.Error()
	for _, want := range []string{changed.Old, changed.New, "changed"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if transport.Retryable(err) {
		t.Error("a changed host key must not be retried")
	}
}

func TestMaxSessionsOpensExtraConnection(t *testing.T) {
	if !realsshd.Available() {
		t.Skip("no sshd")
	}
	e := newEnv(t, "")
	srv := realsshd.Start(t, realsshd.Options{MaxSessions: 2})
	s := session.New(session.Options{Host: e.host(srv, "limited"), Dialer: e.dialer})
	defer s.Close()

	var panes []*session.Pane
	for range 5 {
		p, err := s.OpenPane(transport.Size{Cols: 80, Rows: 24})
		if err != nil {
			t.Fatal(err)
		}
		panes = append(panes, p)
	}
	for i, p := range panes {
		deadline := time.Now().Add(15 * time.Second)
		for !p.Connected() {
			if time.Now().After(deadline) {
				t.Fatalf("pane %d never connected", i)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	// Every pane works.
	for i, p := range panes {
		_, _ = fmt.Fprintf(p, "echo pane-%d-ok\n", i)
		want := fmt.Sprintf("pane-%d-ok", i)
		readUntil(t, p, want, 10*time.Second)
	}
}

// readUntil reads from p until it has seen substr after the echoed command.
func readUntil(t *testing.T, p io.Reader, substr string, d time.Duration) string {
	t.Helper()
	var seen lockedBuilder
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			seen.Write(buf[:n])
			// The echo of the typed command contains the text too, so look
			// for it on a line of its own (output starts at line begin).
			if strings.Contains(strings.ReplaceAll(seen.String(), "echo "+substr, ""), substr) || err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("timed out waiting for %q; saw %q", substr, seen.String())
	}
	return seen.String()
}

func TestOutageReconnectKeepsPaneAndDropsInput(t *testing.T) {
	if !realsshd.Available() {
		t.Skip("no sshd")
	}
	e := newEnv(t, "")
	srv := realsshd.Start(t, realsshd.Options{})
	relay := nettest.StartRelay(t, srv.Addr())
	h := e.host(srv, "flaky")
	h.Addr, h.Port = "127.0.0.1", relay.PortInt(t)
	h.KeepAlive = 1
	marker := filepath.Join(t.TempDir(), "replayed")

	s := session.New(session.Options{
		Host: h, Dialer: e.dialer,
		Backoff: []time.Duration{200 * time.Millisecond},
		Notice: func(ev session.Event, _ time.Time) []byte {
			if ev == session.EventLost {
				return []byte("[[LOST]]")
			}
			return []byte("[[RESTORED]]")
		},
	})
	defer s.Close()
	p, err := s.OpenPane(transport.Size{Cols: 100, Rows: 30})
	if err != nil {
		t.Fatal(err)
	}
	waitConnected(t, p)
	_, _ = fmt.Fprint(p, "echo first-$((20+1))\n")
	readUntil(t, p, "first-21", 15*time.Second)

	// The network goes dead: bytes stop moving but nothing closes, like a
	// paused container.
	relay.Pause()
	deadline := time.Now().Add(20 * time.Second)
	for p.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("keepalive never declared the paused connection dead")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Reads must block through the outage; EOF would close the terminal.
	type res struct {
		got string
		err error
	}
	rc := make(chan res, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			sb.Write(buf[:n])
			if err != nil || strings.Contains(sb.String(), "[[RESTORED]]") {
				rc <- res{sb.String(), err}
				return
			}
		}
	}()
	// Typing during the outage goes nowhere.
	_, _ = fmt.Fprintf(p, "touch %s\n", marker)
	if p.Dropped() == 0 {
		t.Error("input during the outage was not counted as dropped")
	}
	select {
	case r := <-rc:
		t.Fatalf("Read returned during the outage: %q, %v", r.got, r.err)
	case <-time.After(500 * time.Millisecond):
	}

	// The network comes back: the old TCP connection is dead for good, new
	// ones work.
	relay.Cut()
	relay.Resume()
	select {
	case r := <-rc:
		if r.err != nil {
			t.Fatalf("Read failed after reconnecting: %v", r.err)
		}
		if !strings.Contains(r.got, "[[LOST]]") {
			t.Errorf("no connection-lost notice in %q", r.got)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("pane never reconnected")
	}

	_, _ = fmt.Fprint(p, "echo second-$((40+2))\n")
	readUntil(t, p, "second-42", 15*time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("input typed during the outage was replayed after reconnecting")
	}
	if got := s.State().State; got != session.StateConnected {
		t.Errorf("state = %v", got)
	}
}

// lockedBuilder is a strings.Builder safe for one writer and one reader.
type lockedBuilder struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *lockedBuilder) Write(p []byte) { b.mu.Lock(); b.sb.Write(p); b.mu.Unlock() }

func (b *lockedBuilder) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

func waitConnected(t *testing.T, p *session.Pane) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !p.Connected() {
		if time.Now().After(deadline) {
			t.Fatal("pane never connected")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func pemEncode(typ string, b []byte) []byte {
	var sb strings.Builder
	sb.WriteString("-----BEGIN " + typ + "-----\n")
	enc := base64.StdEncoding.EncodeToString(b)
	for len(enc) > 70 {
		sb.WriteString(enc[:70] + "\n")
		enc = enc[70:]
	}
	sb.WriteString(enc + "\n-----END " + typ + "-----\n")
	return []byte(sb.String())
}
