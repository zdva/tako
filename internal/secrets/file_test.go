package secrets

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
)

func testMachine(id string) func() ([]byte, error) {
	return func() ([]byte, error) {
		if id == "" {
			return nil, errors.New("no machine id")
		}
		return []byte(id), nil
	}
}

func openAt(t *testing.T, path, id string) *File {
	t.Helper()
	f, err := open(path, testMachine(id))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func assertOwnerOnly(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("%s mode %o, want 0600", path, st.Mode().Perm())
	}
}

func TestFile_RoundTripAndCopy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "nested", "secrets.enc")
	f := openAt(t, path, "machine-a")
	const pass = "S3cr3t-Pa55-xyz"
	if err := f.Set(ctx, "host/h/password", []byte(pass)); err != nil {
		t.Fatal(err)
	}
	got, err := f.Get(ctx, "host/h/password")
	if err != nil || string(got) != pass {
		t.Fatalf("Get = %q, %v", got, err)
	}
	got[0] = 'X'
	again, err := f.Get(ctx, "host/h/password")
	if err != nil || string(again) != pass {
		t.Fatalf("stored value changed through the returned slice: %q, %v", again, err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(ctx, "host/h/password", []byte("other")); !errors.Is(err, errClosed) {
		t.Fatalf("Set after Close: %v", err)
	}

	f2 := openAt(t, path, "machine-a")
	got, err = f2.Get(ctx, "host/h/password")
	if err != nil || string(got) != pass {
		t.Fatalf("reopen Get = %q, %v", got, err)
	}
	assertOwnerOnly(t, path)
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), keyFileName)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("machine binding wrote a key file: %v", err)
	}
}

