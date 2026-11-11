// Package testutil groups helpers that only tests use: an in-process SSH
// server (memssh), a harness that runs a real OpenSSH sshd (realsshd), and
// small network fakes (nettest). Production code must not import it.
package testutil
