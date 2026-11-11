package transfer

import (
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
)

// netError marks an error as a lost or missing connection.
type netError struct{ err error }

func (e *netError) Error() string { return e.err.Error() }
func (e *netError) Unwrap() error { return e.err }

// isNetwork reports whether err means the connection failed (so a transfer
// should wait and continue) rather than that the request itself is wrong
// (permission denied, no space, no such file).
func isNetwork(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := errors.AsType[*netError](err); ok {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	var t interface{ Timeout() bool }
	if errors.As(err, &t) && t.Timeout() {
		return true
	}
	for _, e := range []error{syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE, syscall.ETIMEDOUT} {
		if errors.Is(err, e) {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"connection lost", "connection reset", "broken pipe", "use of closed", "not connected", "connection closed", "keepalive", "exited without exit status", "i/o timeout"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// isNotExist recognizes "no such file" from SFTP, which does not wrap
// fs.ErrNotExist in every path.
func isNotExist(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "not exist") ||
		strings.Contains(strings.ToLower(err.Error()), "no such file")
}
