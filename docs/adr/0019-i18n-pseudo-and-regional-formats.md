# 0019. Pseudo-locale and system-region number and date formats

Status: Accepted

## Context

English and Simplified Chinese are complete, but a missing key or a string that
bypasses the tables only shows when someone reads every screen, and translated
text is often longer than English. Dates, clocks and numbers were formatted one
way for everyone.

## Decision

- `i18n.Pseudo` (`en-XA`) derives a table from English: accents, +40% filler,
  `⟦ … ⟧` around every message, printf verbs untouched. It is not offered in
  the settings window; tests select it through the settings file.
- `TestPseudoLocale_EverySurfaceComesFromTheTables` walks the main window, host
  sheet, palette, settings pages and transfer sheet: any visible text without
  `⟦` that is not user data fails, as does text outside the window.
  `TestKeys_MatchLocales` keeps `keys.go` and both JSON files identical in key
  set.
- `i18n.Locale` (built on `golang.org/x/text`) formats numbers, dates and the
  12/24-hour clock from the **system region** (`ParseLocale(LANG)`), not from the
  UI language, so a Chinese UI on a US machine shows US dates. Durations come
  from table keys (`dur.sec`, `dur.min`, `dur.hourmin`).
- UTF-8 only (ADR 0005): the pseudo-locale and tests never involve other
  encodings.

## Consequences

- A hard-coded string is caught by a test, not by a reviewer.
- Long strings must wrap or ellipsize; single-line spots (search hint, tagline)
  are marked `NoWrap` + `Ellipsis`.
- Right-to-left languages are out of scope.
