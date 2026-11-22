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

Tako is licensed under the **GNU General Public License, version 3 or (at your
option) any later version** (GPL-3.0-or-later, see [LICENSE](LICENSE) and
[ADR 0022](docs/adr/0022-license-gpl-3-or-later.md)).

**Inbound = outbound:** a contribution is licensed to the project and to everyone
who receives it under the same GPL-3.0-or-later. You keep your copyright; there is
no copyright assignment and no separate contributor license agreement.

**Developer Certificate of Origin:** sign every commit off with
`git commit -s`, which adds a `Signed-off-by: Your Name <you@example.com>` line
and means you certify the [DCO 1.1](https://developercertificate.org/): the work
is yours to submit (or you have the right to submit it) under this license.
Do not contribute code you cannot license under the GPL (for example code copied
from a project under an incompatible license). The sign-off is not enforced by CI
yet; maintainers may ask you to add it.

New dependencies must be GPL-3.0-or-later compatible (permissive licenses such as
MIT, BSD, Apache-2.0 are fine; GPL-2.0-only, SSPL, "non-commercial" and
proprietary licenses are not). Add the dependency to
[THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md) in the same pull request.
