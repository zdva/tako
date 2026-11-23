package ssh_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/secrets"
	"github.com/zdva/tako/internal/testutil/memssh"
	"github.com/zdva/tako/internal/testutil/nettest"
	"github.com/zdva/tako/internal/transport"
)

func startServer(t *testing.T, opts memssh.Options) *memssh.Server {
	t.Helper()
	s, err := memssh.Start(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s
}

func TestDial_PasswordFromStore(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{Password: "pw1"})
	store := secrets.NewMemory()
	_ = store.Set(t.Context(), "ref", []byte("pw1"))
	pr := &scriptPrompter{trustKeys: true}
	d := newDialer(t, pr, store)

	tr, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", pwAuth("ref")))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = tr.Close() }()
	if pr.secretPrompts() != 0 {
		t.Error("asked for a password that was in the store")
	}
	out, err := tr.Exec(t.Context(), "echo hi")
	if err != nil || !strings.Contains(string(out), "hi") {
		t.Errorf("Exec = %q, %v", out, err)
	}
}

func TestDial_PasswordPromptedAndRemembered(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{Password: "pw1"})
	store := secrets.NewMemory()
	pr := &scriptPrompter{trustKeys: true, password: "pw1", remember: true}
	d := newDialer(t, pr, store)
	var gotRef string
	d.Remembered = func(_ model.Host, idx int, ref string) {
		if idx != 0 {
			t.Errorf("auth index = %d", idx)
		}
		gotRef = ref
	}
	h := hostAt(t, srv.Addr(), "tako", pwAuth(""))
	tr, err := d.Dial(t.Context(), h)
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Close()
	if gotRef == "" {
		t.Fatal("Remembered was not called")
	}
	if v, err := store.Get(t.Context(), gotRef); err != nil || string(v) != "pw1" {
		t.Errorf("stored secret = %q, %v", v, err)
	}
	if gotRef != model.SecretRef(h.ID, "password") {
		t.Errorf("ref = %q", gotRef)
	}
}

func TestDial_WrongPasswordIsNotRetryable(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{Password: "right"})
	pr := &scriptPrompter{trustKeys: true, password: "wrong"}
	d := newDialer(t, pr, nil)
	_, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", pwAuth("")))
	if !errors.Is(err, transport.ErrAuthFailed) {
		t.Fatalf("err = %v, want ErrAuthFailed", err)
	}
	if transport.Retryable(err) {
		t.Error("an auth failure must not be retried")
	}
}

func TestDial_UserCancelsPrompt(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{})
	d := newDialer(t, nil, nil) // DenyPrompter
	_, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", pwAuth("")))
	if err == nil {
		t.Fatal("expected an error")
	}
	if transport.Retryable(err) {
		t.Errorf("err %v should not be retryable", err)
	}
}

func TestDial_HostKeyUnknownAskedOnceThenTrusted(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{})
	pr := &scriptPrompter{trustKeys: true, password: "tako"}
	d := newDialer(t, pr, nil)
	h := hostAt(t, srv.Addr(), "tako", pwAuth(""))

	for range 2 {
		tr, err := d.Dial(t.Context(), h)
		if err != nil {
			t.Fatal(err)
		}
		_ = tr.Close()
	}
	if len(pr.hostKeys) != 1 {
		t.Fatalf("host key prompts = %d, want 1", len(pr.hostKeys))
	}
	if want := gossh.FingerprintSHA256(srv.HostKey()); pr.hostKeys[0].Fingerprint != want {
		t.Errorf("fingerprint = %q, want %q", pr.hostKeys[0].Fingerprint, want)
	}
	data, err := os.ReadFile(d.KnownHosts.Path())
	if err != nil || len(data) == 0 {
		t.Fatalf("known_hosts not written: %v", err)
	}
}

