package ssh

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"

	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/transport"
)

// dialTCP opens the first TCP connection of a path: directly, or through the
// host's proxy. password is the proxy password, if any.
func dialTCP(ctx context.Context, addr string, p *model.Proxy, password []byte, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	if p == nil {
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", transport.ErrUnreachable, err)
		}
		return conn, nil
	}
	switch p.Kind {
	case model.ProxySOCKS5:
		return dialSOCKS5(ctx, &d, addr, p, password)
	case model.ProxyHTTP:
		return dialHTTPConnect(ctx, &d, addr, p, password)
	default:
		return nil, fmt.Errorf("%w: unknown proxy kind %q", transport.ErrProxyFailed, p.Kind)
	}
}

func dialSOCKS5(ctx context.Context, d *net.Dialer, addr string, p *model.Proxy, password []byte) (net.Conn, error) {
	var auth *proxy.Auth
	if p.User != "" {
		auth = &proxy.Auth{User: p.User, Password: string(password)}
	}
	sd, err := proxy.SOCKS5("tcp", p.Addr, auth, d)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", transport.ErrProxyFailed, err)
	}
	cd, ok := sd.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("%w: SOCKS5 dialer has no context support", transport.ErrProxyFailed)
	}
	conn, err := cd.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: SOCKS5 %s: %w", transport.ErrProxyFailed, p.Addr, err)
	}
	return conn, nil
}

// dialHTTPConnect tunnels through an HTTP proxy with CONNECT.
func dialHTTPConnect(ctx context.Context, d *net.Dialer, addr string, p *model.Proxy, password []byte) (net.Conn, error) {
	conn, err := d.DialContext(ctx, "tcp", p.Addr)
	if err != nil {
		return nil, fmt.Errorf("%w: reaching proxy %s: %w", transport.ErrProxyFailed, p.Addr, err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: addr},
		Host:   addr,
		Header: make(http.Header),
	}
	if p.User != "" {
		cred := base64.StdEncoding.EncodeToString([]byte(p.User + ":" + string(password)))
		req.Header.Set("Proxy-Authorization", "Basic "+cred)
	}
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: writing CONNECT: %w", transport.ErrProxyFailed, err)
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: reading CONNECT reply: %w", transport.ErrProxyFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: proxy answered %s", transport.ErrProxyFailed, resp.Status)
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, errors.Join(transport.ErrProxyFailed, err)
	}
	if br.Buffered() > 0 {
		return &bufferedConn{Conn: conn, r: br}, nil
	}
	return conn, nil
}

// bufferedConn serves bytes the CONNECT reply reader already buffered.
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
