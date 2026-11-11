package transfer

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wei-dong/tako/internal/model"
	"github.com/wei-dong/tako/internal/transport"
)

// DirectRunner copies a file or directory server to server without passing
// the bytes through this computer.
type DirectRunner interface {
	Run(ctx context.Context, spec Spec, h DirectHooks) error
}

// DirectHooks let a runner report to its job.
type DirectHooks struct {
	// Total says how many bytes there are in all.
	Total func(int64)
	// Done says how many bytes are in place.
	Done func(int64)
	// Ask reports a same-name file and waits for the user's answer.
	Ask func(Conflict) (Decision, error)
	// Policy is the job's current same-name policy.
	Policy func() Policy
}

func (q *Queue) attemptDirect(ctx context.Context, j *job) error {
	h := DirectHooks{
		Total: func(n int64) { q.set(j, func(i *JobInfo) { i.Total = n; i.FilesTotal = 1 }) },
		Done:  func(n int64) { j.cur.Store(n) },
		Ask: func(c Conflict) (Decision, error) {
			d, err := q.ask(ctx, j, c)
			if err == nil && d.All {
				q.mu.Lock()
				j.policy = PolicySkip
				if d.Overwrite {
					j.policy = PolicyOverwrite
				}
				q.mu.Unlock()
			}
			return d, err
		},
		Policy: func() Policy {
			q.mu.Lock()
			defer q.mu.Unlock()
			return j.policy
		},
	}
	spec := j.info.Spec
	spec.Policy = h.Policy()
	q.mu.Lock()
	runner := q.opts.Direct
	q.mu.Unlock()
	if runner == nil {
		return ErrDirectDisabled
	}
	err := runner.Run(ctx, spec, h)
	if err == nil {
		q.set(j, func(i *JobInfo) { i.FilesDone = i.FilesTotal })
	}
	return err
}

// DirectEnv is what the direct runner needs from the application.
type DirectEnv interface {
	Provider
	// Host returns the saved host with this ID.
	Host(id model.ID) (model.Host, bool)
	// HostKeyLines returns the trusted known_hosts lines of h.
	HostKeyLines(h model.Host) []string
	// Exec runs a command on a host.
	Exec(ctx context.Context, id model.ID, cmd string) ([]byte, error)
	// ExecAgent runs a command on a host with the user's SSH agent forwarded
	// to it for that command only.
	ExecAgent(ctx context.Context, id model.ID, cmd string) ([]byte, error)
}

// Direct is the [DirectRunner] that has the destination server pull from the
// source with rsync (when both have it) or scp, authenticating with the
// user's SSH agent forwarded for that one command. It writes no private key
// anywhere; the only file it leaves on the destination is a one-line
// known_hosts naming the source's public key, removed afterwards.
type Direct struct {
	Env DirectEnv
	// PollEvery is how often progress is read from the destination;
	// zero means 500 ms.
	PollEvery time.Duration
}

var _ DirectRunner = (*Direct)(nil)

// Errors specific to direct mode.
var (
	// ErrNoHostKey is returned when the source's host key is not trusted yet.
	ErrNoHostKey = errors.New("the source server's host key is not trusted yet; connect to it once first")
	// ErrDirectLocal is returned when a direct job names this computer.
	ErrDirectLocal = errors.New("direct transfer is between two servers")
)

