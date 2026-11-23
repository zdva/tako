package inputbus

import "io"

// SyncConn sits between a terminal widget and its session. The widget writes
// everything it sends (keys, pastes, its answers to programs) to SyncConn;
// SyncConn passes it to the session and publishes it on the bus, which
// mirrors typed input to the other synced blocks.
//
// Bus destinations must write to Raw, never to the widget or to SyncConn:
// going back through the widget would publish the chunk again and loop.
type SyncConn struct {
	// Raw is the session: an SSH connection or a local PTY.
	Raw io.ReadWriteCloser
	Bus *Bus
	ID  ID
}

// Read returns the session's output.
func (c *SyncConn) Read(p []byte) (int, error) { return c.Raw.Read(p) }

// Write sends p to the session first, then mirrors it. The session's own
// result is returned: a failure of other blocks never fails this one.
func (c *SyncConn) Write(p []byte) (int, error) {
	n, err := c.Raw.Write(p)
	if err != nil {
		return n, err
	}
	c.Bus.Publish(c.ID, p)
	return n, nil
}

// Close closes the session.
func (c *SyncConn) Close() error { return c.Raw.Close() }

// Resize forwards the terminal's size to the session when it can take one;
// the terminal plugin discovers this method by interface assertion.
func (c *SyncConn) Resize(cols, rows int) error {
	if r, ok := c.Raw.(interface{ Resize(cols, rows int) error }); ok {
		return r.Resize(cols, rows)
	}
	return nil
}

// ResizePixels forwards the size with the cells' size in pixels to the
// session when it takes one, as a local pseudo-terminal does, else the size
// alone to Resize.
func (c *SyncConn) ResizePixels(cols, rows, cellWidth, cellHeight int) error {
	if r, ok := c.Raw.(interface {
		ResizePixels(cols, rows, cellWidth, cellHeight int) error
	}); ok {
		return r.ResizePixels(cols, rows, cellWidth, cellHeight)
	}
	return c.Resize(cols, rows)
}
