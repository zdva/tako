//go:build e2e

package automation

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// binPath is the e2e-tagged tako binary. TAKO_BIN overrides the build.
var binPath string

func TestMain(m *testing.M) {
	if path := os.Getenv("TAKO_BIN"); path != "" {
		binPath = path
		os.Exit(m.Run())
	}
	if !haveDisplay() {
		os.Exit(m.Run())
	}
	root, err := moduleRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	dir, err := os.MkdirTemp("", "tako-e2e-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "tako")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-tags", "e2e", "-o", binPath, "./cmd/tako")
	cmd.Dir = root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "building tako with -tags e2e: %v\n", err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func haveDisplay() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("finding the module: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

func requireApp(t *testing.T) {
	t.Helper()
	if !haveDisplay() {
		t.Skip("Linux needs DISPLAY or WAYLAND_DISPLAY; run xvfb-run -a make e2e-auto")
	}
	if binPath == "" {
		t.Fatal("the e2e binary was not built and TAKO_BIN is unset")
	}
}
