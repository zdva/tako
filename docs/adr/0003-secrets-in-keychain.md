# 0003. Secrets live in the system keychain only

Status: Superseded by [0023](0023-encrypted-secret-store.md)

Current behavior is [0023](0023-encrypted-secret-store.md): passwords live in
Tako's encrypted file. The repository README describes that file, not this
decision.

This decision was accepted for M1 and replaced when a launch on macOS asked
the user to sign in to the keychain. The text below is the original decision.

## Context

Hosts need passwords, key passphrases and proxy credentials. Config files get
synced, exported, attached to bug reports and committed by mistake.

## Decision

- Passwords, passphrases and tokens are stored only through `secrets.Store`,
  whose real implementation is MyGo's credential store (macOS Keychain,
  Windows Credential Manager, Linux Secret Service). Service name
  `dev.tako.app`, reference `host/<id>/<kind>`.
- `hosts.json`, `session.json`, the logs and any export contain only the
  *reference*, never the value.
- Where the OS has no credential store, `secrets.Layered` keeps the value in
  memory for the running session, returns `ErrNotPersisted`, and the UI says so.
  Nothing falls back to a file.
- Logs go through a redacting `slog` handler: keys containing `pass`,
  `secret`, `token`, `key`, `cred` or `auth` are masked; counts and durations
  are kept.
- Private keys are referenced by path and never copied into Tako's files.

## Consequences

A grep test (`internal/ui/hostsheet_test.go`) enters a known password and
asserts it appears in none of `hosts.json`, `session.json`, the log or the
export. On a headless Linux box without Secret Service, "remember" only lasts
for the session. That sentence described this decision. The current README
describes [0023](0023-encrypted-secret-store.md).
