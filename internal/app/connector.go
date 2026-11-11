package app

import (
	"context"
	"strings"
	"time"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/transport"
	tui "github.com/wei-dong/tako/internal/ui"
)

// Connector tries a host for the Add/Edit sheet: it connects, learns the
// operating system, and disconnects.
type Connector struct {
	Dialer transport.Dialer
}

var _ tui.Connector = (*Connector)(nil)

// Test implements [tui.Connector].
func (c *Connector) Test(ctx context.Context, h model.Host) (tui.TestResult, error) {
	start := time.Now()
	t, err := c.Dialer.Dial(ctx, h)
	if err != nil {
		return tui.TestResult{}, err //nolint:wrapcheck // the dialer's error is already classified and explained
	}
	defer t.Close() //nolint:errcheck // the test connection is thrown away
	res := tui.TestResult{Latency: time.Since(start)}
	res.OS, res.OSName = detectOS(ctx, t)
	return res, nil
}

// detectOS asks the server what it runs. Failure is not an error: the
// connection works, the system is just unknown.
func detectOS(ctx context.Context, t transport.Transport) (model.OSKind, string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := t.Exec(ctx, "uname -s")
	if err == nil {
		switch strings.TrimSpace(string(out)) {
		case "Linux":
			name, _ := t.Exec(ctx, `sh -c '. /etc/os-release 2>/dev/null && echo "$PRETTY_NAME"'`)
			return model.OSLinux, firstLine(name, "Linux")
		case "Darwin":
			v, _ := t.Exec(ctx, "sw_vers -productVersion")
			return model.OSMacOS, "macOS " + firstLine(v, "")
		}
	}
	if out, err := t.Exec(ctx, "ver"); err == nil && strings.Contains(strings.ToLower(string(out)), "windows") {
		return model.OSWindows, firstLine(out, "Windows")
	}
	return "", ""
}

func firstLine(b []byte, fallback string) string {
	for line := range strings.SplitSeq(string(b), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return fallback
}
