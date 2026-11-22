package transfer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/zdva/tako/internal/transport"
)

const (
	// resumeRewind is how far a resumed copy to a remote server steps back
	// from the end of the partial file. SFTP writes run several requests in
	// flight, so after a lost connection the tail of the partial file may
	// have gaps; the in-flight window is 64 requests of 128 KiB, and this is
	// twice that.
	resumeRewind = 16 << 20
	// resumeAlign keeps the resume offset on a block boundary.
	resumeAlign = 64 << 10
)

// errShort is a source that ended before its measured size.
var errShort = errors.New("the source ended early")

// copyJob is one file copy.
type copyJob struct {
	src, dst         Endpoint
	srcPath, dstPath string
	size             int64
	verify           VerifyMode
	// cur receives the bytes of this file that are in place; copyFile sets it
	// to the resume offset first.
	cur *atomic.Int64
	// resumed is set to the offset the copy continued from (0 for a fresh one).
	resumed int64
}

func isLocal(e Endpoint) bool {
	_, ok := e.(localEndpoint)
	return ok
}

// copyFile copies the file to dstPath+PartSuffix, verifies it and renames it
// into place. A part file left by an earlier attempt is continued.
func copyFile(ctx context.Context, c *copyJob) error {
	dfs := c.dst.FS()
	part := c.dstPath + PartSuffix
	for attempt := range 2 {
		off := resumeOffset(c, part)
		if attempt > 0 {
			off = 0 // the continued copy failed verification: redo it whole
		}
		c.resumed = off
		c.cur.Store(off)
		if err := stream(ctx, c, part, off); err != nil {
			return err
		}
		err := verify(ctx, c, part, off > 0)
		if err == nil {
			if err := dfs.Rename(part, c.dstPath); err != nil {
				return fmt.Errorf("finishing %s: %w", c.dstPath, err)
			}
			return nil
		}
		if !errors.Is(err, ErrVerify) || off == 0 {
			return err
		}
		_ = dfs.Remove(part)
	}
	return ErrVerify
}

// resumeOffset is where a copy into part continues: the size of the part file
// it finds, stepped back when the destination writes in parallel.
func resumeOffset(c *copyJob, part string) int64 {
	fi, err := c.dst.FS().Stat(part)
	if err != nil {
		return 0
	}
	n := fi.Size()
	if n > c.size {
		return 0
	}
	if !isLocal(c.dst) {
		n = max(n-resumeRewind, 0)
	}
	return n - n%resumeAlign
}

// stream copies the source from off into the part file.
func stream(ctx context.Context, c *copyJob, part string, off int64) error {
	in, err := c.src.FS().Open(c.srcPath)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	flag := os.O_WRONLY | os.O_CREATE
	if off == 0 {
		flag |= os.O_TRUNC
	}
	out, err := c.dst.FS().OpenFile(part, flag)
	if err != nil {
		return err
	}
	if off > 0 {
		if err := out.Truncate(off); err != nil {
			_ = out.Close()
			return fmt.Errorf("trimming %s: %w", part, err)
		}
		if _, err := in.Seek(off, io.SeekStart); err != nil {
			_ = out.Close()
			return fmt.Errorf("seeking in %s: %w", c.srcPath, err)
		}
		if _, err := out.Seek(off, io.SeekStart); err != nil {
			_ = out.Close()
			return fmt.Errorf("seeking in %s: %w", part, err)
		}
	}
	n, err := pump(ctx, c, in, out)
	cerr := out.Close()
	if err != nil {
		return err
	}
	if cerr != nil {
		return fmt.Errorf("closing %s: %w", part, cerr)
	}
	if want := c.size - off; n != want {
		return fmt.Errorf("copying %s: %d of %d bytes: %w", c.srcPath, n, want, errShort)
	}
	fi, err := c.dst.FS().Stat(part)
	if err != nil {
		return err
	}
	if fi.Size() != c.size {
		return fmt.Errorf("%s is %d bytes, want %d: %w", part, fi.Size(), c.size, ErrVerify)
	}
	return nil
}

// pump moves the bytes, picking the arrangement that keeps SFTP's parallel
// requests busy: a remote source is read with WriteTo, a remote destination
// written with ReadFrom, and remote to remote runs both through a pipe.
func pump(ctx context.Context, c *copyJob, in, out transport.File) (int64, error) {
	cw := &countWriter{ctx: ctx, w: out, n: c.cur}
	srcRemote, dstRemote := !isLocal(c.src), !isLocal(c.dst)
	wt, canWrite := in.(io.WriterTo)
	rf, canRead := out.(io.ReaderFrom)
	switch {
	case srcRemote && dstRemote && canWrite && canRead:
		return relay(ctx, c, wt, rf)
	case dstRemote && canRead:
		n, err := rf.ReadFrom(&countReader{ctx: ctx, r: in, n: c.cur})
		return n, wrapIO("writing", c.dstPath, err)
	case srcRemote && canWrite:
		n, err := wt.WriteTo(cw)
		return n, wrapIO("reading", c.srcPath, err)
	default:
		n, err := io.Copy(cw, struct{ io.Reader }{in}) // no WriterTo: a plain copy loop
		return n, wrapIO("copying", c.srcPath, err)
	}
}

