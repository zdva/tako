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

- Passwords, passphrases and tokens are stored only in the system keychain and
  never written to `hosts.json`, `session.json`, logs or exports
  ([ADR 0003](docs/adr/0003-secrets-in-keychain.md)). A test asserts this.
- A changed SSH host key is always refused ([ADR 0004](docs/adr/0004-host-key-policy.md)).
- Input typed while a connection is down is dropped, never replayed
  ([ADR 0006](docs/adr/0006-session-pane-contract.md)).
- Logs are structured and redacted by key name.
- Dependencies are scanned with `govulncheck` in CI.

## Known limits (M1)

- Release binaries are not code-signed or notarized yet.
- On a machine with no credential store, "remember password" keeps the secret
  in memory for the session only.
- The first run of an unpackaged (`go build`) binary downloads `libghostty-vt`
  from the MyGo releases and verifies its SHA-256; packaged builds ship it.
- Tako's own `known_hosts` is a plain file with 0600 permissions; it is not
  encrypted.
