package model

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
)

// DefaultPort is the SSH port used when a host leaves its port at zero.
const DefaultPort = 22

// ID identifies a host for its whole life. It is generated once and never
// reused, so renaming or editing a host does not break references to it.
type ID string

// NewID returns a fresh random host ID such as "h_3f9a1c2b4d5e".
func NewID() ID {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on supported platforms; falling back to
		// the clock keeps IDs unique enough to avoid a panic on a broken one.
		return ID(fmt.Sprintf("h_%012x", time.Now().UnixNano()&0xffffffffffff))
	}
	return ID("h_" + hex.EncodeToString(b[:]))
}

// OSKind is the operating system of a host. It decides how monitoring data is
// collected (from M3) and which icon the host shows.
type OSKind string

// The known operating systems.
const (
	OSLinux   OSKind = "linux"
	OSMacOS   OSKind = "macos"
	OSWindows OSKind = "windows"
)

// AuthKind is one way of proving who you are to an SSH server.
type AuthKind string

// The supported authentication kinds.
const (
	AuthPassword            AuthKind = "password"
	AuthKey                 AuthKind = "key"
	AuthAgent               AuthKind = "agent"
	AuthKeyboardInteractive AuthKind = "keyboard-interactive"
)

// HostKeyPolicy decides what happens when a server's key is not known yet.
// A key that differs from a known one is always refused.
type HostKeyPolicy string

// The host key policies.
const (
	// HostKeyAsk shows the fingerprint on first contact and lets the user
	// accept it. It is the default.
	HostKeyAsk HostKeyPolicy = "ask"
	// HostKeyStrict refuses any host whose key is not already trusted.
	HostKeyStrict HostKeyPolicy = "strict"
)

// ProxyKind is the protocol of a connection proxy.
type ProxyKind string

// The supported proxy protocols.
const (
	ProxySOCKS5 ProxyKind = "socks5"
	ProxyHTTP   ProxyKind = "http"
)

// Auth is one authentication method of a host. Several can be set; they are
// tried in order.
type Auth struct {
	Kind AuthKind `json:"kind"`
	// KeyPath is the private key file when Kind is AuthKey. A leading "~/" is
	// expanded when the key is read.
	KeyPath string `json:"keyPath,omitempty"`
	// SecretRef names the keychain entry holding the password or key
	// passphrase. Empty means "ask every time". It is never a secret itself.
	SecretRef string `json:"secretRef,omitempty"`
}

// Hop is one jump host of a ProxyJump chain. The jump host is itself a host of
// the library, so its credentials are stored once.
type Hop struct {
	HostID ID `json:"hostId"`
}

// Proxy is a SOCKS5 or HTTP proxy to dial through.
type Proxy struct {
	Kind ProxyKind `json:"kind"`
	// Addr is host:port of the proxy.
	Addr string `json:"addr"`
	// User is the proxy login, if it needs one. Its password is in the
	// keychain under SecretRef.
	User      string `json:"user,omitempty"`
	SecretRef string `json:"secretRef,omitempty"`
}

// Host is a machine Tako can open terminals on.
type Host struct {
	ID   ID     `json:"id"`
	Name string `json:"name"`
	// Addr is a host name or IP address, without a port.
	Addr string `json:"addr"`
	// Port is the SSH port; zero means DefaultPort.
	Port int      `json:"port,omitzero"`
	User string   `json:"user"`
	OS   OSKind   `json:"os,omitempty"`
	Tags []string `json:"tags,omitempty"`
	// Auth lists the methods to try, in order.
	Auth []Auth `json:"auth,omitempty"`
	// Jump is the ProxyJump chain, first hop first.
	Jump  []Hop  `json:"jump,omitempty"`
	Proxy *Proxy `json:"proxy,omitempty"`
	// StartDir and StartCmd run after the shell opens.
	StartDir string `json:"startDir,omitempty"`
	StartCmd string `json:"startCmd,omitempty"`
	// KeepAlive is the interval of SSH keepalives in seconds; zero means the
	// default of 30 seconds.
	KeepAlive     int           `json:"keepAlive,omitzero"`
	HostKeyPolicy HostKeyPolicy `json:"hostKeyPolicy,omitempty"`
	Note          string        `json:"note,omitempty"`
}

// Validation errors.
var (
	ErrNameEmpty    = errors.New("host name is empty")
	ErrAddrEmpty    = errors.New("host address is empty")
	ErrUserEmpty    = errors.New("user name is empty")
	ErrPortInvalid  = errors.New("port is out of range")
	ErrAuthInvalid  = errors.New("authentication method is invalid")
	ErrProxyInvalid = errors.New("proxy is invalid")
	ErrJumpLoop     = errors.New("jump chain contains a loop")
)

