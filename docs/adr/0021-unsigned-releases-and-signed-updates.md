# 0021. Unsigned installers, ed25519-signed updates from GitHub Releases

Status: Accepted (owner's decision, M4)

## Context

The owner has no Apple Developer or Windows code-signing certificate and does
not want to buy one for now. Updates still must not be an open door: anyone able
to put a file on the download host must not be able to run code on users'
machines.

## Decision

- Installers are **unsigned** (macOS ad hoc; Windows no signature; Linux
  packages ungpg'd) and published with `SHA256SUMS`. docs/RELEASE.md says how
  users get past Gatekeeper and SmartScreen and how to add signing later without
  code changes.
- Updates use MyGo's updater (`plugins/updater/native`): `mygo build` signs each
  update archive and delta with an **ed25519** key; `updates.publicKey` in
  `mygo.json` is compiled into the app, which refuses anything not signed with
  it and keeps running the old version. The secret key lives only in the
  repository secret `MYGO_UPDATER_PRIVATE_KEY`. Releases are GitHub Releases
  (`updates.github`); the workflow creates a draft that a person publishes.
- The public key in the tree belongs to a throw-away pair; the release checklist
  replaces it.
- Packages: `.tar.gz` + `install.sh`, `.deb`, PKGBUILD and the update archives
  come from `mygo build`; the `.rpm` from nfpm (`packaging/nfpm.yaml.tmpl`); the
  dmg needs macOS and the NSIS installer needs `makensis`, so they are built in
  CI.

## Consequences

- First launch on macOS and Windows shows a warning; users must act once.
- The updater's own signature check is independent of platform signing, so
  signing can be added later and updates keep working.
- Losing the update key strands installed apps; it is the one secret to back up.
