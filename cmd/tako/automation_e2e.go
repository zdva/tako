//go:build e2e

package main

// The automation plugin is the official way for another process to drive a
// running Tako window (tree, act, input, capture, idle). A blank import
// registers it. It listens only when MYGO_AUTOMATION names a socket, and
// this file is excluded from production builds. Release builds of
// `mygo build` also pass -tags mygo_noinspector, which leaves the plugin
// out even if it is imported; drive the app with `go build -tags e2e`.
import _ "github.com/egoist/mygo/plugins/automation"
