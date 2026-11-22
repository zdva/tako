package transfer

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zdva/tako/internal/model"
)

var knownHostsOpt = regexp.MustCompile(`UserKnownHostsFile=([^' ]+)`)

// directEnv runs "remote" commands on this computer: the endpoints are
// remote-style but backed by the local file system, and the copy that scp
// would do is done by run.
type directEnv struct {
	t     *testing.T
	eps   map[model.ID]Endpoint
	keys  []string
	rsync bool

	mu         sync.Mutex
	agentCmds  []string
	khExisted  bool
	khPath     string
	run        func(cmd string) ([]byte, error)
	execsAgent int
}

func newDirectEnv(t *testing.T) *directEnv {
	t.Helper()
	t.Setenv("HOME", t.TempDir()) // the destination's home, where the known_hosts file goes
	ep := Remote(localFS{}, func(context.Context, string) ([]byte, error) { return nil, errors.New("no hashing here") })
	return &directEnv{
		t: t, eps: map[model.ID]Endpoint{"a": ep, "b": ep},
		keys: []string{"[10.0.0.1]:22 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAItest"},
	}
}

func (e *directEnv) Endpoint(_ context.Context, l Location) (Endpoint, error) {
	if l.Local() {
		return Local(), nil
	}
	return e.eps[l.Host], nil
}

func (e *directEnv) Reset(Location) {}

func (e *directEnv) Host(id model.ID) (model.Host, bool) {
	return model.Host{ID: id, Name: string(id), Addr: "10.0.0.1", User: "me"}, true
}

func (e *directEnv) HostKeyLines(model.Host) []string { return e.keys }

func (e *directEnv) Exec(context.Context, model.ID, string) ([]byte, error) {
	if e.rsync {
		return []byte("yes\n"), nil
	}
	return nil, errors.New("exited with status 1")
}

func (e *directEnv) ExecAgent(_ context.Context, _ model.ID, cmd string) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.execsAgent++
	e.agentCmds = append(e.agentCmds, cmd)
	if m := knownHostsOpt.FindStringSubmatch(cmd); m != nil {
		e.khPath = m[1]
		_, err := os.Stat(m[1])
		e.khExisted = err == nil
	}
	return e.run(cmd)
}

// hooks collects what a direct job reports.
type hooks struct {
	total, done int64
	policy      Policy
	ask         func(Conflict) (Decision, error)
	mu          sync.Mutex
	seen        []int64
}

func (h *hooks) hooks() DirectHooks {
	return DirectHooks{
		Total: func(n int64) { h.total = n },
		Done: func(n int64) {
			h.mu.Lock()
			h.done = n
			h.seen = append(h.seen, n)
			h.mu.Unlock()
		},
		Policy: func() Policy { return h.policy },
		Ask: func(c Conflict) (Decision, error) {
			if h.ask == nil {
				return Decision{}, errors.New("unexpected question")
			}
			return h.ask(c)
		},
	}
}

func copyFileTo(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDirect_FileThroughScp(t *testing.T) {
	dir := t.TempDir()
	want := randomFile(t, filepath.Join(dir, "src", "big.bin"), 700<<10)
	env := newDirectEnv(t)
	env.run = func(string) ([]byte, error) {
		copyFileTo(t, filepath.Join(dir, "src", "big.bin"), filepath.Join(dir, "dst", "big.bin"+PartSuffix))
		return nil, nil
	}
	h := &hooks{}
	d := &Direct{Env: env, PollEvery: 5 * time.Millisecond}
	spec := Spec{
		Mode: ModeDirect, Verify: VerifyHash,
		Src: Location{Host: "a", Path: filepath.Join(dir, "src", "big.bin")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "dst")},
	}
	if err := d.Run(t.Context(), spec, h.hooks()); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, filepath.Join(dir, "dst", "big.bin")); !bytes.Equal(got, want) {
		t.Fatal("content differs")
	}
	if h.total != int64(len(want)) || h.done != h.total {
		t.Errorf("total %d done %d", h.total, h.done)
	}
	cmd := env.agentCmds[0]
	for _, must := range []string{"scp", "StrictHostKeyChecking=yes", "BatchMode=yes", "ForwardAgent=no", "me@10.0.0.1:", "big.bin" + PartSuffix} {
		if !strings.Contains(cmd, must) {
			t.Errorf("the command lacks %q: %s", must, cmd)
		}
	}
	if strings.Contains(cmd, "PRIVATE KEY") || strings.Contains(cmd, "-i ") {
		t.Errorf("the command carries key material: %s", cmd)
	}
	if !env.khExisted {
		t.Error("the source's key was not on the destination while the copy ran")
	}
	if _, err := os.Stat(env.khPath); err == nil {
		t.Error("the known_hosts file is still on the destination")
	}
	if _, err := os.Stat(filepath.Join(dir, "dst", "big.bin"+PartSuffix)); err == nil {
		t.Error("the part file is still there")
	}
}

