# 0022. License: GPL-3.0-or-later, dependency compatibility, DCO

Status: Accepted (owner's decision)

## Context

Tako was developed with a placeholder MIT licence and the module path
`github.com/wei-dong/tako`. The owner decided to publish the project as
free software and chose the GNU GPL. A public GitHub account and repository
name were also chosen (`zdva`, `tako`).

## Decision

- The project is licensed under the **GNU General Public License, version 3
  or (at the recipient's option) any later version** (SPDX: `GPL-3.0-or-later`).
  `LICENSE` is the official GPLv3 text. "Or later" is the form recommended by
  the FSF and matches the standard GPL notice; the owner selected GPL-3.0 as
  the licence family.
- The public repository and the Go module path are `github.com/zdva/tako`.
- Contributions are inbound = outbound: they are licensed under the same
  GPL-3.0-or-later. There is no copyright assignment and no CLA. Committers
  sign off with `git commit -s` (Developer Certificate of Origin 1.1), as
  described in `CONTRIBUTING.md`.
- Direct and transitive dependencies must be compatible with GPL-3.0-or-later
  (permissive licences such as MIT, BSD and Apache-2.0 are fine; GPL-2.0-only,
  SSPL, "non-commercial" and proprietary licences are not). Their notices live
  in `THIRD_PARTY_LICENSES.md`. A review of the current module graph found only
  MIT, BSD-2-Clause, BSD-3-Clause, Apache-2.0, Unlicense (dual with BSD-3-Clause
  on `go-text/typesetting`), plus the Go runtime (BSD-3-Clause) and Ghostty's
  `libghostty-vt` (MIT). All of these may be used in a GPL-3.0-or-later program.
- The maintainer e-mail `tako@example.invalid` in package metadata stays a
  placeholder until a real address is chosen.

## Consequences

- Downstream distributors must provide corresponding source and keep
  derivatives under a GPL-compatible licence.
- New dependencies need a licence check and an update to
  `THIRD_PARTY_LICENSES.md` in the same change.
- Historical milestone tags (`v0.1.0-m1` through `v0.4.0`) still record the
  earlier placeholder MIT text; the licence change applies from this commit
  onward.
