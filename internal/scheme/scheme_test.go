package scheme_test

import (
	"os"
	"strings"
	"testing"

	"github.com/wei-dong/tako/internal/scheme"
)

func TestBuiltinSchemesAreValidAndUnique(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, s := range scheme.Builtin {
		if err := s.Validate(); err != nil {
			t.Errorf("%s: %v", s.ID, err)
		}
		if seen[s.ID] {
			t.Errorf("duplicate ID %s", s.ID)
		}
		seen[s.ID] = true
		if strings.HasPrefix(s.ID, "custom:") {
			t.Errorf("%s: built-in IDs must not look custom", s.ID)
		}
	}
	if len(scheme.Builtin) < 6 {
		t.Errorf("only %d built-in schemes", len(scheme.Builtin))
	}
}

func TestValidateRejectsBadColors(t *testing.T) {
	t.Parallel()
	good := scheme.Builtin[0]
	tests := map[string]func(*scheme.Scheme){
		"fg":      func(s *scheme.Scheme) { s.Foreground = "red" },
		"palette": func(s *scheme.Scheme) { s.Palette[9] = "#12345" },
		"cursor":  func(s *scheme.Scheme) { s.Cursor = "#GGGGGG" },
		"name":    func(s *scheme.Scheme) { s.Name = " " },
		"id":      func(s *scheme.Scheme) { s.ID = "" },
	}
	for name, mutate := range tests {
		s := good
		mutate(&s)
		if s.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Solarized Dark": "solarized-dark", "  My  Scheme!! ": "my-scheme", "日本語": "", "a_b-c": "a-b-c",
	} {
		if got := scheme.Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindPrefersCustomAndMergeReplaces(t *testing.T) {
	t.Parallel()
	mine := scheme.Builtin[0]
	mine.Name = "Mine"
	custom := scheme.Merge(nil, mine)
	got, ok := scheme.Find(mine.ID, custom)
	if !ok || got.Name != "Mine" {
		t.Fatalf("Find = %+v, %v", got, ok)
	}
	mine.Name = "Mine 2"
	custom = scheme.Merge(custom, mine)
	if len(custom) != 1 || custom[0].Name != "Mine 2" {
		t.Fatalf("Merge did not replace: %+v", custom)
	}
	if _, ok := scheme.Find("nope", custom); ok {
		t.Fatal("found a scheme that does not exist")
	}
	if g, ok := scheme.Find("nord", nil); !ok || g.Name != "Nord" {
		t.Fatalf("builtin lookup failed: %+v", g)
	}
}

func TestParseITerm(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/dracula.itermcolors")
	if err != nil {
		t.Fatal(err)
	}
	s, err := scheme.ParseITerm(data, "Dracula Pro")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "custom:dracula-pro" || s.Name != "Dracula Pro" || !s.Dark {
		t.Errorf("header: %+v", s)
	}
	want := scheme.Builtin[2] // Dracula
	if s.Foreground != want.Foreground || s.Background != want.Background || s.Palette != want.Palette ||
		s.Cursor != want.Cursor || s.Selection != want.Selection {
		t.Errorf("colors differ from the built-in Dracula:\n got %+v\nwant %+v", s, want)
	}
	if err := s.Validate(); err != nil {
		t.Error(err)
	}
}

func TestParseITermErrors(t *testing.T) {
	t.Parallel()
	good, err := os.ReadFile("testdata/dracula.itermcolors")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"empty":     nil,
		"not xml":   []byte("hello"),
		"truncated": good[:len(good)/2],
		"no ansi":   []byte(`<plist><dict><key>Foreground Color</key><dict></dict></dict></plist>`),
		"deep":      []byte(strings.Repeat("<dict><key>a</key>", 40)),
		"huge":      make([]byte, scheme.MaxITermSize+1),
	}
	for name, data := range cases {
		if _, err := scheme.ParseITerm(data, "x"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if s, err := scheme.ParseITerm(good, "日本"); err != nil || s.ID != "custom:imported" {
		t.Errorf("name with no ASCII: %+v, %v", s, err)
	}
}

func FuzzParseITerm(f *testing.F) {
	good, err := os.ReadFile("testdata/dracula.itermcolors")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(good, "x")
	f.Add([]byte(`<plist><dict><key>Ansi 0 Color</key><dict><key>Red Component</key><real>NaN</real></dict></dict></plist>`), "")
	f.Fuzz(func(t *testing.T, data []byte, name string) {
		s, err := scheme.ParseITerm(data, name)
		if err == nil {
			if verr := s.Validate(); verr != nil {
				t.Fatalf("parsed scheme is invalid: %v", verr)
			}
		}
	})
}