// Run implements [DirectRunner].
func (d *Direct) Run(ctx context.Context, spec Spec, h DirectHooks) error {
	if spec.Src.Local() || spec.Dst.Local() {
		return ErrDirectLocal
	}
	src, ok := d.Env.Host(spec.Src.Host)
	if !ok {
		return fmt.Errorf("the source host: %w", ErrNoSuchJob)
	}
	epA, err := d.Env.Endpoint(ctx, spec.Src)
	if err != nil {
		return connectErr(ctx, spec.Src, err)
	}
	epB, err := d.Env.Endpoint(ctx, spec.Dst)
	if err != nil {
		return connectErr(ctx, spec.Dst, err)
	}
	items, _, err := plan(ctx, epA, spec.Src.Path)
	if err != nil {
		return err
	}
	isDir := len(items) > 0 && items[0].dir && items[0].rel == ""
	total := totalSize(items)
	h.Total(total)
	bfs := epB.FS()
	if err := bfs.MkdirAll(spec.Dst.Path); err != nil {
		return err
	}
	final := bfs.Join(spec.Dst.Path, baseName(spec.Src.Path, true))
	skip, err := d.conflict(ctx, bfs, final, isDir, total, h)
	if err != nil || skip {
		if skip {
			h.Done(total)
		}
		return err
	}

	kh, cleanup, err := d.trustSource(bfs, src)
	if err != nil {
		return err
	}
	defer cleanup()

	useRsync := d.hasRsync(ctx, spec.Src.Host) && d.hasRsync(ctx, spec.Dst.Host)
	target := final + PartSuffix
	if isDir {
		target = final
	}
	cmd := directCommand(src, spec.Src.Path, target, kh, isDir, useRsync)

	if err := d.pull(ctx, epB, spec.Dst.Host, cmd, target, isDir, total, h); err != nil {
		return err
	}
	if !isDir {
		if err := bfs.Rename(target, final); err != nil {
			return err
		}
	}
	if err := d.check(ctx, epA, epB, spec, final, isDir, items, total); err != nil {
		return err
	}
	h.Done(total)
	return nil
}

// pull runs the copy command on the destination, with the agent forwarded for
// that command only, while polling the destination's size for progress.
func (d *Direct) pull(ctx context.Context, b Endpoint, dst model.ID, cmd, target string, isDir bool, total int64, h DirectHooks) error {
	stopPoll := d.poll(ctx, b, target, isDir, total, h)
	out, err := d.Env.ExecAgent(ctx, dst, cmd)
	stopPoll()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return fmt.Errorf("the server-to-server copy failed: %w: %s", err, tail(out))
}

// trustSource writes, on the destination, a known_hosts file that holds only
// the source's key (the destination has never met it). The returned function
// removes the file.
func (d *Direct) trustSource(bfs transport.FS, src model.Host) (path string, cleanup func(), err error) {
	lines := d.Env.HostKeyLines(src)
	if len(lines) == 0 {
		return "", nil, ErrNoHostKey
	}
	home, err := bfs.Home()
	if err != nil {
		return "", nil, err
	}
	kh := bfs.Join(home, ".tako-kh-"+rand.Text())
	if err := writeSmall(bfs, kh, strings.Join(lines, "\n")+"\n"); err != nil {
		return "", nil, err
	}
	return kh, func() {
		_ = bfs.Remove(kh) // the file holds only a public key; failing here is harmless
	}, nil
}

