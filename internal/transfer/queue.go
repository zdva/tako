package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults.
const (
	DefaultConcurrency = 3
	DefaultRetries     = 5
	DefaultRetryDelay  = 2 * time.Second
	maxRetryDelay      = 30 * time.Second
	defaultTick        = 250 * time.Millisecond
)

// Causes a job's context is cancelled with.
var (
	errPaused   = errors.New("paused")
	errCanceled = errors.New("canceled")
	errClosed   = errors.New("queue closed")
)

// Options configure a [Queue].
type Options struct {
	// Provider supplies the endpoints. Required.
	Provider Provider
	// Direct runs direct jobs; nil turns direct mode off.
	Direct DirectRunner
	// Concurrency is how many jobs run at once; zero means
	// [DefaultConcurrency].
	Concurrency int
	// Retries is how many times a job retries by itself after a lost
	// connection before it waits for the user; zero means [DefaultRetries],
	// negative means never.
	Retries int
	// RetryDelay is the first wait before a retry (it doubles up to 30 s);
	// zero means [DefaultRetryDelay].
	RetryDelay time.Duration
	// Tick is how often progress is reported; zero means 250 ms.
	Tick time.Duration
}

// Decision answers a same-name conflict.
type Decision struct {
	// Overwrite replaces the file; otherwise it is skipped.
	Overwrite bool
	// All applies the answer to the rest of the job.
	All bool
}

// Queue runs transfer jobs.
type Queue struct {
	opts Options

	mu      sync.Mutex
	jobs    []*job
	nextID  int
	conc    int
	closed  bool
	tickOn  bool
	wg      sync.WaitGroup
	subsMu  sync.Mutex
	subs    map[int]func(JobInfo)
	nextSub int
	wake    chan struct{}
	stop    chan struct{}
}

type job struct {
	info    JobInfo
	cancel  context.CancelCauseFunc
	running bool        // a goroutine is working on it
	timer   *time.Timer // pending automatic retry
	decide  chan Decision
	policy  Policy
	retries int
	rewind  int64 // how far a continued copy steps back

	cur       atomic.Int64 // bytes of the current file in place
	doneBytes atomic.Int64 // bytes of finished files
	done      map[string]bool
	part      string // the part file being written, for clean-up
	partEnd   Endpoint

	lastBytes int64
	lastAt    time.Time
}

// New returns a queue.
func New(o Options) *Queue {
	q := &Queue{
		opts: o, conc: cmpOr(o.Concurrency, DefaultConcurrency),
		subs: map[int]func(JobInfo){}, wake: make(chan struct{}, 1), stop: make(chan struct{}),
	}
	q.wg.Go(q.ticker)
	return q
}

func cmpOr[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// Add queues a job and returns its ID.
func (q *Queue) Add(spec Spec) (int, error) {
	if spec.Mode == ModeDirect && q.opts.Direct == nil {
		return 0, ErrDirectDisabled
	}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return 0, ErrQueueClosed
	}
	q.nextID++
	j := &job{
		info: JobInfo{ID: q.nextID, Spec: spec, State: StateQueued,
			Name: baseName(spec.Src.Path, !spec.Src.Local())},
		policy: spec.Policy, done: map[string]bool{}, decide: make(chan Decision, 1),
	}
	q.jobs = append(q.jobs, j)
	q.mu.Unlock()
	q.changed(j)
	q.pump()
	return j.info.ID, nil
}

// SetConcurrency changes how many jobs run at once.
func (q *Queue) SetConcurrency(n int) {
	q.mu.Lock()
	q.conc = max(n, 1)
	q.mu.Unlock()
	q.pump()
}

// Snapshot lists every job, oldest first.
func (q *Queue) Snapshot() []JobInfo {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]JobInfo, len(q.jobs))
	for i, j := range q.jobs {
		out[i] = q.infoLocked(j)
	}
	return out
}

// Job returns one job's snapshot.
func (q *Queue) Job(id int) (JobInfo, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if j := q.findLocked(id); j != nil {
		return q.infoLocked(j), true
	}
	return JobInfo{}, false
}

// ActiveCount is how many jobs are not finished (the toolbar badge).
func (q *Queue) ActiveCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, j := range q.jobs {
		if j.info.State.Active() {
			n++
		}
	}
	return n
}

// Subscribe calls fn with a job's snapshot whenever it changes (state at
// once, progress every tick). fn must return quickly. It returns the
// unsubscribe function.
func (q *Queue) Subscribe(fn func(JobInfo)) func() {
	q.subsMu.Lock()
	defer q.subsMu.Unlock()
	q.nextSub++
	id := q.nextSub
	q.subs[id] = fn
	return func() {
		q.subsMu.Lock()
		delete(q.subs, id)
		q.subsMu.Unlock()
	}
}

