package session

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wei-dong/tako/internal/transport"
)

// Pane is one interactive terminal channel on a [Session]. See the package
// documentation for its contract.
type Pane struct {
	out    chan []byte
	closed chan struct{}
	once   sync.Once

	mu      sync.Mutex // guards the fields below
	shell   transport.Shell
	size    transport.Size
	pending []byte // unread tail of the last output chunk
	exited  bool   // the remote shell ended on its own

	resizeTimer        *time.Timer
	startDir, startCmd string      // typed into the first shell only
	started            atomic.Bool // a shell has been attached at least once
	dropped            atomic.Uint64
	onDrop             atomic.Pointer[func()]
}

func newPane(size transport.Size) *Pane {
	return &Pane{
		out:    make(chan []byte, 64),
		closed: make(chan struct{}),
		size:   size,
	}
}

// Read implements io.Reader. It blocks while the connection is down.
func (p *Pane) Read(b []byte) (int, error) {
	p.mu.Lock()
	if len(p.pending) > 0 {
		n := copy(b, p.pending)
		p.pending = p.pending[n:]
		p.mu.Unlock()
		return n, nil
	}
	p.mu.Unlock()
	select {
	case chunk := <-p.out:
		n := copy(b, chunk)
		if n < len(chunk) {
			p.mu.Lock()
			p.pending = chunk[n:]
			p.mu.Unlock()
		}
		return n, nil
	case <-p.closed:
		// Drain what was already produced before reporting the end.
		select {
		case chunk := <-p.out:
			n := copy(b, chunk)
			if n < len(chunk) {
				p.mu.Lock()
				p.pending = chunk[n:]
				p.mu.Unlock()
			}
			return n, nil
		default:
		}
		return 0, io.EOF
	}
}

// Write implements io.Writer. While the pane is disconnected the bytes are
// dropped and counted, and the call still succeeds, because a terminal treats
// a Write error as the end of its input.
func (p *Pane) Write(b []byte) (int, error) {
	select {
	case <-p.closed:
		return 0, io.ErrClosedPipe
	default:
	}
	p.mu.Lock()
	sh := p.shell
	p.mu.Unlock()
	if sh == nil {
		p.dropped.Add(uint64(len(b)))
		if fn := p.onDrop.Load(); fn != nil {
			(*fn)()
		}
		return len(b), nil
	}
	if _, err := sh.Write(b); err != nil {
		// The connection broke under us; the session will notice and
		// reconnect. These bytes are lost like any other while down.
		p.dropped.Add(uint64(len(b)))
	}
	return len(b), nil
}

// resizeDelay is how long a window-change waits for the size to settle, so
// dragging a window edge sends one request instead of one per frame.
const resizeDelay = 100 * time.Millisecond

// Resize implements the terminal's Resize(cols, rows int) error. The size is
// remembered at once and applied to the next shell after a reconnect; the
// running shell is told after the size has been still for [resizeDelay].
func (p *Pane) Resize(cols, rows int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.size.Cols == cols && p.size.Rows == rows {
		return nil
	}
	p.size = transport.Size{Cols: cols, Rows: rows}
	if p.shell == nil {
		return nil
	}
	if p.resizeTimer != nil {
		p.resizeTimer.Stop()
	}
	p.resizeTimer = time.AfterFunc(resizeDelay, p.applyResize)
	return nil
}

func (p *Pane) applyResize() {
	p.mu.Lock()
	sh, size := p.shell, p.size
	p.mu.Unlock()
	if sh == nil {
		return
	}
	// A failed resize on a dying connection is retried by the reconnect,
	// which opens the next shell at the stored size.
	_ = sh.Resize(size.Cols, size.Rows)
}

// Size returns the last size set with Resize (or the initial size).
func (p *Pane) Size() transport.Size {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.size
}

// Connected reports whether a remote shell is attached right now.
func (p *Pane) Connected() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shell != nil
}

// Dropped returns how many input bytes were discarded while disconnected.
func (p *Pane) Dropped() uint64 { return p.dropped.Load() }

// OnDrop sets a function called (on the writer's goroutine) whenever input is
// discarded because the pane is disconnected. Pass nil to clear it.
func (p *Pane) OnDrop(fn func()) {
	if fn == nil {
		p.onDrop.Store(nil)
		return
	}
	p.onDrop.Store(&fn)
}

// Done is closed when the pane has ended for good.
func (p *Pane) Done() <-chan struct{} { return p.closed }

// ShellExited reports whether the pane ended because the remote shell exited
// (as opposed to a local Close).
func (p *Pane) ShellExited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exited
}

// Close ends the pane: Read returns io.EOF and the remote shell is closed.
func (p *Pane) Close() error {
	var err error
	p.once.Do(func() {
		close(p.closed)
		p.mu.Lock()
		if p.resizeTimer != nil {
			p.resizeTimer.Stop()
		}
		sh := p.shell
		p.shell = nil
		p.mu.Unlock()
		if sh != nil {
			if cerr := sh.Close(); cerr != nil && !errors.Is(cerr, io.EOF) {
				err = cerr
			}
		}
	})
	return err
}

// inject adds locally generated output (such as a "connection lost" notice)
// to the stream the terminal reads.
func (p *Pane) inject(b []byte) {
	if len(b) == 0 {
		return
	}
	select {
	case p.out <- b:
	case <-p.closed:
	}
}

// attach connects a new remote shell. It starts the output pump, which ends
// when the shell does, and returns a channel closed when that happens.
func (p *Pane) attach(sh transport.Shell) <-chan struct{} {
	p.mu.Lock()
	if p.shell != nil {
		_ = p.shell.Close()
	}
	p.shell = sh
	p.mu.Unlock()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		buf := make([]byte, 32*1024)
		for {
			n, err := sh.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case p.out <- chunk:
				case <-p.closed:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return ended
}

// detach forgets the current shell after the connection died.
func (p *Pane) detach(sh transport.Shell) {
	p.mu.Lock()
	if p.shell == sh {
		p.shell = nil
	}
	p.mu.Unlock()
	_ = sh.Close()
}

// markExited ends the pane because the remote shell exited.
func (p *Pane) markExited() {
	p.mu.Lock()
	p.exited = true
	p.shell = nil
	p.mu.Unlock()
	_ = p.Close()
}
