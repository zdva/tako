package hostfile_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/zdva/tako/internal/importer/hostfile"
	"github.com/zdva/tako/internal/model"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func parse(t *testing.T, name string) hostfile.Result {
	t.Helper()
	res, err := hostfile.Parse(name, read(t, name), hostfile.Options{})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func byName(t *testing.T, res hostfile.Result, name string) model.Host {
	t.Helper()
	for _, h := range res.Hosts {
		if h.Name == name {
			return h
		}
	}
	t.Fatalf("no host %q in %v", name, names(res))
	return model.Host{}
}

func names(res hostfile.Result) []string {
	var out []string
	for _, h := range res.Hosts {
		out = append(out, h.Name)
	}
	return out
}

func TestXshell_UTF16Session(t *testing.T) {
	t.Parallel()
	res := parse(t, "web-01.xsh")
	if res.Format != hostfile.FormatXshell || len(res.Hosts) != 1 {
		t.Fatalf("%+v", res)
	}
	h := res.Hosts[0]
	if h.Name != "web-01" || h.Addr != "10.0.1.11" || h.User != "deploy" || h.Port != 0 || h.Note != "Web 服务器 №1" {
		t.Errorf("host: %+v", h)
	}
	if len(h.Auth) != 1 || h.Auth[0].Kind != model.AuthPassword || h.Auth[0].SecretRef != "" {
		t.Errorf("auth: %+v", h.Auth)
	}
	if res.Passwords != 1 {
		t.Errorf("passwords counted: %d", res.Passwords)
	}
	if strings.Contains(strings.Join(res.Warnings, "|"), "Tm8w") {
		t.Error("a password leaked into a warning")
	}
}

func TestXshell_LegacyAnsiSession(t *testing.T) {
	t.Parallel()
	res := parse(t, "legacy.xsh")
	h := res.Hosts[0]
	if h.Addr != "192.168.0.5" || h.Port != 2222 || h.User != "root" {
		t.Errorf("host: %+v", h)
	}
	if h.Auth[0].Kind != model.AuthAgent || len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "mykey") {
		t.Errorf("key auth: %+v %q", h.Auth, res.Warnings)
	}
}

func TestXshell_ArchiveKeepsFoldersAsTagsAndSkipsWhatItCannotUse(t *testing.T) {
	t.Parallel()
	res := parse(t, "sessions.xts")
	if len(res.Hosts) != 2 {
		t.Fatalf("hosts: %v", names(res))
	}
	web := byName(t, res, "web-02")
	if !slices.Equal(web.Tags, []string{"Production", "Web"}) || web.Auth[0].Kind != model.AuthKeyboardInteractive {
		t.Errorf("web-02: %+v", web)
	}
	if db := byName(t, res, "db-01"); db.Port != 5022 || !slices.Equal(db.Tags, []string{"Production"}) {
		t.Errorf("db-01: %+v", db)
	}
	all := strings.Join(res.Warnings, "\n")
	for _, want := range []string{"TELNET sessions are not supported", "no user name", "corp-socks"} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings lack %q:\n%s", want, all)
		}
	}
}

func TestXshell_DefaultUserFillsMissingLogin(t *testing.T) {
	t.Parallel()
	res, err := hostfile.Parse("sessions.xts", read(t, "sessions.xts"), hostfile.Options{DefaultUser: "me"})
	if err != nil {
		t.Fatal(err)
	}
	if h := byName(t, res, "proxied"); h.User != "me" {
		t.Errorf("proxied: %+v", h)
	}
}

func TestXshell_Folder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Lab"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Lab", "web-01.xsh"), read(t, "web-01.xsh"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := hostfile.Load(dir, hostfile.Options{})
	if err != nil || len(res.Hosts) != 1 || !slices.Equal(res.Hosts[0].Tags, []string{"Lab"}) {
		t.Fatalf("%+v, %v", res, err)
	}
	if _, err := hostfile.Load(t.TempDir(), hostfile.Options{}); err == nil {
		t.Error("an empty folder imported")
	}
}

func TestSecureCRT_Sessions(t *testing.T) {
	t.Parallel()
	one := parse(t, "web-01.ini")
	h := one.Hosts[0]
	if one.Format != hostfile.FormatSecureCRT || h.Addr != "10.1.1.1" || h.User != "ubuntu" || h.Port != 0 {
		t.Errorf("web-01: %+v", h)
	}
	if h.Auth[0].Kind != model.AuthKey || !strings.HasSuffix(h.Auth[0].KeyPath, "id_ed25519") || one.Passwords != 1 {
		t.Errorf("auth %+v passwords %d", h.Auth, one.Passwords)
	}
	db := parse(t, "db-01.ini")
	if p := db.Hosts[0].Port; p != 5000 {
		t.Errorf("hex port 00001388 = %d", p)
	}
	all := strings.Join(db.Warnings, "\n")
	if !strings.Contains(all, "Corp Gateway") || !strings.Contains(all, "variables") {
		t.Errorf("warnings: %s", all)
	}
	if db.Hosts[0].Auth[0].Kind != model.AuthPassword {
		t.Errorf("a variable-based identity became a key path: %+v", db.Hosts[0].Auth)
	}
	if _, err := hostfile.Parse("telnet.ini", read(t, "telnet.ini"), hostfile.Options{}); err == nil {
		t.Error("a telnet-only file imported hosts")
	}
}

