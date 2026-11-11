package hostlib

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/secrets"
	"github.com/wei-dong/tako/internal/store"
)

func open(t *testing.T) (*Library, *secrets.Memory, string) {
	t.Helper()
	return openAt(t, filepath.Join(t.TempDir(), "hosts.json"))
}

func openAt(t *testing.T, path string) (*Library, *secrets.Memory, string) {
	t.Helper()
	sec := secrets.NewMemory()
	l, err := Open(store.Doc{Path: path}, sec, func(err error) { t.Errorf("save error: %v", err) })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l, sec, path
}

func host(name string, tags ...string) model.Host {
	return model.Host{
		Name: name, Addr: name + ".example.com", User: "ubuntu", Tags: tags,
		Auth: []model.Auth{{Kind: model.AuthAgent}},
	}
}

func TestLibrary_AddGetUpdateDelete(t *testing.T) {
	t.Parallel()
	l, _, _ := open(t)
	ctx := t.Context()
	h, err := l.Add(host("web-01", "prod"))
	if err != nil {
		t.Fatal(err)
	}
	if h.ID == "" || h.OS != model.OSLinux {
		t.Errorf("Add did not fill defaults: %+v", h)
	}
	got, ok := l.Get(h.ID)
	if !ok || got.Name != "web-01" {
		t.Fatalf("Get = %+v, %v", got, ok)
	}
	h.Addr = "10.0.0.1"
	if err := l.Update(h); err != nil {
		t.Fatal(err)
	}
	if got, _ := l.Get(h.ID); got.Addr != "10.0.0.1" {
		t.Errorf("Update did not stick: %+v", got)
	}
	if err := l.Delete(ctx, h.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Get(h.ID); ok {
		t.Error("host still there after Delete")
	}
	if err := l.Delete(ctx, h.ID); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("second Delete err = %v", err)
	}
}

