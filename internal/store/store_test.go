package store

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type testDoc struct {
	Version int      `json:"version"`
	Items   []string `json:"items"`
}

func newDoc(t *testing.T) Doc {
	t.Helper()
	return Doc{Path: filepath.Join(t.TempDir(), "doc.json"), Version: 2, Migrations: map[int]Migration{
		1: func(raw []byte) ([]byte, error) {
			return []byte(strings.Replace(string(raw), `"version": 1`, `"version": 2`, 1)), nil
		},
	}}
}

func TestDoc_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	d := newDoc(t)
	want := testDoc{Version: 2, Items: []string{"a", "b"}}
	if err := d.Save(want); err != nil {
		t.Fatal(err)
	}
	var got testDoc
	if err := d.Load(&got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || len(got.Items) != 2 || got.Items[1] != "b" {
		t.Errorf("Load = %+v", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(d.Path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("file mode = %o, want 600", perm)
		}
	}
}

func TestDoc_LoadMissing(t *testing.T) {
	t.Parallel()
	var got testDoc
	if err := newDoc(t).Load(&got); !errors.Is(err, ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

func TestDoc_SaveRejectsWrongVersion(t *testing.T) {
	t.Parallel()
	if err := newDoc(t).Save(testDoc{Version: 1}); err == nil {
		t.Error("Save with the wrong version succeeded")
	}
}

func TestDoc_MigratesOldVersionAndBacksUp(t *testing.T) {
	t.Parallel()
	d := newDoc(t)
	old := `{"version": 1, "items": ["x"]}`
	if err := os.WriteFile(d.Path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	var got testDoc
	if err := d.Load(&got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.Items[0] != "x" {
		t.Errorf("Load = %+v", got)
	}
	bak, err := os.ReadFile(d.Path + ".bak.v1")
	if err != nil || string(bak) != old {
		t.Errorf("backup = %q, %v", bak, err)
	}
}

func TestDoc_RefusesNewerVersion(t *testing.T) {
	t.Parallel()
	d := newDoc(t)
	if err := os.WriteFile(d.Path, []byte(`{"version": 9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var got testDoc
	if err := d.Load(&got); !errors.Is(err, ErrTooNew) {
		t.Fatalf("err = %v, want ErrTooNew", err)
	}
	if _, err := os.Stat(d.Path); err != nil {
		t.Errorf("newer file must be left in place: %v", err)
	}
}

func TestDoc_CorruptFileIsSetAsideAndBackupUsed(t *testing.T) {
	t.Parallel()
	d := newDoc(t)
	if err := d.Save(testDoc{Version: 2, Items: []string{"good"}}); err != nil {
		t.Fatal(err)
	}
	if err := RotateBackups(d.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(d.Path, []byte(`{"version": 2, "items": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	var got testDoc
	if err := d.Load(&got); err != nil {
		t.Fatalf("Load with a backup: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0] != "good" {
		t.Errorf("Load = %+v, want the backup's content", got)
	}
	matches, _ := filepath.Glob(d.Path + ".corrupt-*")
	if len(matches) != 1 {
		t.Errorf("corrupt files = %v, want exactly one", matches)
	}
}

func TestDoc_CorruptWithoutBackupFails(t *testing.T) {
	t.Parallel()
	d := newDoc(t)
	if err := os.WriteFile(d.Path, []byte(`not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	var got testDoc
	if err := d.Load(&got); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestRotateBackups_KeepsThree(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "f.json")
	for i := range 5 {
		if err := os.WriteFile(path, []byte{byte('0' + i)}, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := RotateBackups(path); err != nil {
			t.Fatal(err)
		}
	}
	for n, want := range map[int]string{1: "4", 2: "3", 3: "2"} {
		got, err := os.ReadFile(backupPath(path, n))
		if err != nil || string(got) != want {
			t.Errorf("backup %d = %q, %v; want %q", n, got, err, want)
		}
	}
	if _, err := os.Stat(backupPath(path, 4)); err == nil {
		t.Error("a fourth backup exists")
	}
}

func TestWriteFileAtomic_LeavesNoTempFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "f")
	if err := WriteFileAtomic(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("directory has %d entries, want 1", len(entries))
	}
}

func TestSaver_CoalescesRequests(t *testing.T) {
	t.Parallel()
	calls := make(chan struct{}, 10)
	s := NewSaver(20*time.Millisecond, func() error { calls <- struct{}{}; return nil }, nil)
	defer s.Close()
	for range 5 {
		s.Request()
	}
	select {
	case <-calls:
	case <-time.After(2 * time.Second):
		t.Fatal("save never ran")
	}
	time.Sleep(60 * time.Millisecond)
	if len(calls) != 0 {
		t.Errorf("save ran %d extra times", len(calls))
	}
}

func TestSaver_FlushWritesPendingNow(t *testing.T) {
	t.Parallel()
	n := 0
	s := NewSaver(time.Hour, func() error { n++; return nil }, nil)
	s.Request()
	s.Flush()
	if n != 1 {
		t.Errorf("save ran %d times after Flush, want 1", n)
	}
	s.Flush()
	if n != 1 {
		t.Errorf("a second Flush saved again")
	}
	s.Close()
	s.Request()
	s.Flush()
	if n != 1 {
		t.Errorf("a closed saver saved")
	}
}

func TestSaver_ReportsErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	got := make(chan error, 1)
	s := NewSaver(time.Hour, func() error { return boom }, func(err error) { got <- err })
	s.Request()
	s.Flush()
	if err := <-got; !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestAcquire_ExclusiveAndReleasable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	l, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Acquire err = %v, want ErrLocked", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
	_ = l2.Release()
}

func TestAcquire_TakesOverStaleLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// PID 0x7ffffffe is far beyond any real process ID.
	if err := os.WriteFile(filepath.Join(dir, "tako.lock"), []byte("2147483646"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := Acquire(dir)
	if err != nil {
		t.Fatalf("stale lock not taken over: %v", err)
	}
	_ = l.Release()
}

func TestDefaultDirs_EnvOverride(t *testing.T) {
	t.Setenv(EnvConfigDir, "/tmp/tako-cfg")
	t.Setenv(EnvStateDir, "/tmp/tako-state")
	d, err := DefaultDirs()
	if err != nil {
		t.Fatal(err)
	}
	if d.Config != "/tmp/tako-cfg" || d.State != "/tmp/tako-state" {
		t.Errorf("dirs = %+v", d)
	}
	if filepath.Base(d.Hosts()) != "hosts.json" || filepath.Base(d.Session()) != "session.json" {
		t.Errorf("document paths: %s %s", d.Hosts(), d.Session())
	}
}
