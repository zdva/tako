# Releasing Tako

Status of `v0.4.0` (first release candidate): the tag is on the repository.
**There is no GitHub Release and no installer has been published.** The list
at the end needs a person. `main` has moved on since the tag; read
[CHANGELOG.md](../CHANGELOG.md) Unreleased before cutting the next tag.

## What a release is

A tag `vX.Y.Z` that matches `version` in `mygo.json` and has a `## [X.Y.Z]`
section in `CHANGELOG.md` (the section becomes the release notes and the update
window text). `.github/workflows/release.yml` then:

1. checks the tag, the changelog, the update key, `go vet`, `go test -race` and
   `make vuln`;
2. creates a **draft** GitHub release;
3. builds, in parallel: macOS (dmg, arm64 + amd64), Windows (NSIS installer,
   amd64 + arm64, cross-built on Ubuntu), Linux (tar.gz + `install.sh`, `.deb`,
   PKGBUILD for amd64 + arm64);
4. adds `.rpm` packages built by [nfpm](https://nfpm.goreleaser.com) from
   `packaging/nfpm.yaml.tmpl`;
5. uploads every installer, the signed update archives, deltas and
   `update-<platform>.json` manifests;
6. attaches `SHA256SUMS`.

A person reviews the draft and publishes it; installed apps then offer the update.

On this machine only Linux can be built end to end (`make package-linux`
→ tar.gz, `.deb`, PKGBUILD; `.rpm` with `nfpm`). The dmg needs macOS and the NSIS
installer needs `makensis`, so those two are produced by CI only. `make build-all`
cross-compiles the binaries of all platforms and is part of the gates.

## Updates and the signing key

Updates are verified by MyGo's updater with an **ed25519 key** (`mygo keygen`):
the public half is `updates.publicKey` in `mygo.json`, the secret half is the
repository secret `MYGO_UPDATER_PRIVATE_KEY`. It is the only secret the release
needs. The key in `mygo.json` today belongs to a throw-away pair made while
testing; **generate your own and replace it before the first public release**:

```sh
go tool mygo keygen -o ~/tako-update-keys     # keep the .key out of the repo
gh secret set MYGO_UPDATER_PRIVATE_KEY < ~/tako-update-keys/mygo-update.key
# paste the contents of mygo-update.pub into mygo.json: updates.publicKey
# and set updates.github to owner/repo
```

Losing the secret key strands installed apps (they accept only updates signed
with it); anyone who has it can ship code to your users. Keep a copy in a
password manager.

## Unsigned builds, and adding signing later

By decision (M4) the installers carry **no Apple or Windows signature**:

- macOS: the app is ad-hoc signed; Gatekeeper warns on other Macs. Users open it
  with right-click → Open the first time (or `xattr -dr com.apple.quarantine /Applications/Tako.app`).
- Windows: SmartScreen shows "unknown publisher"; users choose *More info → Run
  anyway*.
- Linux packages are not GPG-signed; `SHA256SUMS` lets users verify downloads.

To add signing later nothing in the code changes:

- **macOS**: put a Developer ID certificate (`.p12`) and notary credentials in
  the repository secrets (`MACOS_CERTIFICATE`, `MACOS_CERTIFICATE_PASSWORD`,
  `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD`), import them into a keychain
  in the macOS job as MyGo's
  [GitHub Actions guide](https://github.com/egoist/mygo/blob/main/docs/github-actions.md)
  shows, and set `macos.signingIdentity` in `mygo.json`.
- **Windows**: a code-signing certificate through MyGo's `windows` signing
  options (see MyGo `docs/distribution.md`, "Signing").
- The update signature (ed25519) stays as it is; it is independent of both.

## Cutting a release

```sh
# 1. version: mygo.json "version", cmd/tako/main.go "version", CHANGELOG section
# 2. gates
make check && make integration && make vuln && make build-all && make e2e
# 3. tag (and only then push, when the repository exists)
git tag -a vX.Y.Z -m "Tako X.Y.Z"
git push origin main vX.Y.Z
```

## Pre-release checklist (needs a person)

Everything below still needs a person. The notes were written from a Linux
build machine, with no real Mac, Windows machine or GPU:

- [ ] **Run on a real Mac** (Apple silicon and Intel if possible): install the dmg,
      open, connect, paste, IME (Pinyin / Japanese), dark mode switch, the
      macOS menu bar and ⌘ shortcuts, Settings window, window dragging with the
      hidden title bar, confirming launch does not ask for the system keychain
      (the optional `tako --migrate-keychain` command may), sleep/wake reconnect.
- [ ] **Run on real Windows 10/11**: install the NSIS setup, uninstall, connect,
      PowerShell host monitoring (CIM), ConPTY-free terminals, IME, High-DPI,
      SmartScreen path, file transfer to and from a Windows OpenSSH server.
- [ ] **Real fixtures** for the collectors and importers: macOS `top` / `vm_stat`
      output, Windows CIM output, an NVIDIA and an AMD GPU host (`nvidia-smi`,
      `rocm-smi`), and real Xshell, SecureCRT and Termius exports to replace the
      hand-written ones in `testdata/`.
- [ ] **Updater end to end**: publish a draft `v0.4.0`, install it, publish
      `v0.4.1`, check that the app finds, verifies and installs it, and that a
      tampered archive is refused.
- [ ] **GitHub repository** (`zdva/tako` exists and `updates.github` is set): add the
      `MYGO_UPDATER_PRIVATE_KEY` secret, protect `main`, enable Dependabot and
      private vulnerability reporting (see SECURITY.md).
- [x] **Module path** `github.com/zdva/tako` and the public repository
      `github.com/zdva/tako` are decided.
- [x] **Licence**: GPL-3.0-or-later ([ADR 0022](adr/0022-license-gpl-3-or-later.md));
      `LICENSE` and `THIRD_PARTY_LICENSES.md` ship inside every package.
- [ ] **A real maintainer e-mail**: `tako@example.invalid` in `mygo.json`
      (`linux.maintainer`) and `packaging/nfpm.yaml.tmpl` is a placeholder; it
      appears in the `.deb`/`.rpm` metadata.
- [ ] **Source offer**: the release tag in the public repository is the
      "corresponding source" of each binary; keep tags and releases in step.
- [ ] **macOS and Windows CI** jobs run with `continue-on-error` in `ci.yml`
      and are not blocking. On 2026-11-23 (`d3775bf`, the `main` CI run) both
      `test (macos-latest)` and `test (windows-latest)` failed, as did `lint` and
      `integration`. Make the platform jobs blocking once they are green.
- [ ] **Screenshots** under `docs/screenshots/` are from Linux (Xvfb) and are
      not on the repository homepage. Retake on macOS before a store page or
      website.