func (q *Queue) findLocked(id int) *job {
	for _, j := range q.jobs {
		if j.info.ID == id {
			return j
		}
	}
	return nil
}

func (q *Queue) infoLocked(j *job) JobInfo {
	info := j.info
	if info.State == StateRunning || info.State == StateAsking || info.State == StatePaused {
		info.Done = j.doneBytes.Load() + j.cur.Load()
		if info.Total > 0 {
			info.Done = min(info.Done, info.Total)
		}
	}
	if info.State == StatePaused || info.State == StateFailed {
		info.Resumable = j.doneBytes.Load() + max(j.cur.Load()-j.rewind, 0)
	}
	if info.Conflict != nil {
		c := *info.Conflict
		info.Conflict = &c
	}
	return info
}

// changed tells subscribers about j.
func (q *Queue) changed(j *job) {
	q.mu.Lock()
	info := q.infoLocked(j)
	q.mu.Unlock()
	q.subsMu.Lock()
	fns := make([]func(JobInfo), 0, len(q.subs))
	for _, fn := range q.subs {
		fns = append(fns, fn)
	}
	q.subsMu.Unlock()
	for _, fn := range fns {
		fn(info)
	}
}

// Pause stops a job where it is; Resume continues it.
func (q *Queue) Pause(id int) error {
	q.mu.Lock()
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		return ErrNoSuchJob
	}
	stopTimer(j)
	switch {
	case j.running:
		j.cancel(errPaused)
		q.mu.Unlock()
		return nil
	case j.info.State == StateQueued || j.info.State == StatePaused:
		j.info.State, j.info.Reason = StatePaused, ReasonUser
		q.mu.Unlock()
		q.changed(j)
		return nil
	}
	q.mu.Unlock()
	return nil
}

// Resume continues a paused or failed job (and retries one that is waiting
// for an automatic retry right now).
func (q *Queue) Resume(id int) error {
	q.mu.Lock()
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		return ErrNoSuchJob
	}
	if (j.info.State == StatePaused || j.info.State == StateFailed) && !j.running {
		stopTimer(j)
		j.info.State, j.info.Reason, j.info.Err = StateQueued, ReasonNone, ""
		j.info.Attempt, j.retries = 0, 0
		j.info.Finished = time.Time{}
		q.mu.Unlock()
		q.changed(j)
		q.pump()
		return nil
	}
	q.mu.Unlock()
	return nil
}

// Cancel ends a job and removes its half-written file.
func (q *Queue) Cancel(id int) error {
	q.mu.Lock()
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		return ErrNoSuchJob
	}
	stopTimer(j)
	switch {
	case j.running:
		j.cancel(errCanceled)
		q.mu.Unlock()
		return nil
	case j.info.State.Active() || j.info.State == StateFailed:
		j.info.State, j.info.Finished = StateCanceled, time.Now()
		q.mu.Unlock()
		q.changed(j)
		return nil
	}
	q.mu.Unlock()
	return nil
}

// Remove drops a finished job from the list.
func (q *Queue) Remove(id int) {
	q.mu.Lock()
	q.jobs = slices.DeleteFunc(q.jobs, func(j *job) bool {
		return j.info.ID == id && !j.info.State.Active()
	})
	q.mu.Unlock()
}

// ClearFinished drops every finished job.
func (q *Queue) ClearFinished() {
	q.mu.Lock()
	q.jobs = slices.DeleteFunc(q.jobs, func(j *job) bool { return !j.info.State.Active() && j.info.State != StateFailed })
	q.mu.Unlock()
}

// Resolve answers the question an Asking job is waiting on.
func (q *Queue) Resolve(id int, d Decision) error {
	q.mu.Lock()
	j := q.findLocked(id)
	if j == nil {
		q.mu.Unlock()
		return ErrNoSuchJob
	}
	asking := j.info.State == StateAsking
	q.mu.Unlock()
	if !asking {
		return ErrNotAsking
	}
	select {
	case j.decide <- d:
	default:
	}
	return nil
}

// Close stops every job (leaving partial files for a later continue) and
// waits for the queue's goroutines.
func (q *Queue) Close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	for _, j := range q.jobs {
		stopTimer(j)
		if j.running {
			j.cancel(errClosed)
		}
	}
	q.mu.Unlock()
	close(q.stop)
	q.wg.Wait()
}

func stopTimer(j *job) {
	if j.timer != nil {
		j.timer.Stop()
		j.timer = nil
	}
}

