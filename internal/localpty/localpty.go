// Package localpty runs a local shell in a pseudo-terminal that Tako owns,
// for a terminal.Options.Conn. MyGo's terminal runs local programs in a
// pseudo-terminal of its own, where Tako cannot see what is typed; as a
// Conn, the typing passes through Tako (inputbus.SyncConn) and local
// terminals take part in input sync as SSH ones do.
//
// It starts the program as MyGo's terminal does (terminal.New without a
// Conn): the user's shell as a login shell, in the home directory unless
// told otherwise, with TERM, COLORTERM and a UTF-8 LANG; once the view tells
// it the size, or at 80×24 half a second later; hung up on Close and killed
// two seconds later if it is still running.
package localpty

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/zdva/tako/internal/localpty/pty"
)

// Options describe the program.
type Options struct {
	// Command is the program and its arguments; empty runs the user's
	// shell as a login shell.
	Command []string
	// Dir is the working directory: the home directory when empty.
	Dir string
	// Env adds to the environment, as "KEY=value".
	Env []string
}

// startAfter is how long a program waits for its first size.
const startAfter = 500 * time.Millisecond

// leftModes turns off what a program asks of the terminal for itself (the
// mouse, focus and key reports, a hidden cursor), sent ahead of the shell's
// output when the shell takes the terminal back from a job that left them
// on, as MyGo's terminal does for its own programs.
const leftModes = "\x1b[?9;1000;1002;1003;1004;1005;1006;1015;1016l\x1b[=0;1u\x1b[>4;0m\x1b[?25h"

// Conn is a local program in a pseudo-terminal: Read returns its output and
// Write types into it. It is safe for one reader and concurrent writers.
type Conn struct {
	launch pty.Options

	mu       sync.Mutex
	proc     *pty.PTY
	startErr error
	closed   bool
	timer    *time.Timer
	started  chan struct{} // closed once proc or startErr is set
	once     sync.Once

	// The reader's own state.
	pending  []byte
	fg       int
	readDone chan struct{}
	readOnce sync.Once
}

// Start makes a Conn for the program of o, which starts once ResizePixels
// gives it a size, or at 80×24 half a second later.
func Start(o Options) (*Conn, error) {
	path, args, err := command(o.Command)
	if err != nil {
		return nil, err
	}
	dir := o.Dir
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	c := &Conn{
		launch:   pty.Options{Path: path, Args: args, Dir: dir, Env: environment(o.Env)},
		started:  make(chan struct{}),
		readDone: make(chan struct{}),
	}
	c.mu.Lock()
	c.timer = time.AfterFunc(startAfter, func() { c.start(80, 24, 0, 0) })
	c.mu.Unlock()
	return c, nil
}

// start starts the program at a size, once.
func (c *Conn) start(cols, rows, w, h int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc != nil || c.startErr != nil {
		return
	}
	c.timer.Stop()
	if c.closed {
		c.startErr = io.EOF
		c.markStarted()
		return
	}
	o := c.launch
	o.Cols, o.Rows, o.Width, o.Height = cols, rows, w, h
	p, err := pty.Start(o)
	if err != nil {
		c.startErr = err
		c.markStarted()
		return
	}
	c.proc = p
	c.markStarted()
	go func() {
		<-p.Exited()
		// What the program wrote before exiting is still to be read; then
		// hang up whatever else has the terminal, as terminal apps do.
		select {
		case <-c.readDone:
		case <-time.After(100 * time.Millisecond):
		}
		_ = p.Close()
	}()
}

func (c *Conn) markStarted() { c.once.Do(func() { close(c.started) }) }