func TestDirect_RsyncWhenBothHaveIt(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "src", "f.bin"), 1000)
	env := newDirectEnv(t)
	env.rsync = true
	env.run = func(string) ([]byte, error) {
		copyFileTo(t, filepath.Join(dir, "src", "f.bin"), filepath.Join(dir, "dst", "f.bin"+PartSuffix))
		return nil, nil
	}
	h := &hooks{}
	err := (&Direct{Env: env}).Run(t.Context(), Spec{
		Mode: ModeDirect,
		Src:  Location{Host: "a", Path: filepath.Join(dir, "src", "f.bin")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "dst")},
	}, h.hooks())
	if err != nil {
		t.Fatal(err)
	}
	if cmd := env.agentCmds[0]; !strings.HasPrefix(cmd, "rsync ") || !strings.Contains(cmd, "--append-verify") {
		t.Errorf("not an rsync command: %s", cmd)
	}
}

func TestDirect_Directory(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "src", "tree", "a.bin"), 3000)
	randomFile(t, filepath.Join(dir, "src", "tree", "sub", "b.bin"), 5000)
	env := newDirectEnv(t)
	env.run = func(cmd string) ([]byte, error) {
		if !strings.Contains(cmd, "scp -q") || !strings.Contains(cmd, " -r ") {
			t.Errorf("a directory is copied with scp -r: %s", cmd)
		}
		copyFileTo(t, filepath.Join(dir, "src", "tree", "a.bin"), filepath.Join(dir, "dst", "tree", "a.bin"))
		copyFileTo(t, filepath.Join(dir, "src", "tree", "sub", "b.bin"), filepath.Join(dir, "dst", "tree", "sub", "b.bin"))
		return nil, nil
	}
	h := &hooks{}
	err := (&Direct{Env: env}).Run(t.Context(), Spec{
		Mode: ModeDirect,
		Src:  Location{Host: "a", Path: filepath.Join(dir, "src", "tree")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "dst")},
	}, h.hooks())
	if err != nil {
		t.Fatal(err)
	}
	if h.total != 8000 {
		t.Errorf("total %d", h.total)
	}
}

func TestDirect_DirectoryThatCameUpShortFails(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "src", "tree", "a.bin"), 3000)
	randomFile(t, filepath.Join(dir, "src", "tree", "b.bin"), 5000)
	env := newDirectEnv(t)
	env.run = func(string) ([]byte, error) {
		copyFileTo(t, filepath.Join(dir, "src", "tree", "a.bin"), filepath.Join(dir, "dst", "tree", "a.bin"))
		return nil, nil
	}
	err := (&Direct{Env: env}).Run(t.Context(), Spec{
		Mode: ModeDirect,
		Src:  Location{Host: "a", Path: filepath.Join(dir, "src", "tree")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "dst")},
	}, (&hooks{}).hooks())
	if !errors.Is(err, ErrVerify) {
		t.Fatalf("err = %v, want ErrVerify", err)
	}
}

func TestDirect_WrongSizeFailsVerification(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "src", "f.bin"), 5000)
	env := newDirectEnv(t)
	env.run = func(string) ([]byte, error) {
		if err := os.MkdirAll(filepath.Join(dir, "dst"), 0o755); err != nil {
			t.Fatal(err)
		}
		return nil, os.WriteFile(filepath.Join(dir, "dst", "f.bin"+PartSuffix), []byte("short"), 0o644)
	}
	err := (&Direct{Env: env}).Run(t.Context(), Spec{
		Mode: ModeDirect,
		Src:  Location{Host: "a", Path: filepath.Join(dir, "src", "f.bin")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "dst")},
	}, (&hooks{}).hooks())
	if !errors.Is(err, ErrVerify) {
		t.Fatalf("err = %v, want ErrVerify", err)
	}
}

func TestDirect_WrongContentFailsHashVerification(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "src", "f.bin"), 5000)
	env := newDirectEnv(t)
	env.run = func(string) ([]byte, error) {
		if err := os.MkdirAll(filepath.Join(dir, "dst"), 0o755); err != nil {
			t.Fatal(err)
		}
		return nil, os.WriteFile(filepath.Join(dir, "dst", "f.bin"+PartSuffix), make([]byte, 5000), 0o644)
	}
	spec := Spec{
		Mode: ModeDirect, Verify: VerifyHash,
		Src: Location{Host: "a", Path: filepath.Join(dir, "src", "f.bin")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "dst")},
	}
	if err := (&Direct{Env: env}).Run(t.Context(), spec, (&hooks{}).hooks()); !errors.Is(err, ErrVerify) {
		t.Fatalf("err = %v, want ErrVerify", err)
	}
	// The same bytes with the size check only pass: that is what Auto/Size mean.
	spec.Verify = VerifySize
	if err := (&Direct{Env: env}).Run(t.Context(), spec, (&hooks{policy: PolicyOverwrite}).hooks()); err != nil {
		t.Fatalf("size-only check: %v", err)
	}
}

func TestDirect_CommandFailureSaysWhyAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "src", "f.bin"), 100)
	env := newDirectEnv(t)
	env.run = func(string) ([]byte, error) {
		return []byte("Permission denied (publickey)."), errors.New("exited with status 1")
	}
	err := (&Direct{Env: env}).Run(t.Context(), Spec{
		Mode: ModeDirect,
		Src:  Location{Host: "a", Path: filepath.Join(dir, "src", "f.bin")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "dst")},
	}, (&hooks{}).hooks())
	if err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(env.khPath); err == nil {
		t.Error("the known_hosts file survived a failed copy")
	}
}

