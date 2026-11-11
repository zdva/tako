package model

import (
	"testing"
	"time"
)

func TestHost_DefaultsAndShortForms(t *testing.T) {
	t.Parallel()
	h := Host{Addr: "10.0.0.1", User: "ops"}
	if h.EffectivePort() != DefaultPort || h.HostPort() != "10.0.0.1:22" {
		t.Errorf("default port: %d %s", h.EffectivePort(), h.HostPort())
	}
	h.Port = 2222
	if h.EffectivePort() != 2222 || h.HostPort() != "10.0.0.1:2222" {
		t.Errorf("custom port: %d %s", h.EffectivePort(), h.HostPort())
	}
	if (Host{Addr: "::1", Port: 22}).HostPort() != "[::1]:22" {
		t.Error("IPv6 must be bracketed")
	}
	if h.KeepAliveInterval() != 30*time.Second {
		t.Errorf("default keepalive = %v", h.KeepAliveInterval())
	}
	h.KeepAlive = 5
	if h.KeepAliveInterval() != 5*time.Second {
		t.Errorf("keepalive = %v", h.KeepAliveInterval())
	}
	if h.Policy() != HostKeyAsk {
		t.Errorf("default policy = %v", h.Policy())
	}
	h.HostKeyPolicy = HostKeyStrict
	if h.Policy() != HostKeyStrict {
		t.Errorf("policy = %v", h.Policy())
	}
	if h.Target() != "ops@10.0.0.1" || (Host{Addr: "x"}).Target() != "x" {
		t.Errorf("target = %q", h.Target())
	}
}
