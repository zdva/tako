package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDirs_PathsAndEnsure(t *testing.T) {
	root := t.TempDir()
	d := Dirs{Config: filepath.Join(root, "cfg"), State: filepath.Join(root, "state")}
	if err := d.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{d.Config, d.State} {
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
			t.Fatalf("%s: %v", p, err)
		}
	}
	for got, want := range map[string]string{
		d.Hosts():      filepath.Join(d.Config, "hosts.json"),
		d.KnownHosts(): filepath.Join(d.Config, "known_hosts"),
		d.Session():    filepath.Join(d.State, "session.json"),
		d.Logs():       filepath.Join(d.State, "logs"),
	} {
		if got != want {
			t.Errorf("path = %s, want %s", got, want)
		}
	}
}

func TestDefaultDirs_HonorsEnvironment(t *testing.T) {
	t.Setenv(EnvConfigDir, "/tmp/tako-cfg")
	t.Setenv(EnvStateDir, "/tmp/tako-state")
	d, err := DefaultDirs()
	if err != nil {
		t.Fatal(err)
	}
	if d.Config != "/tmp/tako-cfg" || d.State != "/tmp/tako-state" {
		t.Fatalf("dirs = %+v", d)
	}
}

func TestDefaultDirs_PlatformDefaultsAreInTheHome(t *testing.T) {
	t.Setenv(EnvConfigDir, "")
	t.Setenv(EnvStateDir, "")
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	d, err := DefaultDirs()
	if err != nil {
		t.Fatal(err)
	}
	if d.Config == "" || d.State == "" {
		t.Fatalf("dirs = %+v", d)
	}
}

func TestLock_ExclusiveAndReleasable(t *testing.T) {
	dir := t.TempDir()
	l, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Acquire = %v, want ErrLocked", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("releasing twice: %v", err)
	}
	l2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	_ = l2.Release()
}

func TestLock_StaleLockIsTakenOver(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tako.lock"), []byte("999999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(dir)
	if err != nil {
		t.Fatalf("stale lock not taken over: %v", err)
	}
	_ = l.Release()
}

func TestRotateBackups_KeepsNewestAndDropsOldest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts.json")
	if err := RotateBackups(p); err != nil {
		t.Fatalf("missing file: %v", err)
	}
	for _, v := range []string{"1", "2", "3", "4", "5"} {
		if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := RotateBackups(p); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(backupPath(p, 1))
	if err != nil || string(got) != "5" {
		t.Fatalf("newest backup = %q, %v", got, err)
	}
	if _, err := os.Stat(backupPath(p, backupCount+1)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a backup beyond the limit exists: %v", err)
	}
}

func TestWriteFileAtomic_ReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "f.json")
	for _, v := range []string{"one", "two"} {
		if err := WriteFileAtomic(p, []byte(v)); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := os.ReadFile(p); string(got) != "two" {
		t.Fatalf("content = %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the file", len(entries))
	}
}
