package monitor

import (
	"context"
	"strings"
	"time"

	"github.com/wei-dong/tako/internal/model"
)

// Raw is what a collector carries from one parse to the next: the counters
// that speeds are computed from. The zero value means "no previous reading".
type Raw struct {
	valid bool
	at    int64 // unix nanoseconds
	cpu   cpuTimes
	cores []cpuTimes
	disk  [2]uint64 // read, written bytes
	net   [2]uint64 // received, sent bytes
}

// Collector knows how to measure one operating system.
type Collector interface {
	// Command is the one command that returns every metric. It must work
	// whatever the remote login shell is.
	Command() string
	// Parse reads the output of Command, taken at time at. prev is the Raw of
	// the previous call (zero for the first); the returned Raw goes to the next.
	Parse(out []byte, prev Raw, at time.Time) (Sample, Raw, error)
	// InfoCommand returns the command that describes the machine, and
	// ParseInfo reads its output.
	InfoCommand() string
	ParseInfo(out []byte) (Info, error)
}

// ForOS returns the collector for an operating system; unknown systems get
// the Linux one, the commonest.
func ForOS(os model.OSKind) Collector {
	switch os {
	case model.OSMacOS:
		return Darwin{}
	case model.OSWindows:
		return Windows{}
	case model.OSLinux:
		return Linux{}
	}
	return Linux{}
}

// ProbeCommand tells Linux and macOS (uname) from Windows (no uname).
const ProbeCommand = "uname -s"

// DetectOS reads the output of [ProbeCommand]. A failed probe (err != nil)
// means a system without uname: Windows.
func DetectOS(out []byte, err error) model.OSKind {
	s := strings.TrimSpace(string(out))
	switch {
	case strings.HasPrefix(s, "Linux"):
		return model.OSLinux
	case strings.HasPrefix(s, "Darwin"):
		return model.OSMacOS
	case err != nil || s == "":
		return model.OSWindows
	}
	return model.OSLinux // another Unix: the Linux collector fails soft on it
}

// Exec runs a command on the machine.
type Exec func(ctx context.Context, cmd string) ([]byte, error)

// sections splits the output of a command that prints "##NAME" lines before
// each part.
func sections(out []byte) map[string]string {
	m := map[string]string{}
	var name string
	var b strings.Builder
	flush := func() {
		if name != "" {
			m[name] = b.String()
		}
		b.Reset()
	}
	for line := range strings.Lines(strings.ReplaceAll(string(out), "\r\n", "\n")) {
		if rest, ok := strings.CutPrefix(line, "##"); ok {
			flush()
			name = strings.TrimSpace(rest)
			continue
		}
		b.WriteString(line)
	}
	flush()
	return m
}
