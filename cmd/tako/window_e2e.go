//go:build e2e

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// windowSize reads TAKO_WINDOW (for example 720x480) so a real-window test
// can open Tako narrower than a fullscreen display. A missing or unusable
// value keeps the production default. The window still cannot shrink below
// its minimum.
func windowSize() (int, int) {
	w, h := defaultWidth, defaultHeight
	v := os.Getenv("TAKO_WINDOW")
	if v == "" {
		return w, h
	}
	rawW, rawH, ok := strings.Cut(v, "x")
	if !ok {
		fmt.Fprintf(os.Stderr, "tako: TAKO_WINDOW=%q is not WxH\n", v)
		return w, h
	}
	ww, errW := strconv.Atoi(rawW)
	hh, errH := strconv.Atoi(rawH)
	if errW != nil || errH != nil || ww < minWidth || hh < minHeight {
		fmt.Fprintf(os.Stderr, "tako: TAKO_WINDOW=%q ignored; the minimum is %dx%d\n", v, minWidth, minHeight)
		return w, h
	}
	return ww, hh
}
