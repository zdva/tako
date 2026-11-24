//go:build e2e

package automation

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zdva/tako/internal/store"
)

// callError is a protocol error. Code is the one the plugin returns
// (timeout, no_element, …).
type callError struct {
	Code, Message string
}

func (e *callError) Error() string { return e.Code + ": " + e.Message }

// client speaks the automation protocol on one connection. Requests run
// one at a time, which is how a single connection is answered.
type client struct {
	conn net.Conn
	r    *bufio.Reader
	seq  int
}

func dial(path string) (*client, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return &client{conn: conn, r: bufio.NewReader(conn)}, nil
}

func (c *client) Close() error { return c.conn.Close() }

func (c *client) call(method string, params any) (json.RawMessage, error) {
	c.seq++
	body, err := json.Marshal(struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
		Params any    `json:"params,omitempty"`
	}{c.seq, method, params})
	if err != nil {
		return nil, err
	}
	if err := c.conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := c.conn.Write(append(body, '\n')); err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	line, err := c.r.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *callError      `json:"error"`
	}
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	return resp.Result, nil
}

// rect is a box in DIPs, relative to the window content.
type rect struct {
	X, Y, Width, Height float64
}

func (r rect) cx() float64     { return r.X + r.Width/2 }
func (r rect) cy() float64     { return r.Y + r.Height/2 }
func (r rect) right() float64  { return r.X + r.Width }
func (r rect) bottom() float64 { return r.Y + r.Height }

func (r rect) contains(x, y float64) bool {
	return x >= r.X-1 && y >= r.Y-1 && x <= r.right()+1 && y <= r.bottom()+1
}

func intersects(a, b rect) bool {
	return a.X < b.right() && b.X < a.right() && a.Y < b.bottom() && b.Y < a.bottom()
}

// node is one element of an automation tree.
type node struct {
	ID     string
	Parent string
	Role   string
	Key    string
	Label  string
	Value  string
	Bounds rect
	States []string
	Now    *float64
}

func (n node) has(state string) bool { return slices.Contains(n.States, state) }

func (n node) right() float64  { return n.Bounds.right() }
func (n node) bottom() float64 { return n.Bounds.bottom() }

type uiTree struct {
	Focus string
	Nodes []node
}

func (t uiTree) byKey(key string) []node {
	var out []node
	for _, n := range t.Nodes {
		if n.Key == key {
			out = append(out, n)
		}
	}
	return out
}

func (t uiTree) one(key string) (node, bool) {
	all := t.byKey(key)
	if len(all) != 1 {
		return node{}, false
	}
	return all[0], true
}

func (t uiTree) byRole(role string) []node {
	var out []node
	for _, n := range t.Nodes {
		if n.Role == role {
			out = append(out, n)
		}
	}
	return out
}

func (t uiTree) byLabel(label string) []node {
	var out []node
	for _, n := range t.Nodes {
		if n.Label == label {
			out = append(out, n)
		}
	}
	return out
}

func (t uiTree) hasText(label string) bool {
	for _, n := range t.Nodes {
		if n.Label == label || n.Value == label {
			return true
		}
	}
	return false
}

