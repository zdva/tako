// Package secrets keeps passwords and key passphrases out of Tako's files.
//
// Everything secret goes through the [Store] interface. The real
// implementation, [Keychain], uses MyGo's credential store (macOS Keychain,
// Windows Credential Manager, the Linux Secret Service). Where the system has
// none, [Layered] keeps secrets in memory for the running session only, and
// says so with [ErrNotPersisted] so the UI can tell the user.
//
// Values are []byte so callers can zero them after use.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/egoist/mygo"
)

// Service is the keychain service name Tako stores its secrets under.
const Service = "dev.tako.app"

// Errors returned by a Store.
var (
	// ErrNotFound means no secret is stored for the reference.
	ErrNotFound = errors.New("secret not found")
	// ErrNotPersisted means the secret was kept for this session only because
	// the system has no credential store. The value is still usable.
	ErrNotPersisted = errors.New("secret kept for this session only")
)

// Store keeps secrets by reference. References look like
// "host/h_3f9a/password"; see model.SecretRef.
type Store interface {
	Get(ctx context.Context, ref string) ([]byte, error)
	Set(ctx context.Context, ref string, value []byte) error
	Delete(ctx context.Context, ref string) error
}

// Keychain is the system credential store.
type Keychain struct {
	module mygo.SecretsModule
}

// NewKeychain returns the system store under [Service].
func NewKeychain() *Keychain {
	return &Keychain{module: mygo.Secrets.Service(Service)}
}

// Get implements [Store].
func (k *Keychain) Get(ctx context.Context, ref string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("reading secret: %w", err)
	}
	v, err := k.module.Get(ref)
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("reading secret from the keychain: %w", err)
	}
	return v, nil
}

// Set implements [Store].
func (k *Keychain) Set(ctx context.Context, ref string, value []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("storing secret: %w", err)
	}
	if err := k.module.Set(ref, value); err != nil {
		return fmt.Errorf("storing secret in the keychain: %w", err)
	}
	return nil
}

// Delete implements [Store]. Deleting a missing secret is not an error.
func (k *Keychain) Delete(ctx context.Context, ref string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("deleting secret: %w", err)
	}
	if err := k.module.Delete(ref); err != nil {
		return fmt.Errorf("deleting secret from the keychain: %w", err)
	}
	return nil
}

// Memory is a Store held in memory. It backs [Layered] when the system has no
// credential store, and tests.
type Memory struct {
	mu    sync.Mutex
	items map[string][]byte
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{items: make(map[string][]byte)} }

// Get implements [Store].
func (m *Memory) Get(_ context.Context, ref string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.items[ref]
	if !ok {
		return nil, ErrNotFound
	}
	return slices.Clone(v), nil
}

// Set implements [Store].
func (m *Memory) Set(_ context.Context, ref string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[ref] = slices.Clone(value)
	return nil
}

// Delete implements [Store].
func (m *Memory) Delete(_ context.Context, ref string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.items[ref]; ok {
		clear(v)
		delete(m.items, ref)
	}
	return nil
}

// Refs returns the references held, for tests.
func (m *Memory) Refs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	refs := make([]string, 0, len(m.items))
	for ref := range m.items {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	return refs
}

// Layered tries a persistent store and falls back to memory when it is
// unavailable.
type Layered struct {
	persistent Store
	session    *Memory
}

// NewLayered returns a Store that uses persistent when it works and keeps
// secrets for the session otherwise.
func NewLayered(persistent Store) *Layered {
	return &Layered{persistent: persistent, session: NewMemory()}
}

// Get implements [Store]. A secret held for the session wins over the
// persistent one.
func (l *Layered) Get(ctx context.Context, ref string) ([]byte, error) {
	if v, err := l.session.Get(ctx, ref); err == nil {
		return v, nil
	}
	v, err := l.persistent.Get(ctx, ref)
	if err != nil && !errors.Is(err, ErrNotFound) && errors.Is(err, errors.ErrUnsupported) {
		return nil, ErrNotFound
	}
	return v, err
}

// Set implements [Store]. When the system has no credential store it keeps
// the value in memory and returns an error wrapping [ErrNotPersisted].
func (l *Layered) Set(ctx context.Context, ref string, value []byte) error {
	err := l.persistent.Set(ctx, ref, value)
	if err == nil {
		return nil
	}
	if !errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	if serr := l.session.Set(ctx, ref, value); serr != nil {
		return serr
	}
	return fmt.Errorf("%w: %w", ErrNotPersisted, err)
}

// SetSession keeps a secret for this run only, never in the keychain: the
// choice "do not remember" at the connect prompt.
func (l *Layered) SetSession(ctx context.Context, ref string, value []byte) error {
	return l.session.Set(ctx, ref, value)
}

// Delete implements [Store], removing the secret from both layers.
func (l *Layered) Delete(ctx context.Context, ref string) error {
	if err := l.session.Delete(ctx, ref); err != nil {
		return err
	}
	if err := l.persistent.Delete(ctx, ref); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		return err
	}
	return nil
}
