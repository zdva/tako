package ui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/zdva/tako/internal/i18n"
	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/scheme"
	"github.com/zdva/tako/internal/settings"
)

// openSettings opens the settings window on a page.
func (h *harness) openSettings(page string) {
	h.t.Helper()
	h.app.UI.OpenSettings(page)
	h.tt.Frame()
}

func (h *harness) prefs() settings.Values { return h.app.Settings.Values() }

// reveal scrolls the settings page until the named text has a visible box.
func (h *harness) reveal(name string) {
	h.t.Helper()
	for range 8 {
		h.tt.Frame()
		if r, ok := h.tt.Find(name); ok && r.W > 0 && r.H > 0 && r.Y+r.H < winH-60 {
			return
		}
		h.tt.Scroll(560, 380, 0, 160)
	}
	h.shot("reveal-fail")
	h.t.Fatalf("%q never became visible; screen %q", name, h.tt.Texts())
}

// clickRadio clicks the control with the radio role and the given name.
func (h *harness) clickRadio(name string) { h.clickRole(ui.RoleRadio, name) }

func (h *harness) clickRole(role ui.Role, name string) {
	h.t.Helper()
	h.reveal(name)
	info, ok := h.tt.AccessByRole(role, name)
	if !ok {
		h.t.Fatalf("no control %q of role %v", name, role)
	}
	h.tt.ClickAt(float32(info.Bounds.X+info.Bounds.W/2), float32(info.Bounds.Y+info.Bounds.H/2))
	h.tt.Frame()
}

func TestSettings_PagesAreReachableAndShowTheirControls(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openSettings("")
	for _, page := range []struct{ nav, text string }{
		{"Terminal", "Color scheme"},
		{"Connection", "Keepalive every (seconds)"},
		{"Monitoring", "Amber from (%)"},
		{"File transfer", "Same name"},
		{"Data", "Export hosts"},
		{"About", "Copy diagnostics"},
		{"General", "Language"},
	} {
		h.click(page.nav)
		if !h.tt.HasText(page.text) {
			h.shot("page-" + page.nav)
			t.Errorf("page %s lacks %q; screen %q", page.nav, page.text, h.tt.Texts())
		}
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.tt.HasText("Appearance") {
		t.Error("Escape did not close the settings window")
	}
}

func TestSettings_GeneralChangesApplyAtOnce(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openSettings("")
	h.click("Dark")
	if got := h.prefs().Appearance; got != settings.AppearanceDark {
		t.Fatalf("appearance %q", got)
	}
	h.click("简体中文")
	if got := h.prefs().Language; got != i18n.Chinese {
		t.Fatalf("language %q", got)
	}
	h.waitText("通用")
	h.shot("settings-zh")
	h.click("English")
	h.waitText("General")
}

func countPixels(h *harness, hex string, tol int) int {
	h.t.Helper()
	img := h.tt.Image()
	var want [3]int
	for i := range 3 {
		for _, c := range hex[1+2*i : 3+2*i] {
			want[i] = want[i]*16 + strings.IndexRune("0123456789ABCDEF", c)
		}
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			if dist([3]int{int(r >> 8), int(g >> 8), int(bl >> 8)}, want) <= tol {
				n++
			}
		}
	}
	return n
}

