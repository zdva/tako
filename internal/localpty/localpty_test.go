//go:build !windows

package localpty_test

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/zdva/tako/internal/localpty"
)

// termSeq matches a control sequence a shell prints while starting: CSI
// (bash on macOS sends "\x1b[?1034h" before the first prompt), OSC, or one
// other escape.
var termSeq = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[^[\]])`)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

// output collects what a Conn's program writes until it ends.
type output struct {
	mu   sync.Mutex
	b    bytes.Buffer
	done chan struct{}
}

func read(c *localpty.Conn) *output {
	o := &output{done: make(chan struct{})}
	go func() {
		defer close(o.done)
		buf := make([]byte, 4096)
		for {
			n, err := c.Read(buf)
			o.mu.Lock()
			o.b.Write(buf[:n])
			o.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return o
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
}

func (o *output) wait(t *testing.T, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(o.String(), what) {
		if time.Now().After(deadline) {
			t.Fatalf("no %q in the output: %q", what, o.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (o *output) ended(t *testing.T) {
	t.Helper()
	select {
	case <-o.done:
	case <-time.After(5 * time.Second):
		t.Fatalf("the program did not end: %q", o.String())
	}
}

// waitPrompt waits until the shell has printed a prompt and stopped writing,
// so a later keystroke is not echoed by the line discipline and then again
// by the shell.
func (o *output) waitPrompt(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var since time.Time
	var last string
	for {
		s := o.String()
		if s != last {
			last = s
			since = time.Now()
		}
		if promptReady(s) && !since.IsZero() && time.Since(since) >= 80*time.Millisecond {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("no shell prompt: %q", s)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// promptReady reports whether s ends in a shell prompt once control
// sequences are removed. A prompt ends in $, %, # or >.
func promptReady(s string) bool {
	s = termSeq.ReplaceAllString(s, "")
	s = strings.TrimRight(s, " \t\r\n")
	if s == "" {
		return false
	}
	switch s[len(s)-1] {
	case '$', '%', '#', '>':
		return true
	default:
		return false
	}
}

func TestConn_RunsInDirWithTheTerminalsEnvironmentAndSize(t *testing.T) {
	dir := t.TempDir()
	c, err := localpty.Start(localpty.Options{
		Command: []string{"/bin/sh", "-c", `pwd; echo "env:$TERM:$COLORTERM:$TAKO_X"; stty size; sleep 0.5; stty size`},
		Dir:     dir,
		Env:     []string{"TAKO_X=yes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.ResizePixels(100, 30, 8, 16); err != nil { // starts it
		t.Fatal(err)
	}
	o := read(c)
	o.wait(t, "30 100")
	if err := c.ResizePixels(120, 40, 8, 16); err != nil {
		t.Fatal(err)
	}
	o.ended(t)
	got := o.String()
	real, _ := os.Readlink(dir)
	for _, want := range []string{"env:xterm-256color:truecolor:yes", "30 100", "40 120"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %q", want, got)
		}
	}
	if !strings.Contains(got, dir) && (real == "" || !strings.Contains(got, real)) {
		t.Errorf("not run in %s: %q", dir, got)
	}
}

func TestConn_StartsAt80x24WithoutASize(t *testing.T) {
	c, err := localpty.Start(localpty.Options{Command: []string{"/bin/sh", "-c", "stty size"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	o := read(c)
	o.ended(t)
	if !strings.Contains(o.String(), "24 80") {
		t.Fatalf("size %q, want 24 80", o.String())
	}
}

func TestConn_TypingReachesTheShellOnceWithUTF8(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("ENV", "")
	c, err := localpty.Start(localpty.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	o := read(c)
	_ = c.ResizePixels(80, 24, 8, 16)
	// Typing before the prompt is ready is echoed by the kernel and then
	// again by the shell (macOS bash also prints "\x1b[?1034h" as it
	// starts). The command still runs once; wait, then count only what
	// the ready shell echoes.
	o.waitPrompt(t)
	before := o.String()
	if _, err := c.Write([]byte("echo 中文-$((40+2))\r")); err != nil {
		t.Fatal(err)
	}
	o.wait(t, "中文-42")
	time.Sleep(100 * time.Millisecond)
	got := o.String()
	if n := strings.Count(got, "中文-42"); n != 1 {
		t.Errorf("ran %d times: %q", n, got)
	}
	after := strings.TrimPrefix(got, before)
	if n := strings.Count(after, "echo 中文-$((40+2))"); n != 1 {
		t.Errorf("the typed line echoed %d times after the prompt: %q", n, got)
	}
	if _, err := c.Write([]byte("exit\r")); err != nil {
		t.Fatal(err)
	}
	o.ended(t)
}

func TestPromptReady_SeesThePromptAfterBashStartup(t *testing.T) {
	const startup = "echo 中文-$((40+2))\r\n\x1b[?1034hmm6:~ v$ "
	if !promptReady(startup) {
		t.Fatal("the prompt after bash's meta-mode sequence was missed")
	}
	if promptReady("\x1b[?1034h") {
		t.Fatal("the meta-mode sequence counted as a prompt")
	}
	if promptReady("") || promptReady("starting") {
		t.Fatal("output with no prompt counted as ready")
	}
}

func TestConn_CloseHangsUpTheShell(t *testing.T) {
	c, err := localpty.Start(localpty.Options{Command: []string{"/bin/sh", "-c", "echo up; sleep 30"}})
	if err != nil {
		t.Fatal(err)
	}
	o := read(c)
	_ = c.ResizePixels(80, 24, 8, 16)
	o.wait(t, "up")
	start := time.Now()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	o.ended(t)
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("hung up after %v", took)
	}
	if err := c.Close(); err != nil {
		t.Errorf("a second Close: %v", err)
	}
}

func TestConn_CloseBeforeItStarted(t *testing.T) {
	c, err := localpty.Start(localpty.Options{Command: []string{"/bin/sh", "-c", "echo never"}})
	if err != nil {
		t.Fatal(err)
	}
	o := read(c)
	_ = c.Close()
	o.ended(t)
	if _, err := c.Write([]byte("x")); err == nil {
		t.Error("Write after Close should fail")
	}
	time.Sleep(600 * time.Millisecond) // past the start timer
	if strings.Contains(o.String(), "never") {
		t.Error("a closed Conn started its program")
	}
}

func TestConn_UnknownProgram(t *testing.T) {
	if _, err := localpty.Start(localpty.Options{Command: []string{"/no/such/tako-shell"}}); err == nil {
		t.Fatal("want an error for a program that does not exist")
	}
}
