//go:build e2e && linux

package e2e

import (
	"strconv"
	"testing"
	"time"
)

// openSettingsPage opens the settings window and clicks the i-th page of its
// sidebar (the dialog is 760 wide and 560 high, centered).
func openSettingsPage(t *testing.T, i int) {
	t.Helper()
	left, top := (windowW-760)/2, (windowH-560)/2
	xdo(t, "mousemove", strconv.Itoa(left+74), strconv.Itoa(top+65+32*i), "click", "1")
	time.Sleep(300 * time.Millisecond)
}

func darkSix(t *testing.T, settings map[string]any, lang string) (*fixture, *run) {
	t.Helper()
	f := newFixture(t)
	settings["compactBlocks"] = true
	f.writeSettings(settings)
	f.seedDemo([]string{"web-01", "web-02", "db-01", "gpu-01", "win-build", "mac-mini"},
		map[string]bool{"web-01": true, "web-02": true, "db-01": true})
	r := startShots(t, f, lang)
	r.connected(6)
	time.Sleep(1200 * time.Millisecond)
	typeInto(t, 400, 300, "df -h / | head -n 2", "uptime")
	typeInto(t, 800, 300, "ls /")
	return f, r
}

func TestScreenshots_M4(t *testing.T) {
	dir := shotsDir(t)

	_, r := darkSix(t, map[string]any{"appearance": "dark"}, "zh_CN.UTF-8")
	shot(t, dir, "main-dark-zh")
	xdo(t, "key", "ctrl+shift+comma")
	time.Sleep(400 * time.Millisecond)
	for i, name := range []string{"settings-general", "settings-terminal", "settings-connection", "settings-monitor",
		"settings-transfer", "settings-data", "settings-about"} {
		openSettingsPage(t, i)
		shot(t, dir, name+"-dark-zh")
	}
	r.stop()

	_, r = darkSix(t, map[string]any{"appearance": "dark", "terminal": map[string]any{"scheme": "dracula"}}, "en_US.UTF-8")
	shot(t, dir, "main-dark-dracula-en")
	xdo(t, "key", "ctrl+shift+comma")
	time.Sleep(400 * time.Millisecond)
	openSettingsPage(t, 1)
	shot(t, dir, "settings-terminal-dark-en")
	r.stop()

	_, r = darkSix(t, map[string]any{"terminal": map[string]any{"scheme": "solarized-light"}}, "en_US.UTF-8")
	shot(t, dir, "main-light-solarized-en")
	xdo(t, "key", "ctrl+shift+comma")
	time.Sleep(400 * time.Millisecond)
	openSettingsPage(t, 0)
	shot(t, dir, "settings-general-light-en")
	r.stop()

	_, r = darkSix(t, map[string]any{"language": "en-XA"}, "en_US.UTF-8")
	shot(t, dir, "main-pseudo")
	r.stop()
}

// stop ends the app so the next scenario starts a fresh one.
func (r *run) stop() {
	_ = r.cmd.Process.Kill()
	_ = r.cmd.Wait()
}
