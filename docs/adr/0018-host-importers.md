# 0018. Importers parse bytes and never touch secrets; export carries no secret

Status: Accepted

## Context

People arrive with hosts in Xshell, SecureCRT and Termius. Those programs store
passwords encrypted with their own keys (or machine-bound keys), their files are
untrusted input, and we could not obtain real exports while building.

## Decision

- `internal/importer/hostfile` is a set of pure functions from bytes to
  `Result{Hosts, Passwords, Warnings}`; it has no access to the secret store, the
  network or the file system (the caller passes a `fs.FS`). It never decodes the
  other program's passwords; it only counts them.
- Limits: 32 MB per file, 128 MB in total, 20 000 files, 100 000 hosts; zip
  archives are read in memory and entries with `..` paths are ignored.
- What cannot be represented (telnet, serial, RDP, keys in the other program's
  store, proxies) becomes a warning that names the session, never a silent
  change. Duplicates are skipped by (name, address, port, user).
- The importers are fuzzed (`FuzzParse`), and a round-trip test proves the Tako
  export has no secret and no `SecretRef`: `hostlib.Library.Export` strips them
  and `AddMany` remaps IDs and jump chains on import.
- Formats were taken from public documentation and hand-written fixtures; real
  exports replace them before a release (docs/RELEASE.md).

## Consequences

- An import never changes the secret store; the user types each password once.
- A new format is one file with a `Detect` rule and a fixture.
- We cannot claim compatibility with every version of those programs; the
  warnings and the README say so.
