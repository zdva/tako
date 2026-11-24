# AGENTS.md

Guidance for AI coding agents and humans working in this repository.

## What this is

Tako: a desktop SSH fleet terminal in Go, UI **only** with MyGo
(`github.com/egoist/mygo`, see [ADR 0001](docs/adr/0001-mygo-only-ui.md)).

People using the app start at the repository [README](README.md) (简体中文;
English is [README.en.md](README.en.md)) and the map in
[docs/README.md](docs/README.md). This file stays at the repository root so
coding agents can find it.

For implementation, read [docs/SPEC.md](docs/SPEC.md), then [docs/adr/](docs/adr).
An ADR marked *Superseded* is history. Secrets are Tako's encrypted file
([ADR 0023](docs/adr/0023-encrypted-secret-store.md)); ADR 0003 is not current.

## Layout

```
cmd/tako            assembles the app and the window; no logic
internal/app        wiring (hand-written constructor injection), logging, connector
internal/ui         every screen (MyGo); ui.Tester tests; generated i18n keys
internal/workspace  blocks, tabs, focus, session snapshot/restore
internal/session    Session (link + reconnect) and Pane (the never-EOF contract)
internal/inputbus   synchronized input fan-out and reply filtering
internal/layout     pure geometry: grid, minimap
internal/hostlib    host library (hosts.json), tags, import/export
internal/importer   ~/.ssh/config importer
internal/transport  ports (Transport, Shell, Prompter) + transport/ssh adapter
internal/secrets    Store port, encrypted file, in-memory session fallback
internal/store      atomic JSON documents, directory lock, backups, Dirs
internal/settings   settings.json
internal/model      Host, Auth, IDs, validation
internal/i18n       en / zh-Hans tables
internal/theme      design tokens, terminal themes
internal/testutil   memssh, nettest, realsshd (tests only)
test/integration    real-OpenSSH suite (-tags integration)
test/e2e            Xvfb live-window suite (-tags e2e)
docs/               spec, design, tech plan, ADRs, testing, screenshots
```

Dependency direction: `ui → app → core → ports`. Core must not import `ui`,
`app`, MyGo or `golang.org/x/crypto/ssh`; `depguard` in `.golangci.yml` fails
the build if you do.

## Commands

```sh
make check          # fmt, vet, lint, race tests, coverage gate — run before every commit
make race | cover | lint | fmt | tidy
make integration    # needs scripts/fetch-sshd.sh or system sshd (Linux)
make e2e            # needs Xvfb/xdotool (Linux)
make build-all      # cross-compile all platforms
```

## Conventions

- **Go style**: follow the samber `golang-*` skills (code-style, naming,
  error-handling, concurrency, context, project-layout, structs-interfaces,
  testing, lint, safety, security, documentation) and JetBrains `use-modern-go`.
  Before editing a Go file run the `use-modern-go` `list` tool for that file;
  where the two disagree, `use-modern-go` wins. Go 1.27: `wg.Go`, `range n`,
  `min/max`, `slices/maps`, `errors.AsType`, `t.Context()`, `strings.CutLast`.
- **Errors**: wrap with `%w`; handle once (return *or* log); sentinel/typed
  errors for things callers branch on. Logging is `log/slog`, structured.
- **Concurrency**: every goroutine has an owner and an exit (context); the UI
  goroutine owns UI state; other goroutines call `ui.App.Post`.
- **Secrets**: never in files, logs, test output or commit messages. Values are
  `[]byte`. Add new sensitive log keys to the redaction list.
- **UTF-8 only.** No encoding options.
- **UI**: build with MyGo builders; add every user-visible string to
  `internal/i18n/locales/{en,zh-Hans}.json` and `internal/ui/keys.go` (a test
  checks they line up). Prefer `SingleLine()` + `MaxWidth` over `NoWrap()` when
  text must ellipsize. Create terminals only after the first frame.
- **Commits**: Conventional Commits (`feat(ui): …`), small and logical.
- **Docs**: exported identifiers have doc comments; a decision that is hard to
  reverse gets an ADR.

## Do

- Add a test with every change; UI behaviour gets a `ui.Tester` test.
- Keep `internal/layout`, `inputbus`, `model` dependency-free.
- Run `make check` and, for transport/session changes, `make integration`.

## Don't

- Don't add a second UI toolkit, a DI framework, or cgo.
- Don't push, tag or publish without the owner's say-so. Never commit secrets,
  real keys, or machine-specific paths.
- Don't weaken `.golangci.yml` to make a warning go away; fix it or add a
  narrow `//nolint:linter // reason`.
- Don't make `Pane.Write` return an error or `Pane.Read` return EOF during an outage.

## Known placeholders

The maintainer e-mail `tako@example.invalid` (in `mygo.json` and
`packaging/nfpm.yaml.tmpl`) is a placeholder. The module path is
`github.com/zdva/tako`; the license is GPL-3.0-or-later (ADR 0022): new
dependencies must be compatible and listed in `THIRD_PARTY_LICENSES.md`.
