package ui_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zdva/tako/internal/workspace"
)

func readSession(t *testing.T, path string) (workspace.State, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return workspace.State{}, err //nolint:wrapcheck // the test only looks at it
	}
	var st workspace.State
	return st, json.Unmarshal(data, &st)
}

func TestRestore_BlocksAndTabsComeBackAfterQuit(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(3)
	h.app.Model.AddTab(h.app.Model.Blocks()[1]) //nolint:errcheck // checked through the tab count below
	if _, err := h.app.Model.OpenLocal(""); err != nil {
		t.Fatal(err)
	}
	h.waitFor("four blocks", func() bool { return len(h.app.Model.Blocks()) == 4 })
	names := blockNames(h)

	h.restart()
	h.waitFor("four restored blocks", func() bool { return len(h.app.Model.Blocks()) == 4 })
	if got := blockNames(h); strings.Join(got, ",") != strings.Join(names, ",") {
		t.Fatalf("restored blocks %v, want %v", got, names)
	}
	if n := len(h.app.Model.Blocks()[1].Tabs); n != 2 {
		t.Fatalf("block 2 has %d tabs after restore, want 2", n)
	}
	h.waitFor("reconnected", func() bool { return h.connected(3) })
	h.shot("restored")
}

func blockNames(h *harness) []string {
	var out []string
	for _, b := range h.app.Model.Blocks() {
		out = append(out, b.Name)
	}
	return out
}

// A crash cannot run Close: the debounced save alone must leave a session
// file a new run can restore from.
func TestRestore_RecoversFromTheDebouncedSaveAfterACrash(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(2)
	deadline := time.Now().Add(5 * time.Second)
	var st workspace.State
	for {
		got, err := readSession(t, h.dirs.Session())
		if err == nil && len(got.Blocks) == 2 {
			st = got
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session.json never held both blocks: %+v, %v", got, err)
		}
		h.tt.Frame()
		time.Sleep(20 * time.Millisecond)
	}
	crashed := filepath.Join(t.TempDir(), "session.json")
	data, err := os.ReadFile(h.dirs.Session())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crashed, data, 0o600); err != nil {
		t.Fatal(err)
	}
	h.stop() // stands in for the process going away; its own final save is overwritten below
	if err := os.WriteFile(h.dirs.Session(), data, 0o600); err != nil {
		t.Fatal(err)
	}

	h.start("en_US.UTF-8", "")
	h.app.Restore()
	h.waitFor("two blocks", func() bool { return len(h.app.Model.Blocks()) == len(st.Blocks) })
	h.waitFor("reconnected", func() bool { return h.connected(2) })
}

func TestRestore_SkipsDeletedHostsAndSurvivesGarbage(t *testing.T) {
	h := newHarness(t, harnessOpts{})
	h.openAll(2)
	h.stop()
	if err := os.WriteFile(h.dirs.Session(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.start("en_US.UTF-8", "")
	h.app.Restore()
	h.tt.Frame()
	if n := len(h.app.Model.Blocks()); n != 0 {
		t.Fatalf("restored %d blocks from garbage", n)
	}
	if !h.tt.HasText("Welcome to Tako") {
		t.Fatalf("expected the empty state, got %q", h.tt.Texts())
	}
}
