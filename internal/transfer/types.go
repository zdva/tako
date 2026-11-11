package transfer

import (
	"errors"
	"time"

	"github.com/wei-dong/tako/internal/model"
)

// PartSuffix marks a file that is still being written.
const PartSuffix = ".tako-part"

// Location is a path on this computer (Host is empty) or on a host.
type Location struct {
	Host model.ID
	Path string
}

// Local reports whether the location is on this computer.
func (l Location) Local() bool { return l.Host == "" }

// Mode is how bytes travel between two hosts.
type Mode int

// Modes.
const (
	// ModeRelay streams through this computer. It also covers uploads and
	// downloads, which have only this computer in the middle.
	ModeRelay Mode = iota
	// ModeDirect has the destination pull from the source with scp or rsync.
	ModeDirect
)

// Policy is what to do when the destination already has a file of that name.
type Policy int

// Policies.
const (
	// PolicyAsk stops the job until the user decides.
	PolicyAsk Policy = iota
	// PolicyOverwrite replaces the file.
	PolicyOverwrite
	// PolicySkip leaves the file alone.
	PolicySkip
)

// VerifyMode is how a finished file is checked.
type VerifyMode int

// Verify modes.
const (
	// VerifyAuto compares sizes, and also SHA-256 for a file that was resumed
	// (its first part came from an earlier attempt).
	VerifyAuto VerifyMode = iota
	// VerifyHash always compares SHA-256.
	VerifyHash
	// VerifySize only compares sizes.
	VerifySize
)

// Spec describes one job: copy Src (a file or a directory) into the
// directory Dst.
type Spec struct {
	Src, Dst Location
	Mode     Mode
	Policy   Policy
	Verify   VerifyMode
	// Fresh ignores part files that were already there when the job began;
	// the job's own retries still continue from the part it wrote.
	Fresh bool
}

// State is where a job is in its life.
type State int

// States.
const (
	StateQueued State = iota
	StateRunning
	// StatePaused is a job the user stopped or whose connection was lost; it
	// continues from where it was.
	StatePaused
	// StateAsking waits for a decision about a same-name file.
	StateAsking
	StateDone
	StateFailed
	StateCanceled
)

func (s State) String() string {
	return [...]string{"queued", "running", "paused", "asking", "done", "failed", "canceled"}[s]
}

// Active reports whether the job still holds a place in the queue (it is not
// finished).
func (s State) Active() bool { return s <= StateAsking }

// Reason says why a job is paused.
type Reason int

// Reasons.
const (
	ReasonNone Reason = iota
	ReasonUser
	// ReasonNetwork is a lost connection.
	ReasonNetwork
)

// Conflict is a same-name file the user must decide about.
type Conflict struct {
	Path             string
	SrcSize, DstSize int64
}

// JobInfo is a snapshot of a job for display.
type JobInfo struct {
	ID    int
	Spec  Spec
	Name  string // the base name of the source
	State State
	// Reason is set while Paused.
	Reason Reason
	// Bytes done and total over the whole job; Total is 0 until the source has
	// been measured.
	Done, Total int64
	// Files done and total (a single file is 1 of 1).
	FilesDone, FilesTotal int
	// Skipped counts files left alone because of the policy or because they
	// are links.
	Skipped int
	// Current is the file being copied, relative to the job's root.
	Current string
	// Resumable is the byte count a paused or failed job will continue from.
	Resumable int64
	// Speed in bytes per second, smoothed.
	Speed float64
	// Err is why a job Failed, or the last error of a Paused one.
	Err string
	// Conflict is set while Asking.
	Conflict *Conflict
	// Resumed is set once a file continued from an earlier part.
	Resumed bool
	// Attempt counts automatic retries since the last progress.
	Attempt  int
	Started  time.Time
	Finished time.Time
}

// ETA estimates the time left, or 0 when unknown.
func (j JobInfo) ETA() time.Duration {
	if j.State != StateRunning || j.Speed < 1 || j.Total <= j.Done {
		return 0
	}
	return time.Duration(float64(j.Total-j.Done) / j.Speed * float64(time.Second))
}

// Errors.
var (
	// ErrDirectDisabled is returned for a direct job when direct mode is off.
	ErrDirectDisabled = errors.New("direct transfer is not enabled")
	// ErrNoSuchJob is returned for an unknown job ID.
	ErrNoSuchJob = errors.New("no such transfer")
	// ErrNotAsking is returned by Resolve for a job that asked nothing.
	ErrNotAsking = errors.New("the transfer is not waiting for a decision")
	// ErrVerify is returned when a copied file does not match its source.
	ErrVerify = errors.New("the copy does not match the original")
	// ErrQueueClosed is returned by Add after Close.
	ErrQueueClosed = errors.New("the transfer queue is closed")
)
