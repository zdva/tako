package ssh_test

import (
	"net"
	"os"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/testutil/memssh"
	"github.com/wei-dong/tako/internal/testutil/nettest"
	"github.com/wei-dong/tako/internal/transport"
)

func pubKeys(a *nettest.Agent) []gossh.PublicKey { return []gossh.PublicKey{a.Signer.PublicKey()} }

func TestDial_AgentWithNoSocketFailsAuth(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	srv := startServer(t, memssh.Options{})
	d := newDialer(t, &scriptPrompter{trustKeys: true}, nil)
	h := hostAt(t, srv.Addr(), "tako", model.Auth{Kind: model.AuthAgent})
	_, err := d.Dial(t.Context(), h)
	if err == nil || transport.Retryable(err) {
		t.Fatalf("err = %v, want a non-retryable failure", err)
	}
}

func TestDial_AgentFromSocketEnvironment(t *testing.T) {
	agent := nettest.StartAgent(t)
	t.Setenv("SSH_AUTH_SOCK", agent.Socket)
	srv := startServer(t, memssh.Options{AuthorizedKeys: pubKeys(agent)})
	d := newDialer(t, &scriptPrompter{trustKeys: true}, nil)
	tr, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", model.Auth{Kind: model.AuthAgent}))
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Close()
}

func TestDial_AgentFromInjectedConnection(t *testing.T) {
	t.Parallel()
	agent := nettest.StartAgent(t)
	srv := startServer(t, memssh.Options{AuthorizedKeys: pubKeys(agent)})
	d := newDialer(t, &scriptPrompter{trustKeys: true}, nil)
	d.AgentConn = func() (net.Conn, error) { return net.Dial("unix", agent.Socket) }
	tr, err := d.Dial(t.Context(), hostAt(t, srv.Addr(), "tako", model.Auth{Kind: model.AuthAgent}))
	if err != nil {
		t.Fatal(err)
	}
	_ = tr.Close()
}

func TestKnownHosts_ForgetRemovesOnlyThatHost(t *testing.T) {
	t.Parallel()
	srvA, srvB := startServer(t, memssh.Options{}), startServer(t, memssh.Options{})
	d := newDialer(t, &scriptPrompter{trustKeys: true, password: "tako"}, nil)
	for _, s := range []*memssh.Server{srvA, srvB} {
		tr, err := d.Dial(t.Context(), hostAt(t, s.Addr(), "tako", pwAuth("")))
		if err != nil {
			t.Fatal(err)
		}
		_ = tr.Close()
	}
	before, err := os.ReadFile(d.KnownHosts.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := d.KnownHosts.Forget(srvA.Addr()); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(d.KnownHosts.Path())
	if len(after) == 0 || len(after) >= len(before) {
		t.Fatalf("known_hosts shrank from %d to %d bytes, want one line removed", len(before), len(after))
	}
	if err := d.KnownHosts.Forget("nobody.example.com:22"); err != nil {
		t.Fatalf("forgetting an unknown host: %v", err)
	}
}
