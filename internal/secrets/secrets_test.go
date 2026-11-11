package secrets

import (
	"context"
	"errors"
	"testing"
)

type failingStore struct{ err error }

func (f failingStore) Get(context.Context, string) ([]byte, error) { return nil, f.err }
func (f failingStore) Set(context.Context, string, []byte) error   { return f.err }
func (f failingStore) Delete(context.Context, string) error        { return f.err }

func TestMemory_RoundTrip(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	m := NewMemory()
	if _, err := m.Get(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing: err = %v, want ErrNotFound", err)
	}
	if err := m.Set(ctx, "a", []byte("pw")); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, "a")
	if err != nil || string(got) != "pw" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	got[0] = 'X' // the returned slice is a copy
	again, _ := m.Get(ctx, "a")
	if string(again) != "pw" {
		t.Errorf("stored value changed through returned slice: %q", again)
	}
	if err := m.Delete(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Get(ctx, "a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete: err = %v", err)
	}
}

func TestLayered_FallsBackWhenUnsupported(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	l := NewLayered(failingStore{err: errors.ErrUnsupported})
	err := l.Set(ctx, "k", []byte("v"))
	if !errors.Is(err, ErrNotPersisted) {
		t.Fatalf("Set err = %v, want ErrNotPersisted", err)
	}
	got, err := l.Get(ctx, "k")
	if err != nil || string(got) != "v" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if _, err := l.Get(ctx, "other"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get other: err = %v, want ErrNotFound", err)
	}
	if err := l.Delete(ctx, "k"); err != nil {
		t.Errorf("Delete: %v", err)
	}
}

func TestLayered_UsesPersistentWhenItWorks(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := NewMemory()
	l := NewLayered(p)
	if err := l.Set(ctx, "k", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if refs := p.Refs(); len(refs) != 1 || refs[0] != "k" {
		t.Errorf("persistent refs = %v", refs)
	}
}

func TestLayered_OtherErrorsPropagate(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	l := NewLayered(failingStore{err: boom})
	if err := l.Set(t.Context(), "k", nil); !errors.Is(err, boom) {
		t.Errorf("Set err = %v, want boom", err)
	}
}

func TestLayered_SetSessionNeverReachesThePersistentStore(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	persistent := NewMemory()
	l := NewLayered(persistent)
	if err := l.SetSession(ctx, "k", []byte("v")); err != nil {
		t.Fatal(err)
	}
	if got, err := l.Get(ctx, "k"); err != nil || string(got) != "v" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if _, err := persistent.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the persistent store has the session secret: %v", err)
	}
	if err := l.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Get(ctx, "k"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete: %v", err)
	}
}