// conflict applies the same-name policy to the destination root. It reports
// whether the job is to be skipped.
func (d *Direct) conflict(ctx context.Context, bfs transport.FS, final string, isDir bool, total int64, h DirectHooks) (bool, error) {
	fi, err := bfs.Stat(final)
	if err != nil {
		if isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if fi.IsDir() != isDir {
		return false, fmt.Errorf("%s: %w", final, errNameTaken)
	}
	policy := h.Policy()
	if policy == PolicyAsk {
		dec, err := h.Ask(Conflict{Path: final, SrcSize: total, DstSize: fi.Size()})
		if err != nil {
			return false, err
		}
		policy = PolicySkip
		if dec.Overwrite {
			policy = PolicyOverwrite
		}
	}
	return policy == PolicySkip, context.Cause(ctx)
}

func (d *Direct) hasRsync(ctx context.Context, host model.ID) bool {
	out, err := d.Env.Exec(ctx, host, "command -v rsync >/dev/null 2>&1 && echo yes")
	return err == nil && strings.TrimSpace(string(out)) == "yes"
}

// check verifies the result: for a file by size and, when asked, SHA-256
// computed on each server; for a directory by file count and total size.
func (d *Direct) check(ctx context.Context, a, b Endpoint, spec Spec, final string, isDir bool, items []item, total int64) error {
	if isDir {
		got, _, err := plan(ctx, b, final)
		if err != nil {
			return err
		}
		if totalSize(got) < total || countFiles(got) < countFiles(items) {
			return fmt.Errorf("%s has %d bytes in %d files, want %d in %d: %w",
				final, totalSize(got), countFiles(got), total, countFiles(items), ErrVerify)
		}
		return nil
	}
	fi, err := b.FS().Stat(final)
	if err != nil {
		return err
	}
	if fi.Size() != total {
		return fmt.Errorf("%s is %d bytes, want %d: %w", final, fi.Size(), total, ErrVerify)
	}
	if spec.Verify != VerifyHash {
		return nil
	}
	as, aerr := a.Hash(ctx, spec.Src.Path)
	bs, berr := b.Hash(ctx, final)
	if err := errors.Join(aerr, berr); err != nil {
		return err
	}
	if as != bs {
		return fmt.Errorf("%s: %w", final, ErrVerify)
	}
	return nil
}

// poll reports the destination's size while the copy runs; the returned
// function stops it.
func (d *Direct) poll(ctx context.Context, b Endpoint, target string, isDir bool, total int64, h DirectHooks) func() {
	pctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		t := time.NewTicker(cmpOr(d.PollEvery, 500*time.Millisecond))
		defer t.Stop()
		for {
			select {
			case <-pctx.Done():
				return
			case <-t.C:
			}
			if isDir {
				continue // walking a whole tree every half second would cost more than it tells
			}
			if fi, err := b.FS().Stat(target); err == nil {
				h.Done(min(fi.Size(), total))
			}
		}
	})
	return func() { cancel(); wg.Wait() }
}

// directCommand builds the command run on the destination server.
func directCommand(src model.Host, srcPath, target, knownHosts string, isDir, rsync bool) string {
	port := cmpOr(src.Port, 22)
	host := src.Addr
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	remote := src.User + "@" + host + ":" + srcPath
	opts := []string{
		"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes",
		"-o", "UserKnownHostsFile=" + knownHosts, "-o", "ForwardAgent=no",
		"-o", "PasswordAuthentication=no", "-o", "ConnectTimeout=15",
	}
	quoted := make([]string, len(opts))
	for i, o := range opts {
		quoted[i] = ShellQuote(o)
	}
	if rsync {
		ssh := "ssh -p " + strconv.Itoa(port) + " " + strings.Join(opts, " ")
		args := "-a --partial --append-verify -e " + ShellQuote(ssh)
		if isDir {
			return "rsync " + args + " " + ShellQuote(remote+"/") + " " + ShellQuote(target+"/")
		}
		return "rsync " + args + " " + ShellQuote(remote) + " " + ShellQuote(target)
	}
	base := "scp -q -P " + strconv.Itoa(port) + " " + strings.Join(quoted, " ")
	if isDir {
		// scp -r copies the directory itself into the destination's parent.
		return "mkdir -p " + ShellQuote(target) + " && " + base + " -r " + ShellQuote(remote+"/.") + " " + ShellQuote(target)
	}
	return base + " " + ShellQuote(remote) + " " + ShellQuote(target)
}

func writeSmall(fsys transport.FS, path, content string) error {
	f, err := fsys.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return err
	}
	_, werr := f.Write([]byte(content))
	if err := errors.Join(werr, f.Close()); err != nil {
		_ = fsys.Remove(path)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// tail is the end of a command's output, for an error message.
func tail(out []byte) string {
	s := strings.TrimSpace(string(out))
	if len(s) > 300 {
		s = "…" + s[len(s)-300:]
	}
	return s
}
