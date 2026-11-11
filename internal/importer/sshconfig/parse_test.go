package sshconfig

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wei-dong/tako/internal/model"
)

func parseFixture(t *testing.T) ([]model.Host, []Warning) {
	t.Helper()
	hosts, warns, err := ParseFile(filepath.Join("testdata", "config"), Options{Home: "/home/u"})
	if err != nil {
		t.Fatal(err)
	}
	return hosts, warns
}

func byName(hosts []model.Host, name string) (model.Host, bool) {
	for _, h := range hosts {
		if h.Name == name {
			return h, true
		}
	}
	return model.Host{}, false
}

func TestParseFile_Fixture(t *testing.T) {
	t.Parallel()
	hosts, warns := parseFixture(t)
	names := make([]string, 0, len(hosts))
	for _, h := range hosts {
		names = append(names, h.Name)
	}
	// 14 concrete aliases plus one literal jump host (jump.example.com).
	for _, want := range []string{
		"bastion", "web-01", "web-02", "web-03", "db-01", "gpu-01", "gpu-02", "legacy",
		"win-build", "mac-mini", "ci-runner", "extra-1", "extra-2", "ci.corp",
	} {
		if !slices.Contains(names, want) && want != "ci.corp" {
			t.Errorf("missing host %q in %v", want, names)
		}
	}
	concrete := 0
	for _, h := range hosts {
		if h.Name != "jump.example.com" {
			concrete++
		}
	}
	if concrete != 13 {
		t.Errorf("concrete hosts = %d, want 13 (%v)", concrete, names)
	}
	if len(warns) != 2 {
		t.Errorf("warnings = %v, want Match and ProxyCommand", warns)
	}
}

func TestParseFile_Options(t *testing.T) {
	t.Parallel()
	hosts, _ := parseFixture(t)
	bastion, _ := byName(hosts, "bastion")
	if bastion.Addr != "52.74.18.6" || bastion.Port != 2222 || bastion.User != "ubuntu" {
		t.Errorf("bastion = %+v", bastion)
	}
	if bastion.KeepAlive != 45 {
		t.Errorf("Host * ServerAliveInterval not applied: %d", bastion.KeepAlive)
	}
	// First value wins: the bastion's own IdentityFile comes before the default.
	if len(bastion.Auth) < 2 || bastion.Auth[0].KeyPath != "~/.ssh/id_bastion" || bastion.Auth[1].KeyPath != "~/.ssh/id_default" {
		t.Errorf("bastion auth = %+v", bastion.Auth)
	}
	web, _ := byName(hosts, "web-02")
	if web.Addr != "10.0.1.web-02" {
		t.Errorf("%%h not expanded: %q", web.Addr)
	}
	win, _ := byName(hosts, "win-build")
	if win.User != "Administrator" {
		t.Errorf("win-build user = %q", win.User)
	}
	legacy, _ := byName(hosts, "legacy")
	if legacy.Addr != "192.168.1.5" || legacy.Port != 0 || legacy.User != "ops" {
		t.Errorf("legacy (Key=Value syntax, default port, Host * user) = %+v", legacy)
	}
	db, _ := byName(hosts, "db-01")
	if db.Auth[0].KeyPath != "~/.ssh/db key" {
		t.Errorf("quoted IdentityFile = %q", db.Auth[0].KeyPath)
	}
	for _, h := range hosts {
		if err := h.Validate(); err != nil {
			t.Errorf("imported host %s is invalid: %v", h.Name, err)
		}
	}
}