// pump starts queued jobs while there is room.
func (q *Queue) pump() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	running := 0
	for _, j := range q.jobs {
		if j.running {
			running++
		}
	}
	for _, j := range q.jobs {
		if running >= q.conc {
			break
		}
		if j.info.State != StateQueued || j.running {
			continue
		}
		running++
		j.running = true
		ctx, cancel := context.WithCancelCause(context.Background())
		j.cancel = cancel
		j.info.State, j.info.Reason, j.info.Err = StateRunning, ReasonNone, ""
		if j.info.Started.IsZero() {
			j.info.Started = time.Now()
		}
		j.lastAt, j.lastBytes = time.Now(), j.doneBytes.Load()+j.cur.Load()
		q.wg.Go(func() { q.run(ctx, j) })
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// ticker reports progress while jobs run and sleeps otherwise.
func (q *Queue) ticker() {
	every := cmpOr(q.opts.Tick, defaultTick)
	for {
		select {
		case <-q.stop:
			return
		case <-q.wake:
		}
		t := time.NewTicker(every)
		for q.tickOnce(t.C) {
		}
		t.Stop()
	}
}

// tickOnce waits one tick, reports, and says whether any job still runs.
func (q *Queue) tickOnce(c <-chan time.Time) bool {
	select {
	case <-q.stop:
		return false
	case <-c:
	}
	q.mu.Lock()
	var live []*job
	now := time.Now()
	for _, j := range q.jobs {
		if !j.running {
			continue
		}
		live = append(live, j)
		if j.info.State != StateRunning {
			j.info.Speed = 0
			continue
		}
		bytes := j.doneBytes.Load() + j.cur.Load()
		if dt := now.Sub(j.lastAt).Seconds(); dt > 0 {
			inst := max(float64(bytes-j.lastBytes)/dt, 0)
			if j.info.Speed == 0 {
				j.info.Speed = inst
			} else {
				j.info.Speed = 0.7*j.info.Speed + 0.3*inst
			}
		}
		j.lastBytes, j.lastAt = bytes, now
	}
	q.mu.Unlock()
	for _, j := range live {
		q.changed(j)
	}
	return len(live) > 0
}

// run is one attempt at a job.
func (q *Queue) run(ctx context.Context, j *job) {
	progressBefore := j.doneBytes.Load() + j.cur.Load()
	err := q.attempt(ctx, j)
	cause := context.Cause(ctx)

	q.mu.Lock()
	j.running = false
	j.info.Speed = 0
	switch {
	case err == nil:
		j.info.State, j.info.Finished = StateDone, time.Now()
		j.info.Done, j.info.Err = j.info.Total, ""
		j.info.Current, j.info.Conflict = "", nil
	case errors.Is(cause, errPaused):
		j.info.State, j.info.Reason, j.info.Err = StatePaused, ReasonUser, ""
	case errors.Is(cause, errCanceled):
		j.info.State, j.info.Finished = StateCanceled, time.Now()
		j.info.Conflict = nil
	case errors.Is(cause, errClosed):
		j.info.State, j.info.Reason = StatePaused, ReasonUser
	case isNetwork(err):
		j.info.State, j.info.Reason, j.info.Err = StatePaused, ReasonNetwork, err.Error()
		if j.doneBytes.Load()+j.cur.Load() > progressBefore {
			j.retries = 0 // it got somewhere: the next failure is a new one
		}
		if limit := q.retryLimit(); j.retries < limit && !q.closed {
			j.retries++
			j.info.Attempt = j.retries
			delay := min(cmpOr(q.opts.RetryDelay, DefaultRetryDelay)<<(j.retries-1), maxRetryDelay)
			j.timer = time.AfterFunc(delay, func() { q.retry(j) })
		}
	default:
		j.info.State, j.info.Err, j.info.Finished = StateFailed, err.Error(), time.Now()
		j.info.Conflict = nil
	}
	canceled := j.info.State == StateCanceled
	part, partEnd := j.part, j.partEnd
	j.part, j.partEnd = "", nil
	if j.info.State != StatePaused {
		j.cancel(nil)
	}
	q.mu.Unlock()

	if canceled && part != "" && partEnd != nil {
		_ = partEnd.FS().Remove(part)
	}
	q.changed(j)
	q.pump()
}

func (q *Queue) retryLimit() int {
	switch {
	case q.opts.Retries < 0:
		return 0
	case q.opts.Retries == 0:
		return DefaultRetries
	}
	return q.opts.Retries
}

// retry is the automatic retry timer.
func (q *Queue) retry(j *job) {
	q.mu.Lock()
	if j.info.State != StatePaused || j.info.Reason != ReasonNetwork || j.running || q.closed {
		q.mu.Unlock()
		return
	}
	j.timer = nil
	j.info.State = StateQueued
	q.mu.Unlock()
	q.changed(j)
	q.pump()
}

func (q *Queue) set(j *job, f func(*JobInfo)) {
	q.mu.Lock()
	f(&j.info)
	q.mu.Unlock()
}

// attempt does the work of a job until it finishes or is interrupted.
func (q *Queue) attempt(ctx context.Context, j *job) error {
	q.changed(j)
	spec := j.info.Spec
	if spec.Mode == ModeDirect {
		return q.attemptDirect(ctx, j)
	}
	src, err := q.opts.Provider.Endpoint(ctx, spec.Src)
	if err != nil {
		return connectErr(ctx, spec.Src, err)
	}
	dst, err := q.opts.Provider.Endpoint(ctx, spec.Dst)
	if err != nil {
		return connectErr(ctx, spec.Dst, err)
	}
	err = q.copyTree(ctx, j, src, dst)
	if err != nil && isNetwork(err) {
		q.opts.Provider.Reset(spec.Src)
		q.opts.Provider.Reset(spec.Dst)
	}
	return err
}

// connectErr marks a failure to reach an endpoint as a network failure so the
// job waits and retries, unless the job was stopped.
func connectErr(ctx context.Context, l Location, err error) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}
	return &netError{fmt.Errorf("reaching %s: %w", cmpOr(string(l.Host), "this computer"), err)}
}

