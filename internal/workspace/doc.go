// Package workspace is Tako's model of what is open: an ordered list of
// blocks (one machine each, with terminal tabs), which block has the focus
// and which blocks take part in input sync.
//
// A [Model] is owned by the UI goroutine: call its methods from MyGo's main
// thread. Callbacks from sessions and terminals reach it through the Post
// function it is given, which runs them on that thread.
package workspace
