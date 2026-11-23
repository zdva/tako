package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zdva/tako/internal/hostlib"
	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/secrets"
	"github.com/zdva/tako/internal/store"
)

type denyStore struct{}

func (denyStore) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("system credential store was used")
}

func (denyStore) Set(context.Context, string, []byte) error {
	return errors.New("system credential store was used")
}

func (denyStore) Delete(context.Context, string) error {
	return errors.New("system credential store was used")
}

func TestRunMigrate_NoReferencesDoesNotTouchTheSource(t *testing.T) {
	t.Parallel()
	dirs := store.Dirs{Config: t.TempDir(), State: t.TempDir()}
	var buf bytes.Buffer
	if err := runMigrate(t.Context(), dirs, denyStore{}, &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "system credential store was used") {
		t.Fatalf("output: %s", buf.String())
	}
	if _, err := os.Stat(dirs.Secrets()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an encrypted file was created with nothing to copy: %v", err)
	}
}

func TestRunMigrate_CopiesWithoutWritingThePassword(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	dirs := store.Dirs{Config: t.TempDir(), State: t.TempDir()}
	const pass = "S3cr3t-Pa55-xyz"
	const ref = "host/h_test/password"
	lib, err := hostlib.Open(store.Doc{Path: dirs.Hosts(), Version: hostlib.Version}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.Add(model.Host{
		Name: "web", Addr: "10.0.0.8", Port: 22, User: "ops",
		Auth: []model.Auth{{Kind: model.AuthPassword, SecretRef: ref}},
	}); err != nil {
		t.Fatal(err)
	}
	lib.Flush()
	lib.Close()

	src := secrets.NewMemory()
	if err := src.Set(ctx, ref, []byte(pass)); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runMigrate(ctx, dirs, src, &buf); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), pass) {
		t.Fatal("the migration report contains the password")
	}
	if _, err := src.Get(ctx, ref); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("source still has the secret: %v", err)
	}
	for _, path := range []string{dirs.Hosts(), dirs.Secrets(), filepath.Join(dirs.Config, "secrets.key")} {
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(pass)) {
			t.Errorf("%s contains the password", path)
		}
	}
	dst, err := secrets.Open(dirs.Secrets())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dst.Close() })
	got, err := dst.Get(ctx, ref)
	if err != nil || string(got) != pass {
		t.Fatalf("encrypted store = %q, %v", got, err)
	}
}

func TestLaunchPathDoesNotConstructTheCredentialStore(t *testing.T) {
	t.Parallel()
	mainSrc, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(mainSrc, []byte("newSystemCredentialStore")) || bytes.Contains(mainSrc, []byte("mygo"+".Secrets")) {
		t.Fatal("main.go constructs the system credential store; only the migration command may")
	}
	mig, err := os.ReadFile("migrate.go")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(mig, []byte("mygo"+".Secrets")) {
		t.Fatal("migrate.go calls the credential store directly; keychain.go is the only adapter")
	}
}