// relay runs a remote file's parallel reads into a remote file's parallel
// writes. A short queue of buffers between them lets each side keep its
// requests in flight while the other is busy; it is the only memory the copy
// holds besides SFTP's own windows.
func relay(ctx context.Context, c *copyJob, wt io.WriterTo, rf io.ReaderFrom) (int64, error) {
	p := newBufPipe()
	type result struct {
		n   int64
		err error
	}
	readDone := make(chan result, 1)
	go func() {
		n, err := wt.WriteTo(&countWriter{ctx: ctx, w: p, n: nil})
		p.closeWrite(err) // nil reads as io.EOF
		readDone <- result{n, err}
	}()
	n, werr := rf.ReadFrom(&countReader{ctx: ctx, r: p, n: c.cur})
	p.closeRead(errors.Join(werr, io.ErrClosedPipe))
	r := <-readDone
	if werr != nil {
		return n, wrapIO("writing", c.dstPath, werr)
	}
	if r.err != nil && !errors.Is(r.err, io.ErrClosedPipe) {
		return n, wrapIO("reading", c.srcPath, r.err)
	}
	return n, nil
}

const (
	// pipeDepth is how many buffers may wait between reader and writer.
	pipeDepth = 8
	// pipeChunk is the size of one buffer.
	pipeChunk = 256 << 10
)

var chunkPool = sync.Pool{New: func() any { b := make([]byte, 0, pipeChunk); return &b }}

// bufPipe is io.Pipe with a bounded queue of buffers in between.
type bufPipe struct {
	ch   chan *[]byte
	done chan struct{} // closed when the reading side gives up
	once sync.Once
	mu   sync.Mutex
	rerr error // why the reader gave up
	werr error // how the writer ended; nil is io.EOF
	cur  *[]byte
	off  int
}

func newBufPipe() *bufPipe {
	return &bufPipe{ch: make(chan *[]byte, pipeDepth), done: make(chan struct{})}
}

// Write copies p into queued buffers, blocking while the queue is full.
func (b *bufPipe) Write(p []byte) (int, error) {
	total := 0
	for len(p) > 0 {
		chunk, ok := chunkPool.Get().(*[]byte)
		if !ok {
			chunk = new([]byte)
		}
		n := min(len(p), pipeChunk)
		*chunk = append((*chunk)[:0], p[:n]...)
		select {
		case b.ch <- chunk:
		case <-b.done:
			chunkPool.Put(chunk)
			b.mu.Lock()
			defer b.mu.Unlock()
			return total, b.rerr
		}
		p = p[n:]
		total += n
	}
	return total, nil
}

// Read serves the queued buffers in order.
func (b *bufPipe) Read(p []byte) (int, error) {
	if b.cur == nil {
		chunk, ok := <-b.ch
		if !ok {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.werr != nil {
				return 0, b.werr
			}
			return 0, io.EOF
		}
		b.cur, b.off = chunk, 0
	}
	n := copy(p, (*b.cur)[b.off:])
	b.off += n
	if b.off == len(*b.cur) {
		chunkPool.Put(b.cur)
		b.cur = nil
	}
	return n, nil
}

// closeWrite ends the stream; err (nil for a clean end) is what Read reports
// once the queue is empty.
func (b *bufPipe) closeWrite(err error) {
	b.mu.Lock()
	b.werr = err
	b.mu.Unlock()
	close(b.ch)
}

// closeRead makes Write fail with err.
func (b *bufPipe) closeRead(err error) {
	b.once.Do(func() {
		b.mu.Lock()
		b.rerr = err
		b.mu.Unlock()
		close(b.done)
	})
}

func wrapIO(verb, path string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s %s: %w", verb, path, err)
}

// verify compares the part file with the source: sizes always, SHA-256 when
// asked or when this copy continued an earlier one.
func verify(ctx context.Context, c *copyJob, part string, resumed bool) error {
	if c.verify == VerifySize || c.verify == VerifyAuto && !resumed {
		return nil
	}
	type result struct {
		sum string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		s, err := c.src.Hash(ctx, c.srcPath)
		ch <- result{s, err}
	}()
	dsum, derr := c.dst.Hash(ctx, part)
	r := <-ch
	if err := errors.Join(r.err, derr); err != nil {
		return err
	}
	if r.sum != dsum {
		return fmt.Errorf("%s: %w", c.dstPath, ErrVerify)
	}
	return nil
}

// countWriter counts what passes through and stops when ctx ends.
type countWriter struct {
	ctx context.Context //nolint:containedctx // an adapter that lives for one copy
	w   io.Writer
	n   *atomic.Int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	if err := context.Cause(c.ctx); err != nil {
		return 0, err
	}
	n, err := c.w.Write(p)
	if c.n != nil {
		c.n.Add(int64(n))
	}
	return n, err //nolint:wrapcheck // io.Writer contract
}

// countReader counts what passes through and stops when ctx ends.
type countReader struct {
	ctx context.Context //nolint:containedctx // an adapter that lives for one copy
	r   io.Reader
	n   *atomic.Int64
}

func (c *countReader) Read(p []byte) (int, error) {
	if err := context.Cause(c.ctx); err != nil {
		return 0, err
	}
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err //nolint:wrapcheck // io.Reader contract
}

// baseName is the last element of a path written in either style.
func baseName(p string, remote bool) string {
	p = strings.TrimRight(p, "/")
	if !remote {
		p = strings.TrimRight(p, `\`)
		if i := strings.LastIndexAny(p, `/\`); i >= 0 {
			return p[i+1:]
		}
		return p
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