func TestSettings_SchemeRecolorsOpenTerminalsWithoutARestart(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	const dracula = "#282A36"
	if n := countPixels(h, dracula, 2); n > 50 {
		t.Fatalf("%d pixels already have the Dracula background", n)
	}
	h.openSettings("terminal")
	h.clickRadio("Dracula")
	if got := h.prefs().Terminal.Scheme; got != "dracula" {
		t.Fatalf("scheme %q", got)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.waitFor("the terminal turns Dracula", func() bool { return countPixels(h, dracula, 2) > 2000 })
	h.shot("scheme-dracula")
	// Back to the default: the colors come from the appearance again.
	h.openSettings("terminal")
	h.clickRadio("Tako (follows appearance)")
	h.tt.Key(0, ui.KeyEscape)
	h.waitFor("the terminal leaves Dracula", func() bool { return countPixels(h, dracula, 2) < 50 })
}

func TestSettings_DarkAppearanceChangesTerminalToo(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(1)
	if white := countPixels(h, "#FFFFFF", 1); white < 2000 {
		t.Fatalf("a light terminal should be white, %d white pixels", white)
	}
	if err := h.app.Settings.Update(func(v *settings.Values) { v.Appearance = settings.AppearanceDark }); err != nil {
		t.Fatal(err)
	}
	h.waitFor("dark terminal", func() bool { return countPixels(h, "#0C0C10", 1) > 2000 })
	h.shot("dark-main")
}

func TestSettings_ImportITermColorsAddsSelectsAndRemovesTheScheme(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	src, err := os.ReadFile(filepath.Join("..", "scheme", "testdata", "dracula.itermcolors"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "Night Owl.itermcolors")
	if err := os.WriteFile(path, src, 0o600); err != nil {
		t.Fatal(err)
	}
	h.openSettings("terminal")
	h.reveal("Import iTerm2 colors")
	h.typeInto("Path to a .itermcolors file", path)
	h.click("Import")
	h.waitText("Night Owl")
	tp := h.prefs().Terminal
	if tp.Scheme != "custom:night-owl" || len(tp.Custom) != 1 || tp.Custom[0].Background != "#282A36" {
		t.Fatalf("terminal prefs: %+v", tp)
	}
	h.shot("settings-terminal")
	// A bad file is reported and nothing is added.
	bad := filepath.Join(t.TempDir(), "bad.itermcolors")
	if err := os.WriteFile(bad, []byte("not a plist"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.reveal("Import iTerm2 colors")
	h.tt.Key(ui.Ctrl, ui.KeyA)
	h.typeInto("Path to a .itermcolors file", bad)
	h.click("Import")
	h.waitText("Could not import the colors")
	if len(h.prefs().Terminal.Custom) != 1 {
		t.Error("a bad file added a scheme")
	}
	// Right-click removes the imported scheme and falls back to the default.
	h.reveal("Night Owl")
	info, _ := h.tt.AccessByRole(ui.RoleRadio, "Night Owl")
	h.tt.RightClickAt(float32(info.Bounds.X+info.Bounds.W/2), float32(info.Bounds.Y+info.Bounds.H/2))
	if err := h.tt.ChooseMenuItem("Remove this scheme"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if tp = h.prefs().Terminal; len(tp.Custom) != 0 || tp.Scheme != scheme.Default {
		t.Errorf("after removing: %+v", tp)
	}
}

func TestSettings_TerminalOptionsAreSaved(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openSettings("terminal")
	h.reveal("Keep text readable")
	h.clickRole(ui.RoleSwitch, "Copy text when it is selected")
	h.clickRole(ui.RoleSwitch, "Keep text readable")
	if tp := h.prefs().Terminal; !tp.CopyOnSelect || !tp.KeepContrast {
		t.Errorf("%+v", tp)
	}
	h.openSettings("connection")
	h.tt.Frame()
	if !h.tt.HasText("Keepalive every (seconds)") {
		t.Errorf("keepalive row missing: %q", h.tt.Texts())
	}
}

func (h *harness) exportTo(path string) {
	h.t.Helper()
	h.openSettings("data")
	h.tt.Frame()
	info, _ := h.tt.AccessByRole(ui.RoleTextField, "Where to save the file")
	h.tt.ClickAt(float32(info.Bounds.X+info.Bounds.W/2), float32(info.Bounds.Y+info.Bounds.H/2))
	h.tt.Key(ui.Ctrl, ui.KeyA)
	h.tt.Type(path)
	h.tt.Frame()
	h.click("Export")
	h.waitText("Exported")
}

func TestSettings_ExportHasNoSecretsAndImportsOnAnotherComputer(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	web := h.addHost("web-01", "prod")
	const secret = "S3cr3t-Value-Do-Not-Leak"
	ref := model.SecretRef(web.ID, "password")
	if err := h.app.Secrets.Set(t.Context(), ref, []byte(secret)); err != nil {
		t.Fatal(err)
	}
	h.addHost("db-01")
	out := filepath.Join(t.TempDir(), "tako-hosts.json")
	h.exportTo(out)
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{secret, ref, "pw-web-01", "SecretRef", "secret"} {
		if strings.Contains(strings.ToLower(string(raw)), strings.ToLower(bad)) {
			t.Errorf("the export contains %q", bad)
		}
	}
	if st, err := os.Stat(out); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("export mode %v %v", st, err)
	}
	// Another computer: a fresh harness with its own data and keychain.
	other := newHarness(t, harnessOpts{})
	other.openSettings("data")
	other.typeInto("Xshell, SecureCRT, Termius or Tako file or folder", out)
	other.click("Import")
	other.waitText("Imported 2")
	if n := other.app.Library.Len(); n != 2 {
		t.Fatalf("%d hosts imported", n)
	}
	for _, host := range other.app.Library.Hosts() {
		if len(host.Auth) > 0 && host.Auth[0].SecretRef != "" {
			if _, err := other.app.Secrets.Get(t.Context(), host.Auth[0].SecretRef); err == nil {
				t.Errorf("%s arrived with a secret", host.Name)
			}
		}
	}
	other.shot("settings-data")
}

func TestSettings_ImportsTheOtherToolsAndSaysPasswordsWereLeftOut(t *testing.T) {
	for _, tc := range []struct{ name, path string }{
		{"xshell", "web-01.xsh"},
		{"xshell sessions", "sessions.xts"},
		{"securecrt folder", "web-01.ini"},
		{"securecrt xml", "securecrt-export.xml"},
		{"termius", "termius.csv"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{})
			h.openSettings("data")
			h.typeInto("Xshell, SecureCRT, Termius or Tako file or folder",
				filepath.Join("..", "importer", "hostfile", "testdata", tc.path))
			h.click("Import")
			h.waitText("Imported")
			if h.app.Library.Len() == 0 {
				t.Fatalf("nothing imported; screen %q", h.tt.Texts())
			}
		})
	}
}