func TestDial_HostKeyDeclined(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{})
	d := newDialer(t, &scriptPrompter{trustKeys: false, password: "tako"}, nil)
	_, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", pwAuth("")))
	if !errors.Is(err, transport.ErrHostKeyRejected) {
		t.Fatalf("err = %v, want ErrHostKeyRejected", err)
	}
	if _, statErr := os.Stat(d.KnownHosts.Path()); statErr == nil {
		t.Error("a declined key was written to known_hosts")
	}
}

func TestDial_StrictPolicyRefusesUnknownWithoutAsking(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{})
	pr := &scriptPrompter{trustKeys: true, password: "tako"}
	d := newDialer(t, pr, nil)
	h := hostAt(t, srv.Addr(), "tako", pwAuth(""))
	h.HostKeyPolicy = model.HostKeyStrict

	_, err := d.Dial(t.Context(), h)
	if _, ok := errors.AsType[*transport.HostKeyUnknownError](err); !ok {
		t.Fatalf("err = %v, want HostKeyUnknownError", err)
	}
	if len(pr.hostKeys) != 0 {
		t.Error("strict policy asked the user")
	}
}

func TestDial_ChangedHostKeyIsRefusedWithBothFingerprints(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{})
	addr := srv.Addr()
	pr := &scriptPrompter{trustKeys: true, password: "tako"}
	d := newDialer(t, pr, nil)
	h := hostAt(t, addr, "tako", pwAuth(""))
	tr, err := d.Dial(t.Context(), h)
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Close()
	oldFP := gossh.FingerprintSHA256(srv.HostKey())
	srv.Stop()

	// The same address now presents a different key: a possible attack.
	srv2 := startServer(t, memssh.Options{Addr: addr})
	_, err = d.Dial(t.Context(), h)
	changed, ok := errors.AsType[*transport.HostKeyChangedError](err)
	if !ok {
		t.Fatalf("err = %v, want HostKeyChangedError", err)
	}
	if !errors.Is(err, transport.ErrHostKeyRejected) || transport.Retryable(err) {
		t.Error("a changed key must be a non-retryable rejection")
	}
	if changed.Old != oldFP || changed.New != gossh.FingerprintSHA256(srv2.HostKey()) {
		t.Errorf("fingerprints old=%q new=%q", changed.Old, changed.New)
	}
	if len(pr.hostKeys) != 1 {
		t.Errorf("a changed key must not be offered for trust again; prompts = %d", len(pr.hostKeys))
	}
}

func writeKey(t *testing.T, passphrase string) (path string, pub gossh.PublicKey) {
	t.Helper()
	pubK, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var block *pem.Block
	if passphrase == "" {
		block, err = gossh.MarshalPrivateKey(priv, "")
	} else {
		block, err = gossh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "id")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	pub, err = gossh.NewPublicKey(pubK)
	if err != nil {
		t.Fatal(err)
	}
	return path, pub
}

func TestDial_PrivateKey(t *testing.T) {
	t.Parallel()
	path, pub := writeKey(t, "")
	srv := startServer(t, memssh.Options{AuthorizedKeys: []gossh.PublicKey{pub}})
	pr := &scriptPrompter{trustKeys: true}
	d := newDialer(t, pr, nil)
	tr, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", model.Auth{Kind: model.AuthKey, KeyPath: path}))
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Close()
	if pr.secretPrompts() != 0 {
		t.Error("an unencrypted key asked for a passphrase")
	}
}

func TestDial_EncryptedKeyAsksPassphrase(t *testing.T) {
	t.Parallel()
	path, pub := writeKey(t, "open sesame")
	srv := startServer(t, memssh.Options{AuthorizedKeys: []gossh.PublicKey{pub}})

	pr := &scriptPrompter{trustKeys: true, password: "open sesame"}
	d := newDialer(t, pr, nil)
	tr, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", model.Auth{Kind: model.AuthKey, KeyPath: path}))
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Close()
	if pr.secretPrompts() != 1 {
		t.Errorf("passphrase prompts = %d, want 1: %+v", pr.secretPrompts(), pr.secretsAs)
	}

	bad := &scriptPrompter{trustKeys: true, password: "nope"}
	d2 := newDialer(t, bad, nil)
	_, err = d2.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", model.Auth{Kind: model.AuthKey, KeyPath: path}))
	if err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	if bad.secretPrompts() > 3 {
		t.Errorf("asked for the passphrase %d times, want at most 3", bad.secretPrompts())
	}
}

