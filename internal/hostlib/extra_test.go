package hostlib

import (
	"errors"
	"testing"

	"github.com/zdva/tako/internal/model"
)

func TestLibrary_TagColorsLenAndFlush(t *testing.T) {
	l, _, path := open(t)
	if l.Len() != 0 {
		t.Fatalf("Len = %d", l.Len())
	}
	if _, err := l.Add(host("web-01", "prod")); err != nil {
		t.Fatal(err)
	}
	if l.Len() != 1 {
		t.Fatalf("Len = %d", l.Len())
	}
	if got := l.TagColor("prod"); got == "" {
		t.Error("a new tag gets a palette color")
	}
	l.SetTagColor("prod", "#112233")
	l.SetTagColor("extra", "#445566")
	if l.TagColor("prod") != "#112233" || l.TagColor("extra") != "#445566" || l.TagColor("nope") != "" {
		t.Errorf("tag colors: %q %q %q", l.TagColor("prod"), l.TagColor("extra"), l.TagColor("nope"))
	}
	l.Flush()
	reopened, _, _ := openAt(t, path)
	if reopened.TagColor("prod") != "#112233" {
		t.Fatalf("color was not saved: %q", reopened.TagColor("prod"))
	}
}

func TestLibrary_UpdateRejectsUnknownAndDuplicate(t *testing.T) {
	l, _, _ := open(t)
	a, _ := l.Add(host("a"))
	if _, err := l.Add(host("b")); err != nil {
		t.Fatal(err)
	}
	ghost := host("ghost")
	ghost.ID = "h_nope"
	if err := l.Update(ghost); err == nil {
		t.Error("updating an unknown host succeeded")
	}
	a.Name = "b"
	if err := l.Update(a); !errors.Is(err, ErrDuplicateName) {
		t.Errorf("duplicate rename: %v", err)
	}
	bad := a
	bad.Name = ""
	if err := l.Update(bad); !errors.Is(err, model.ErrNameEmpty) {
		t.Errorf("invalid update: %v", err)
	}
}
