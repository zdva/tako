// Package transport defines how Tako reaches a machine, independently of the
// protocol and of the UI.
//
// A [Dialer] turns a model.Host into a [Transport]: one established
// connection on which several channels can be opened (an interactive [Shell],
// one-shot commands). The SSH implementation lives in transport/ssh. Everything
// that needs the user (host key confirmation, a password, a one-time code) goes
// through a [Prompter], which the UI implements; the transport never imports
// the UI.
//
// Errors are classified with the sentinels below so callers can decide whether
// retrying makes sense (see [Retryable]).
package transport
