package ssh

import (
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if got := expandHome("~/.ssh/id"); got != filepath.Join(home, ".ssh", "id") {
		t.Errorf("expandHome = %q", got)
	}
	for _, p := range []string{"/abs/key", "rel/key", "~other/key", ""} {
		if got := expandHome(p); got != p {
			t.Errorf("expandHome(%q) = %q, want it unchanged", p, got)
		}
	}
}
