package ui_test

import (
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"go.uber.org/goleak"

	"github.com/zdva/tako/internal/app"
	"github.com/zdva/tako/internal/model"
	"github.com/zdva/tako/internal/store"
	"github.com/zdva/tako/internal/testutil/memssh"
	"github.com/zdva/tako/internal/transfer"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// MyGo's rasterizer keeps a pool of workers for the life of the process.
		goleak.IgnoreAnyFunction("github.com/egoist/mygo/internal/raster.help"),
	)
}

const winW, winH = 1280, 800

type harness struct {
	t    *testing.T
	app  *app.App
	tt   *ui.Tester
	srv  *memssh.Server
	dirs store.Dirs

	resizeDelay time.Duration
	files       transfer.Provider
}

type harnessOpts struct {
	locale    string
	sshConfig string
	keepDirs  *store.Dirs
	// resizeDelay overrides how long a terminal size must stay still.
	resizeDelay time.Duration
	// exec answers the test server's exec requests (nil echoes them).
	exec func(cmd string) (string, int)
	// files replaces the SSH file endpoints of the transfer sheet.
	files transfer.Provider
}

func newHarness(t *testing.T, o harnessOpts) *harness {
	t.Helper()
	if err := terminal.Load(); err != nil {
		t.Skipf("the terminal library is not available: %v", err)
	}
	srv, err := memssh.Start(t.Context(), memssh.Options{Password: "pw", Exec: o.exec})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Stop)
	dirs := store.Dirs{Config: t.TempDir(), State: t.TempDir()}
	if o.keepDirs != nil {
		dirs = *o.keepDirs
	}
	locale := o.locale
	if locale == "" {
		locale = "en_US.UTF-8"
	}
	h := &harness{t: t, srv: srv, dirs: dirs, resizeDelay: o.resizeDelay, files: o.files}
	h.start(locale, o.sshConfig)
	t.Cleanup(h.stop)
	return h
}

func (h *harness) start(locale, sshConfig string) {
	h.t.Helper()
	a, err := app.New(h.t.Context(), app.Options{
		Dirs: h.dirs, Locale: locale, Version: "test",
		SSHConfig: sshConfig, Home: h.t.TempDir(),
		SaveDelay: 20 * time.Millisecond, Backoff: []time.Duration{100 * time.Millisecond},
		ResizeDelay: h.resizeDelay, Files: h.files,
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.app = a
	h.tt = ui.NewTester(a.UI.Build, winW, winH)
	h.tt.Frame()
}

// stop closes the app as a quit would.
func (h *harness) stop() {
	h.t.Helper()
	h.app.Close()
}

func (h *harness) addr() (string, int) {
	host, ps, _ := strings.Cut(h.srv.Addr(), ":")
	port := 0
	for _, c := range ps {
		port = port*10 + int(c-'0')
	}
	return host, port
}

// addHost puts a password host for the test server into the library.
func (h *harness) addHost(name string, tags ...string) model.Host {
	h.t.Helper()
	addr, port := h.addr()
	ref := model.SecretRef(model.ID("pw-"+name), "password")
	if err := h.app.Secrets.Set(h.t.Context(), ref, []byte("pw")); err != nil {
		h.t.Fatal(err)
	}
	saved, err := h.app.Library.Add(model.Host{
		Name: name, Addr: addr, Port: port, User: "tako", Tags: tags,
		Auth: []model.Auth{{Kind: model.AuthPassword, SecretRef: ref}},
	})
	if err != nil {
		h.t.Fatal(err)
	}
	h.tt.Frame()
	return saved
}

// waitFor redraws until cond holds, so callbacks posted by other goroutines run.
func (h *harness) waitFor(what string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for h.tt.Frame(); !cond(); h.tt.Frame() {
		if time.Now().After(deadline) {
			h.shot("timeout")
			h.t.Fatalf("timed out waiting for %s; screen: %q", what, h.tt.Texts())
		}
		time.Sleep(15 * time.Millisecond)
	}
}

func (h *harness) waitText(s string) {
	h.t.Helper()
	h.waitFor("text "+s, func() bool { return h.tt.HasText(s) })
}

func (h *harness) waitGone(s string) {
	h.t.Helper()
	h.waitFor("text gone "+s, func() bool { return !h.tt.HasText(s) })
}

func (h *harness) click(s string) {
	h.t.Helper()
	h.tt.Frame()
	if err := h.tt.Click(s); err != nil {
		h.shot("click-fail")
		h.t.Fatalf("clicking %q: %v; screen: %v", s, err, h.tt.Texts())
	}
	h.tt.Frame()
}

// shot saves the current frame when TAKO_SHOTS names a directory.
func (h *harness) shot(name string) {
	h.t.Helper()
	dir := os.Getenv("TAKO_SHOTS")
	if dir == "" {
		return
	}
	h.tt.Advance(600 * time.Millisecond) // let panels settle into their places
	h.tt.Frame()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := png.Encode(f, h.tt.Image()); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) connected(n int) bool {
	c, _ := h.app.Model.Counts()
	return c >= n
}

// typeInto focuses the control with the accessible name label and types text.
func (h *harness) typeInto(label, text string) {
	h.t.Helper()
	h.tt.Frame()
	info, ok := h.tt.AccessByRole(ui.RoleTextField, label)
	if !ok {
		h.shot("no-field")
		h.t.Fatalf("no control named %q; screen: %v", label, h.tt.Texts())
	}
	h.tt.ClickAt(float32(info.Bounds.X+info.Bounds.W/2), float32(info.Bounds.Y+info.Bounds.H/2))
	h.tt.Frame()
	h.tt.Type(text)
	h.tt.Frame()
}

// restart quits the app as the user would and starts a new one on the same
// data and the same server; the new one restores the session. Saved
// passwords come back from the encrypted file.
func (h *harness) restart() {
	h.t.Helper()
	h.stop()
	h.start("en_US.UTF-8", "")
	h.app.Restore()
	h.tt.Frame()
}
