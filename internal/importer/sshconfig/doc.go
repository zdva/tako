// Package sshconfig imports hosts from an OpenSSH client configuration
// (~/.ssh/config).
//
// Parsing is a pure function of the file contents: it never touches the
// secret store or the network. It understands Host blocks (wildcard blocks supply
// defaults, as in OpenSSH where the first value found wins), HostName, User,
// Port, IdentityFile, ProxyJump and Include, with a depth limit. Constructs it
// cannot represent (Match blocks, ProxyCommand) are reported as warnings.
package sshconfig
