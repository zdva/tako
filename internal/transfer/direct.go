package transfer

import "context"

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
	err := q.opts.Direct.Run(ctx, spec, h)
	if err == nil {
		q.set(j, func(i *JobInfo) { i.FilesDone = i.FilesTotal })
	}
	return err
}
