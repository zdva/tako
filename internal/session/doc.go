// Package session turns a flaky network connection into something a terminal
// can treat as a stable device.
//
// A [Session] owns the connection(s) to one host and reconnects them forever
// with backoff, except after errors a retry cannot fix (a refused password, a
// changed host key). A [Pane] is one terminal channel on the session. Its
// contract, which terminal widgets rely on, is:
//
//   - Read blocks while the connection is down and never returns io.EOF
//     because of a network failure; it returns io.EOF only after Close or when
//     the remote shell itself exits.
//   - Write never fails while the pane is open. While disconnected the bytes
//     are dropped (never queued, never replayed after reconnecting) and
//     counted.
//   - After a reconnect the pane gets a fresh shell at the last known size,
//     and the scrollback the terminal already holds is untouched.
//
// All methods are safe for concurrent use. Callbacks run on goroutines of this
// package, never while a lock is held.
package session
