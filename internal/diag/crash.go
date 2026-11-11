package diag

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"time"
)

const (
	pendingName = "crash.pending"
	// KeepReports is how many dated crash reports are kept.
	KeepReports = 5
	reportGlob  = "crash-*.log"
)

// Crashes watches for crashes of this run and reports the ones of the last.
type Crashes struct {
	dir  string
	file *os.File
	// Previous is the path of the report left by the last run, or "".
	Previous string
}

// Install makes the runtime write a report of a crash of this run to dir.
// If the last run crashed, its report is kept as crash-<time>.log and
// Previous names it. Close the result on a clean exit.
func Install(dir string) (*Crashes, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating the crash directory: %w", err)
	}
	c := &Crashes{dir: dir}
	pending := filepath.Join(dir, pendingName)
	if st, err := os.Stat(pending); err == nil && st.Size() > 0 {
		dst := filepath.Join(dir, "crash-"+st.ModTime().Format("20060102-150405")+".log")
		if err := os.Rename(pending, dst); err == nil {
			c.Previous = dst
		}
	}
	f, err := os.OpenFile(pending, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening the crash file: %w", err)
	}
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("setting the crash output: %w", err)
	}
	c.file = f
	c.prune()
	return c, nil
}

// Close stops watching and removes the empty pending file: the exit was clean.
func (c *Crashes) Close() error {
	if c == nil || c.file == nil {
		return nil
	}
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	err := c.file.Close()
	c.file = nil
	if st, serr := os.Stat(filepath.Join(c.dir, pendingName)); serr == nil && st.Size() == 0 {
		_ = os.Remove(filepath.Join(c.dir, pendingName))
	}
	if err != nil {
		return fmt.Errorf("closing the crash file: %w", err)
	}
	return nil
}

// Dismiss forgets the previous crash for this run (the user has seen it).
func (c *Crashes) Dismiss() { c.Previous = "" }

// Reports lists the crash reports, newest first.
func (c *Crashes) Reports() []string {
	names, _ := filepath.Glob(filepath.Join(c.dir, reportGlob))
	slices.Sort(names)
	slices.Reverse(names)
	return names
}

func (c *Crashes) prune() {
	for _, old := range c.Reports()[min(KeepReports, len(c.Reports())):] {
		_ = os.Remove(old)
	}
}

// ReadReport returns up to max bytes from the start of a crash report: the
// panic message and the first goroutine are at the top.
func ReadReport(path string, max int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("opening the crash report: %w", err)
	}
	defer f.Close()
	buf := make([]byte, max)
	n, err := f.Read(buf)
	if err != nil && !errors.Is(err, fs.ErrClosed) && n == 0 {
		return "", fmt.Errorf("reading the crash report: %w", err)
	}
	return strings.ToValidUTF8(string(buf[:n]), "?"), nil
}

// Stamp is the time a report was written, from its name.
func Stamp(path string) time.Time {
	base := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(path), "crash-"), ".log")
	t, _ := time.ParseInLocation("20060102-150405", base, time.Local)
	return t
}
