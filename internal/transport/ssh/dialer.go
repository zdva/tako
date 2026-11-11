package ssh

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/transport"
)

// DefaultDialTimeout bounds one TCP connect plus SSH handshake.
const DefaultDialTimeout = 15 * time.Second

// Dialer implements [transport.Dialer] over SSH.
type Dialer struct {
	// KnownHosts verifies server keys. Required.
	KnownHosts *KnownHosts
	// Secrets resolves keychain references. May be nil: every secret is then
	// asked for.
	Secrets secrets.Store
	// Prompter asks the user. nil means [transport.DenyPrompter].
	Prompter transport.Prompter
	// Lookup finds jump hosts by ID. Required for hosts that have a jump chain.
	Lookup func(model.ID) (model.Host, bool)
	// Remembered is told when the user chose to remember a secret, with the
	// host, the index of the Auth entry and the keychain reference now in use,
	// so the caller can store the reference in the host.
	Remembered func(h model.Host, authIndex int, ref string)
	// DialTimeout bounds one connect plus handshake; zero means
	// [DefaultDialTimeout].
	DialTimeout time.Duration
	// KeepAliveTimeout is how long a keepalive may go unanswered before the
	// connection counts as dead; zero means 15 seconds.
	KeepAliveTimeout time.Duration
	// AgentConn opens a connection to the SSH agent; nil uses SSH_AUTH_SOCK.
	AgentConn func() (net.Conn, error)
}

var _ transport.Dialer = (*Dialer)(nil)

// Dial connects to h through its proxy and jump hosts, authenticates, and
// returns the live transport.
func (d *Dialer) Dial(ctx context.Context, h model.Host) (transport.Transport, error) {
	if err := h.Validate(); err != nil {
		return nil, fmt.Errorf("dialing %s: %w", h.Name, err)
	}
	if d.KnownHosts == nil {
		return nil, errors.New("dialing: no known_hosts store configured")
	}
	prompter := d.Prompter
	if prompter == nil {
		prompter = transport.DenyPrompter{}
	}
	hops, err := d.path(h)
	if err != nil {
		return nil, err
	}
	t := &Transport{done: make(chan struct{}), kaTimeout: cmp.Or(d.KeepAliveTimeout, keepaliveTimeout)}
	var prev *gossh.Client
	for i, hop := range hops {
		client, err := d.connect(ctx, t, prev, h, hop, prompter)
		if err != nil {
			t.closeResources()
			if i < len(hops)-1 && !errors.Is(err, transport.ErrAuthFailed) &&
				!errors.Is(err, transport.ErrHostKeyRejected) {
				return nil, fmt.Errorf("jump host %s: %w: %w", hop.Name, transport.ErrProxyFailed, err)
			}
			return nil, fmt.Errorf("connecting to %s: %w", hop.Name, err)
		}
		t.clients = append(t.clients, client)
		prev = client
	}
	t.start(h.KeepAliveInterval())
	return t, nil
}

// path returns the jump hosts followed by the target.
func (d *Dialer) path(h model.Host) ([]model.Host, error) {
	if len(h.Jump) == 0 {
		return []model.Host{h}, nil
	}
	if d.Lookup == nil {
		return nil, fmt.Errorf("host %s has jump hosts but no lookup is configured", h.Name)
	}
	jumps, err := model.ResolveJumps(h, d.Lookup)
	if err != nil {
		return nil, fmt.Errorf("resolving jump hosts of %s: %w", h.Name, err)
	}
	return append(jumps, h), nil
}

// connect establishes one hop: over a new TCP connection for the first hop,
// through prev for the others.
func (d *Dialer) connect(
	ctx context.Context, t *Transport, prev *gossh.Client, target, hop model.Host, prompter transport.Prompter,
) (*gossh.Client, error) {
	timeout := cmp.Or(d.DialTimeout, DefaultDialTimeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var conn net.Conn
	var err error
	if prev == nil {
		conn, err = d.firstConn(ctx, target, hop, timeout)
	} else {
		conn, err = dialThrough(ctx, prev, hop.HostPort())
	}
	if err != nil {
		return nil, err
	}

	auths := &authSet{
		ctx: ctx, host: hop, secrets: d.Secrets, prompter: prompter,
		remember: d.Remembered, agentSrc: d.AgentConn,
	}
	t.addResource(auths)
	hostport := hop.HostPort()
	cfg := &gossh.ClientConfig{
		User:              hop.User,
		Auth:              auths.methods(),
		HostKeyCallback:   d.KnownHosts.callback(ctx, hop, prompter),
		HostKeyAlgorithms: d.KnownHosts.algorithms(hostport),
		ClientVersion:     "SSH-2.0-Tako",
	}
	// ssh.NewClientConn has no context; closing the conn aborts it.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	sc, chans, reqs, err := gossh.NewClientConn(conn, hostport, cfg)
	stopped := !stop()
	if err != nil {
		_ = conn.Close()
		if stopped || ctx.Err() != nil {
			return nil, fmt.Errorf("%w: %w", transport.ErrUnreachable, ctxCause(ctx, err))
		}
		return nil, classify(err)
	}
	return gossh.NewClient(sc, chans, reqs), nil
}

func (d *Dialer) firstConn(ctx context.Context, target, hop model.Host, timeout time.Duration) (net.Conn, error) {
	var proxyPass []byte
	if p := target.Proxy; p != nil && p.SecretRef != "" && d.Secrets != nil {
		proxyPass, _ = d.Secrets.Get(ctx, p.SecretRef)
	}
	return dialTCP(ctx, hop.HostPort(), target.Proxy, proxyPass, timeout)
}

// dialThrough opens a TCP connection to addr from the far end of client.
func dialThrough(ctx context.Context, client *gossh.Client, addr string) (net.Conn, error) {
	type result struct {
		c   net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := client.Dial("tcp", addr)
		ch <- result{c, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("%w: forwarding to %s: %w", transport.ErrProxyFailed, addr, r.err)
		}
		return r.c, nil
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.c != nil {
				_ = r.c.Close()
			}
		}()
		return nil, fmt.Errorf("%w: %w", transport.ErrUnreachable, ctx.Err())
	}
}

// classify maps an error from the ssh handshake onto the transport sentinels.
func classify(err error) error {
	switch {
	case errors.Is(err, transport.ErrHostKeyRejected), errors.Is(err, transport.ErrCanceled):
		return err
	case isAuthError(err):
		return fmt.Errorf("%w: %w", transport.ErrAuthFailed, err)
	default:
		return fmt.Errorf("%w: %w", transport.ErrUnreachable, err)
	}
}

func isAuthError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods remain")
}

func ctxCause(ctx context.Context, fallback error) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return fallback
}
