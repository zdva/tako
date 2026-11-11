package model

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func TestHost_Validate(t *testing.T) {
	t.Parallel()
	ok := Host{Name: "a", Addr: "h", User: "u", Auth: []Auth{{Kind: AuthPassword}}}
	tests := []struct {
		name   string
		mutate func(*Host)
		want   error
	}{
		{"valid", func(*Host) {}, nil},
		{"empty name", func(h *Host) { h.Name = " " }, ErrNameEmpty},
		{"empty addr", func(h *Host) { h.Addr = "" }, ErrAddrEmpty},
		{"empty user", func(h *Host) { h.User = "" }, ErrUserEmpty},
		{"port too big", func(h *Host) { h.Port = 70000 }, ErrPortInvalid},
		{"negative port", func(h *Host) { h.Port = -1 }, ErrPortInvalid},
		{"key without file", func(h *Host) { h.Auth = []Auth{{Kind: AuthKey}} }, ErrAuthInvalid},
		{"unknown auth", func(h *Host) { h.Auth = []Auth{{Kind: "magic"}} }, ErrAuthInvalid},
		{"proxy without port", func(h *Host) { h.Proxy = &Proxy{Kind: ProxySOCKS5, Addr: "p"} }, ErrProxyInvalid},
		{"unknown proxy kind", func(h *Host) { h.Proxy = &Proxy{Kind: "x", Addr: "p:1"} }, ErrProxyInvalid},
		{"good proxy", func(h *Host) { h.Proxy = &Proxy{Kind: ProxyHTTP, Addr: "p:3128"} }, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := ok.Clone()
			tc.mutate(&h)
			if err := h.Validate(); !errors.Is(err, tc.want) {
				t.Errorf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestHost_Defaults(t *testing.T) {
	t.Parallel()
	h := Host{Addr: "::1", User: "u"}
	if h.EffectivePort() != 22 || h.HostPort() != "[::1]:22" {
		t.Errorf("port %d, hostport %q", h.EffectivePort(), h.HostPort())
	}
	if h.KeepAliveInterval() != 30*time.Second || h.Policy() != HostKeyAsk {
		t.Errorf("keepalive %v, policy %q", h.KeepAliveInterval(), h.Policy())
	}
	if h.Target() != "u@::1" {
		t.Errorf("Target = %q", h.Target())
	}
}

func TestHost_CloneIsDeep(t *testing.T) {
	t.Parallel()
	h := Host{Tags: []string{"a"}, Auth: []Auth{{Kind: AuthAgent}}, Proxy: &Proxy{Addr: "x:1"}}
	c := h.Clone()
	c.Tags[0], c.Auth[0].Kind, c.Proxy.Addr = "b", AuthPassword, "y:2"
	if h.Tags[0] != "a" || h.Auth[0].Kind != AuthAgent || h.Proxy.Addr != "x:1" {
		t.Errorf("Clone shares memory with the original: %+v", h)
	}
}

func TestHost_SecretRefs(t *testing.T) {
	t.Parallel()
	h := Host{
		Auth:  []Auth{{Kind: AuthPassword, SecretRef: "a"}, {Kind: AuthAgent}},
		Proxy: &Proxy{SecretRef: "p"},
	}
	if got := h.SecretRefs(); !slices.Equal(got, []string{"a", "p"}) {
		t.Errorf("SecretRefs = %v", got)
	}
	if SecretRef("h_1", "password") != "host/h_1/password" {
		t.Error("SecretRef format changed")
	}
}

func TestNewID_IsUnique(t *testing.T) {
	t.Parallel()
	seen := map[ID]bool{}
	for range 1000 {
		id := NewID()
		if seen[id] || len(id) != 14 {
			t.Fatalf("bad or duplicate ID %q", id)
		}
		seen[id] = true
	}
}

func TestResolveJumps(t *testing.T) {
	t.Parallel()
	hosts := map[ID]Host{
		"a": {ID: "a", Name: "a"},
		"b": {ID: "b", Name: "b"},
		"c": {ID: "c", Name: "c", Jump: []Hop{{HostID: "a"}}},
	}
	find := func(id ID) (Host, bool) { h, ok := hosts[id]; return h, ok }
	tests := []struct {
		name string
		host Host
		want error
		ids  string
	}{
		{"chain", Host{ID: "t", Jump: []Hop{{HostID: "a"}, {HostID: "b"}}}, nil, "ab"},
		{"missing", Host{ID: "t", Jump: []Hop{{HostID: "zz"}}}, ErrNotFound, ""},
		{"self", Host{ID: "t", Jump: []Hop{{HostID: "t"}}}, ErrJumpLoop, ""},
		{"repeat", Host{ID: "t", Jump: []Hop{{HostID: "a"}, {HostID: "a"}}}, ErrJumpLoop, ""},
		{"hop with own chain is not followed", Host{ID: "t", Jump: []Hop{{HostID: "c"}}}, nil, "c"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			chain, err := ResolveJumps(tc.host, find)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			var ids string
			for _, h := range chain {
				ids += string(h.ID)
			}
			if ids != tc.ids {
				t.Errorf("chain = %q, want %q", ids, tc.ids)
			}
		})
	}
}
