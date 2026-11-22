package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/transfer"
	"github.com/zdva/tako/internal/transport"
	tssh "github.com/zdva/tako/internal/transport/ssh"
)

// fleet gives the transfer queue and the file browser a connection of their
// own to each host: one SSH connection with one SFTP channel, dialed on first
// use and again after a failure. File transfers therefore neither compete
// with the terminals for channels nor die when a terminal reconnects.
type fleet struct {
	dialer    *tssh.Dialer
	lookup    func(model.ID) (model.Host, bool)
	agentConn func() (net.Conn, error)

	mu    sync.Mutex
	conns map[model.ID]*fleetConn
	// dialing serializes dials per host so a burst of jobs connects once.
	dialing map[model.ID]*sync.Mutex
}

type fleetConn struct {
	tr transport.Transport
	ep transfer.Endpoint
}

var (
	_ transfer.Provider  = (*fleet)(nil)
	_ transfer.DirectEnv = (*fleet)(nil)
)

func newFleet(d *tssh.Dialer, lookup func(model.ID) (model.Host, bool), agent func() (net.Conn, error)) *fleet {
	if agent == nil {
		agent = defaultAgentConn
	}
	return &fleet{dialer: d, lookup: lookup, agentConn: agent, conns: map[model.ID]*fleetConn{}, dialing: map[model.ID]*sync.Mutex{}}
}

func defaultAgentConn() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK is not set")
	}
	var d net.Dialer
	c, err := d.DialContext(context.Background(), "unix", sock)
	if err != nil {
		return nil, fmt.Errorf("dialing the SSH agent: %w", err)
	}
	return c, nil
}

// Endpoint implements [transfer.Provider].
func (f *fleet) Endpoint(ctx context.Context, l transfer.Location) (transfer.Endpoint, error) {
	if l.Local() {
		return transfer.Local(), nil
	}
	c, err := f.conn(ctx, l.Host)
	if err != nil {
		return nil, err
	}
	return c.ep, nil
}

func (f *fleet) conn(ctx context.Context, id model.ID) (*fleetConn, error) {
	f.mu.Lock()
	gate := f.dialing[id]
	if gate == nil {
		gate = &sync.Mutex{}
		f.dialing[id] = gate
	}
	f.mu.Unlock()
	gate.Lock()
	defer gate.Unlock()

	f.mu.Lock()
	if c := f.conns[id]; c != nil {
		select {
		case <-c.tr.Done():
			delete(f.conns, id)
		default:
			f.mu.Unlock()
			return c, nil
		}
	}
	f.mu.Unlock()

	h, ok := f.lookup(id)
	if !ok {
		return nil, fmt.Errorf("host %s: %w", id, errUnknownHost)
	}
	tr, err := f.dialer.Dial(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", h.Name, err)
	}
	fs, err := tr.SFTP(ctx)
	if err != nil {
		_ = tr.Close()
		return nil, fmt.Errorf("starting file transfer on %s: %w", h.Name, err)
	}
	c := &fleetConn{tr: tr, ep: transfer.Remote(fs, tr.Exec)}
	f.mu.Lock()
	f.conns[id] = c
	f.mu.Unlock()
	return c, nil
}

var errUnknownHost = errors.New("unknown host")

// Reset implements [transfer.Provider].
func (f *fleet) Reset(l transfer.Location) {
	f.mu.Lock()
	c := f.conns[l.Host]
	delete(f.conns, l.Host)
	f.mu.Unlock()
	if c != nil {
		_ = c.tr.Close()
	}
}

// Host implements [transfer.DirectEnv].
func (f *fleet) Host(id model.ID) (model.Host, bool) { return f.lookup(id) }

// HostKeyLines implements [transfer.DirectEnv].
func (f *fleet) HostKeyLines(h model.Host) []string {
	port := h.Port
	if port == 0 {
		port = 22
	}
	return f.dialer.KnownHosts.Lines(h.Addr, port)
}

// Exec implements [transfer.DirectEnv].
func (f *fleet) Exec(ctx context.Context, id model.ID, cmd string) ([]byte, error) {
	c, err := f.conn(ctx, id)
	if err != nil {
		return nil, err
	}
	return c.tr.Exec(ctx, cmd) //nolint:wrapcheck // the transport's error already names the failure
}

// ExecAgent implements [transfer.DirectEnv]. It runs the command on a
// connection of its own that lives for that command only: an SSH server
// accepts one forwarded agent per connection, and closing the connection is
// what ends the forwarding.
func (f *fleet) ExecAgent(ctx context.Context, id model.ID, cmd string) ([]byte, error) {
	h, ok := f.lookup(id)
	if !ok {
		return nil, fmt.Errorf("host %s: %w", id, errUnknownHost)
	}
	tr, err := f.dialer.Dial(ctx, h)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", h.Name, err)
	}
	defer func() { _ = tr.Close() }()
	ae, ok := tr.(transport.AgentExecer)
	if !ok {
		return nil, errors.New("this connection cannot forward the SSH agent")
	}
	return ae.ExecAgent(ctx, cmd, f.agentConn) //nolint:wrapcheck // the transport's error already names the failure
}

func (f *fleet) close() {
	f.mu.Lock()
	conns := f.conns
	f.conns = map[model.ID]*fleetConn{}
	f.mu.Unlock()
	for _, c := range conns {
		_ = c.tr.Close()
	}
}
