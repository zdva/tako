package inputbus

import (
	"bytes"
	"io"
	"testing"
)

type fakeRaw struct {
	bytes.Buffer
	closed       bool
	cols, rows   int
	failWrites   bool
	resizeCalled bool
}

func (f *fakeRaw) Write(p []byte) (int, error) {
	if f.failWrites {
		return 0, io.ErrClosedPipe
	}
	return f.Buffer.Write(p)
}
func (f *fakeRaw) Close() error { f.closed = true; return nil }
func (f *fakeRaw) Resize(c, r int) error {
	f.cols, f.rows, f.resizeCalled = c, r, true
	return nil
}

type plainRaw struct{ io.ReadWriter }

func (plainRaw) Close() error { return nil }

func TestSyncConn_WritesRawFirstThenMirrors(t *testing.T) {
	t.Parallel()
	bus, recs := newBus(t, "a", "b")
	bus.SetSynced("a", true)
	bus.SetSynced("b", true)
	raw := &fakeRaw{}
	c := &SyncConn{Raw: raw, Bus: bus, ID: "a"}
	if n, err := c.Write([]byte("ls\n")); err != nil || n != 3 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if raw.String() != "ls\n" {
		t.Errorf("raw got %q", raw.String())
	}
	waitFor(t, "mirror", func() bool { return recs["b"].String() == "ls\n" })
	if !bus.Synced("a") || bus.Synced("zzz") {
		t.Error("Synced reports the wrong state")
	}
}

func TestSyncConn_FailedWriteIsNotMirrored(t *testing.T) {
	t.Parallel()
	bus, recs := newBus(t, "a", "b")
	bus.SetSynced("a", true)
	bus.SetSynced("b", true)
	c := &SyncConn{Raw: &fakeRaw{failWrites: true}, Bus: bus, ID: "a"}
	if _, err := c.Write([]byte("x")); err == nil {
		t.Fatal("write error swallowed")
	}
	if recs["b"].String() != "" {
		t.Error("a failed write was mirrored")
	}
}

func TestSyncConn_ReadCloseResize(t *testing.T) {
	t.Parallel()
	bus, _ := newBus(t)
	raw := &fakeRaw{}
	raw.WriteString("out")
	c := &SyncConn{Raw: raw, Bus: bus, ID: "a"}
	buf := make([]byte, 8)
	if n, _ := c.Read(buf); string(buf[:n]) != "out" {
		t.Errorf("Read = %q", buf[:n])
	}
	if err := c.Resize(100, 30); err != nil || raw.cols != 100 || raw.rows != 30 {
		t.Errorf("Resize: %v %d×%d", err, raw.cols, raw.rows)
	}
	if err := (&SyncConn{Raw: plainRaw{&bytes.Buffer{}}}).Resize(1, 1); err != nil {
		t.Errorf("Resize on a raw without it: %v", err)
	}
	if err := c.Close(); err != nil || !raw.closed {
		t.Errorf("Close: %v closed=%v", err, raw.closed)
	}
}
