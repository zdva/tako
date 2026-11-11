package store

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Environment variables that override the default directories. They exist for
// tests, demos and portable installs.
const (
	EnvConfigDir = "TAKO_CONFIG_DIR"
	EnvStateDir  = "TAKO_STATE_DIR"
)

// Dirs are the directories Tako stores data in.
type Dirs struct {
	// Config holds what the user would back up or move: hosts, settings,
	// known_hosts.
	Config string
	// State holds what can be rebuilt: the restored session and logs.
	State string
}

// DefaultDirs returns the per-user directories for this platform:
// ~/.config/tako and ~/.local/state/tako on Linux,
// ~/Library/Application Support/Tako on macOS and %AppData%\Tako (config) and
// %LocalAppData%\Tako (state) on Windows.
func DefaultDirs() (Dirs, error) {
	cfgBase, err := os.UserConfigDir()
	if err != nil {
		return Dirs{}, fmt.Errorf("finding the user config directory: %w", err)
	}
	d := Dirs{Config: filepath.Join(cfgBase, appDirName()), State: filepath.Join(cfgBase, appDirName())}
	switch runtime.GOOS {
	case "linux":
		d.State = filepath.Join(xdgStateHome(), "tako")
	case "windows":
		if local := os.Getenv("LocalAppData"); local != "" {
			d.State = filepath.Join(local, "Tako")
		}
	}
	if v := os.Getenv(EnvConfigDir); v != "" {
		d.Config = v
	}
	if v := os.Getenv(EnvStateDir); v != "" {
		d.State = v
	}
	return d, nil
}

func appDirName() string {
	if runtime.GOOS == "linux" {
		return "tako"
	}
	return "Tako"
}

func xdgStateHome() string {
	if v := os.Getenv("XDG_STATE_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return os.TempDir()
	}
	return filepath.Join(home, ".local", "state")
}

// Ensure creates both directories with owner-only permissions.
func (d Dirs) Ensure() error {
	for _, dir := range []string{d.Config, d.State} {
		if err := os.MkdirAll(dir, dirMode); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	return nil
}

// Hosts is the host library document.
func (d Dirs) Hosts() string { return filepath.Join(d.Config, "hosts.json") }

// Session is the restored-session document.
func (d Dirs) Session() string { return filepath.Join(d.State, "session.json") }

// KnownHosts is Tako's own OpenSSH-format known_hosts file.
func (d Dirs) KnownHosts() string { return filepath.Join(d.Config, "known_hosts") }

// Logs is the log directory.
func (d Dirs) Logs() string { return filepath.Join(d.State, "logs") }