func TestLibrary_AddRejectsInvalidAndDuplicate(t *testing.T) {
	t.Parallel()
	l, _, _ := open(t)
	if _, err := l.Add(model.Host{Name: "x"}); !errors.Is(err, model.ErrAddrEmpty) {
		t.Errorf("err = %v, want ErrAddrEmpty", err)
	}
	if _, err := l.Add(host("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Add(host("A")); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("err = %v, want ErrDuplicateName", err)
	}
}

func TestLibrary_PersistsAndReloads(t *testing.T) {
	t.Parallel()
	l, _, path := open(t)
	if _, err := l.Add(host("web-01", "prod")); err != nil {
		t.Fatal(err)
	}
	l.Close()
	l2, err := Open(store.Doc{Path: path}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Close()
	if hs := l2.Hosts(); len(hs) != 1 || hs[0].Name != "web-01" {
		t.Errorf("reloaded hosts = %+v", hs)
	}
	if tags := l2.Tags(); len(tags) != 1 || tags[0].Name != "prod" || tags[0].Color == "" {
		t.Errorf("reloaded tags = %+v", tags)
	}
}

func TestLibrary_DeleteRemovesSecretsAndGuardsJumpHosts(t *testing.T) {
	t.Parallel()
	l, sec, _ := open(t)
	ctx := t.Context()
	bastion := host("bastion")
	bastion.Auth = []model.Auth{{Kind: model.AuthPassword, SecretRef: "ref/bastion"}}
	b, err := l.Add(bastion)
	if err != nil {
		t.Fatal(err)
	}
	if err := sec.Set(ctx, "ref/bastion", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	target := host("target")
	target.Jump = []model.Hop{{HostID: b.ID}}
	tgt, err := l.Add(target)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(ctx, b.ID); !errors.Is(err, ErrInUse) {
		t.Fatalf("Delete jump host err = %v, want ErrInUse", err)
	}
	if err := l.Delete(ctx, tgt.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.Delete(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if refs := sec.Refs(); len(refs) != 0 {
		t.Errorf("secrets left behind: %v", refs)
	}
}

func TestLibrary_UpdateDropsOrphanedSecret(t *testing.T) {
	t.Parallel()
	l, sec, _ := open(t)
	ctx := t.Context()
	h := host("a")
	h.Auth = []model.Auth{{Kind: model.AuthPassword, SecretRef: "ref/a"}}
	h, err := l.Add(h)
	if err != nil {
		t.Fatal(err)
	}
	_ = sec.Set(ctx, "ref/a", []byte("pw"))
	h.Auth = []model.Auth{{Kind: model.AuthAgent}}
	if err := l.Update(h); err != nil {
		t.Fatal(err)
	}
	if refs := sec.Refs(); len(refs) != 0 {
		t.Errorf("orphaned secret kept: %v", refs)
	}
}

func TestLibrary_Search(t *testing.T) {
	t.Parallel()
	l, _, _ := open(t)
	for _, h := range []model.Host{host("web-01", "prod"), host("web-02", "prod"), host("gpu-01", "gpu", "prod")} {
		if _, err := l.Add(h); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		query string
		want  int
	}{
		{"", 3},
		{"web", 2},
		{"WEB-01", 1},
		{"#gpu", 1},
		{"#prod", 3},
		{"web #prod", 2},
		{"ubuntu", 3},
		{"nothing", 0},
		{"#web", 0},
	}
	for _, tc := range tests {
		t.Run(tc.query, func(t *testing.T) {
			t.Parallel()
			if got := len(l.Search(tc.query)); got != tc.want {
				t.Errorf("Search(%q) = %d hosts, want %d", tc.query, got, tc.want)
			}
		})
	}
}

func TestLibrary_GroupsByFirstTagWithUntaggedLast(t *testing.T) {
	t.Parallel()
	l, _, _ := open(t)
	for _, h := range []model.Host{host("loose"), host("web-01", "prod"), host("gpu-01", "gpu", "prod")} {
		if _, err := l.Add(h); err != nil {
			t.Fatal(err)
		}
	}
	groups := l.Groups("")
	var names []string
	for _, g := range groups {
		names = append(names, g.Tag.Name+":"+strings.Join(hostNames(g.Hosts), ","))
	}
	want := "prod:web-01 gpu:gpu-01 :loose"
	if got := strings.Join(names, " "); got != want {
		t.Errorf("groups = %q, want %q", got, want)
	}
}

func hostNames(hs []model.Host) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Name
	}
	return out
}

func TestLibrary_AddManySkipsDuplicatesAndRemapsJumps(t *testing.T) {
	t.Parallel()
	l, _, _ := open(t)
	existing, err := l.Add(host("bastion"))
	if err != nil {
		t.Fatal(err)
	}
	in := []model.Host{
		{ID: "imp1", Name: "bastion", Addr: existing.Addr, User: existing.User},
		{ID: "imp2", Name: "inner", Addr: "10.0.0.5", User: "root", Jump: []model.Hop{{HostID: "imp1"}}},
		{ID: "imp3", Name: "inner", Addr: "10.0.0.6", User: "root"},
	}
	added, skipped, err := l.AddMany(in)
	if err != nil {
		t.Fatal(err)
	}
	if skipped != 1 || len(added) != 2 {
		t.Fatalf("added %d skipped %d", len(added), skipped)
	}
	if added[0].Jump[0].HostID != existing.ID {
		t.Errorf("jump not remapped: %+v", added[0].Jump)
	}
	if added[1].Name != "inner (2)" {
		t.Errorf("duplicate name not made unique: %q", added[1].Name)
	}
}

func TestLibrary_ExportHasNoSecretRefs(t *testing.T) {
	t.Parallel()
	l, _, _ := open(t)
	h := host("a")
	h.Auth = []model.Auth{{Kind: model.AuthPassword, SecretRef: "host/h_1/password"}}
	h.Proxy = &model.Proxy{Kind: model.ProxySOCKS5, Addr: "p:1080", SecretRef: "proxy/h_1"}
	if _, err := l.Add(h); err != nil {
		t.Fatal(err)
	}
	data, err := l.Export()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secretRef") || strings.Contains(string(data), "host/h_1") {
		t.Errorf("export leaks keychain references:\n%s", data)
	}
}

func TestLibrary_OnChangeFires(t *testing.T) {
	t.Parallel()
	l, _, _ := open(t)
	n := 0
	l.OnChange(func() { n++ })
	if _, err := l.Add(host("a")); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("OnChange fired %d times, want 1", n)
	}
}
