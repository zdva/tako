// Package diag writes crash reports and builds the "copy diagnostics" text.
//
// Crashes are caught by the Go runtime itself (debug.SetCrashOutput), so a
// panic on any goroutine, in the terminal library or in a callback, leaves a
// report; the next start moves it to a dated file and tells the user. A
// diagnostics report is for pasting into an issue: it holds the version, the
// system and the last log lines, scrubbed of addresses, host names, user
// names, paths under the home folder and anything that looks like a key or a
// token.
package diag