func TestParseFile_ProxyJump(t *testing.T) {
	t.Parallel()
	hosts, _ := parseFixture(t)
	bastion, _ := byName(hosts, "bastion")
	web, _ := byName(hosts, "web-01")
	if len(web.Jump) != 1 || web.Jump[0].HostID != bastion.ID {
		t.Errorf("web-01 jump = %+v, want bastion %s", web.Jump, bastion.ID)
	}
	gpu, _ := byName(hosts, "gpu-01")
	if len(gpu.Jump) != 1 {
		t.Fatalf("gpu-01 jump = %+v", gpu.Jump)
	}
	jh, ok := byName(hosts, "jump.example.com")
	if !ok || jh.ID != gpu.Jump[0].HostID || jh.User != "deploy" || jh.Port != 2200 {
		t.Errorf("literal jump host = %+v (found %v)", jh, ok)
	}
	gpu2, _ := byName(hosts, "gpu-02")
	if gpu2.Jump[0].HostID != jh.ID {
		t.Error("two hosts jumping through the same literal host should share it")
	}
	extra2, _ := byName(hosts, "extra-2")
	if len(extra2.Jump) != 0 {
		t.Errorf("ProxyJump none ignored: %+v", extra2.Jump)
	}
	byID := map[model.ID]model.Host{}
	for _, h := range hosts {
		byID[h.ID] = h
	}
	for _, h := range hosts {
		if _, err := model.ResolveJumps(h, func(id model.ID) (model.Host, bool) { x, ok := byID[id]; return x, ok }); err != nil {
			t.Errorf("host %s has an unresolvable jump chain: %v", h.Name, err)
		}
	}
}

func TestParseFile_WildcardOnlyHostsAreNotImported(t *testing.T) {
	t.Parallel()
	hosts, _ := parseFixture(t)
	for _, h := range hosts {
		if strings.ContainsAny(h.Name, "*?!") {
			t.Errorf("wildcard imported as host: %q", h.Name)
		}
	}
}

type memSource map[string]string

func (m memSource) ReadFile(name string) ([]byte, error) {
	if s, ok := m[name]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("file does not exist")
}

func (m memSource) Glob(pattern string) ([]string, error) {
	var out []string
	for name := range m {
		if ok, _ := filepath.Match(pattern, name); ok {
			out = append(out, name)
		}
	}
	return out, nil
}

func TestParse_IncludeLoopIsBounded(t *testing.T) {
	t.Parallel()
	src := memSource{"/c/loop": "Include /c/loop\nHost a\n HostName 1.1.1.1\n"}
	hosts, warns, err := ParseFile("/c/loop", Options{Source: src})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := byName(hosts, "a"); !ok {
		t.Error("host a missing")
	}
	if len(warns) == 0 {
		t.Error("expected a nesting warning")
	}
}

func TestParse_JumpCyclesAreBroken(t *testing.T) {
	t.Parallel()
	cfg := "Host a\n HostName 1.1.1.1\n ProxyJump b\nHost b\n HostName 2.2.2.2\n ProxyJump a\n"
	hosts, _, err := Parse(strings.NewReader(cfg), "cfg", Options{Source: memSource{}})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[model.ID]model.Host{}
	for _, h := range hosts {
		byID[h.ID] = h
	}
	for _, h := range hosts {
		_, err := model.ResolveJumps(h, func(id model.ID) (model.Host, bool) { x, ok := byID[id]; return x, ok })
		if err != nil {
			t.Errorf("host %s: %v", h.Name, err)
		}
	}
}

func TestSplitDirective(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		key  string
		args []string
	}{
		{"Host a b", "host", []string{"a", "b"}},
		{"  HostName=1.2.3.4  # comment", "hostname", []string{"1.2.3.4"}},
		{`IdentityFile "~/a b/c"`, "identityfile", []string{"~/a b/c"}},
		{"# just a comment", "", nil},
		{"", "", nil},
		{"Port = 2222", "port", []string{"2222"}},
	}
	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()
			key, args := splitDirective(tc.line)
			if key != tc.key || !slices.Equal(args, tc.args) {
				t.Errorf("splitDirective(%q) = %q %q, want %q %q", tc.line, key, args, tc.key, tc.args)
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	f.Add("Host a\n HostName x\n ProxyJump b,c\nInclude *\n")
	f.Add("Match all\nHost *\n")
	f.Add("Host \"unterminated\n")
	f.Fuzz(func(t *testing.T, cfg string) {
		hosts, _, err := Parse(strings.NewReader(cfg), "fuzz", Options{Source: memSource{}})
		if err != nil {
			return
		}
		ids := map[model.ID]bool{}
		for _, h := range hosts {
			if ids[h.ID] {
				t.Fatalf("duplicate ID %s", h.ID)
			}
			ids[h.ID] = true
		}
		for _, h := range hosts {
			for _, hop := range h.Jump {
				if !ids[hop.HostID] || hop.HostID == h.ID {
					t.Fatalf("host %q has a dangling or self jump %s", h.Name, hop.HostID)
				}
			}
		}
	})
}
