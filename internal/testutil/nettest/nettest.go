// Package nettest provides loopback network fakes for tests: a SOCKS5 proxy,
// an HTTP CONNECT proxy, a TCP relay whose link can be paused or cut (to
// emulate a network drop), and an in-process ssh-agent.
package nettest

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// serve accepts connections until the test ends, handling each in a goroutine
// the test waits for.
func serve(t testing.TB, ln net.Listener, handle func(net.Conn)) {
	t.Helper()
	var wg sync.WaitGroup
	var mu sync.Mutex
	open := map[net.Conn]struct{}{}
	wg.Go(func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			open[c] = struct{}{}
			mu.Unlock()
			wg.Go(func() {
				defer func() {
					mu.Lock()
					delete(open, c)
					mu.Unlock()
					_ = c.Close()
				}()
				handle(c)
			})
		}
	})
	t.Cleanup(func() {
		_ = ln.Close()
		mu.Lock()
		for c := range open {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
}

func listen(t testing.TB) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening: %v", err)
	}
	return ln
}

// SOCKS5 is a running SOCKS5 proxy.
type SOCKS5 struct {
	Addr string
	// Connections counts CONNECT requests served.
	Connections atomic.Int64
}

// StartSOCKS5 starts a no-auth SOCKS5 proxy, or one requiring user/pass
// authentication when user is not empty.
func StartSOCKS5(t testing.TB, user, pass string) *SOCKS5 {
	t.Helper()
	ln := listen(t)
	p := &SOCKS5{Addr: ln.Addr().String()}
	serve(t, ln, func(c net.Conn) { p.handle(c, user, pass) })
	return p
}

