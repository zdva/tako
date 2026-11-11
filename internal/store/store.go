package store

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Permissions of everything Tako writes.
const (
	fileMode fs.FileMode = 0o600
	dirMode  fs.FileMode = 0o700
)

// backupCount is how many rotated backups RotateBackups keeps.
const backupCount = 3

// Errors returned by Load.
var (
	// ErrNotExist means the document has never been written.
	ErrNotExist = errors.New("document does not exist")
	// ErrTooNew means the file was written by a newer Tako.
	ErrTooNew = errors.New("document was written by a newer version of Tako")
	// ErrCorrupt means the file could not be parsed. It has been moved aside.
	ErrCorrupt = errors.New("document is corrupt")
)

// Migration upgrades a document's raw JSON from version From to From+1.
type Migration func(raw []byte) ([]byte, error)

// Doc describes one JSON document on disk.
type Doc struct {
	// Path is the file.
	Path string
	// Version is the current version of the document's schema.
	Version int
	// Migrations maps a version to the step that upgrades it to the next.
	Migrations map[int]Migration
}

// versionOnly reads just the version of a document.
type versionOnly struct {
	Version int `json:"version"`
}

// Load reads the document into v, which must be a pointer to a struct with a
// `json:"version"` field. A missing file returns ErrNotExist. When the file is
// corrupt it is renamed to "<path>.corrupt-<time>" and Load tries the newest
// backup before giving up with ErrCorrupt.
func (d Doc) Load(v any) error {
	data, err := os.ReadFile(d.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotExist
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", d.Path, err)
	}
	loadErr := d.decode(data, v)
	if loadErr == nil || errors.Is(loadErr, ErrTooNew) {
		return loadErr
	}
	// Corrupt: keep the evidence, then try the newest backup.
	aside := d.Path + ".corrupt-" + time.Now().Format("20060102-150405")
	if rerr := os.Rename(d.Path, aside); rerr != nil {
		return fmt.Errorf("setting aside corrupt %s: %w", d.Path, errors.Join(loadErr, rerr))
	}
	for i := 1; i <= backupCount; i++ {
		bak, rerr := os.ReadFile(backupPath(d.Path, i))
		if rerr != nil {
			continue
		}
		if derr := d.decode(bak, v); derr == nil {
			return nil
		}
	}
	return fmt.Errorf("%w: %s (moved to %s): %w", ErrCorrupt, d.Path, aside, loadErr)
}

// decode migrates and unmarshals data.
func (d Doc) decode(data []byte, v any) error {
	var ver versionOnly
	if err := json.Unmarshal(data, &ver); err != nil {
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	if ver.Version > d.Version {
		return fmt.Errorf("%w: file is version %d, this Tako reads up to %d",
			ErrTooNew, ver.Version, d.Version)
	}
	if ver.Version < d.Version {
		if err := d.backupOld(data, ver.Version); err != nil {
			return err
		}
		var err error
		if data, err = d.migrate(data, ver.Version); err != nil {
			return err
		}
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return nil
}

func (d Doc) backupOld(data []byte, version int) error {
	path := d.Path + ".bak.v" + strconv.Itoa(version)
	if err := os.WriteFile(path, data, fileMode); err != nil {
		return fmt.Errorf("backing up %s before migration: %w", d.Path, err)
	}
	return nil
}

func (d Doc) migrate(data []byte, from int) ([]byte, error) {
	for ver := from; ver < d.Version; ver++ {
		step, ok := d.Migrations[ver]
		if !ok {
			return nil, fmt.Errorf("no migration from version %d of %s", ver, d.Path)
		}
		var err error
		if data, err = step(data); err != nil {
			return nil, fmt.Errorf("migrating %s from version %d: %w", d.Path, ver, err)
		}
	}
	return data, nil
}

// Save writes v atomically. v must marshal to a JSON object whose "version"
// is the document's current version.
func (d Doc) Save(v any) error {
	data, err := json.Marshal(v, jsontext.WithIndent("  "))
	if err != nil {
		return fmt.Errorf("encoding %s: %w", d.Path, err)
	}
	var ver versionOnly
	if err := json.Unmarshal(data, &ver); err != nil || ver.Version != d.Version {
		return fmt.Errorf("saving %s: document version is %d, want %d", d.Path, ver.Version, d.Version)
	}
	return WriteFileAtomic(d.Path, append(data, '\n'))
}

// RotateBackups copies the current file to "<path>.bak.1", shifting older
// backups up and dropping the oldest. Call it once per start, before the
// first write.
func RotateBackups(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s for backup: %w", path, err)
	}
	for i := backupCount; i > 1; i-- {
		if err := os.Rename(backupPath(path, i-1), backupPath(path, i)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("rotating backups of %s: %w", path, err)
		}
	}
	if err := os.WriteFile(backupPath(path, 1), data, fileMode); err != nil {
		return fmt.Errorf("writing backup of %s: %w", path, err)
	}
	return nil
}

func backupPath(path string, n int) string { return path + ".bak." + strconv.Itoa(n) }

// WriteFileAtomic writes data to path through a temp file in the same
// directory, so a crash never leaves a half-written file.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	cleanup := func(cause error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return cause
	}
	if err := tmp.Chmod(fileMode); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		return cleanup(fmt.Errorf("setting permissions of %s: %w", tmpName, err))
	}
	if _, err := tmp.Write(data); err != nil {
		return cleanup(fmt.Errorf("writing %s: %w", tmpName, err))
	}
	if err := tmp.Sync(); err != nil {
		return cleanup(fmt.Errorf("syncing %s: %w", tmpName, err))
	}
	if err := tmp.Close(); err != nil {
		return cleanup(fmt.Errorf("closing %s: %w", tmpName, err))
	}
	if err := os.Rename(tmpName, path); err != nil {
		return cleanup(fmt.Errorf("replacing %s: %w", path, err))
	}
	syncDir(dir)
	return nil
}

// syncDir flushes a directory entry so the rename survives a crash. It is
// best effort: Windows cannot open directories this way.
func syncDir(dir string) {
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = f.Sync()
	_ = f.Close()
}
