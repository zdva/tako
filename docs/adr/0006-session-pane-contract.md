# 0006. `session.Pane` never reaches EOF while a link is down

Status: Accepted (M1)

## Context

The terminal widget treats a read error or a failed write as the end of the
terminal and closes its input queue for good. A flaky network must not kill a
terminal, scrollback must survive, and keystrokes typed during an outage must
not be replayed later (dangerous: `rm`, `reboot`).

## Decision

`session.Pane` sits between the terminal and the SSH shell and is the only
thing the terminal sees:

- `Read` blocks while the link is down and resumes after reconnect; it returns
  `io.EOF` only when the pane is closed or the remote shell exits on purpose.
- `Write` never returns an error while disconnected: the bytes are dropped and
  counted, and the UI shows "input during the outage is not sent".
- `Resize` is debounced and re-applied after reconnect.
- Reconnect backoff 1, 2, 5, 10, 30 s (±20% jitter), then every 30 s, forever,
  until the user closes the block. A semaphore (`maxParallelDials = 4`) bounds
  simultaneous dials after a network change.
- A localized notice line ("── connection lost hh:mm:ss ──") is written into
  the pane at the start of an outage.

## Consequences

Covered by `TestOutageReconnectKeepsPaneAndDropsInput` against a real sshd and
by the unit tests in `internal/session`. The shell state itself is lost on
reconnect (a new remote shell starts); keeping it would need tmux and is out
of scope.