// EffectivePort returns the port to dial.
func (h Host) EffectivePort() int {
	if h.Port == 0 {
		return DefaultPort
	}
	return h.Port
}

// HostPort returns "addr:port" ready for net.Dial; IPv6 addresses are
// bracketed.
func (h Host) HostPort() string {
	return net.JoinHostPort(h.Addr, strconv.Itoa(h.EffectivePort()))
}

// KeepAliveInterval returns the keepalive interval, applying the default.
func (h Host) KeepAliveInterval() time.Duration {
	if h.KeepAlive <= 0 {
		return 30 * time.Second
	}
	return time.Duration(h.KeepAlive) * time.Second
}

// Policy returns the host key policy, applying the default.
func (h Host) Policy() HostKeyPolicy {
	if h.HostKeyPolicy == "" {
		return HostKeyAsk
	}
	return h.HostKeyPolicy
}

// Target returns "user@addr", the short form shown under a host name.
func (h Host) Target() string {
	if h.User == "" {
		return h.Addr
	}
	return h.User + "@" + h.Addr
}

// Clone returns a deep copy of h.
func (h Host) Clone() Host {
	h.Tags = slices.Clone(h.Tags)
	h.Auth = slices.Clone(h.Auth)
	h.Jump = slices.Clone(h.Jump)
	if h.Proxy != nil {
		p := *h.Proxy
		h.Proxy = &p
	}
	return h
}

// Validate reports the first problem that would stop a connection to h.
func (h Host) Validate() error {
	switch {
	case strings.TrimSpace(h.Name) == "":
		return ErrNameEmpty
	case strings.TrimSpace(h.Addr) == "":
		return ErrAddrEmpty
	case strings.TrimSpace(h.User) == "":
		return ErrUserEmpty
	case h.Port < 0 || h.Port > 65535:
		return fmt.Errorf("%w: %d", ErrPortInvalid, h.Port)
	}
	for _, a := range h.Auth {
		if err := a.validate(); err != nil {
			return err
		}
	}
	if h.Proxy != nil {
		if err := h.Proxy.validate(); err != nil {
			return err
		}
	}
	return nil
}

func (a Auth) validate() error {
	switch a.Kind {
	case AuthPassword, AuthAgent, AuthKeyboardInteractive:
		return nil
	case AuthKey:
		if strings.TrimSpace(a.KeyPath) == "" {
			return fmt.Errorf("%w: key authentication needs a key file", ErrAuthInvalid)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrAuthInvalid, a.Kind)
	}
}

func (p Proxy) validate() error {
	if p.Kind != ProxySOCKS5 && p.Kind != ProxyHTTP {
		return fmt.Errorf("%w: unknown kind %q", ErrProxyInvalid, p.Kind)
	}
	if _, _, err := net.SplitHostPort(p.Addr); err != nil {
		return fmt.Errorf("%w: address %q needs host:port", ErrProxyInvalid, p.Addr)
	}
	return nil
}

// SecretRefs returns every keychain reference the host uses, so deleting the
// host can delete its secrets too.
func (h Host) SecretRefs() []string {
	var refs []string
	for _, a := range h.Auth {
		if a.SecretRef != "" {
			refs = append(refs, a.SecretRef)
		}
	}
	if h.Proxy != nil && h.Proxy.SecretRef != "" {
		refs = append(refs, h.Proxy.SecretRef)
	}
	return refs
}

// SecretRef builds the canonical keychain key for a host's secret of the
// given kind, such as "host/h_3f9a/password".
func SecretRef(id ID, kind string) string {
	return "host/" + string(id) + "/" + kind
}

// ResolveJumps returns the hosts of h's jump chain, first hop first, looked up
// with find. It fails when a hop is missing or the chain loops back on h or on
// itself. The chain is the whole path: a hop's own Jump list is not followed.
func ResolveJumps(h Host, find func(ID) (Host, bool)) ([]Host, error) {
	seen := map[ID]bool{h.ID: true}
	chain := make([]Host, 0, len(h.Jump))
	for _, hop := range h.Jump {
		if seen[hop.HostID] {
			return nil, fmt.Errorf("%w: %s", ErrJumpLoop, hop.HostID)
		}
		seen[hop.HostID] = true
		jh, ok := find(hop.HostID)
		if !ok {
			return nil, fmt.Errorf("jump host %s: %w", hop.HostID, ErrNotFound)
		}
		chain = append(chain, jh)
	}
	return chain, nil
}

// ErrNotFound reports a missing host.
var ErrNotFound = errors.New("not found")

// Tag is a named, colored label hosts are grouped by in the sidebar.
type Tag struct {
	Name string `json:"name"`
	// Color is a #RRGGBB value from the design palette.
	Color string `json:"color,omitempty"`
}