func TestDirect_Refusals(t *testing.T) {
	env := newDirectEnv(t)
	env.run = func(string) ([]byte, error) { t.Fatal("nothing should run"); return nil, nil }
	d := &Direct{Env: env}
	h := (&hooks{}).hooks()
	if err := d.Run(t.Context(), Spec{Src: Location{Path: "/x"}, Dst: Location{Host: "b", Path: "/y"}}, h); !errors.Is(err, ErrDirectLocal) {
		t.Errorf("local source: %v", err)
	}
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "f.bin"), 10)
	env.keys = nil
	err := d.Run(t.Context(), Spec{Src: Location{Host: "a", Path: filepath.Join(dir, "f.bin")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "d")}}, h)
	if !errors.Is(err, ErrNoHostKey) {
		t.Errorf("untrusted source: %v", err)
	}
}

func TestDirect_SamePolicies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy Policy
		answer Decision
		runs   int
	}{
		{"skip", PolicySkip, Decision{}, 0},
		{"overwrite", PolicyOverwrite, Decision{}, 1},
		{"ask-skip", PolicyAsk, Decision{}, 0},
		{"ask-overwrite", PolicyAsk, Decision{Overwrite: true}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			randomFile(t, filepath.Join(dir, "src", "f.bin"), 4000)
			randomFile(t, filepath.Join(dir, "dst", "f.bin"), 10)
			env := newDirectEnv(t)
			env.run = func(string) ([]byte, error) {
				copyFileTo(t, filepath.Join(dir, "src", "f.bin"), filepath.Join(dir, "dst", "f.bin"+PartSuffix))
				return nil, nil
			}
			var asked []Conflict
			h := &hooks{policy: tc.policy, ask: func(c Conflict) (Decision, error) { asked = append(asked, c); return tc.answer, nil }}
			err := (&Direct{Env: env}).Run(t.Context(), Spec{
				Mode: ModeDirect,
				Src:  Location{Host: "a", Path: filepath.Join(dir, "src", "f.bin")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "dst")},
			}, h.hooks())
			if err != nil {
				t.Fatal(err)
			}
			if env.execsAgent != tc.runs {
				t.Errorf("copy ran %d times, want %d", env.execsAgent, tc.runs)
			}
			if (tc.policy == PolicyAsk) != (len(asked) == 1) {
				t.Errorf("asked %d times under policy %v", len(asked), tc.policy)
			}
			if len(asked) == 1 && (asked[0].SrcSize != 4000 || asked[0].DstSize != 10) {
				t.Errorf("question %+v", asked[0])
			}
			wantSize := int64(10)
			if tc.runs == 1 {
				wantSize = 4000
			}
			if fi, err := os.Stat(filepath.Join(dir, "dst", "f.bin")); err != nil || fi.Size() != wantSize {
				t.Errorf("destination is %v (%v), want %d bytes", fi, err, wantSize)
			}
		})
	}
}

func TestDirect_ProgressFollowsTheGrowingPartFile(t *testing.T) {
	dir := t.TempDir()
	randomFile(t, filepath.Join(dir, "src", "f.bin"), 4000)
	env := newDirectEnv(t)
	part := filepath.Join(dir, "dst", "f.bin"+PartSuffix)
	env.run = func(string) ([]byte, error) {
		if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(part, make([]byte, 1000), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(60 * time.Millisecond)
		copyFileTo(t, filepath.Join(dir, "src", "f.bin"), part)
		return nil, nil
	}
	h := &hooks{}
	err := (&Direct{Env: env, PollEvery: 5 * time.Millisecond}).Run(t.Context(), Spec{
		Mode: ModeDirect,
		Src:  Location{Host: "a", Path: filepath.Join(dir, "src", "f.bin")}, Dst: Location{Host: "b", Path: filepath.Join(dir, "dst")},
	}, h.hooks())
	if err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	var partial bool
	for _, n := range h.seen {
		partial = partial || (n > 0 && n < 4000)
	}
	if !partial {
		t.Errorf("progress never showed a partial copy: %v", h.seen)
	}
}

func TestDirectCommand_QuotesAndIPv6(t *testing.T) {
	src := model.Host{Addr: "fe80::1", User: "me", Port: 2222}
	got := directCommand(src, "/a b/it's", "/dst/x", "/h/kh", false, false)
	for _, want := range []string{"-P 2222", `'me@[fe80::1]:/a b/it'\''s'`} {
		if !strings.Contains(got, want) {
			t.Errorf("%q lacks %q", got, want)
		}
	}
}

func TestTail(t *testing.T) {
	if got := tail([]byte("  short \n")); got != "short" {
		t.Error(got)
	}
	long := strings.Repeat("x", 500)
	if got := tail([]byte(long)); len([]rune(got)) != 301 || !strings.HasPrefix(got, "…") {
		t.Errorf("long tail %d", len(got))
	}
}
