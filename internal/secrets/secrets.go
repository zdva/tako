// Package secrets keeps passwords and key passphrases out of Tako's plain files.
//
// Everything secret goes through the [Store] interface. The store Tako ships
// is [File]: an encrypted file in the config directory, mode 0600, with no
// master password and no call to the operating system's credential store.
// [Memory] holds secrets for one run, and [Layered] uses it for the choice
// "do not remember".
//
// Values are []byte so callers can zero them after use. The file store keeps
// a copy in memory for the life of the process; that is the threat model in
// ADR 0023, not an accident.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

// LegacyService is the system credential-store service name used before Tako
// kept its own encrypted file. The one-time migration reads items stored
// under it. The running app does not.
const LegacyService = "dev.tako.app"

// Errors returned by a Store.
var (
	// ErrNotFound means no secret is stored for the reference.
	ErrNotFound = errors.New("secret not found")
	// ErrNotPersisted means the secret was kept for this session only. The
	// value is still usable until the process exits.
	ErrNotPersisted = errors.New("secret kept for this session only")
	// ErrSealed means a secret file is present but cannot be decrypted. Open
	// moves it aside and returns an empty store along with this error.
	ErrSealed = errors.New("secret store cannot be decrypted")
	// ErrBindingUnavailable means the file is bound to this machine and the
	// machine id could not be read. The file is left untouched.
	ErrBindingUnavailable = errors.New("machine binding is unavailable")
	// ErrTooNew means the file was written by a newer Tako. It is left untouched.
	ErrTooNew = errors.New("secret store was written by a newer version of Tako")
	// ErrTooLarge means the value is above the store's limit.
	ErrTooLarge = errors.New("secret is too large")
)

// errClosed means Close has already dropped the in-memory copies.
var errClosed = errors.New("secret store is closed")

// Store keeps secrets by reference. References look like
// "host/h_3f9a/password"; see model.SecretRef.
type Store interface {
	Get(ctx context.Context, ref string) ([]byte, error)
	Set(ctx context.Context, ref string, value []byte) error
	Delete(ctx context.Context, ref string) error
}

// Memory is a Store held in memory. It backs the "do not remember" choice
// and tests.
type Memory struct {
	mu     sync.Mutex
	items  map[string][]byte
	closed bool
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{items: make(map[string][]byte)} }

// Get implements [Store].
func (m *Memory) Get(_ context.Context, ref string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, errClosed
	}
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
	if m.closed {
		return errClosed
	}
	m.items[ref] = slices.Clone(value)
	return nil
}

// Delete implements [Store].
func (m *Memory) Delete(_ context.Context, ref string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errClosed
	}
	if v, ok := m.items[ref]; ok {
		clear(v)
		delete(m.items, ref)
	}
	return nil
}

// Close drops the in-memory copies. Later calls fail.
func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	for ref, v := range m.items {
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

// ephemeral never persists. [Layered] then keeps values for the session and
// reports [ErrNotPersisted], without touching a file that could not be opened.
type ephemeral struct{}

// NewEphemeral returns a Store that cannot persist. Use it when the encrypted
// file is present but its machine binding cannot be read, so a new file must
// not replace it.
func NewEphemeral() Store { return ephemeral{} }

func (ephemeral) Get(context.Context, string) ([]byte, error) { return nil, ErrNotFound }
func (ephemeral) Set(context.Context, string, []byte) error   { return errors.ErrUnsupported }
func (ephemeral) Delete(context.Context, string) error        { return nil }

// Layered tries a persistent store and keeps a second copy in memory for
// secrets the user asked not to save.
type Layered struct {
	persistent Store
	session    *Memory
}

// NewLayered returns a Store that uses persistent when it works. A persistent
// store that returns [errors.ErrUnsupported] keeps the value for the session
// and reports [ErrNotPersisted].
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

// Set implements [Store]. When the persistent store cannot store secrets it
// keeps the value in memory and returns an error wrapping [ErrNotPersisted].
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

// SetSession keeps a secret for this run only, never in the persistent store:
// the choice "do not remember" at the connect prompt.
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

// Close drops in-memory copies and closes the persistent store when it
// implements [io.Closer]. The encrypted file is left on disk.
func (l *Layered) Close() error {
	err := l.session.Close()
	if c, ok := l.persistent.(io.Closer); ok {
		err = errors.Join(err, c.Close())
	}
	return err
}
