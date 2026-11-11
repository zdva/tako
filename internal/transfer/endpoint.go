package transfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/wei-dong/tako/internal/transport"
)

// Endpoint is a place files live: this computer or one host.
type Endpoint interface {
	FS() transport.FS
	// Hash returns the hex SHA-256 of a file, computed where the file is when
	// that is possible, so verifying costs no network traffic.
	Hash(ctx context.Context, path string) (string, error)
}

// Provider supplies endpoints. An empty host is this computer.
type Provider interface {
	// Endpoint returns a working endpoint, connecting if needed.
	Endpoint(ctx context.Context, host Location) (Endpoint, error)
	// Reset forgets the cached endpoint of host after it failed, so the next
	// Endpoint call starts afresh.
	Reset(host Location)
}

// ExecFunc runs a command on a host.
type ExecFunc func(ctx context.Context, cmd string) ([]byte, error)

type localEndpoint struct{ fs localFS }

// Local is the endpoint for this computer's files.
func Local() Endpoint { return localEndpoint{} }

func (e localEndpoint) FS() transport.FS { return e.fs }

func (localEndpoint) Hash(ctx context.Context, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return hashReader(ctx, f)
}

func hashReader(ctx context.Context, r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, &ctxReader{ctx: ctx, r: r}); err != nil {
		return "", fmt.Errorf("hashing: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type remoteEndpoint struct {
	fs   transport.FS
	exec ExecFunc
}

// Remote is the endpoint for a host. exec may be nil (a host that cannot run
// commands); hashes are then computed by reading the file back.
func Remote(fs transport.FS, exec ExecFunc) Endpoint { return remoteEndpoint{fs: fs, exec: exec} }

func (e remoteEndpoint) FS() transport.FS { return e.fs }

func (e remoteEndpoint) Hash(ctx context.Context, path string) (string, error) {
	if e.exec != nil {
		q := ShellQuote(path)
		out, err := e.exec(ctx, "sha256sum -- "+q+" 2>/dev/null || shasum -a 256 -- "+q+" 2>/dev/null")
		if err == nil {
			if sum, ok := parseSum(out); ok {
				return sum, nil
			}
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("hashing %s: %w", path, context.Cause(ctx))
		}
	}
	f, err := e.fs.Open(path)
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	return hashReader(ctx, f)
}

// parseSum reads the digest from sha256sum / shasum output.
func parseSum(out []byte) (string, bool) {
	f, _, _ := strings.Cut(strings.TrimSpace(string(out)), " ")
	if len(f) != sha256.Size*2 {
		return "", false
	}
	if _, err := hex.DecodeString(f); err != nil {
		return "", false
	}
	return strings.ToLower(f), true
}

// ShellQuote quotes s for a POSIX shell.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// localFS is [transport.FS] over the operating system's files.
type localFS struct{}

var _ transport.FS = localFS{}

func (localFS) Open(name string) (transport.File, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	return f, nil
}

func (localFS) OpenFile(name string, flag int) (transport.File, error) {
	f, err := os.OpenFile(name, flag, 0o644) //nolint:gosec // the user's own files: the umask decides, as for cp
	if err != nil {
		return nil, fmt.Errorf("opening: %w", err)
	}
	return f, nil
}

func (localFS) Stat(name string) (fs.FileInfo, error) {
	fi, err := os.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	return fi, nil
}

func (localFS) ReadDir(name string) ([]fs.FileInfo, error) {
	es, err := os.ReadDir(name)
	if err != nil {
		return nil, fmt.Errorf("listing: %w", err)
	}
	out := make([]fs.FileInfo, 0, len(es))
	for _, e := range es {
		fi, err := e.Info() // lstat: links stay links
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // gone since the listing
			}
			return nil, fmt.Errorf("listing: %w", err)
		}
		out = append(out, fi)
	}
	return out, nil
}

func (localFS) MkdirAll(name string) error {
	err := os.MkdirAll(name, 0o755) //nolint:gosec // the user's own folders: the umask decides, as for mkdir -p
	if err != nil {
		return fmt.Errorf("creating: %w", err)
	}
	return nil
}

func (localFS) Remove(name string) error {
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("removing: %w", err)
	}
	return nil
}

func (localFS) Rename(oldName, newName string) error {
	if err := os.Rename(oldName, newName); err != nil {
		return fmt.Errorf("renaming: %w", err)
	}
	return nil
}

func (localFS) Home() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding the home directory: %w", err)
	}
	return h, nil
}

func (localFS) Join(elem ...string) string { return filepath.Join(elem...) }
func (localFS) Close() error               { return nil }

// ctxReader stops a copy when its context ends.
type ctxReader struct {
	ctx context.Context //nolint:containedctx // a reader adapter lives for one copy
	r   io.Reader
}

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := context.Cause(c.ctx); err != nil {
		return 0, err
	}
	return c.r.Read(p) //nolint:wrapcheck // io.Reader contract
}
