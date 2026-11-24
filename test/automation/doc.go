// Package automation drives a Tako window that was built with MyGo's
// automation plugin (`go build -tags e2e`) over the MYGO_AUTOMATION socket.
//
// The tests are behind the e2e build tag. Without it this package has nothing
// to run, so `go test ./...` stays the headless suite. On Linux the tests need
// a display (`xvfb-run -a make e2e-auto`). See docs/TESTING.md.
package automation
