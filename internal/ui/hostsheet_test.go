package ui_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wei-dong/tako/internal/model"
)

func openAddSheet(h *harness) {
	h.t.Helper()
	h.click("Add host")
	h.waitText("Test connection")
}

func fillBasics(h *harness, name string) {
	h.t.Helper()
	addr, port := h.addr()
	h.typeInto("Name", name)
	h.typeInto("Address", addr)
	h.typeInto("Port", "")
	// The port field starts at 22; select all and replace.
	h.tt.Key(ui.Ctrl, ui.KeyA)
	h.tt.Type(strconv.Itoa(port))
	h.typeInto("User", "tako")
}

func TestAddHost_PasswordTestConnectionAndSave(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	openAddSheet(h)
	fillBasics(h, "web-01")
	h.typeInto("Password", "pw")
	h.shot("add-host")

	h.click("Test connection")
	h.waitText("Trust and connect") // first contact: the host key prompt
	h.shot("hostkey-prompt")
	h.click("Trust and connect")
	h.waitFor("a successful test", func() bool {
		for _, s := range h.tt.Texts() {
			if strings.HasPrefix(s, "Connected in") {
				return true
			}
		}
		return false
	})
	h.shot("add-host-tested")

	h.click("Save and connect")
	h.waitFor("the host in the library", func() bool { return h.app.Library.Len() == 1 })
	h.waitFor("a connected block", func() bool { return h.connected(1) })

	saved := h.app.Library.Hosts()[0]
	if saved.Name != "web-01" || saved.User != "tako" || len(saved.Auth) != 1 || saved.Auth[0].Kind != model.AuthPassword {
		t.Fatalf("saved host = %+v", saved)
	}
	if got, err := h.app.Secrets.Get(t.Context(), saved.Auth[0].SecretRef); err != nil || string(got) != "pw" {
		t.Fatalf("the password was not kept in the keychain: %q, %v", got, err)
	}
	h.app.Library.Flush()
	data, err := os.ReadFile(h.dirs.Hosts())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"pw"`) {
		t.Errorf("hosts.json holds the password:\n%s", data)
	}
}

func TestAddHost_ValidationAndDuplicate(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.addHost("web-01")
	openAddSheet(h)
	h.click("Save and connect")
	h.waitText("Enter a name.")

	h.typeInto("Name", "web-01")
	h.typeInto("Address", "10.0.0.1")
	h.typeInto("User", "ops")
	h.click("Save and connect")
	h.waitText("Another host already has this name.")
	if h.app.Library.Len() != 1 {
		t.Fatalf("library has %d hosts", h.app.Library.Len())
	}
	h.shot("add-host-error")
}

func TestAddHost_WrongPasswordExplains(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	openAddSheet(h)
	fillBasics(h, "web-01")
	h.typeInto("Password", "nope")
	h.click("Test connection")
	h.waitText("Trust and connect")
	h.click("Trust and connect")
	// The saved password failed, so the dialer asks for the real one.
	const want = "The server refused every sign-in method. Check the user name, password or key."
	for range 5 {
		h.waitFor("a prompt or the error", func() bool { return h.tt.HasText("Password for web-01") || h.tt.HasText(want) })
		if h.tt.HasText(want) {
			return
		}
		h.typeInto("Password for web-01", "still wrong")
		h.click("Connect")
	}
	h.waitText(want)
}

func TestEditHost_KeepsSecretWhenLeftEmpty(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	host := h.addHost("web-01")
	if err := h.tt.RightClick("web-01"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if err := h.tt.ChooseMenuItem("Edit"); err != nil {
		t.Fatalf("%v; menu: %v", err, h.tt.Menu())
	}
	h.waitText("Edit host")
	h.typeInto("Name", "-renamed")
	h.click("Save and connect")
	h.waitFor("the rename", func() bool {
		got, _ := h.app.Library.Get(host.ID)
		return got.Name == "web-01-renamed"
	})
	got, _ := h.app.Library.Get(host.ID)
	if got.Auth[0].SecretRef != host.Auth[0].SecretRef {
		t.Fatalf("secret ref changed from %q to %q", host.Auth[0].SecretRef, got.Auth[0].SecretRef)
	}
}

// Acceptance: secrets are in the keychain only, never in hosts.json,
// session.json, the logs or an export.
func TestSecrets_NeverReachFilesLogsOrExport(t *testing.T) {
	const pass, proxyPass = "S3cr3t-Pa55-xyz", "Pr0xy-S3cret-xyz"
	h := newHarness(t, harnessOpts{})
	openAddSheet(h)
	fillBasics(h, "web-01")
	h.typeInto("Password", pass)
	h.tt.Frame()
	h.click("SOCKS5")
	h.typeInto("Proxy address", "127.0.0.1:9") // nothing listens: the connection fails and is logged
	h.typeInto("Proxy password", proxyPass)
	h.click("Save and connect")
	h.waitFor("saved", func() bool { return h.app.Library.Len() == 1 })
	h.waitFor("a block", func() bool { return len(h.app.Model.Blocks()) == 1 })
	time.Sleep(300 * time.Millisecond) // let the debounced save and a failed attempt happen

	saved := h.app.Library.Hosts()[0]
	for _, ref := range []string{saved.Auth[0].SecretRef, saved.Proxy.SecretRef} {
		if ref == "" {
			t.Fatalf("a secret has no keychain reference: %+v", saved)
		}
	}
	if got, err := h.app.Secrets.Get(t.Context(), saved.Auth[0].SecretRef); err != nil || string(got) != pass {
		t.Fatalf("password not in the keychain: %q %v", got, err)
	}
	if got, err := h.app.Secrets.Get(t.Context(), saved.Proxy.SecretRef); err != nil || string(got) != proxyPass {
		t.Fatalf("proxy password not in the keychain: %q %v", got, err)
	}

	export, err := h.app.Library.Export()
	if err != nil {
		t.Fatal(err)
	}
	h.stop()
	assertNoSecret(t, "export", export, pass, proxyPass)
	for _, dir := range []string{h.dirs.Config, h.dirs.State} {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err //nolint:wrapcheck // test walk
			}
			data, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr //nolint:wrapcheck // test walk
			}
			assertNoSecret(t, p, data, pass, proxyPass)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertNoSecret(t *testing.T, where string, data []byte, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(string(data), s) {
			t.Errorf("%s contains the secret %q", where, s)
		}
	}
}
