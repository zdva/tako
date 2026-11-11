package diag

import (
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// Info is what a diagnostics report says about this run.
type Info struct {
	Version    string
	Locale     string
	Language   string
	Appearance string
	Display    string
	Hosts      int
	Blocks     int
	Started    time.Time
	// Names are host names, addresses and user names to hide from the log
	// lines and the crash report.
	Names []string
	// Home is the home folder, shown as "~".
	Home string
}

var (
	ipv4 = regexp.MustCompile(`\b\d{1,3}(?:\.\d{1,3}){3}\b`)
	// ipv6 needs "::" or all eight groups, so clock times such as 15:04:05
	// are left alone.
	ipv6   = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){1,7}:(?:[0-9a-f]{1,4}(?::[0-9a-f]{1,4}){0,6})?|\b(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}\b`)
	email  = regexp.MustCompile(`[\w.+-]+@[\w-]+(?:\.[\w-]+)+`)
	userAt = regexp.MustCompile(`\b[\w.-]+@[\w.-]+\b`)
	blob   = regexp.MustCompile(`[A-Za-z0-9+/=_-]{32,}`)
	secret = regexp.MustCompile(`(?i)("?(?:pass\w*|secret\w*|token|key|credential\w*)"?\s*[:=]\s*)("[^"]*"|\S+)`)
)

// Scrub removes what a report must not carry from one line: the given names,
// the home folder, addresses, user@host pairs, long key-like strings and the
// value of anything called password, secret, token or key.
func Scrub(line string, names []string, home string) string {
	if home != "" && home != "/" {
		line = strings.ReplaceAll(line, home, "~")
	}
	for _, n := range names {
		if len(n) >= 2 {
			line = strings.ReplaceAll(line, n, "<host>")
		}
	}
	line = secret.ReplaceAllString(line, "${1}[redacted]")
	line = email.ReplaceAllString(line, "<user@host>")
	line = userAt.ReplaceAllString(line, "<user@host>")
	line = scrubIPv4(line)
	line = ipv6.ReplaceAllString(line, "<ip>")
	return blob.ReplaceAllString(line, "<redacted>")
}

// scrubIPv4 replaces dotted quads, but not parts of longer dotted numbers
// such as version strings.
func scrubIPv4(line string) string {
	var b strings.Builder
	last := 0
	for _, m := range ipv4.FindAllStringIndex(line, -1) {
		start, end := m[0], m[1]
		if _, err := netip.ParseAddr(line[start:end]); err != nil {
			continue
		}
		if start >= 2 && line[start-1] == '.' && isDigit(line[start-2]) {
			continue
		}
		if end+1 < len(line) && line[end] == '.' && isDigit(line[end+1]) {
			continue
		}
		b.WriteString(line[last:start])
		b.WriteString("<ip>")
		last = end
	}
	b.WriteString(line[last:])
	return b.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// Build returns the diagnostics text: a header, the tail of the log, and the
// top of the last crash report when there is one.
func Build(in Info, logTail []string, crashReport string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Tako %s\n", in.Version)
	fmt.Fprintf(&b, "Go %s, %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 8 {
				fmt.Fprintf(&b, "Commit %s\n", s.Value[:8])
			}
		}
	}
	fmt.Fprintf(&b, "Locale %s, language %s, appearance %s, display %s\n", orNone(in.Locale), orNone(in.Language), orNone(in.Appearance), orNone(in.Display))
	fmt.Fprintf(&b, "Hosts %d, blocks %d", in.Hosts, in.Blocks)
	if !in.Started.IsZero() {
		fmt.Fprintf(&b, ", running %s", time.Since(in.Started).Round(time.Second))
	}
	b.WriteString("\n")
	if crashReport != "" {
		b.WriteString("\n--- last crash report (top) ---\n")
		for line := range strings.Lines(crashReport) {
			b.WriteString(Scrub(strings.TrimRight(line, "\r\n"), in.Names, in.Home))
			b.WriteString("\n")
		}
	}
	if len(logTail) > 0 {
		b.WriteString("\n--- log (last lines) ---\n")
		for _, l := range logTail {
			b.WriteString(Scrub(l, in.Names, in.Home))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func orNone(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// Tail returns the last n lines of a text file, at most maxBytes read.
func Tail(path string, n, maxBytes int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	size := st.Size()
	off := max(size-int64(maxBytes), 0)
	buf := make([]byte, size-off)
	if _, err := f.ReadAt(buf, off); err != nil && len(buf) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	if off > 0 && len(lines) > 0 {
		lines = lines[1:] // the first line is cut
	}
	return lines[max(len(lines)-n, 0):]
}
