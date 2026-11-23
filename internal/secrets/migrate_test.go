package secrets

import (
	"context"
	"errors"
	"testing"
)

func TestMigrate_CopiesThenDeletes(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	src := NewMemory()
	dst := NewMemory()
	const pass = "S3cr3t-Pa55-xyz"
	if err := src.Set(ctx, "host/h/password", []byte(pass)); err != nil {
		t.Fatal(err)
	}
	if err := src.Set(ctx, "other", []byte("leave-me")); err != nil {
		t.Fatal(err)
	}
	got, err := Migrate(ctx, dst, src, []string{"host/h/password", "missing", "host/h/password", ""})
	if err != nil {
		t.Fatal(err)
	}
	if got.Copied != 1 || got.Missing != 1 || got.Deleted != 1 || got.DeleteErrs != 0 {
		t.Fatalf("result = %+v", got)
	}
	val, err := dst.Get(ctx, "host/h/password")
	if err != nil || string(val) != pass {
		t.Fatalf("dst = %q, %v", val, err)
	}
	if _, err := src.Get(ctx, "host/h/password"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("src still has the copied secret: %v", err)
	}
	left, err := src.Get(ctx, "other")
	if err != nil || string(left) != "leave-me" {
		t.Fatalf("unrelated secret = %q, %v", left, err)
	}
}

type failAfter struct {
	n   int
	mem *Memory
}

func (f *failAfter) Get(ctx context.Context, ref string) ([]byte, error) {
	return f.mem.Get(ctx, ref)
}

func (f *failAfter) Set(ctx context.Context, ref string, value []byte) error {
	if f.n == 0 {
		return errors.New("disk full")
	}
	f.n--
	return f.mem.Set(ctx, ref, value)
}

func (f *failAfter) Delete(ctx context.Context, ref string) error {
	return f.mem.Delete(ctx, ref)
}

func TestMigrate_WriteFailureDoesNotDeleteTheSource(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	src := NewMemory()
	if err := src.Set(ctx, "a", []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := src.Set(ctx, "b", []byte("two")); err != nil {
		t.Fatal(err)
	}
	dst := &failAfter{n: 1, mem: NewMemory()}
	_, err := Migrate(ctx, dst, src, []string{"a", "b"})
	if err == nil {
		t.Fatal("expected a write failure")
	}
	for _, ref := range []string{"a", "b"} {
		if _, gerr := src.Get(ctx, ref); gerr != nil {
			t.Errorf("src lost %s: %v", ref, gerr)
		}
	}
}

type deleteFail struct{ mem *Memory }

func (d deleteFail) Get(ctx context.Context, ref string) ([]byte, error) {
	return d.mem.Get(ctx, ref)
}
func (d deleteFail) Set(context.Context, string, []byte) error { return nil }
func (d deleteFail) Delete(context.Context, string) error      { return errors.New("keychain busy") }

func TestMigrate_DeleteFailureReportsTheCopy(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	src := deleteFail{mem: NewMemory()}
	if err := src.mem.Set(ctx, "a", []byte("one")); err != nil {
		t.Fatal(err)
	}
	dst := NewMemory()
	got, err := Migrate(ctx, dst, src, []string{"a"})
	if err == nil {
		t.Fatal("expected a delete failure")
	}
	if got.Copied != 1 || got.DeleteErrs != 1 {
		t.Fatalf("result = %+v", got)
	}
	if val, gerr := dst.Get(ctx, "a"); gerr != nil || string(val) != "one" {
		t.Fatalf("dst = %q, %v", val, gerr)
	}
}
