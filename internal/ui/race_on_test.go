//go:build race

package ui_test

// raceEnabled is true when the race detector slows every frame down by an
// order of magnitude, which tests that depend on wall-clock debounces must allow for.
const raceEnabled = true