func (p *SOCKS5) handle(c net.Conn, user, pass string) {
	br := bufio.NewReader(c)
	head := make([]byte, 2)
	if _, err := io.ReadFull(br, head); err != nil || head[0] != 5 {
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(br, methods); err != nil {
		return
	}
	if user != "" {
		_, _ = c.Write([]byte{5, 2})
		if !socksUserPass(br, c, user, pass) {
			return
		}
	} else {
		_, _ = c.Write([]byte{5, 0})
	}
	req := make([]byte, 4)
	if _, err := io.ReadFull(br, req); err != nil || req[1] != 1 {
		return
	}
	var host string
	switch req[3] {
	case 1:
		ip := make([]byte, 4)
		_, _ = io.ReadFull(br, ip)
		host = net.IP(ip).String()
	case 3:
		n, _ := br.ReadByte()
		name := make([]byte, n)
		_, _ = io.ReadFull(br, name)
		host = string(name)
	case 4:
		ip := make([]byte, 16)
		_, _ = io.ReadFull(br, ip)
		host = net.IP(ip).String()
	default:
		return
	}
	var portb [2]byte
	if _, err := io.ReadFull(br, portb[:]); err != nil {
		return
	}
	target, err := net.Dial("tcp", net.JoinHostPort(host, strconv.FormatUint(uint64(binary.BigEndian.Uint16(portb[:])), 10)))
	if err != nil {
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	p.Connections.Add(1)
	_, _ = c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	rest := io.MultiReader(io.LimitReader(br, int64(br.Buffered())), c)
	go func() { _, _ = io.Copy(target, rest); _ = target.Close() }()
	_, _ = io.Copy(c, target)
}

func socksUserPass(br *bufio.Reader, c net.Conn, user, pass string) bool {
	if v, _ := br.ReadByte(); v != 1 {
		return false
	}
	ul, _ := br.ReadByte()
	u := make([]byte, ul)
	_, _ = io.ReadFull(br, u)
	pl, _ := br.ReadByte()
	pw := make([]byte, pl)
	_, _ = io.ReadFull(br, pw)
	ok := string(u) == user && string(pw) == pass
	if ok {
		_, _ = c.Write([]byte{1, 0})
	} else {
		_, _ = c.Write([]byte{1, 1})
	}
	return ok
}

// HTTPProxy is a running HTTP CONNECT proxy.
type HTTPProxy struct {
	Addr        string
	Connections atomic.Int64
}

// StartHTTPProxy starts a CONNECT proxy, requiring basic auth when user is not
// empty.
func StartHTTPProxy(t testing.TB, user, pass string) *HTTPProxy {
	t.Helper()
	ln := listen(t)
	p := &HTTPProxy{Addr: ln.Addr().String()}
	serve(t, ln, func(c net.Conn) {
		br := bufio.NewReader(c)
		req, err := http.ReadRequest(br)
		if err != nil || req.Method != http.MethodConnect {
			return
		}
		if user != "" {
			u, pw, ok := req.BasicAuth()
			if h := req.Header.Get("Proxy-Authorization"); h != "" && !ok {
				r := &http.Request{Header: http.Header{"Authorization": {h}}}
				u, pw, ok = r.BasicAuth()
			}
			if !ok || u != user || pw != pass {
				_, _ = io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
				return
			}
		}
		target, err := net.Dial("tcp", req.Host)
		if err != nil {
			_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		p.Connections.Add(1)
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
		go func() { _, _ = io.Copy(target, br); _ = target.Close() }()
		_, _ = io.Copy(c, target)
	})
	return p
}

// Relay is a TCP relay to a fixed target whose link can be paused or cut.
type Relay struct {
	Addr   string
	target string

	mu     sync.Mutex
	paused bool
	resume chan struct{}
	conns  map[net.Conn]struct{}
}

// StartRelay relays 127.0.0.1:<port> to target.
func StartRelay(t testing.TB, target string) *Relay {
	t.Helper()
	ln := listen(t)
	r := &Relay{Addr: ln.Addr().String(), target: target, resume: make(chan struct{}), conns: map[net.Conn]struct{}{}}
	close(r.resume)
	serve(t, ln, r.handle)
	return r
}

func (r *Relay) handle(c net.Conn) {
	up, err := net.Dial("tcp", r.target)
	if err != nil {
		return
	}
	r.mu.Lock()
	r.conns[c], r.conns[up] = struct{}{}, struct{}{}
	r.mu.Unlock()
	defer func() {
		_ = up.Close()
		r.mu.Lock()
		delete(r.conns, c)
		delete(r.conns, up)
		r.mu.Unlock()
	}()
	done := make(chan struct{}, 2)
	pipe := func(dst, src net.Conn) {
		buf := make([]byte, 32*1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				r.waitResume()
				if _, werr := dst.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		_ = dst.Close()
		done <- struct{}{}
	}
	go pipe(up, c)
	go pipe(c, up)
	<-done
	<-done
}

func (r *Relay) waitResume() {
	r.mu.Lock()
	ch := r.resume
	r.mu.Unlock()
	<-ch
}

// PortInt returns the relay's listening port.
func (r *Relay) PortInt(t testing.TB) int {
	t.Helper()
	_, ps, err := net.SplitHostPort(r.Addr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(ps)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Pause makes the relay hold all traffic, as a dead link does: the TCP
// connections stay open but nothing moves.
func (r *Relay) Pause() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.paused {
		r.paused = true
		r.resume = make(chan struct{})
	}
}

// Resume lets held traffic flow again.
func (r *Relay) Resume() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.paused {
		r.paused = false
		close(r.resume)
	}
}

// Cut closes every relayed connection.
func (r *Relay) Cut() {
	r.mu.Lock()
	conns := make([]net.Conn, 0, len(r.conns))
	for c := range r.conns {
		conns = append(conns, c)
	}
	r.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Agent is an in-process ssh-agent on a Unix socket.
type Agent struct {
	// Socket is the path to give to SSH_AUTH_SOCK.
	Socket string
	// Signer is the key the agent holds; its public key must be authorized.
	Signer ssh.Signer
}

// StartAgent starts an agent holding a fresh Ed25519 key.
func StartAgent(t testing.TB) *Agent {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: priv, Comment: "tako-test"}); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	serve(t, ln, func(c net.Conn) {
		_ = agent.ServeAgent(keyring, c) // it returns io.EOF when the client hangs up
	})
	return &Agent{Socket: sock, Signer: signer}
}

// Dial opens a connection to the agent, for ssh.Dialer.AgentConn.
func (a *Agent) Dial() (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(context.Background(), "unix", a.Socket)
	if err != nil {
		return nil, fmt.Errorf("dialing agent: %w", err)
	}
	return conn, nil
}
