package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"time"

	"github.com/zdva/tako/internal/model"
)

// Size is a terminal size in character cells.
type Size struct {
	Cols, Rows int
}

// Shell is an interactive remote shell on a PTY. Read returns what the shell
// prints (stdout and stderr merged by the PTY); Write types into it.
type Shell interface {
	io.ReadWriteCloser
	// Resize tells the remote PTY its new size.
	Resize(cols, rows int) error
}

// Transport is one established connection to a host.
type Transport interface {
	// OpenShell starts an interactive shell on a new channel.
	OpenShell(ctx context.Context, size Size) (Shell, error)
	// Exec runs a command on its own channel (no PTY) and returns its
	// combined output.
	Exec(ctx context.Context, cmd string) ([]byte, error)
	// SFTP opens the file transfer subsystem on its own channel.
	SFTP(ctx context.Context) (FS, error)
	// Ping measures the round trip of the connection.
	Ping(ctx context.Context) (time.Duration, error)
	// Close ends the connection and every channel on it.
	Close() error
	// Done is closed when the connection ends, for any reason.
	Done() <-chan struct{}
	// Err is why the connection ended; it is nil while Done is open.
	Err() error
}

// AgentExecer is a [Transport] that can run a command with the user's SSH
// agent forwarded to it, for the length of that command only (a direct
// server-to-server copy needs the keys without leaving any on the server).
type AgentExecer interface {
	// ExecAgent runs cmd with agent forwarding requested on its channel.
	// agent opens a connection to the local agent for each request the remote
	// side makes; nothing is forwarded after ExecAgent returns.
	ExecAgent(ctx context.Context, cmd string, agent func() (net.Conn, error)) ([]byte, error)
}

// FS is a file system, local or remote over SFTP. Paths use "/" on remote
// systems.
type FS interface {
	// Open opens a file for reading.
	Open(name string) (File, error)
	// OpenFile opens a file with os.OpenFile flags (O_WRONLY|O_CREATE|...).
	OpenFile(name string, flag int) (File, error)
	Stat(name string) (fs.FileInfo, error)
	ReadDir(name string) ([]fs.FileInfo, error)
	MkdirAll(name string) error
	Remove(name string) error
	Rename(oldName, newName string) error
	// Home is the directory a new session starts in.
	Home() (string, error)
	// Join joins path elements the way this system writes paths.
	Join(elem ...string) string
	Close() error
}

// File is an open file of an [FS].
type File interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
	Stat() (fs.FileInfo, error)
	Truncate(size int64) error
}

// Dialer connects to hosts.
type Dialer interface {
	Dial(ctx context.Context, h model.Host) (Transport, error)
}

// Classified connection errors. Wrap them with %w and test with errors.Is.
var (
	// ErrHostKeyRejected means the user (or a strict policy) refused the
	// server's key.
	ErrHostKeyRejected = errors.New("host key rejected")
	// ErrAuthFailed means the server refused every credential we offered.
	ErrAuthFailed = errors.New("authentication failed")
	// ErrUnreachable means the host could not be reached: refused, timed out,
	// no route, or dropped the connection.
	ErrUnreachable = errors.New("host unreachable")
	// ErrProxyFailed means a proxy or jump host failed before the target was
	// reached.
	ErrProxyFailed = errors.New("proxy or jump host failed")
	// ErrChannelLimit means the server refused another channel on this
	// connection (OpenSSH MaxSessions); open a second connection.
	ErrChannelLimit = errors.New("server refused another session on this connection")
	// ErrCanceled means the user cancelled a prompt.
	ErrCanceled = errors.New("cancelled by the user")
)

// HostKeyChangedError reports that a server presented a different key than the
// one trusted before. Connections to it are always refused until the user
// explicitly trusts the new key.
type HostKeyChangedError struct {
	// Host is the "host:port" the key belongs to.
	Host string
	// Old and New are SHA256 fingerprints.
	Old, New string
}

func (e *HostKeyChangedError) Error() string {
	return fmt.Sprintf("host key for %s changed: was %s, now %s", e.Host, e.Old, e.New)
}

// Is makes errors.Is(err, ErrHostKeyRejected) true for a changed key.
func (e *HostKeyChangedError) Is(target error) bool { return target == ErrHostKeyRejected }

// HostKeyUnknownError reports a key that is not trusted yet while the policy
// forbids asking (strict).
type HostKeyUnknownError struct {
	Host        string
	Fingerprint string
}

func (e *HostKeyUnknownError) Error() string {
	return fmt.Sprintf("host key for %s (%s) is not trusted and the policy is strict", e.Host, e.Fingerprint)
}

// Is makes errors.Is(err, ErrHostKeyRejected) true for an unknown strict key.
func (e *HostKeyUnknownError) Is(target error) bool { return target == ErrHostKeyRejected }

// Retryable reports whether trying to connect again could succeed. Network
// trouble is retryable; a refused credential, key or user decision is not.
func Retryable(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, ErrAuthFailed), errors.Is(err, ErrHostKeyRejected), errors.Is(err, ErrCanceled),
		errors.Is(err, context.Canceled):
		return false
	default:
		return true
	}
}

// HostKeyRequest asks the user whether to trust a server's key.
type HostKeyRequest struct {
	// Host is the target as "host:port"; Name is the library name of the host.
	Host, Name string
	// KeyType is the algorithm ("ssh-ed25519"), Fingerprint is "SHA256:...".
	KeyType, Fingerprint string
}

// SecretRequest asks for a password or key passphrase.
type SecretRequest struct {
	Host model.Host
	// Kind is "password" or "passphrase".
	Kind string
	// Detail says what needs unlocking, such as a key file path.
	Detail string
}

// SecretReply is the answer to a [SecretRequest].
type SecretReply struct {
	Value []byte
	// Remember asks to keep the value in the keychain.
	Remember bool
}

// ChallengeRequest is a keyboard-interactive round: a name, instructions and
// questions, each answered by one string.
type ChallengeRequest struct {
	Host        model.Host
	Name        string
	Instruction string
	Questions   []string
	// Echo says whether each answer may be shown as typed.
	Echo []bool
}

// Prompter asks the user. The UI implements it. Each method blocks until the
// user answers and must return ErrCanceled (or ctx.Err()) when they dismiss it.
type Prompter interface {
	ConfirmHostKey(ctx context.Context, req HostKeyRequest) (bool, error)
	Secret(ctx context.Context, req SecretRequest) (SecretReply, error)
	Challenge(ctx context.Context, req ChallengeRequest) ([]string, error)
}

// DenyPrompter refuses every request. It is the safe default for
// non-interactive use (tests, background reconnects).
type DenyPrompter struct{}

// ConfirmHostKey implements [Prompter].
func (DenyPrompter) ConfirmHostKey(context.Context, HostKeyRequest) (bool, error) { return false, nil }

// Secret implements [Prompter].
func (DenyPrompter) Secret(context.Context, SecretRequest) (SecretReply, error) {
	return SecretReply{}, ErrCanceled
}

// Challenge implements [Prompter].
func (DenyPrompter) Challenge(context.Context, ChallengeRequest) ([]string, error) {
	return nil, ErrCanceled
}
