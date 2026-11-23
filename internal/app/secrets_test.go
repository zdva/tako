package app

import (
	"bytes"
	"os"
	"testing"

	"github.com/zdva/tako/internal/store"
)

func TestNew_PersistsSecretsInTheEncryptedFile(t *testing.T) {
	t.Parallel()
	dirs := store.Dirs{Config: t.TempDir(), State: t.TempDir()}
	a, err := New(t.Context(), Options{
		Dirs: dirs, Locale: "en_US.UTF-8", Version: "test", Home: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	const pass = "S3cr3t-Pa55-xyz"
	const ref = "host/h_test/password"
	if err := a.Secrets.Set(t.Context(), ref, []byte(pass)); err != nil {
		t.Fatal(err)
	}
	a.Close()

	data, err := os.ReadFile(dirs.Secrets())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(pass)) {
		t.Fatal("secrets.enc contains the password")
	}
	hosts, err := os.ReadFile(dirs.Hosts())
	if err == nil && bytes.Contains(hosts, []byte(pass)) {
		t.Fatal("hosts.json contains the password")
	}

	again, err := New(t.Context(), Options{
		Dirs: dirs, Locale: "en_US.UTF-8", Version: "test", Home: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(again.Close)
	got, err := again.Secrets.Get(t.Context(), ref)
	if err != nil || string(got) != pass {
		t.Fatalf("restored secret = %q, %v", got, err)
	}
}
