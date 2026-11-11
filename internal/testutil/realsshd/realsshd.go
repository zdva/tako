// Package realsshd runs a real OpenSSH sshd, as the current user, on a
// loopback port, for integration tests. It makes the tests exercise genuine
// server behavior: key exchange, PAM keyboard-interactive, MaxSessions,
// direct-tcpip forwarding for ProxyJump and a real PTY shell.
//
// sshd is found in $TAKO_SSHD_ROOT (a prefix with usr/sbin/sshd, as made by
// scripts/fetch-sshd.sh), in <repo>/.cache/openssh, or at /usr/sbin/sshd. When
// none works, [Start] skips the test.
//
// Only public key (and agent) logins work: an unprivileged sshd cannot check
// passwords, because that needs root-only PAM/shadow access. Password and
// keyboard-interactive authentication are covered against the in-process
// server in package memssh instead.
package realsshd

import (
	"bytes"
	"cmp"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// Options configure a server.
type Options struct {
	// MaxSessions is sshd's MaxSessions; zero keeps the default (10).
	MaxSessions int
	// HostKeyDir reuses a host key made by an earlier server (to restart as
	// the same machine). Empty makes a fresh key.
	HostKeyDir string
	// Port restarts on a known port; zero picks a free one.
	Port int
	// NoForwarding disables TCP forwarding (a host that cannot be a jump host).
	NoForwarding bool
}

// Server is a running sshd.
type Server struct {
	// Host and Port are the listen address.
	Host string
	Port int
	// User is the account to log in as (the current user).
	User string
	// ClientKeyPath is an OpenSSH private key (no passphrase) that is
	// authorized for User; ClientKey is the same key as a signer.
	ClientKeyPath string
	ClientKey     ssh.Signer
	// Dir is the server's private working directory; HostKeyDir holds its
	// host key and can be passed to a restart.
	Dir        string
	HostKeyDir string
	// HostKey is the server's public host key.
	HostKey ssh.PublicKey

	cmd  *exec.Cmd
	done chan struct{}
}

// Addr returns "host:port".
func (s *Server) Addr() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Port)) }

// AuthorizeKey adds pub to the keys accepted for User.
func (s *Server) AuthorizeKey(t testing.TB, pub ssh.PublicKey) {
	t.Helper()
	path := filepath.Join(s.Dir, "authorized_keys")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	must(t, err)
	_, err = f.Write(ssh.MarshalAuthorizedKey(pub))
	must(t, err)
	must(t, f.Close())
}

// Stop kills sshd and its sessions.
func (s *Server) Stop() {
	if s.cmd.Process != nil {
		killGroup(s.cmd)
	}
	select {
	case <-s.done:
	case <-time.After(3 * time.Second):
	}
}

type install struct {
	sshd, keygen string
	env          []string
	// portable: sshd's helper binaries and PAM modules live under root.
	root string
}

func locate() (install, bool) {
	var roots []string
	if r := os.Getenv("TAKO_SSHD_ROOT"); r != "" {
		roots = append(roots, r)
	}
	if mod := moduleRoot(); mod != "" {
		roots = append(roots, filepath.Join(mod, ".cache", "openssh"))
	}
	for _, r := range roots {
		sshd := filepath.Join(r, "usr", "sbin", "sshd")
		if _, err := os.Stat(sshd); err != nil {
			continue
		}
		lib := filepath.Join(r, "usr", "lib", "x86_64-linux-gnu")
		return install{
			sshd: sshd, keygen: filepath.Join(r, "usr", "bin", "ssh-keygen"),
			env: append(os.Environ(), "LD_LIBRARY_PATH="+lib), root: r,
		}, true
	}
	if _, err := os.Stat("/usr/sbin/sshd"); err == nil {
		if kg, err := exec.LookPath("ssh-keygen"); err == nil {
			return install{sshd: "/usr/sbin/sshd", keygen: kg, env: os.Environ()}, true
		}
	}
	return install{}, false
}

func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Available reports whether a usable sshd was found.
func Available() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	_, ok := locate()
	return ok
}

