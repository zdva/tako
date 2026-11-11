package ssh_test

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/transport"
	tssh "github.com/wei-dong/tako/internal/transport/ssh"
)

// scriptPrompter answers prompts from fixed values and records them.
type scriptPrompter struct {
	trustKeys bool
	password  string
	remember  bool
	answers   []string // keyboard-interactive answers

	mu        sync.Mutex
	hostKeys  []transport.HostKeyRequest
	secretsAs []transport.SecretRequest
	challenge []transport.ChallengeRequest
}

func (p *scriptPrompter) ConfirmHostKey(_ context.Context, r transport.HostKeyRequest) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hostKeys = append(p.hostKeys, r)
	return p.trustKeys, nil
}

func (p *scriptPrompter) Secret(_ context.Context, r transport.SecretRequest) (transport.SecretReply, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.secretsAs = append(p.secretsAs, r)
	return transport.SecretReply{Value: []byte(p.password), Remember: p.remember}, nil
}

func (p *scriptPrompter) Challenge(_ context.Context, r transport.ChallengeRequest) ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.challenge = append(p.challenge, r)
	out := make([]string, len(r.Questions))
	copy(out, p.answers)
	return out, nil
}

func (p *scriptPrompter) secretPrompts() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.secretsAs)
}

// newDialer returns a dialer with a private known_hosts file.
func newDialer(t *testing.T, pr transport.Prompter, store secrets.Store) *tssh.Dialer {
	t.Helper()
	return &tssh.Dialer{
		KnownHosts:  tssh.NewKnownHosts(filepath.Join(t.TempDir(), "known_hosts")),
		Secrets:     store,
		Prompter:    pr,
		DialTimeout: 5 * time.Second,
	}
}

// hostAt builds a host for a "host:port" address.
func hostAt(t *testing.T, addr, user string, auth ...model.Auth) model.Host {
	t.Helper()
	h, ps, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(ps)
	if err != nil {
		t.Fatal(err)
	}
	return model.Host{ID: model.NewID(), Name: "test", Addr: h, Port: port, User: user, Auth: auth}
}

func pwAuth(ref string) model.Auth { return model.Auth{Kind: model.AuthPassword, SecretRef: ref} }

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
