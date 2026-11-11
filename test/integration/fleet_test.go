//go:build integration && linux

package integration

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/testutil/realsshd"
	"github.com/wei-dong/tako/internal/transfer"
	"github.com/wei-dong/tako/internal/transport"
)

// fleet is a set of live hosts as the transfer queue sees them: it connects
// on demand, keeps one SFTP channel per host and redials after a failure.
type fleet struct {
	t *testing.T
	e *env

	mu    sync.Mutex
	conns map[model.ID]*fleetConn
	dials map[model.ID]int
}

type fleetConn struct {
	tr transport.Transport
	ep transfer.Endpoint
}

func newFleet(t *testing.T, e *env) *fleet {
	f := &fleet{t: t, e: e, conns: map[model.ID]*fleetConn{}, dials: map[model.ID]int{}}
	t.Cleanup(func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, c := range f.conns {
			_ = c.tr.Close()
		}
	})
	return f
}

func (f *fleet) Endpoint(ctx context.Context, l transfer.Location) (transfer.Endpoint, error) {
	if l.Local() {
		return transfer.Local(), nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if c := f.conns[l.Host]; c != nil {
		select {
		case <-c.tr.Done():
			delete(f.conns, l.Host)
		default:
			return c.ep, nil
		}
	}
	h, ok := f.hostLocked(l.Host)
	if !ok {
		return nil, errors.New("unknown host")
	}
	tr, err := f.e.dialer.Dial(ctx, h)
	if err != nil {
		return nil, err
	}
	fs, err := tr.SFTP(ctx)
	if err != nil {
		_ = tr.Close()
		return nil, err
	}
	c := &fleetConn{tr: tr, ep: transfer.Remote(fs, tr.Exec)}
	f.conns[l.Host] = c
	f.dials[l.Host]++
	return c.ep, nil
}

func (f *fleet) Reset(l transfer.Location) {
	f.mu.Lock()
	c := f.conns[l.Host]
	delete(f.conns, l.Host)
	f.mu.Unlock()
	if c != nil {
		_ = c.tr.Close()
	}
}

func (f *fleet) hostLocked(id model.ID) (model.Host, bool) {
	f.e.mu.Lock()
	defer f.e.mu.Unlock()
	h, ok := f.e.hosts[id]
	return h, ok
}

func (f *fleet) transport(ctx context.Context, id model.ID) (transport.Transport, error) {
	if _, err := f.Endpoint(ctx, transfer.Location{Host: id}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conns[id].tr, nil
}

// DirectEnv.

func (f *fleet) Host(id model.ID) (model.Host, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hostLocked(id)
}

func (f *fleet) HostKeyLines(h model.Host) []string {
	return f.e.dialer.KnownHosts.Lines(h.Addr, h.Port)
}

func (f *fleet) Exec(ctx context.Context, id model.ID, cmd string) ([]byte, error) {
	tr, err := f.transport(ctx, id)
	if err != nil {
		return nil, err
	}
	return tr.Exec(ctx, cmd) //nolint:wrapcheck // test helper
}

// ExecAgent runs the command on a connection of its own that lives for that
// command only: sshd allows one forwarded agent per connection, and closing
// the connection is what ends the forwarding.
func (f *fleet) ExecAgent(ctx context.Context, id model.ID, cmd string) ([]byte, error) {
	h, ok := f.Host(id)
	if !ok {
		return nil, errors.New("unknown host")
	}
	tr, err := f.e.dialer.Dial(ctx, h)
	if err != nil {
		return nil, err //nolint:wrapcheck // test helper
	}
	defer func() { _ = tr.Close() }()
	ae, ok := tr.(transport.AgentExecer)
	if !ok {
		return nil, errors.New("no agent forwarding")
	}
	return ae.ExecAgent(ctx, cmd, f.e.agent.Dial) //nolint:wrapcheck // test helper
}

var _ transfer.DirectEnv = (*fleet)(nil)

// twoHosts starts two sshd servers, authorizes their client keys in the
// environment's agent and returns the hosts.
func twoHosts(t *testing.T) (*env, *fleet, model.Host, model.Host) {
	t.Helper()
	if !realsshd.Available() {
		t.Skip("no sshd")
	}
	e := newEnv(t, "")
	a, b := realsshd.Start(t, realsshd.Options{}), realsshd.Start(t, realsshd.Options{})
	for _, s := range []*realsshd.Server{a, b} {
		pem, err := os.ReadFile(s.ClientKeyPath)
		if err != nil {
			t.Fatal(err)
		}
		e.agent.AddKey(t, pem)
	}
	return e, newFleet(t, e), e.host(a, "web-01"), e.host(b, "db-01")
}