func parseTree(raw json.RawMessage) (uiTree, error) {
	var body struct {
		Focus string `json:"focus"`
		Nodes []struct {
			ID     string          `json:"id"`
			Parent string          `json:"parent"`
			Role   string          `json:"role"`
			Key    json.RawMessage `json:"key"`
			Label  string          `json:"label"`
			Value  string          `json:"value"`
			Bounds rect            `json:"bounds"`
			States []string        `json:"states"`
			Now    *float64        `json:"now"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return uiTree{}, err
	}
	out := uiTree{Focus: body.Focus, Nodes: make([]node, 0, len(body.Nodes))}
	for _, n := range body.Nodes {
		item := node{
			ID: n.ID, Parent: n.Parent, Role: n.Role, Label: n.Label, Value: n.Value,
			Bounds: n.Bounds, States: n.States, Now: n.Now,
		}
		if len(n.Key) > 0 && string(n.Key) != "null" {
			if err := json.Unmarshal(n.Key, &item.Key); err != nil {
				item.Key = string(n.Key)
			}
		}
		out.Nodes = append(out.Nodes, item)
	}
	return out, nil
}

var sockSeq atomic.Int64

func socketPath(t *testing.T) string {
	t.Helper()
	name := fmt.Sprintf("tako-e2e-%d-%d.sock", os.Getpid(), sockSeq.Add(1))
	if runtime.GOOS == "windows" {
		return filepath.Join(t.TempDir(), name)
	}
	path := filepath.Join(os.TempDir(), name)
	if len(path) > 100 {
		path = filepath.Join("/tmp", name)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	return path
}

// session is one running e2e build of Tako and the client driving it.
type session struct {
	t     *testing.T
	cmd   *exec.Cmd
	log   *os.File
	cl    *client
	sock  string
	dirs  store.Dirs
	home  string
	marks string
}

type launchOpts struct {
	size string
	// prepare runs after the data directories exist and before the process
	// starts, so a test can seed known_hosts without touching a real home.
	prepare func(dirs store.Dirs, home string)
}

func startSession(t *testing.T, opts launchOpts) *session {
	t.Helper()
	requireApp(t)
	if opts.size == "" {
		opts.size = "1100x720"
	}
	s := &session{t: t, sock: socketPath(t), home: t.TempDir()}
	s.dirs = store.Dirs{Config: filepath.Join(s.home, "config"), State: filepath.Join(s.home, "state")}
	if err := s.dirs.Ensure(); err != nil {
		t.Fatal(err)
	}
	s.marks = filepath.Join(s.home, "marks")
	if err := os.MkdirAll(s.marks, 0o700); err != nil {
		t.Fatal(err)
	}
	if opts.prepare != nil {
		opts.prepare(s.dirs, s.home)
	}
	logPath := filepath.Join(s.dirs.State, "stderr.txt")
	logf, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	s.log = logf
	cmd := exec.Command(binPath)
	cmd.Dir = s.home
	cmd.Env = childEnv(
		"MYGO_AUTOMATION="+s.sock,
		"TAKO_CONFIG_DIR="+s.dirs.Config,
		"TAKO_STATE_DIR="+s.dirs.State,
		"TAKO_WINDOW="+opts.size,
		"HOME="+s.home,
		"USERPROFILE="+s.home,
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
	)
	cmd.Stdout, cmd.Stderr = logf, logf
	s.cmd = cmd
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		_ = logf.Close()
	})
	var dialErr error
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		cl, err := dial(s.sock)
		if err == nil {
			s.cl = cl
			break
		}
		dialErr = err
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if s.cl == nil {
		t.Fatalf("automation socket %s did not open (%v); build with -tags e2e and set MYGO_AUTOMATION.\n%s", s.sock, dialErr, s.stderrTail())
	}
	t.Cleanup(func() { _ = s.cl.Close() })
	t.Cleanup(func() {
		if t.Failed() {
			s.dump()
		}
	})
	if _, err := s.cl.call("windows", nil); err != nil {
		t.Fatalf("the window did not come up: %v\n%s", err, s.stderrTail())
	}
	s.settle()
	return s
}

func childEnv(extra ...string) []string {
	drop := map[string]bool{"SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true}
	for _, e := range extra {
		k, _, _ := strings.Cut(e, "=")
		drop[k] = true
	}
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, e := range os.Environ() {
		k, _, _ := strings.Cut(e, "=")
		if drop[k] {
			continue
		}
		out = append(out, e)
	}
	return append(out, extra...)
}

func (s *session) settle() {
	s.t.Helper()
	_, err := s.cl.call("idle", map[string]any{"timeout": 8000})
	if err == nil {
		return
	}
	if pe, ok := errors.AsType[*callError](err); ok && pe.Code == "timeout" {
		s.t.Log("idle timed out; reading the frame as it is")
		return
	}
	s.t.Fatalf("idle: %v", err)
}

func (s *session) must(method string, params any) json.RawMessage {
	s.t.Helper()
	raw, err := s.cl.call(method, params)
	if err != nil {
		s.t.Fatalf("%s: %v", method, err)
	}
	return raw
}

func (s *session) tree() uiTree {
	s.t.Helper()
	tr, err := parseTree(s.must("tree", nil))
	if err != nil {
		s.t.Fatal(err)
	}
	return tr
}

func (s *session) contentBox() rect {
	s.t.Helper()
	var body struct {
		Windows []struct {
			ContentBounds rect `json:"contentBounds"`
			Focused       bool `json:"focused"`
		} `json:"windows"`
	}
	if err := json.Unmarshal(s.must("windows", nil), &body); err != nil {
		s.t.Fatal(err)
	}
	if len(body.Windows) == 0 {
		s.t.Fatal("no window")
	}
	for _, w := range body.Windows {
		if w.Focused {
			return w.ContentBounds
		}
	}
	return body.Windows[0].ContentBounds
}

func (s *session) pressKey(key string) {
	s.t.Helper()
	s.must("act", map[string]any{"key": key, "action": "press"})
}

func (s *session) pressID(id string) {
	s.t.Helper()
	s.must("act", map[string]any{"id": id, "action": "press"})
}

func (s *session) setValue(key, text string) {
	s.t.Helper()
	s.must("act", map[string]any{"key": key, "action": "setValue", "text": text})
}

func (s *session) mustKey(key string) node {
	s.t.Helper()
	n, ok := s.tree().one(key)
	if !ok {
		s.t.Fatalf("want exactly one element with key %q", key)
	}
	return n
}

func (s *session) input(events ...map[string]any) {
	s.t.Helper()
	s.must("input", map[string]any{"events": events})
}

func (s *session) click(x, y float64) {
	s.t.Helper()
	s.input(map[string]any{"type": "click", "x": x, "y": y})
}

func (s *session) pointer(kind string, x, y float64) {
	s.t.Helper()
	s.input(map[string]any{"type": kind, "x": x, "y": y})
}

// typeLine types text into the focused element and submits it with Enter.
func (s *session) typeLine(text string) {
	s.t.Helper()
	s.input(
		map[string]any{"type": "text", "text": text},
		map[string]any{"type": "key", "key": "Enter"},
	)
}

func (s *session) wait(d time.Duration, what string, cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(d)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func (s *session) noKeychain() {
	s.t.Helper()
	banned := []string{
		"keychain", "credential store", "secitem", "secret service",
		"gnome-keyring", "org.freedesktop.secrets",
	}
	paths := []string{
		s.log.Name(),
		filepath.Join(s.dirs.Logs(), "tako.log"),
	}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		low := strings.ToLower(string(b))
		for _, word := range banned {
			if strings.Contains(low, word) {
				s.t.Errorf("%s mentions %q; launch must not touch the system credential store", path, word)
			}
		}
	}
}

func (s *session) noDialog() {
	s.t.Helper()
	for _, n := range s.tree().Nodes {
		if n.Role == "dialog" || n.Role == "alertDialog" {
			s.t.Fatalf("a dialog is up (%s %q); launch must not ask for a keychain or a password", n.Role, n.Label)
		}
	}
}

func (s *session) stderrTail() string {
	b, err := os.ReadFile(s.log.Name())
	if err != nil {
		return ""
	}
	if len(b) > 4000 {
		b = b[len(b)-4000:]
	}
	return string(b)
}

func (s *session) dump() {
	dir := filepath.Join(os.TempDir(), "tako-e2e-fail")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		s.t.Logf("failure artifacts: %v", err)
		return
	}
	if raw, err := s.cl.call("capture", nil); err == nil {
		var body struct {
			PNG []byte `json:"png"`
		}
		if json.Unmarshal(raw, &body) == nil {
			_ = os.WriteFile(filepath.Join(dir, "window.png"), body.PNG, 0o600)
		}
	}
	if raw, err := s.cl.call("tree", nil); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "tree.json"), raw, 0o600)
	}
	if b, err := os.ReadFile(s.log.Name()); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "stderr.txt"), b, 0o600)
	}
	s.t.Logf("failure artifacts in %s", dir)
}

func (s *session) shot() (image.Image, int, int) {
	s.t.Helper()
	var body struct {
		PNG    []byte `json:"png"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := json.Unmarshal(s.must("capture", nil), &body); err != nil {
		s.t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(body.PNG))
	if err != nil {
		s.t.Fatal(err)
	}
	return img, body.Width, body.Height
}

func (s *session) pixel(img image.Image, w, h int, box rect, x, y float64) color.Color {
	s.t.Helper()
	if box.Width <= 0 || box.Height <= 0 || w <= 0 || h <= 0 {
		s.t.Fatalf("cannot map a pixel: content %.0fx%.0f, png %dx%d", box.Width, box.Height, w, h)
	}
	sx, sy := float64(w)/box.Width, float64(h)/box.Height
	px := int(math.Round(x * sx))
	py := int(math.Round(y * sy))
	px = min(max(px, 0), w-1)
	py = min(max(py, 0), h-1)
	return img.At(px, py)
}

func colorDist(a, b color.Color) int {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return abs(int(ar>>8)-int(br>>8)) + abs(int(ag>>8)-int(bg>>8)) + abs(int(ab>>8)-int(bb>>8)) + abs(int(aa>>8)-int(ba>>8))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func readOpacity(path string) (float64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	var st struct {
		Minimap struct {
			Opacity float64 `json:"opacity"`
		} `json:"minimap"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return 0, false
	}
	return st.Minimap.Opacity, st.Minimap.Opacity > 0
}

// settingsCompact reports whether settings.json turned compact blocks on.
// A missing file is the default, which is off.
func settingsCompact(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var v struct {
		Compact bool `json:"compactBlocks"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return false
	}
	return v.Compact
}

func atoiPort(addr string) (host string, port int) {
	host, raw, ok := strings.Cut(addr, ":")
	if !ok {
		return addr, 0
	}
	n, _ := strconv.Atoi(raw)
	return host, n
}
