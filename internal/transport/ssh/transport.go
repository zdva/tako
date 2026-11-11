package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/wei-dong/tako/internal/transport"
)

// keepaliveTimeout is how long a keepalive may go unanswered before the
// connection is declared dead.
const keepaliveTimeout = 15 * time.Second

// Transport is an established SSH connection, possibly through jump hosts.
type Transport struct {
	clients []*gossh.Client // jump hosts first, target last
	closers []io.Closer     // agent connections and the like

	done      chan struct{}
	closeOnce sync.Once
	mu        sync.Mutex
	err       error
	wg        sync.WaitGroup
	stopKA    chan struct{}
	kaTimeout time.Duration
}

var _ transport.Transport = (*Transport)(nil)

func (t *Transport) addResource(c io.Closer) {
	t.closers = append(t.closers, c)
}

func (t *Transport) closeResources() {
	for _, c := range t.closers {
		_ = c.Close() //nolint:errcheck // best-effort release of agent sockets and jump clients
	}
	for i := len(t.clients) - 1; i >= 0; i-- {
		_ = t.clients[i].Close() //nolint:errcheck // the connection is being torn down anyway
	}
}

func (t *Transport) target() *gossh.Client { return t.clients[len(t.clients)-1] }

// start begins watching the connection and sending keepalives.
func (t *Transport) start(keepalive time.Duration) {
	t.stopKA = make(chan struct{})
	t.wg.Go(func() {
		err := t.target().Wait()
		if err == nil {
			err = io.EOF // the server ended the session; a local Close wins the race in finish
		}
		t.finish(err)
	})
	t.wg.Go(func() { t.keepalive(keepalive) })
}

// finish records why the connection ended and releases everything.
func (t *Transport) finish(cause error) {
	t.closeOnce.Do(func() {
		t.mu.Lock()
		t.err = cause
		t.mu.Unlock()
		close(t.stopKA)
		close(t.done)
		t.closeResources()
	})
}

func (t *Transport) keepalive(interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-t.stopKA:
			return
		case <-tick.C:
		}
		reply := make(chan error, 1)
		go func() {
			_, _, err := t.target().SendRequest("keepalive@openssh.com", true, nil)
			reply <- err
		}()
		select {
		case err := <-reply:
			if err != nil {
				t.finish(fmt.Errorf("keepalive failed: %w", err))
				return
			}
		case <-time.After(t.kaTimeout):
			t.finish(errors.New("keepalive timed out"))
			return
		case <-t.stopKA:
			return
		}
	}
}

// Done implements [transport.Transport].
func (t *Transport) Done() <-chan struct{} { return t.done }

// Err implements [transport.Transport].
func (t *Transport) Err() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}

// Close implements [transport.Transport]. It waits for the transport's
// goroutines to exit.
func (t *Transport) Close() error {
	t.finish(nil)
	t.wg.Wait()
	return nil
}

// OpenShell implements [transport.Transport].
func (t *Transport) OpenShell(ctx context.Context, size transport.Size) (transport.Shell, error) {
	sess, err := t.newSession(ctx)
	if err != nil {
		return nil, err
	}
	modes := gossh.TerminalModes{gossh.ECHO: 1, gossh.TTY_OP_ISPEED: 38400, gossh.TTY_OP_OSPEED: 38400}
	if err := sess.RequestPty("xterm-256color", max(size.Rows, 1), max(size.Cols, 1), modes); err != nil {
		_ = sess.Close() //nolint:errcheck // reporting the earlier failure
		return nil, classifyChannel(fmt.Errorf("requesting a pty: %w", err))
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close() //nolint:errcheck // reporting the earlier failure
		return nil, fmt.Errorf("opening shell input: %w", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close() //nolint:errcheck // reporting the earlier failure
		return nil, fmt.Errorf("opening shell output: %w", err)
	}
	if err := sess.Shell(); err != nil {
		_ = sess.Close() //nolint:errcheck // reporting the earlier failure
		return nil, classifyChannel(fmt.Errorf("starting the shell: %w", err))
	}
	return &shell{sess: sess, in: stdin, out: stdout}, nil
}

// newSession opens a channel, giving up when ctx ends.
func (t *Transport) newSession(ctx context.Context) (*gossh.Session, error) {
	type result struct {
		s   *gossh.Session
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := t.target().NewSession()
		ch <- result{s, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, classifyChannel(fmt.Errorf("opening a session: %w", r.err))
		}
		return r.s, nil
	case <-ctx.Done():
		go func() {
			if r := <-ch; r.s != nil {
				_ = r.s.Close() //nolint:errcheck // abandoned session
			}
		}()
		return nil, fmt.Errorf("opening a session: %w", context.Cause(ctx))
	}
}

// classifyChannel maps a server's refusal to open another session channel to
// ErrChannelLimit. OpenSSH answers "connect failed" when MaxSessions is
// reached; other servers say "administratively prohibited" or "resource
// shortage". Failures after the channel opened (a refused pty, say) are not
// limits and pass through.
func classifyChannel(err error) error {
	if oce, ok := errors.AsType[*gossh.OpenChannelError](err); ok {
		switch oce.Reason {
		case gossh.ResourceShortage, gossh.Prohibited, gossh.ConnectionFailed:
			return fmt.Errorf("%w: %w", transport.ErrChannelLimit, err)
		}
	}
	return err
}

// Exec implements [transport.Transport].
func (t *Transport) Exec(ctx context.Context, cmd string) ([]byte, error) {
	sess, err := t.newSession(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()                         //nolint:errcheck // the session is finished
	stop := context.AfterFunc(ctx, func() { _ = sess.Close() }) //nolint:errcheck // aborts a running command on cancel
	defer stop()
	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		if ctx.Err() != nil {
			return out, fmt.Errorf("running %q: %w", cmd, context.Cause(ctx))
		}
		return out, fmt.Errorf("running %q: %w", cmd, err)
	}
	return out, nil
}

// shell is a PTY session as a transport.Shell.
type shell struct {
	sess *gossh.Session
	in   io.WriteCloser
	out  io.Reader
}

func (s *shell) Read(p []byte) (int, error)  { return s.out.Read(p) } //nolint:wrapcheck // io.Reader contract: callers compare io.EOF
func (s *shell) Write(p []byte) (int, error) { return s.in.Write(p) } //nolint:wrapcheck // io.Writer contract

func (s *shell) Resize(cols, rows int) error {
	if err := s.sess.WindowChange(max(rows, 1), max(cols, 1)); err != nil {
		return fmt.Errorf("resizing the remote pty: %w", err)
	}
	return nil
}

func (s *shell) Close() error {
	_ = s.in.Close() //nolint:errcheck // closing stdin; the session close below reports failure
	if err := s.sess.Close(); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("closing shell: %w", err)
	}
	return nil
}
