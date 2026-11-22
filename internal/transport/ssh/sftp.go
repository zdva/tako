package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"path"
	"sync"
	"time"

	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"

	"github.com/zdva/tako/internal/transport"
)

const (
	// sftpPacket is the largest SFTP data packet asked for; OpenSSH accepts
	// up to 256 KiB.
	sftpPacket = 128 << 10
	// sftpInflight is how many requests one file keeps in flight.
	sftpInflight = 64
)

// SFTP implements [transport.Transport].
func (t *Transport) SFTP(ctx context.Context) (transport.FS, error) {
	sess, err := t.newSession(ctx)
	if err != nil {
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("opening sftp input: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("opening sftp output: %w", err)
	}
	if err := sess.RequestSubsystem("sftp"); err != nil {
		_ = sess.Close()
		return nil, classifyChannel(fmt.Errorf("starting the sftp subsystem: %w", err))
	}
	stop := context.AfterFunc(ctx, func() { _ = sess.Close() })
	c, err := sftp.NewClientPipe(stdout, stdin,
		sftp.MaxPacketUnchecked(sftpPacket),
		sftp.UseConcurrentWrites(true),
		sftp.MaxConcurrentRequestsPerFile(sftpInflight),
	)
	if !stop() && err == nil { // ctx ended while the handshake ran
		err = context.Cause(ctx)
	}
	if err != nil {
		_ = sess.Close()
		return nil, fmt.Errorf("starting sftp: %w", err)
	}
	return &remoteFS{c: c, sess: sess}, nil
}

// remoteFS is an SFTP client as a [transport.FS].
type remoteFS struct {
	c    *sftp.Client
	sess *gossh.Session
	once sync.Once
}

var _ transport.FS = (*remoteFS)(nil)

func (r *remoteFS) Open(name string) (transport.File, error) {
	f, err := r.c.Open(name)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", name, err)
	}
	return f, nil
}

func (r *remoteFS) OpenFile(name string, flag int) (transport.File, error) {
	f, err := r.c.OpenFile(name, flag)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", name, err)
	}
	return f, nil
}

func (r *remoteFS) Stat(name string) (fs.FileInfo, error) {
	fi, err := r.c.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", name, err)
	}
	return fi, nil
}

func (r *remoteFS) ReadDir(name string) ([]fs.FileInfo, error) {
	l, err := r.c.ReadDir(name)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", name, err)
	}
	return l, nil
}

func (r *remoteFS) MkdirAll(name string) error {
	if err := r.c.MkdirAll(name); err != nil {
		return fmt.Errorf("creating %s: %w", name, err)
	}
	return nil
}

func (r *remoteFS) Remove(name string) error {
	if err := r.c.Remove(name); err != nil {
		return fmt.Errorf("removing %s: %w", name, err)
	}
	return nil
}

// Rename replaces newName when it exists (POSIX rename, falling back to the
// protocol's plain rename, which refuses to).
func (r *remoteFS) Rename(oldName, newName string) error {
	err := r.c.PosixRename(oldName, newName)
	if err != nil {
		if _, serr := r.c.Stat(newName); serr == nil {
			_ = r.c.Remove(newName)
		}
		err = r.c.Rename(oldName, newName)
	}
	if err != nil {
		return fmt.Errorf("renaming %s: %w", oldName, err)
	}
	return nil
}

func (r *remoteFS) Home() (string, error) {
	p, err := r.c.Getwd()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return p, nil
}

func (r *remoteFS) Join(elem ...string) string { return path.Join(elem...) }

func (r *remoteFS) Close() error {
	var err error
	r.once.Do(func() {
		err = r.c.Close()
		_ = r.sess.Close()
	})
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("closing sftp: %w", err)
	}
	return nil
}

// Ping implements [transport.Transport]: the round trip of a keepalive.
func (t *Transport) Ping(ctx context.Context) (time.Duration, error) {
	type result struct {
		d   time.Duration
		err error
	}
	ch := make(chan result, 1)
	go func() {
		start := time.Now()
		_, _, err := t.target().SendRequest("keepalive@openssh.com", true, nil)
		ch <- result{time.Since(start), err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return 0, fmt.Errorf("pinging: %w", r.err)
		}
		return r.d, nil
	case <-ctx.Done():
		return 0, fmt.Errorf("pinging: %w", context.Cause(ctx))
	case <-t.done:
		return 0, errors.New("pinging: connection closed")
	}
}

var _ transport.AgentExecer = (*Transport)(nil)

// agentSource is where agent channels from the server are connected to; nil
// means they are refused.
type agentSource struct{ open func() (net.Conn, error) }

// ExecAgent implements [transport.AgentExecer].
func (t *Transport) ExecAgent(ctx context.Context, cmd string, agent func() (net.Conn, error)) ([]byte, error) {
	if err := t.handleAgentChannels(); err != nil {
		return nil, err
	}
	t.agent.Store(&agentSource{open: agent})
	defer t.agent.Store(nil)

	sess, err := t.newSession(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = sess.Close() })
	defer stop()
	// A pty makes the command die with the channel (SIGHUP) instead of
	// finishing a copy nobody is waiting for.
	if err := sess.RequestPty("dumb", 24, 200, gossh.TerminalModes{gossh.ECHO: 0}); err != nil {
		return nil, classifyChannel(fmt.Errorf("requesting a pty: %w", err))
	}
	ok, err := sess.SendRequest("auth-agent-req@openssh.com", true, nil)
	if err != nil || !ok {
		return nil, fmt.Errorf("the server refused agent forwarding: %w", errors.Join(err, errAgentRefused))
	}
	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("running a forwarded command: %w", context.Cause(ctx))
		}
		return out, fmt.Errorf("running a forwarded command: %w", err)
	}
	return out, nil
}

var errAgentRefused = errors.New("agent forwarding refused")

// handleAgentChannels registers, once, the handler for agent channels the
// server opens. It serves them only while an ExecAgent is running.
func (t *Transport) handleAgentChannels() error {
	var err error
	t.agentOnce.Do(func() {
		chans := t.target().HandleChannelOpen("auth-agent@openssh.com")
		if chans == nil {
			err = errors.New("agent channels are already handled")
			return
		}
		t.wg.Go(func() {
			for {
				select {
				case nc, ok := <-chans:
					if !ok {
						return
					}
					t.serveAgent(nc)
				case <-t.done:
					return
				}
			}
		})
	})
	return err
}

func (t *Transport) serveAgent(nc gossh.NewChannel) {
	src := t.agent.Load()
	if src == nil {
		_ = nc.Reject(gossh.Prohibited, "agent forwarding is not active")
		return
	}
	local, err := src.open()
	if err != nil {
		_ = nc.Reject(gossh.ConnectionFailed, "no local agent")
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		_ = local.Close()
		return
	}
	go gossh.DiscardRequests(reqs)
	go func() {
		defer func() { _ = ch.Close(); _ = local.Close() }()
		done := make(chan struct{}, 2)
		go func() { _, _ = io.Copy(local, ch); done <- struct{}{} }()
		go func() { _, _ = io.Copy(ch, local); done <- struct{}{} }()
		<-done
	}()
}