func (q *Queue) copyTree(ctx context.Context, j *job, src, dst Endpoint) error {
	spec := j.info.Spec
	items, links, err := plan(ctx, src, spec.Src.Path)
	if err != nil {
		return err
	}
	q.set(j, func(i *JobInfo) {
		i.Total, i.FilesTotal, i.Skipped = totalSize(items), countFiles(items), links
	})
	j.rewind = 0
	if !isLocal(dst) {
		j.rewind = resumeRewind
	}
	dfs := dst.FS()
	if err := dfs.MkdirAll(spec.Dst.Path); err != nil {
		return err
	}
	dstRoot := dfs.Join(spec.Dst.Path, baseName(spec.Src.Path, !spec.Src.Local()))

	for _, it := range items {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if j.done[it.rel] {
			continue
		}
		target := at(dst, dstRoot, it.rel)
		if it.dir {
			if err := dfs.MkdirAll(target); err != nil {
				return err
			}
			j.done[it.rel] = true
			continue
		}
		skip, err := q.settle(ctx, j, dst, it, target)
		if err != nil {
			return err
		}
		if skip {
			j.doneBytes.Add(it.size)
			q.set(j, func(i *JobInfo) { i.Skipped++; i.FilesDone++ })
			j.done[it.rel] = true
			continue
		}
		q.set(j, func(i *JobInfo) { i.Current = cmpOr(it.rel, j.info.Name) })
		q.mu.Lock()
		j.part, j.partEnd = target+PartSuffix, dst
		q.mu.Unlock()
		c := &copyJob{
			src: src, dst: dst, srcPath: at(src, spec.Src.Path, it.rel), dstPath: target,
			size: it.size, verify: spec.Verify, cur: &j.cur,
		}
		err = copyFile(ctx, c)
		if err != nil {
			return err
		}
		j.cur.Store(0)
		j.doneBytes.Add(it.size)
		q.set(j, func(i *JobInfo) { i.FilesDone++ })
		j.done[it.rel] = true
	}
	return nil
}

func countFiles(items []item) (n int) {
	for _, it := range items {
		if !it.dir {
			n++
		}
	}
	return n
}

// settle applies the same-name policy to a file about to be written. It
// reports whether the file is to be skipped.
func (q *Queue) settle(ctx context.Context, j *job, dst Endpoint, it item, target string) (bool, error) {
	fi, err := dst.FS().Stat(target)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if fi.IsDir() {
		return false, fmt.Errorf("%s is a directory: %w", target, errNameTaken)
	}
	q.mu.Lock()
	policy := j.policy
	q.mu.Unlock()
	if policy == PolicyAsk {
		d, err := q.ask(ctx, j, Conflict{Path: cmpOr(it.rel, j.info.Name), SrcSize: it.size, DstSize: fi.Size()})
		if err != nil {
			return false, err
		}
		policy = PolicySkip
		if d.Overwrite {
			policy = PolicyOverwrite
		}
		if d.All {
			q.mu.Lock()
			j.policy = policy
			q.mu.Unlock()
		}
	}
	return policy == PolicySkip, nil
}

var errNameTaken = errors.New("the name is taken")

// ask puts the job in the Asking state until Resolve answers.
func (q *Queue) ask(ctx context.Context, j *job, c Conflict) (Decision, error) {
	select { // drop a stale answer
	case <-j.decide:
	default:
	}
	q.set(j, func(i *JobInfo) { i.State, i.Conflict = StateAsking, &c })
	q.changed(j)
	select {
	case d := <-j.decide:
		q.set(j, func(i *JobInfo) { i.State, i.Conflict = StateRunning, nil })
		q.changed(j)
		return d, nil
	case <-ctx.Done():
		return Decision{}, context.Cause(ctx)
	}
}
