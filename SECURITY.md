# Security policy

Tako holds credentials and opens shells on people's servers, so security
reports are welcome and taken seriously.

## Reporting a vulnerability

Do **not** open a public issue. Use the repository's private vulnerability
reporting (GitHub: *Security → Report a vulnerability*) once the project is
published. Until then, report to the maintainer who gave you the source.
Please include the version (`tako -version`), platform, steps to reproduce and
the impact you see. We aim to acknowledge within 7 days and to ship a fix or a
mitigation within 90 days.

## Supported versions

Only the latest release (and `main`) receives fixes while Tako is pre-1.0.

## What Tako promises

- Passwords, passphrases and tokens are encrypted in `secrets.enc` inside
  Tako's config directory (mode 0600) and are never written to `hosts.json`,
  `session.json`, logs or exports
  ([ADR 0023](docs/adr/0023-encrypted-secret-store.md)). A test asserts the
  plaintext stays out of those files, including `secrets.enc`. Tako does not
  read or write the macOS Keychain, Windows Credential Manager or Linux
  Secret Service during normal use.
- A changed SSH host key is always refused ([ADR 0004](docs/adr/0004-host-key-policy.md)).
- Input typed while a connection is down is dropped, never replayed
  ([ADR 0006](docs/adr/0006-session-pane-contract.md)).
- Logs are structured and redacted by key name.
- Dependencies are scanned with `govulncheck` in CI.

## Known limits

- Release binaries are not code-signed or notarized yet.
- The encrypted file is not a defense against a process running as the same
  user. The machine id that wraps the key is not itself a password (on Linux
  `/etc/machine-id` is world-readable); other users are stopped by mode 0600.
  A copy of the config directory decrypts on another machine only when the
  fallback `secrets.key` was copied with it, which happens when this machine's
  id could not be read. See ADR 0023.
- Passwords saved by a build that used the system credential store (service
  `dev.tako.app`) are not imported at launch, because that read can show the
  keychain prompt. Run `tako --migrate-keychain` once, knowing the system may
  ask, or type the password again. Leftover items under `dev.tako.app` stay
  until that command deletes the ones `hosts.json` still names, or until you
  delete them yourself.
- The first run of an unpackaged (`go build`) binary downloads `libghostty-vt`
  from the MyGo releases and verifies its SHA-256; packaged builds ship it.
- Tako's own `known_hosts` is a plain file with 0600 permissions; it is not
  encrypted.
