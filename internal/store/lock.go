package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ErrLocked means another Tako already owns the directory.
var ErrLocked = errors.New("another Tako is using this configuration")

// Lock is an exclusive claim on a configuration directory, so two instances
// never write the same files.
type Lock struct{ path string }

// Acquire claims dir. A lock left by a process that no longer exists (a crash)
// is taken over.
func Acquire(dir string) (*Lock, error) {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, "tako.lock")
	for range 2 {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
		if err == nil {
			_, werr := f.WriteString(strconv.Itoa(os.Getpid()))
			cerr := f.Close()
			if err := errors.Join(werr, cerr); err != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("writing lock %s: %w", path, err)
			}
			return &Lock{path: path}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("creating lock %s: %w", path, err)
		}
		if holderAlive(path) {
			return nil, fmt.Errorf("%w (lock %s)", ErrLocked, path)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("removing stale lock %s: %w", path, err)
		}
	}
	return nil, fmt.Errorf("%w (lock %s)", ErrLocked, path)
}

// Release gives the claim up.
func (l *Lock) Release() error {
	if err := os.Remove(l.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("releasing lock %s: %w", l.path, err)
	}
	return nil
}

func holderAlive(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false
	}
	return processAlive(pid)
}