// Read returns the program's output; io.EOF once it ended.
func (c *Conn) Read(b []byte) (int, error) {
	<-c.started
	if len(c.pending) > 0 {
		n := copy(b, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	c.mu.Lock()
	p, startErr := c.proc, c.startErr
	if p == nil {
		c.startErr = io.EOF // a start error shows once, then the end
	}
	c.mu.Unlock()
	if p == nil {
		c.endRead()
		if startErr != nil && startErr != io.EOF {
			// Shown in red where the shell would be, as MyGo's terminal does.
			c.pending = []byte("\x1b[31m" + startErr.Error() + "\x1b[0m")
			n := copy(b, c.pending)
			c.pending = c.pending[n:]
			return n, nil
		}
		return 0, io.EOF
	}
	n, err := p.Read(b)
	if n > 0 && c.shellTookBack(p) {
		out := append([]byte(leftModes), b[:n]...)
		n = copy(b, out)
		c.pending = out[n:]
	}
	if err != nil {
		c.endRead()
	}
	return n, err
}

// shellTookBack reports whether the shell is back in the foreground after a
// job, which may have left modes on.
func (c *Conn) shellTookBack(p *pty.PTY) bool {
	fg := p.Foreground()
	if fg == 0 {
		return false
	}
	back := fg != c.fg && fg == p.Pid() && c.fg != 0
	c.fg = fg
	return back
}

func (c *Conn) endRead() { c.readOnce.Do(func() { close(c.readDone) }) }

// Write types b into the program, once it started.
func (c *Conn) Write(b []byte) (int, error) {
	<-c.started
	c.mu.Lock()
	p := c.proc
	c.mu.Unlock()
	if p == nil {
		return 0, pty.ErrClosed
	}
	return p.Write(b)
}

// ResizePixels sets the size of the terminal; the first one starts the
// program at it.
func (c *Conn) ResizePixels(cols, rows, cellWidth, cellHeight int) error {
	c.mu.Lock()
	p := c.proc
	c.mu.Unlock()
	if p == nil {
		c.start(cols, rows, cols*cellWidth, rows*cellHeight)
		return nil
	}
	return p.Resize(cols, rows, cols*cellWidth, rows*cellHeight)
}

// ClearScreen tells a Windows console that the terminal cleared its screen
// (see pty.PTY.ClearScreen); elsewhere it does nothing.
func (c *Conn) ClearScreen() {
	c.mu.Lock()
	p := c.proc
	c.mu.Unlock()
	if p != nil {
		_ = p.ClearScreen()
	}
}

// Close hangs up the program, which is killed two seconds later if it is
// still running. It is safe to call twice.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.timer.Stop()
	p := c.proc
	if p == nil {
		c.startErr = io.EOF
		c.markStarted()
	}
	c.mu.Unlock()
	if p == nil {
		return nil
	}
	err := p.Close()
	go func() {
		select {
		case <-p.Exited():
		case <-time.After(2 * time.Second):
			_ = p.Kill()
		}
	}()
	return err
}

// command returns the program to run and its arguments, with args[0] its
// name: the user's shell as a login shell ("-zsh") when cmd is empty.
func command(cmd []string) (path string, args []string, err error) {
	if len(cmd) > 0 {
		path, err = exec.LookPath(cmd[0])
		if err != nil {
			return "", nil, err
		}
		return path, cmd, nil
	}
	if runtime.GOOS == "windows" {
		for _, name := range []string{"pwsh.exe", "powershell.exe", os.Getenv("COMSPEC"), "cmd.exe"} {
			if p, err := exec.LookPath(name); name != "" && err == nil {
				return p, []string{name}, nil
			}
		}
		return "", nil, errors.New("localpty: no shell found")
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
		if runtime.GOOS == "darwin" {
			shell = "/bin/zsh"
		}
	}
	return shell, []string{"-" + filepath.Base(shell)}, nil
}

// environment returns the environment of the program: this process's with
// the terminal's variables and extra.
func environment(extra []string) []string {
	env := os.Environ()
	set := func(kv string) {
		k, _, _ := strings.Cut(kv, "=")
		for i, e := range env {
			if strings.HasPrefix(e, k+"=") {
				env[i] = kv
				return
			}
		}
		env = append(env, kv)
	}
	set("TERM=xterm-256color")
	set("COLORTERM=truecolor")
	if runtime.GOOS != "windows" && os.Getenv("LANG") == "" && os.Getenv("LC_ALL") == "" && os.Getenv("LC_CTYPE") == "" {
		set("LANG=en_US.UTF-8")
	}
	for _, kv := range extra {
		set(kv)
	}
	return env
}
