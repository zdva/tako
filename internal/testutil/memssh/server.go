// Package memssh is a small in-process SSH server for fast, hermetic tests.
//
// Its "shell" is a tiny scripted line echo, not a real shell, so tests can
// prove exactly which bytes reached which machine and which window sizes were
// requested. It authenticates one password and optional public keys, can drop
// every connection (as a network failure would) and can refuse extra sessions
// (like OpenSSH's MaxSessions). It is for loopback use only.
package memssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Options configure a server.
type Options struct {
	// Name labels the machine in its prompt.
	Name string
	// Addr is the listen address; "127.0.0.1:0" when empty.
	Addr string
	// User and Password are accepted ("tako"/"tako" when empty).
	User, Password string
	// AuthorizedKeys are also accepted for public key login.
	AuthorizedKeys []ssh.PublicKey
	// HostKey is the identity; a fresh Ed25519 key when nil.
	HostKey ssh.Signer
	// MaxSessions limits concurrent session channels per connection; zero is
	// unlimited.
	MaxSessions int
}

// Server is a running server.
type Server struct {
	opts Options
	cfg  *ssh.ServerConfig
	ln   net.Listener
	addr string

	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	typed  bytes.Buffer
	logins int
	sizes  [][2]int
}

// Start listens and serves until ctx ends or Stop is called.
func Start(ctx context.Context, opts Options) (*Server, error) {
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:0"
	}
	if opts.User == "" {
		opts.User = "tako"
	}
	if opts.Password == "" {
		opts.Password = "tako"
	}
	if opts.Name == "" {
		opts.Name = "host"
	}
	if opts.HostKey == nil {
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generating host key: %w", err)
		}
		if opts.HostKey, err = ssh.NewSignerFromKey(priv); err != nil {
			return nil, fmt.Errorf("wrapping host key: %w", err)
		}
	}
	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("listening on %q: %w", opts.Addr, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{opts: opts, ln: ln, addr: ln.Addr().String(), cancel: cancel, conns: map[net.Conn]struct{}{}}
	s.cfg = s.serverConfig()
	s.cfg.AddHostKey(opts.HostKey)
	s.wg.Go(func() { s.acceptLoop(ctx) })
	s.wg.Go(func() {
		<-ctx.Done()
		_ = s.ln.Close()
		s.DropConnections()
	})
	return s, nil
}

func (s *Server) serverConfig() *ssh.ServerConfig {
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if c.User() == s.opts.User && string(pw) == s.opts.Password {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
	}
	cfg.KeyboardInteractiveCallback = func(c ssh.ConnMetadata, ask ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
		answers, err := ask(c.User(), "", []string{"Password: "}, []bool{false})
		if err != nil {
			return nil, fmt.Errorf("asking for the password: %w", err)
		}
		if c.User() == s.opts.User && len(answers) == 1 && answers[0] == s.opts.Password {
			return nil, nil
		}
		return nil, errors.New("invalid credentials")
	}
	if len(s.opts.AuthorizedKeys) > 0 {
		cfg.PublicKeyCallback = func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			for _, ok := range s.opts.AuthorizedKeys {
				if c.User() == s.opts.User && bytes.Equal(ok.Marshal(), key.Marshal()) {
					return nil, nil
				}
			}
			return nil, errors.New("unknown public key")
		}
	}
	return cfg
}

// Addr returns the listen address.
func (s *Server) Addr() string { return s.addr }

// HostKey returns the public host key.
func (s *Server) HostKey() ssh.PublicKey { return s.opts.HostKey.PublicKey() }

// HostSigner returns the identity, to restart the server as the same machine.
func (s *Server) HostSigner() ssh.Signer { return s.opts.HostKey }

// Typed returns every byte the shells received.
func (s *Server) Typed() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.typed.Bytes())
}

// Logins returns how many connections authenticated.
func (s *Server) Logins() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.logins
}

// Sizes returns the PTY sizes requested so far (cols, rows), in order.
func (s *Server) Sizes() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]int(nil), s.sizes...)
}