func TestDial_MissingKeyFileFallsThroughToNextMethod(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{Password: "pw"})
	pr := &scriptPrompter{trustKeys: true, password: "pw"}
	d := newDialer(t, pr, nil)
	h := hostAt(t, srv.Addr(), "tako",
		model.Auth{Kind: model.AuthKey, KeyPath: filepath.Join(t.TempDir(), "nope")}, pwAuth(""))
	tr, err := d.Dial(t.Context(), h)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = tr.Close()
}

func TestDial_ThroughSOCKS5AndHTTPProxy(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{Password: "pw"})
	socks := nettest.StartSOCKS5(t, "", "")
	socksAuth := nettest.StartSOCKS5(t, "bob", "hunter2")
	httpP := nettest.StartHTTPProxy(t, "", "")
	httpAuth := nettest.StartHTTPProxy(t, "bob", "hunter2")

	cases := []struct {
		name  string
		proxy model.Proxy
		count func() int64
	}{
		{"socks5", model.Proxy{Kind: model.ProxySOCKS5, Addr: socks.Addr}, socks.Connections.Load},
		{"socks5 auth", model.Proxy{Kind: model.ProxySOCKS5, Addr: socksAuth.Addr, User: "bob", SecretRef: "p"}, socksAuth.Connections.Load},
		{"http", model.Proxy{Kind: model.ProxyHTTP, Addr: httpP.Addr}, httpP.Connections.Load},
		{"http auth", model.Proxy{Kind: model.ProxyHTTP, Addr: httpAuth.Addr, User: "bob", SecretRef: "p"}, httpAuth.Connections.Load},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := secrets.NewMemory()
			_ = store.Set(t.Context(), "p", []byte("hunter2"))
			d := newDialer(t, &scriptPrompter{trustKeys: true, password: "pw"}, store)
			h := hostAt(t, srv.Addr(), "tako", pwAuth(""))
			h.Proxy = &tc.proxy
			tr, err := d.Dial(t.Context(), h)
			if err != nil {
				t.Fatalf("Dial: %v", err)
			}
			_ = tr.Close()
			if tc.count() != 1 {
				t.Errorf("proxy served %d connections, want 1", tc.count())
			}
		})
	}
}

func TestDial_ProxyErrorsAreClassified(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{})
	httpAuth := nettest.StartHTTPProxy(t, "bob", "hunter2")
	store := secrets.NewMemory()
	_ = store.Set(t.Context(), "p", []byte("wrong"))
	d := newDialer(t, &scriptPrompter{trustKeys: true, password: "tako"}, store)
	h := hostAt(t, srv.Addr(), "tako", pwAuth(""))
	h.Proxy = &model.Proxy{Kind: model.ProxyHTTP, Addr: httpAuth.Addr, User: "bob", SecretRef: "p"}
	_, err := d.Dial(t.Context(), h)
	if !errors.Is(err, transport.ErrProxyFailed) {
		t.Errorf("rejected proxy login: err = %v, want ErrProxyFailed", err)
	}

	h.Proxy = &model.Proxy{Kind: model.ProxySOCKS5, Addr: "127.0.0.1:1"}
	_, err = d.Dial(t.Context(), h)
	if !errors.Is(err, transport.ErrProxyFailed) && !errors.Is(err, transport.ErrUnreachable) {
		t.Errorf("dead proxy: err = %v", err)
	}
	if !transport.Retryable(err) {
		t.Error("a dead proxy should be retried")
	}
}

func TestDial_UnreachableIsRetryable(t *testing.T) {
	t.Parallel()
	d := newDialer(t, nil, nil)
	_, err := d.Dial(t.Context(), hostAt(t, "127.0.0.1:1", "u", pwAuth("")))
	if !errors.Is(err, transport.ErrUnreachable) || !transport.Retryable(err) {
		t.Errorf("err = %v, want a retryable ErrUnreachable", err)
	}
}

