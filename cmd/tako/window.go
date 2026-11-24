//go:build !e2e

package main

// windowSize is the size the window opens at. Production builds always use
// the defaults; the e2e build can be asked for another size.
func windowSize() (int, int) { return defaultWidth, defaultHeight }