// Start launches sshd and registers its shutdown with t. It skips the test
// when no sshd is available.
func Start(t testing.TB, opts Options) *Server {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("real sshd integration tests run on Linux only")
	}
	in, ok := locate()
	if !ok {
		t.Skip("no sshd found: run scripts/fetch-sshd.sh or install openssh-server")
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// sshd re-executes itself and needs absolute paths everywhere.
	s := &Server{Host: "127.0.0.1", User: u.Username, Dir: dir, HostKeyDir: opts.HostKeyDir}
	if s.HostKeyDir == "" {
		s.HostKeyDir = filepath.Join(dir, "hostkey")
		must(t, os.MkdirAll(s.HostKeyDir, 0o700))
		runKeygen(t, in, filepath.Join(s.HostKeyDir, "ssh_host_ed25519_key"))
	}
	s.ClientKeyPath = filepath.Join(dir, "client_key")
	makeClientKey(t, s)

	for attempt := range 5 {
		port := opts.Port
		if port == 0 {
			port = freePort(t)
		}
		s.Port = port
		cfg := writeConfig(t, in, s, opts)
		if err := s.launch(t, in, cfg); err == nil {
			break
		} else if attempt == 4 || opts.Port != 0 {
			t.Fatalf("starting sshd: %v", err)
		}
	}
	hostPub, err := os.ReadFile(filepath.Join(s.HostKeyDir, "ssh_host_ed25519_key.pub"))
	must(t, err)
	s.HostKey, _, _, _, err = ssh.ParseAuthorizedKey(hostPub)
	must(t, err)
	t.Cleanup(s.Stop)
	return s
}

func must(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func runKeygen(t testing.TB, in install, path string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), in.keygen, "-q", "-t", "ed25519", "-N", "", "-f", path)
	cmd.Env = in.env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v: %s", err, out)
	}
}

func makeClientKey(t testing.TB, s *Server) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	must(t, err)
	block, err := ssh.MarshalPrivateKey(priv, "tako-test")
	must(t, err)
	must(t, os.WriteFile(s.ClientKeyPath, pem.EncodeToMemory(block), 0o600))
	signer, err := ssh.NewSignerFromKey(priv)
	must(t, err)
	s.ClientKey = signer
	sshPub, err := ssh.NewPublicKey(pub)
	must(t, err)
	must(t, os.WriteFile(filepath.Join(s.Dir, "authorized_keys"), ssh.MarshalAuthorizedKey(sshPub), 0o600))
}

func writeConfig(t testing.TB, in install, s *Server, opts Options) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "Port %d\nListenAddress %s\n", s.Port, s.Host)
	fmt.Fprintf(&b, "HostKey %s\nPidFile %s\n", filepath.Join(s.HostKeyDir, "ssh_host_ed25519_key"), filepath.Join(s.Dir, "sshd.pid"))
	fmt.Fprintf(&b, "AuthorizedKeysFile %s\nStrictModes no\n", filepath.Join(s.Dir, "authorized_keys"))
	b.WriteString("UsePAM no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nPubkeyAuthentication yes\n")
	b.WriteString("PermitRootLogin yes\nPrintMotd no\nPrintLastLog no\nLogLevel " + cmp.Or(os.Getenv("TAKO_SSHD_LOGLEVEL"), "ERROR") + "\n")
	if in.root != "" {
		lib := filepath.Join(in.root, "usr", "lib", "openssh")
		fmt.Fprintf(&b, "SshdSessionPath %s\nSshdAuthPath %s\nSubsystem sftp %s\n",
			filepath.Join(lib, "sshd-session"), filepath.Join(lib, "sshd-auth"), filepath.Join(lib, "sftp-server"))
	}
	if opts.MaxSessions > 0 {
		fmt.Fprintf(&b, "MaxSessions %d\n", opts.MaxSessions)
	}
	if opts.NoForwarding {
		b.WriteString("AllowTcpForwarding no\n")
	} else {
		b.WriteString("AllowTcpForwarding yes\n")
	}
	path := filepath.Join(s.Dir, "sshd_config")
	must(t, os.WriteFile(path, []byte(b.String()), 0o600))
	return path
}

func (s *Server) launch(t testing.TB, in install, cfg string) error {
	t.Helper()
	var stderr bytes.Buffer

	cmd := exec.CommandContext(context.WithoutCancel(t.Context()), in.sshd, "-D", "-e", "-f", cfg)
	cmd.Env = in.env
	cmd.WaitDelay = time.Second // session processes keep stderr open
	cmd.Stderr = &stderr
	if os.Getenv("TAKO_SSHD_LOGLEVEL") != "" {
		cmd.Stderr = io.MultiWriter(&stderr, os.Stderr)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("running %s: %w", in.sshd, err)
	}
	s.cmd = cmd
	s.done = make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(s.done)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-s.done:
			return fmt.Errorf("sshd exited early: %s", stderr.String())
		default:
		}
		d := net.Dialer{Timeout: 200 * time.Millisecond}
		if c, err := d.DialContext(t.Context(), "tcp", s.Addr()); err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.Stop()
	return fmt.Errorf("sshd did not listen on %s: %s", s.Addr(), stderr.String())
}

func freePort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	must(t, err)
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // a TCP listener's address is a *net.TCPAddr
}
