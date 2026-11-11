package scheme

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Default is the ID of the scheme that follows Tako's own light and dark
// tokens instead of overriding them.
const Default = ""

// Scheme is one set of terminal colors, every color as "#RRGGBB".
type Scheme struct {
	// ID is stable and unique: built-ins use their slug, imported schemes
	// "custom:<slug>".
	ID   string `json:"id"`
	Name string `json:"name"`
	// Dark says the background is dark; it picks the cursor and the
	// selection's contrast.
	Dark       bool   `json:"dark,omitempty"`
	Foreground string `json:"fg"`
	Background string `json:"bg"`
	// Cursor and Selection may be empty: the foreground is used for the
	// cursor and a translucent foreground for the selection.
	Cursor    string `json:"cursor,omitempty"`
	Selection string `json:"selection,omitempty"`
	// Palette is black, red, green, yellow, blue, magenta, cyan and white,
	// then their bright kinds.
	Palette [16]string `json:"palette"`
}

// Validation errors.
var (
	ErrColor = errors.New("scheme: not a #RRGGBB color")
	ErrName  = errors.New("scheme: no name")
)

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Valid reports whether s is a hex color.
func Valid(s string) bool { return hexColor.MatchString(s) }

// Validate checks that every color is a hex color and the scheme has a name
// and an ID.
func (s Scheme) Validate() error {
	if strings.TrimSpace(s.Name) == "" || s.ID == "" {
		return ErrName
	}
	required := append([]string{s.Foreground, s.Background}, s.Palette[:]...)
	for _, c := range required {
		if !Valid(c) {
			return fmt.Errorf("%w: %q in %s", ErrColor, c, s.Name)
		}
	}
	for _, c := range []string{s.Cursor, s.Selection} {
		if c != "" && !Valid(c) {
			return fmt.Errorf("%w: %q in %s", ErrColor, c, s.Name)
		}
	}
	return nil
}

// Slug turns a name into an ID fragment: lower case ASCII letters, digits and
// dashes.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

// Find returns the scheme with the ID, looking in custom first and then in
// the built-in ones.
func Find(id string, custom []Scheme) (Scheme, bool) {
	if i := slices.IndexFunc(custom, func(s Scheme) bool { return s.ID == id }); i >= 0 {
		return custom[i], true
	}
	if i := slices.IndexFunc(Builtin, func(s Scheme) bool { return s.ID == id }); i >= 0 {
		return Builtin[i], true
	}
	return Scheme{}, false
}

// Merge adds or replaces s in custom (matched by ID) and returns the list.
func Merge(custom []Scheme, s Scheme) []Scheme {
	out := slices.Clone(custom)
	if i := slices.IndexFunc(out, func(x Scheme) bool { return x.ID == s.ID }); i >= 0 {
		out[i] = s
		return out
	}
	return append(out, s)
}

func p(c ...string) [16]string { return [16]string(c) }

// Builtin lists the schemes that ship with Tako. The default scheme (Default)
// is not in the list: it follows the light or dark tokens.
var Builtin = []Scheme{
	{
		ID: "solarized-dark", Name: "Solarized Dark", Dark: true, Foreground: "#839496", Background: "#002B36",
		Cursor: "#93A1A1", Selection: "#073642",
		Palette: p("#073642", "#DC322F", "#859900", "#B58900", "#268BD2", "#D33682", "#2AA198", "#EEE8D5",
			"#002B36", "#CB4B16", "#586E75", "#657B83", "#839496", "#6C71C4", "#93A1A1", "#FDF6E3"),
	},
	{
		ID: "solarized-light", Name: "Solarized Light", Foreground: "#657B83", Background: "#FDF6E3",
		Cursor: "#586E75", Selection: "#EEE8D5",
		Palette: p("#073642", "#DC322F", "#859900", "#B58900", "#268BD2", "#D33682", "#2AA198", "#EEE8D5",
			"#002B36", "#CB4B16", "#586E75", "#657B83", "#839496", "#6C71C4", "#93A1A1", "#FDF6E3"),
	},
	{
		ID: "dracula", Name: "Dracula", Dark: true, Foreground: "#F8F8F2", Background: "#282A36",
		Cursor: "#F8F8F2", Selection: "#44475A",
		Palette: p("#21222C", "#FF5555", "#50FA7B", "#F1FA8C", "#BD93F9", "#FF79C6", "#8BE9FD", "#F8F8F2",
			"#6272A4", "#FF6E6E", "#69FF94", "#FFFFA5", "#D6ACFF", "#FF92DF", "#A4FFFF", "#FFFFFF"),
	},
	{
		ID: "nord", Name: "Nord", Dark: true, Foreground: "#D8DEE9", Background: "#2E3440",
		Cursor: "#D8DEE9", Selection: "#434C5E",
		Palette: p("#3B4252", "#BF616A", "#A3BE8C", "#EBCB8B", "#81A1C1", "#B48EAD", "#88C0D0", "#E5E9F0",
			"#4C566A", "#BF616A", "#A3BE8C", "#EBCB8B", "#81A1C1", "#B48EAD", "#8FBCBB", "#ECEFF4"),
	},
	{
		ID: "one-dark", Name: "One Dark", Dark: true, Foreground: "#ABB2BF", Background: "#282C34",
		Cursor: "#528BFF", Selection: "#3E4451",
		Palette: p("#282C34", "#E06C75", "#98C379", "#E5C07B", "#61AFEF", "#C678DD", "#56B6C2", "#ABB2BF",
			"#5C6370", "#E06C75", "#98C379", "#E5C07B", "#61AFEF", "#C678DD", "#56B6C2", "#FFFFFF"),
	},
	{
		ID: "gruvbox-dark", Name: "Gruvbox Dark", Dark: true, Foreground: "#EBDBB2", Background: "#282828",
		Cursor: "#EBDBB2", Selection: "#504945",
		Palette: p("#282828", "#CC241D", "#98971A", "#D79921", "#458588", "#B16286", "#689D6A", "#A89984",
			"#928374", "#FB4934", "#B8BB26", "#FABD2F", "#83A598", "#D3869B", "#8EC07C", "#EBDBB2"),
	},
	{
		ID: "tokyo-night", Name: "Tokyo Night", Dark: true, Foreground: "#C0CAF5", Background: "#1A1B26",
		Cursor: "#C0CAF5", Selection: "#33467C",
		Palette: p("#15161E", "#F7768E", "#9ECE6A", "#E0AF68", "#7AA2F7", "#BB9AF7", "#7DCFFF", "#A9B1D6",
			"#414868", "#F7768E", "#9ECE6A", "#E0AF68", "#7AA2F7", "#BB9AF7", "#7DCFFF", "#C0CAF5"),
	},
}
