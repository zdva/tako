// Package ssh implements transport.Dialer over SSH using golang.org/x/crypto/ssh.
//
// It builds the whole path to a host: an optional SOCKS5 or HTTP proxy, any
// number of ProxyJump hops (each hop is a library host with its own
// credentials and host key check), the authentication chain (password, key
// with passphrase, ssh-agent, keyboard-interactive), host key verification
// against Tako's own known_hosts, and keepalives.
//
// There is deliberately no way to skip host key verification.
package ssh
