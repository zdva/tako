package hostfile_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wei-dong/tako/internal/hostlib"
	"github.com/wei-dong/tako/internal/importer/hostfile"
	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/store"
)

func openLibrary(t *testing.T) (*hostlib.Library, secrets.Store) {
	t.Helper()
	sec := secrets.NewMemory()
	lib, err := hostlib.Open(store.Doc{Path: filepath.Join(t.TempDir(), "hosts.json"), Version: hostlib.Version}, sec, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lib.Close)
	return lib, sec
}

// TestExportImportMovesHostsToAnotherComputerWithoutSecrets is acceptance 4
// of M4: the export carries no secret and no reference to one, and imports
// into a fresh library (another computer) with the jump chain intact.
func TestExportImportMovesHostsToAnotherComputerWithoutSecrets(t *testing.T) {
	t.Parallel()
	src, sec := openLibrary(t)
	const secret = "s3cr3t-P@ssw0rd-Sentinel"
	bastion, err := src.Add(model.Host{Name: "bastion", Addr: "203.0.113.1", User: "ops", Tags: []string{"prod"},
		Auth: []model.Auth{{Kind: model.AuthPassword, SecretRef: model.SecretRef("b1", "password")}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Set(t.Context(), bastion.Auth[0].SecretRef, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	if _, err := src.Add(model.Host{Name: "db", Addr: "10.0.0.5", User: "pg", Port: 5022, Jump: []model.Hop{{HostID: bastion.ID}},
		Proxy: &model.Proxy{Kind: model.ProxySOCKS5, Addr: "127.0.0.1:1080", User: "u", SecretRef: model.SecretRef("p1", "proxy")},
		Auth:  []model.Auth{{Kind: model.AuthKey, KeyPath: "~/.ssh/id_ed25519", SecretRef: model.SecretRef("k1", "passphrase")}}}); err != nil {
		t.Fatal(err)
	}
	src.SetTagColor("prod", "#E5484D")
	data, err := src.Export()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{secret, "secretRef", "host/h_", "keychain"} {
		if bytes.Contains(bytes.ToLower(data), bytes.ToLower([]byte(bad))) {
			t.Fatalf("the export contains %q:\n%s", bad, data)
		}
	}

	res, err := hostfile.Parse("tako-export.json", data, hostfile.Options{})
	if err != nil || res.Format != hostfile.FormatTako || len(res.Hosts) != 2 {
		t.Fatalf("%+v, %v", res, err)
	}
	dst, _ := openLibrary(t)
	added, skipped, err := dst.AddMany(res.Hosts)
	if err != nil || len(added) != 2 || skipped != 0 {
		t.Fatalf("AddMany: %d added, %d skipped, %v", len(added), skipped, err)
	}
	var db model.Host
	for _, h := range dst.Hosts() {
		if h.Name == "db" {
			db = h
		}
	}
	chain, err := model.ResolveJumps(db, dst.Get)
	if err != nil || len(chain) != 1 || chain[0].Name != "bastion" || chain[0].ID == bastion.ID {
		t.Fatalf("jump chain after import: %+v, %v", chain, err)
	}
	if db.Port != 5022 || db.Proxy == nil || db.Proxy.SecretRef != "" || db.Auth[0].KeyPath != "~/.ssh/id_ed25519" {
		t.Errorf("db: %+v", db)
	}
	// Importing the same file again adds nothing.
	if again, skipped, _ := dst.AddMany(res.Hosts); len(again) != 0 || skipped != 2 {
		t.Errorf("second import: %d added, %d skipped", len(again), skipped)
	}
	again, err := dst.Export()
	if err != nil || strings.Contains(string(again), secret) {
		t.Errorf("re-export leaks: %v", err)
	}
}

func TestTakoImportDropsHandEditedSecretRefs(t *testing.T) {
	t.Parallel()
	doc := `{"version":1,"hosts":[{"id":"h_1","name":"a","addr":"b","user":"c","auth":[{"kind":"password","secretRef":"host/h_9/password"}]},{"id":"h_2","name":"","addr":"x","user":"y"}]}`
	res, err := hostfile.Parse("x.json", []byte(doc), hostfile.Options{})
	if err != nil || len(res.Hosts) != 1 || res.Hosts[0].Auth[0].SecretRef != "" || len(res.Warnings) != 1 {
		t.Fatalf("%+v, %v", res, err)
	}
}