// DropConnections closes every client connection but keeps listening.
func (s *Server) DropConnections() {
	s.mu.Lock()
	conns := make([]net.Conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Stop closes the listener and every connection and waits for the goroutines.
func (s *Server) Stop() {
	s.cancel()
	s.wg.Wait()
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Go(func() {
			s.serveConn(ctx, conn)
			s.mu.Lock()
			delete(s.conns, conn)
			s.mu.Unlock()
		})
	}
}

func (s *Server) serveConn(ctx context.Context, nc net.Conn) {
	defer func() { _ = nc.Close() }()
	conn, chans, reqs, err := ssh.NewServerConn(nc, s.cfg)
	if err != nil {
		return
	}
	defer func() { _ = conn.Close() }()
	s.mu.Lock()
	s.logins++
	s.mu.Unlock()
	go replyRequests(reqs)

	var wg sync.WaitGroup
	defer wg.Wait()
	active := 0
	var amu sync.Mutex
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		amu.Lock()
		if s.opts.MaxSessions > 0 && active >= s.opts.MaxSessions {
			amu.Unlock()
			_ = newCh.Reject(ssh.ResourceShortage, "administratively prohibited (open failed)")
			continue
		}
		active++
		amu.Unlock()
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			amu.Lock()
			active--
			amu.Unlock()
			continue
		}
		wg.Go(func() {
			s.serveSession(ctx, ch, chReqs)
			amu.Lock()
			active--
			amu.Unlock()
		})
	}
}

// replyRequests answers global requests: keepalives succeed, others fail.
func replyRequests(reqs <-chan *ssh.Request) {
	for r := range reqs {
		_ = r.Reply(r.Type == "keepalive@openssh.com", nil)
	}
}

type ptyRequest struct {
	Term                 string
	Cols, Rows, PxW, PxH uint32
	Modes                string
}

type windowChange struct {
	Cols, Rows, PxW, PxH uint32
}

func (s *Server) recordSize(cols, rows uint32) {
	s.mu.Lock()
	s.sizes = append(s.sizes, [2]int{int(cols), int(rows)})
	s.mu.Unlock()
}

func (s *Server) serveSession(ctx context.Context, ch ssh.Channel, reqs <-chan *ssh.Request) {
	defer func() { _ = ch.Close() }()
	done := make(chan struct{})
	started := false
	for {
		select {
		case req, ok := <-reqs:
			if !ok {
				return
			}
			switch req.Type {
			case "pty-req":
				var p ptyRequest
				if err := ssh.Unmarshal(req.Payload, &p); err != nil {
					_ = req.Reply(false, nil)
					continue
				}
				s.recordSize(p.Cols, p.Rows)
				_ = req.Reply(true, nil)
			case "env":
				_ = req.Reply(true, nil)
			case "window-change":
				var w windowChange
				if err := ssh.Unmarshal(req.Payload, &w); err == nil {
					s.recordSize(w.Cols, w.Rows)
				}
			case "shell":
				_ = req.Reply(true, nil)
				started = true
				go func() {
					defer close(done)
					s.echoShell(ctx, ch)
				}()
			case "exec":
				var e struct{ Command string }
				_ = ssh.Unmarshal(req.Payload, &e)
				_ = req.Reply(true, nil)
				_, _ = io.WriteString(ch, "ran: "+e.Command+"\n")
				_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, 0})
				return
			default:
				_ = req.Reply(false, nil)
			}
		case <-done:
			return
		case <-ctx.Done():
			if started {
				<-done
			}
			return
		}
	}
}

// echoShell prints a prompt and echoes what is typed, recording every byte.
func (s *Server) echoShell(ctx context.Context, ch ssh.Channel) {
	prompt := "[" + s.opts.Name + "] $ "
	_, _ = io.WriteString(ch, prompt)
	buf := make([]byte, 4096)
	for ctx.Err() == nil {
		n, err := ch.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.typed.Write(buf[:n])
			s.mu.Unlock()
			out := bytes.ReplaceAll(buf[:n], []byte("\r"), []byte("\r\n"+prompt))
			_, _ = ch.Write(out)
		}
		if err != nil {
			return
		}
	}
}
