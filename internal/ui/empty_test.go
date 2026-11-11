package ui_test

import (
	"path/filepath"
	"testing"
)

func TestEmptyState_OffersImportAndLocalTerminal(t *testing.T) {
	h := newHarness(t, harnessOpts{sshConfig: filepath.Join("..", "importer", "sshconfig", "testdata", "config")})
	for _, s := range []string{"Welcome to Tako", "Add host", "New local terminal", "OpenSSH config"} {
		if !h.tt.HasText(s) {
			t.Errorf("missing %q; screen: %v", s, h.tt.Texts())
		}
	}
	h.shot("empty-state")
}

func TestEmptyState_ChineseFollowsLocale(t *testing.T) {
	h := newHarness(t, harnessOpts{locale: "zh_CN.UTF-8"})
	if !h.tt.HasText("欢迎使用 Tako") {
		t.Fatalf("not Chinese: %v", h.tt.Texts())
	}
}

func TestImport_AddsHostsFromSSHConfig(t *testing.T) {
	h := newHarness(t, harnessOpts{sshConfig: filepath.Join("..", "importer", "sshconfig", "testdata", "config")})
	h.click("OpenSSH config")
	if n := h.app.Library.Len(); n < 12 {
		t.Fatalf("imported %d hosts, want at least 12", n)
	}
	h.tt.Frame()
	for _, name := range []string{"extra-1", "bastion", "web-01", "db-01"} {
		if !h.tt.HasText(name) {
			t.Errorf("host %s is not in the sidebar: %q", name, h.tt.Texts())
		}
	}
	h.shot("imported")
	// Importing again adds nothing: the hosts already exist.
	n := h.app.Library.Len()
	h.click("OpenSSH config")
	if got := h.app.Library.Len(); got != n {
		t.Fatalf("a second import changed the library from %d to %d hosts", n, got)
	}
}
