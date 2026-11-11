# 0020. Crash reports from the Go runtime; scrubbed diagnostics on request

Status: Accepted

## Context

A desktop app that dies leaves the user with nothing to attach to an issue,
and a log full of host names, addresses and paths is not safe to paste.

## Decision

- `diag.Install` points `runtime/debug.SetCrashOutput` at
  `logs/crash.pending`, so a fatal panic on **any** goroutine (terminal
  callbacks included) is written by the runtime itself. On the next start a
  non-empty pending file is moved to `crash-<time>.txt` (the newest five are
  kept) and About says "Tako closed unexpectedly last time" with a button to
  copy diagnostics. A clean run leaves an empty pending file.
- "Copy diagnostics" builds, on demand, a text with version, OS, display
  server, locale, the last log lines and the top of the last crash report. It is
  **scrubbed**: home folder, host names and user names known to the app, IPv4 and
  IPv6 addresses, `user@host`, long key-like strings and the value after
  `password` / `secret` / `token` / `key` are replaced. Nothing is sent
  anywhere; it goes to the clipboard.
- Logging already redacts sensitive attributes (`internal/app/logging.go`).

## Consequences

- Reports contain stack traces with function names and paths of the build, not
  of the user's machine.
- There is no telemetry and no upload; the user decides what to paste.
- The scrubber is heuristic; the About page tells the user to read the text
  before sharing.
