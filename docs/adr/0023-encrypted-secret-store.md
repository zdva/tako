# 0023. Secrets live in Tako's encrypted file

Status: Accepted

Supersedes [0003](0003-secrets-in-keychain.md).

## Context

Passwords, key passphrases and proxy credentials must not sit in `hosts.json`,
`session.json`, logs or exports. Those files are synced, attached to bug
reports and committed by mistake. ADR 0003 put the values in the operating
system's credential store through MyGo (`mygo.Secrets`): macOS Keychain,
Windows Credential Manager, Linux Secret Service, service name `dev.tako.app`.

That store is the system's, not Tako's. On macOS, reading it asks the user to
unlock the keychain when the binary is unsigned or its path changes, which
`go run` does on every launch. MyGo documents that `Secrets.Get` may block
while the system asks. A product that imports hosts from the system still has
to keep its own storage independent of the system's.

A master password at launch would replace one prompt with another. That is
not the goal.

## Decision

- Passwords, passphrases and tokens go through `secrets.Store`. The store the
  app opens is `secrets.File`: `secrets.enc` in the config directory
  (`Dirs.Secrets`). `hosts.json`, `session.json`, logs and exports still hold
  only the reference (`host/<id>/<kind>`), never the value.
- The file is an envelope (version 1) of AES-256-GCM ciphertext. The key is
  HKDF-SHA256 (Go's `crypto/hkdf`, `crypto/aes`, `crypto/cipher`). The input
  keying material is a machine id that is not part of the config directory:
  Linux `/etc/machine-id`, macOS `IOPlatformUUID` via `ioreg`, Windows
  `HKLM\SOFTWARE\Microsoft\Cryptography\MachineGuid`. A random salt lives in
  the envelope. No new module was added.
- If that id cannot be read, the key is 32 random bytes in `secrets.key` next
  to the file, mode 0600. The envelope records which binding it used.
- Both files are written atomically and mode 0600. There is no master
  password and no unlock prompt.
- The running app does not call MyGo's credential store, or the OS store under
  it. `secrets.NewEphemeral` is the session-only fallback when a file is bound
  to the machine but the machine id cannot be read right now, so a new file
  does not replace it. `ErrNotPersisted` still means "kept for this run".
- A file that cannot be decrypted is renamed to `secrets.enc.unreadable-<time>`
  and Tako starts with an empty store. A file from a newer Tako (`ErrTooNew`)
  is left in place and startup fails, so an older build cannot overwrite it.
- Private keys stay referenced by path and are never copied into Tako's files.
- Logs stay on the redacting `slog` handler from ADR 0003. Secret values are
  not logged.

### One-time migration

Startup does not read the old credential-store items. That read is what shows
the keychain dialog, including once per item for an unsigned binary, and it
can do the same for a locked Secret Service collection.

`tako --migrate-keychain` is the opt-in path. It reads each reference still
stored on a host in `hosts.json`, copies the value into `secrets.enc`, then
deletes that item from service `dev.tako.app`. The system may ask once per
item. If the user does not run it, the next connection asks in Tako's own
prompt; choosing "Remember in Tako" writes the encrypted file. Old items are
then unused. Tako never reads them, so they do not prompt. They can be deleted
by hand under the service name `dev.tako.app` (Keychain Access, Credential
Manager, or the Secret Service). Items whose references are no longer in
`hosts.json` are not found by the command; delete those by hand too.

MyGo has no list API, so the command cannot discover items that `hosts.json`
does not name.

## Threat model

This protects against:

- the password appearing in `hosts.json`, `session.json`, logs or an export
- another user on the machine reading the file (mode 0600, config directory
  0700)
- someone who syncs, copies or commits the config directory without also
  having this machine's id (or, in the fallback, `secrets.key`)

The machine id is not a password. On Linux `/etc/machine-id` is
world-readable. On the machine, other users are stopped by the file mode, not
by the encryption. Encryption stops the bytes reading as text and stops the
file decrypting on another computer.

This does not protect against a process already running as the same user. That
process can read `secrets.enc` and the machine id, or `secrets.key` when the
fallback is in use. A full disk image that contains both the config directory
and the machine id decrypts too. Copying `secrets.enc` together with
`secrets.key` decrypts anywhere the fallback was used.

Moving to another computer, or reinstalling Linux or Windows (those ids
change; the Mac hardware UUID does not), means typing passwords again. The
unreadable file is kept beside the new one.

## Consequences

- `internal/ui/hostsheet_test.go` still enters a known password and asserts it
  appears in none of `hosts.json`, `session.json`, the log, the export or the
  files under the config and state directories, which now include
  `secrets.enc`.
- Tests inject the machine id or use `secrets.Memory`. They do not need a
  keychain or a Secret Service. CI does not start one.
- The UI says "Remember in Tako" / "在 Tako 中记住".
- `go run` on a Mac no longer asks for the keychain, unless the user runs
  `--migrate-keychain`.