func TestDial_ContextCancel(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{})
	d := newDialer(t, &scriptPrompter{trustKeys: true, password: "tako"}, nil)
	cctx, stop := context.WithCancel(t.Context())
	stop()
	_, err := d.Dial(cctx, hostAt(t, srv.Addr(), "tako", pwAuth("")))
	if err == nil || transport.Retryable(err) {
		t.Errorf("err = %v, want a non-retryable cancellation", err)
	}
}

func TestTransport_ShellSizeAndKeepAlive(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{Password: "pw"})
	d := newDialer(t, &scriptPrompter{trustKeys: true, password: "pw"}, nil)
	h := hostAt(t, srv.Addr(), "tako", pwAuth(""))
	h.KeepAlive = 1
	tr, err := d.Dial(t.Context(), h)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.Close() }()
	sh, err := tr.OpenShell(t.Context(), transport.Size{Cols: 91, Rows: 33})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sh.Close() }()
	if err := sh.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	if _, err := sh.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { return strings.Contains(string(srv.Typed()), "abc") })
	waitUntil(t, func() bool {
		s := srv.Sizes()
		return len(s) >= 2 && s[0] == [2]int{91, 33} && s[len(s)-1] == [2]int{120, 40}
	})
}

func TestTransport_DoneClosesWhenConnectionDrops(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{Password: "pw"})
	d := newDialer(t, &scriptPrompter{trustKeys: true, password: "pw"}, nil)
	tr, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", pwAuth("")))
	if err != nil {
		t.Fatal(err)
	}
	sh, err := tr.OpenShell(t.Context(), transport.Size{Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	srv.DropConnections()
	select {
	case <-tr.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("Done never closed after the connection dropped")
	}
	if tr.Err() == nil {
		t.Error("Err is nil after a drop")
	}
	buf := make([]byte, 256)
	for { // drain whatever was buffered; the read must then fail
		if _, err := sh.Read(buf); err != nil {
			break
		}
	}
}

func TestTransport_ChannelLimit(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{Password: "pw", MaxSessions: 2})
	d := newDialer(t, &scriptPrompter{trustKeys: true, password: "pw"}, nil)
	tr, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", pwAuth("")))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tr.Close() }()
	for i := range 2 {
		sh, err := tr.OpenShell(t.Context(), transport.Size{Cols: 80, Rows: 24})
		if err != nil {
			t.Fatalf("shell %d: %v", i, err)
		}
		defer func() { _ = sh.Close() }()
	}
	_, err = tr.OpenShell(t.Context(), transport.Size{Cols: 80, Rows: 24})
	if !errors.Is(err, transport.ErrChannelLimit) {
		t.Fatalf("third shell: err = %v, want ErrChannelLimit", err)
	}
	// A refused channel must not take the connection down.
	select {
	case <-tr.Done():
		t.Fatal("connection died after a refused channel")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDial_KeyboardInteractive(t *testing.T) {
	t.Parallel()
	srv := startServer(t, memssh.Options{Password: "otp-123"})
	pr := &scriptPrompter{trustKeys: true, answers: []string{"otp-123"}}
	d := newDialer(t, pr, nil)
	h := hostAt(t, srv.Addr(), "tako", model.Auth{Kind: model.AuthKeyboardInteractive})
	tr, err := d.Dial(t.Context(), h)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	_ = tr.Close()
	if len(pr.challenge) != 1 || len(pr.challenge[0].Questions) != 1 || pr.challenge[0].Questions[0] != "Password: " {
		t.Errorf("challenges = %+v", pr.challenge)
	}

	wrong := &scriptPrompter{trustKeys: true, answers: []string{"nope"}}
	_, err = newDialer(t, wrong, nil).Dial(t.Context(), h)
	if !errors.Is(err, transport.ErrAuthFailed) {
		t.Errorf("wrong answer: err = %v, want ErrAuthFailed", err)
	}
}
