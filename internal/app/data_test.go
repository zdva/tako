package app

import (
	"testing"

	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/secrets"
	"github.com/zdva/tako/internal/settings"
	"github.com/zdva/tako/internal/store"
)

func TestLookup_AppliesTheKeepaliveSetting(t *testing.T) {
	t.Parallel()
	a, err := New(t.Context(), Options{
		Dirs: store.Dirs{Config: t.TempDir(), State: t.TempDir()}, Keychain: secrets.NewMemory(),
		Locale: "en_US.UTF-8", Version: "test", Home: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	own, err := a.Library.Add(model.Host{Name: "own", Addr: "10.0.0.1", Port: 22, User: "u", KeepAlive: 7})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := a.Library.Add(model.Host{Name: "plain", Addr: "10.0.0.2", Port: 22, User: "u"})
	if err != nil {
		t.Fatal(err)
	}
	if h, _ := a.lookup(plain.ID); h.KeepAlive != 30 {
		t.Errorf("default keepalive %d, want 30", h.KeepAlive)
	}
	if err := a.Settings.Update(func(v *settings.Values) { v.Connection.KeepAliveSec = 90 }); err != nil {
		t.Fatal(err)
	}
	if h, _ := a.lookup(plain.ID); h.KeepAlive != 90 {
		t.Errorf("keepalive %d after the setting changed, want 90", h.KeepAlive)
	}
	if h, _ := a.lookup(own.ID); h.KeepAlive != 7 {
		t.Errorf("a host's own keepalive was overridden: %d", h.KeepAlive)
	}
	if got, _ := a.Library.Get(plain.ID); got.KeepAlive != 0 {
		t.Errorf("the default leaked into the saved host: %d", got.KeepAlive)
	}
}
