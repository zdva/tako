# 0017. A settings window; color schemes resolved in `theme`, applied live

Status: Accepted

## Context

Through M3 settings lived in a small sheet in the palette code, and open
terminals kept the colors they were created with until a restart, so switching
dark / light changed the chrome but not the terminals. M4 adds language,
appearance, terminal colors, font, keepalive, monitoring, transfers and data
tools; they need a place with room, and every change must show at once.

## Decision

- `ui.settingsView` is a modal window with a sidebar (General, Terminal,
  Connection, Monitoring, File transfer, Data, About). Each row is a visible
  label (and hint) with its control; every control also carries the label as its
  accessible name. Changes are written through `settings.Update` as they are
  made; there is no Apply button and no restart.
- A **scheme** (`internal/scheme`, pure data: foreground, background, cursor,
  selection, 16 palette entries) is chosen by id. `theme.Tokens.WithScheme`
  merges it into the terminal colors; `Tako (follows appearance)` has no scheme
  and uses the DESIGN.md tokens for the current appearance. Imported iTerm2
  schemes are stored in `settings.json` (`terminal.custom`), ids prefixed
  `custom:`.
- `workspace.Model` owns the terminals and exposes `RestyleTerminals()`; the UI
  calls it when the appearance, the scheme, the font size, copy-on-select or
  contrast changed (`App.syncPrefs` compares a small `termLook` value, so
  unrelated settings do not repaint terminals).
- The `theme` package keeps its test against the tokens written in DESIGN.md
  and the mockup; schemes never replace chrome colors.

## Consequences

- Terminal restyle is live and tested by pixel counts (Tester).
- A saved value the control does not know (a hand-edited language, the
  test-only pseudo-locale) is shown as the first option and left alone until the
  user picks another one, so opening the window never rewrites settings.
- Adding a setting means: a field in `settings`, a row in `settingsview.go`, two
  locale strings and a test.