func TestSecureCRT_XMLExport(t *testing.T) {
	t.Parallel()
	res := parse(t, "securecrt-export.xml")
	if len(res.Hosts) != 2 {
		t.Fatalf("hosts: %v %q", names(res), res.Warnings)
	}
	web := byName(t, res, "web-03")
	if !slices.Equal(web.Tags, []string{"Production", "Web"}) || web.Addr != "10.2.0.3" || web.Port != 0 || res.Passwords != 1 {
		t.Errorf("web-03: %+v (%d passwords)", web, res.Passwords)
	}
	if db := byName(t, res, "db-02"); db.Port != 2200 || !slices.Equal(db.Tags, []string{"Production"}) {
		t.Errorf("db-02: %+v", db)
	}
}

func TestTermius_CSV(t *testing.T) {
	t.Parallel()
	res := parse(t, "termius.csv")
	if res.Format != hostfile.FormatTermius {
		t.Fatalf("format %s", res.Format)
	}
	if len(res.Hosts) != 3 {
		t.Fatalf("hosts %v\n%q", names(res), res.Warnings)
	}
	web := byName(t, res, "web-01")
	if !slices.Equal(web.Tags, []string{"Production", "Web", "prod", "nginx"}) || web.Addr != "203.0.113.11" || web.User != "deploy" {
		t.Errorf("web-01: %+v", web)
	}
	if db := byName(t, res, "db, primary"); db.Port != 5432 || db.User != "postgres" {
		t.Errorf("db: %+v", db)
	}
	if h := byName(t, res, "198.51.100.7"); h.Port != 0 {
		t.Errorf("bad port kept: %+v", h)
	}
	if res.Passwords != 1 {
		t.Errorf("passwords: %d", res.Passwords)
	}
	all := strings.Join(res.Warnings, "\n")
	for _, want := range []string{"telnet hosts are not supported", `port "notaport"`, "no user name"} {
		if !strings.Contains(all, want) {
			t.Errorf("warnings lack %q:\n%s", want, all)
		}
	}
	for _, h := range res.Hosts {
		if strings.Contains(strings.Join(res.Warnings, "")+h.Note, "hunter2") {
			t.Error("password leaked")
		}
	}
}

func TestDetectAndErrors(t *testing.T) {
	t.Parallel()
	if _, err := hostfile.Parse("x.txt", []byte("hello world"), hostfile.Options{}); err == nil {
		t.Error("garbage accepted")
	}
	if _, err := hostfile.Parse("x.csv", []byte("a,b\n1,2\n"), hostfile.Options{}); err == nil {
		t.Error("an unrelated CSV accepted")
	}
	if _, err := hostfile.Parse("x.csv", []byte("Label,Hostname/IP,Username\n"), hostfile.Options{}); err == nil {
		t.Error("a CSV with no rows accepted")
	}
	big := make([]byte, hostfile.MaxFileSize+1)
	if _, err := hostfile.Parse("big", big, hostfile.Options{}); err == nil {
		t.Error("a huge file accepted")
	}
	if _, err := hostfile.Parse("broken.xts", []byte("PK\x03\x04broken"), hostfile.Options{}); err == nil {
		t.Error("a broken archive accepted")
	}
	if _, err := hostfile.Load(filepath.Join(t.TempDir(), "missing"), hostfile.Options{}); err == nil {
		t.Error("a missing file loaded")
	}
}

func FuzzParse(f *testing.F) {
	for _, name := range []string{"web-01.xsh", "legacy.xsh", "sessions.xts", "web-01.ini", "securecrt-export.xml", "termius.csv"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(name, data)
	}
	f.Add("x.json", []byte(`{"version":1,"hosts":[{"name":"a","addr":"b","user":"c","auth":[{"kind":"password","secretRef":"host/x/password"}]}]}`))
	f.Fuzz(func(t *testing.T, name string, data []byte) {
		res, err := hostfile.Parse(name, data, hostfile.Options{DefaultUser: "u"})
		if err != nil {
			return
		}
		for _, h := range res.Hosts {
			if verr := h.Validate(); verr != nil {
				t.Fatalf("imported an invalid host %+v: %v", h, verr)
			}
			for _, a := range h.Auth {
				if a.SecretRef != "" {
					t.Fatalf("imported a secret reference: %+v", h)
				}
			}
		}
	})
}
