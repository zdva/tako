# Contributing to Tako

Thanks for helping. Tako is pre-1.0; small, focused changes are easiest to review.

## Getting set up

Go 1.27.3 or newer, `make`, and `golangci-lint` v2. For the integration and e2e
suites also `curl` + `dpkg` (portable OpenSSH), `xvfb` and `xdotool` (Linux).

```sh
make check
```

must pass before you open a pull request. Read [AGENTS.md](AGENTS.md) for the
layout and rules and [docs/TESTING.md](docs/TESTING.md) for the test levels.

## Branches and commits

- Branch from `main`: `feat/<topic>`, `fix/<topic>`, `docs/<topic>`.
- Use [Conventional Commits](https://www.conventionalcommits.org/):
  `feat(session): …`, `fix(ui): …`, `test(e2e): …`, `docs: …`, `build: …`,
  `refactor: …`, `chore: …`. Scopes are package names. Breaking changes get `!`
  and a `BREAKING CHANGE:` footer.
- Keep commits logical: a reviewer should be able to read them one by one.
  Update `CHANGELOG.md` (Unreleased) for anything a user would notice.

## Pull requests

- One topic per PR, with a description of the problem, the approach and how you
  tested it. Include a screenshot for UI changes (`make screenshots`).
- Add or update tests. Core packages must stay at ≥ 80% coverage.
- Hard-to-reverse decisions get an ADR in `docs/adr/` (next number).
- CI must be green: build, `go test -race`, lint, `govulncheck`, cross-compile.
- At least one maintainer review; address comments with new commits, then squash
  or rebase as the reviewer prefers.

## Reporting bugs and ideas

Use the issue templates. For anything security-related follow [SECURITY.md](SECURITY.md)
instead. Never paste passwords, private keys or full logs with hostnames you
want to keep private.

## Code of conduct

By taking part you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).

## Licensing

The project is MIT-licensed (currently pending the owner's confirmation). By
contributing you agree your work is released under the same license.
