# 0001. MyGo is the only UI toolkit

Status: Accepted (M1)

## Context

Tako is a desktop terminal that shows many live terminals at once on macOS,
Windows and Linux. A native toolkit with an embeddable terminal widget, GPU
rendering and one Go code base for three platforms was the requirement. The
technical spike (`/workspace/tako-spike`) built the grid, the minimap and nine
live SSH terminals on [MyGo](https://github.com/egoist/mygo) v0.8.3 and
measured 0% idle CPU.

## Decision

All user interface is written with MyGo (`github.com/egoist/mygo`, its `ui`
package and the `plugins/terminal` plugin). No second toolkit, no web view, no
cgo UI binding. Only `internal/ui` and the thin wiring in `cmd/tako` and
`internal/secrets` may import MyGo; the `depguard` rules in `.golangci.yml`
enforce it.

## Consequences

- MyGo is 0.x, so its API can change. The version is pinned in `go.mod`, the
  UI is isolated behind `internal/ui`, and every UI behaviour has a
  `ui.Tester` test that runs without a window.
- The terminal needs `libghostty-vt`. A packaged build (`mygo build`) ships it
  next to the executable; a plain `go build` binary downloads it into the user
  cache on first use. Release builds are always packaged.
- Window chrome follows the OS (default title bar) until MyGo's hidden title
  bar is verified on all three platforms.