func TestFile_PlaintextStaysOutOfTheFile(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")
	f := openAt(t, path, "machine-a")
	const pass = `S3cr3t-Pa55-xyz`
	const proxy = "Pr0xy-S3cret-xyz"
	if err := f.Set(ctx, "host/h/password", []byte(pass)); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(ctx, "host/h/proxy", []byte(proxy)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		for _, secret := range []string{pass, proxy} {
			if bytes.Contains(data, []byte(secret)) {
				t.Errorf("%s contains %q", p, secret)
			}
			enc := base64.StdEncoding.EncodeToString([]byte(secret))
			if bytes.Contains(data, []byte(enc)) {
				t.Errorf("%s contains the base64 of %q", p, secret)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFile_OtherMachineCannotDecrypt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "secrets.enc")
	f := openAt(t, path, "machine-a")
	const pass = "S3cr3t-Pa55-xyz"
	if err := f.Set(ctx, "k", []byte(pass)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	fresh, err := open(path, testMachine("machine-b"))
	if !errors.Is(err, ErrSealed) {
		t.Fatalf("other machine: %v", err)
	}
	if fresh == nil {
		t.Fatal("ErrSealed did not return an empty store")
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("unreadable file still at the original path: %v", err)
	}
	matches, err := filepath.Glob(path + ".unreadable-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("aside files %v, %v", matches, err)
	}
	aside, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(aside, []byte(pass)) {
		t.Fatal("the set-aside file contains the password")
	}
	if err := fresh.Set(ctx, "k", []byte("typed-again")); err != nil {
		t.Fatal(err)
	}
	got, err := fresh.Get(ctx, "k")
	if err != nil || string(got) != "typed-again" {
		t.Fatalf("new store Get = %q, %v", got, err)
	}
}

func TestFile_BindingUnavailableLeavesTheFile(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "secrets.enc")
	f := openAt(t, path, "machine-a")
	if err := f.Set(ctx, "k", []byte("S3cr3t-Pa55-xyz")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = open(path, testMachine(""))
	if !errors.Is(err, ErrBindingUnavailable) {
		t.Fatalf("err = %v, want ErrBindingUnavailable", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("the file changed when the machine id could not be read")
	}
}

func TestFile_TooNewIsLeftAlone(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "secrets.enc")
	body := []byte("{\"version\":99,\"binding\":\"machine\"}\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := open(path, testMachine("machine-a"))
	if !errors.Is(err, ErrTooNew) || f != nil {
		t.Fatalf("open = %v, %v", f, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("file = %q, %v", got, err)
	}
}

func TestFile_GarbageIsSetAside(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "secrets.enc")
	if err := os.WriteFile(path, []byte("not a secret store"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := open(path, testMachine("machine-a"))
	if !errors.Is(err, ErrSealed) || f == nil {
		t.Fatalf("open = %v, %v", f, err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("garbage file still in place: %v", statErr)
	}
}

func TestFile_TamperFailsClosed(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "secrets.enc")
	f := openAt(t, path, "machine-a")
	if err := f.Set(ctx, "k", []byte("S3cr3t-Pa55-xyz")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-8] ^= 0x5a
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := open(path, testMachine("machine-a")); !errors.Is(err, ErrSealed) {
		t.Fatalf("tampered file: %v", err)
	}
}

func TestFile_FileKeyFallback(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.enc")
	f := openAt(t, path, "")
	const pass = "S3cr3t-Pa55-xyz"
	if err := f.Set(ctx, "k", []byte(pass)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, keyFileName)
	assertOwnerOnly(t, path)
	assertOwnerOnly(t, keyPath)
	key, err := os.ReadFile(keyPath)
	if err != nil || len(key) != keyLen {
		t.Fatalf("key file %d bytes, %v", len(key), err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(pass)) || bytes.Contains(data, key) {
		t.Fatal("the encrypted file contains the password or the key")
	}
	f2 := openAt(t, path, "")
	got, err := f2.Get(ctx, "k")
	if err != nil || string(got) != pass {
		t.Fatalf("reopen Get = %q, %v", got, err)
	}
	// A different machine id does not matter: the key file is the binding.
	if err := f2.Close(); err != nil {
		t.Fatal(err)
	}
	f3 := openAt(t, path, "some-other-machine")
	got, err = f3.Get(ctx, "k")
	if err != nil || string(got) != pass {
		t.Fatalf("file binding followed the machine id: %q, %v", got, err)
	}
}

func TestFile_DeleteAndMissing(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "secrets.enc")
	f := openAt(t, path, "machine-a")
	if _, err := f.Get(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: %v", err)
	}
	if err := f.Delete(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(ctx, "a", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := f.Set(ctx, "b", []byte("two")); err != nil {
		t.Fatal(err)
	}
	if err := f.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	f2 := openAt(t, path, "machine-a")
	if _, err := f2.Get(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted secret returned: %v", err)
	}
	got, err := f2.Get(ctx, "b")
	if err != nil || string(got) != "two" {
		t.Fatalf("kept secret = %q, %v", got, err)
	}
}

func TestFile_RejectsBadInput(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := openAt(t, filepath.Join(t.TempDir(), "secrets.enc"), "machine-a")
	if err := f.Set(ctx, "", []byte("x")); err == nil {
		t.Fatal("empty reference was accepted")
	}
	if err := f.Set(ctx, "has\x00nul", []byte("x")); err == nil {
		t.Fatal("reference with NUL was accepted")
	}
	if err := f.Set(ctx, "k", bytes.Repeat([]byte("a"), maxSecretBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversize: %v", err)
	}
	cancelCtx, stop := context.WithCancel(ctx)
	stop()
	if err := f.Set(cancelCtx, "k", []byte("x")); err == nil {
		t.Fatal("canceled context was accepted")
	}
}

func TestFile_SessionSecretIsNotWritten(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "secrets.enc")
	f := openAt(t, path, "machine-a")
	l := NewLayered(f)
	if err := l.SetSession(ctx, "temp", []byte("session-only-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("session secret created a file: %v", err)
	}
	const pass = "kept-secret-value"
	if err := l.Set(ctx, "kept", []byte(pass)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(pass)) || bytes.Contains(data, []byte("session-only-secret")) {
		t.Fatal("the file contains a plaintext secret")
	}
	if _, err := f.Get(ctx, "temp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("session secret reached the file store: %v", err)
	}
}

func TestFile_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f := openAt(t, filepath.Join(t.TempDir(), "secrets.enc"), "machine-a")
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			ref := strconv.Itoa(i)
			val := []byte("v-" + ref)
			if err := f.Set(ctx, ref, val); err != nil {
				t.Errorf("Set %s: %v", ref, err)
				return
			}
			got, err := f.Get(ctx, ref)
			if err != nil || string(got) != string(val) {
				t.Errorf("Get %s = %q, %v", ref, got, err)
			}
		})
	}
	wg.Wait()
}

func TestEphemeral_DoesNotPersist(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	plain := NewEphemeral()
	if _, err := plain.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get: %v", err)
	}
	if err := plain.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	l := NewLayered(plain)
	err := l.Set(ctx, "k", []byte("v"))
	if !errors.Is(err, ErrNotPersisted) {
		t.Fatalf("Set err = %v, want ErrNotPersisted", err)
	}
	got, err := l.Get(ctx, "k")
	if err != nil || string(got) != "v" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Close: %v", err)
	}
}

func TestOpen_RoundTripsWithThePlatformBinding(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "secrets.enc")
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	const pass = "S3cr3t-Pa55-xyz"
	if err := f.Set(ctx, "host/h/password", []byte(pass)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(pass)) {
		t.Fatal("Open wrote the password in the clear")
	}
	f2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f2.Close() })
	got, err := f2.Get(ctx, "host/h/password")
	if err != nil || string(got) != pass {
		t.Fatalf("reopen Get = %q, %v", got, err)
	}
}
