package inputbus

import "testing"

func TestStripReplies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain typing", in: "ls -la\r", want: "ls -la\r"},
		{name: "arrow key", in: "\x1b[A", want: "\x1b[A"},
		{name: "function key", in: "\x1b[15~", want: "\x1b[15~"},
		{name: "alt key", in: "\x1bb", want: "\x1bb"},
		{name: "kitty key", in: "\x1b[97;5u", want: "\x1b[97;5u"},
		{name: "bracketed paste is kept", in: "\x1b[200~hello\x1b[201~", want: "\x1b[200~hello\x1b[201~"},
		{name: "shift tab", in: "\x1b[Z", want: "\x1b[Z"},
		{name: "cursor position report", in: "\x1b[24;80R", want: ""},
		{name: "extended cursor position report", in: "\x1b[?24;80R", want: ""},
		{name: "device attributes", in: "\x1b[?62;22c", want: ""},
		{name: "secondary device attributes", in: "\x1b[>1;10;0c", want: ""},
		{name: "mode report", in: "\x1b[?2026;2$y", want: ""},
		{name: "color scheme report", in: "\x1b[?997;1n", want: ""},
		{name: "size report", in: "\x1b[8;24;80t", want: ""},
		{name: "focus in", in: "\x1b[I", want: ""},
		{name: "focus out", in: "\x1b[O", want: ""},
		{name: "sgr mouse press", in: "\x1b[<0;10;5M", want: ""},
		{name: "sgr mouse release", in: "\x1b[<0;10;5m", want: ""},
		{name: "x10 mouse", in: "\x1b[M !!", want: ""},
		{name: "osc color answer with ST", in: "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\", want: ""},
		{name: "osc answer with BEL", in: "\x1b]10;rgb:ffff/ffff/ffff\x07", want: ""},
		{name: "dcs answer", in: "\x1bP1+r544e=787465726d\x1b\\", want: ""},
		{name: "modified F3 is a key", in: "\x1b[1;2R", want: "\x1b[1;2R"},
		{name: "mixed chunk", in: "a\x1b[24;80Rb\x1b[Ac", want: "ab\x1b[Ac"},
		{name: "truncated sequence is kept", in: "x\x1b[", want: "x\x1b["},
		{name: "empty", in: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := string(StripReplies([]byte(tc.in))); got != tc.want {
				t.Fatalf("StripReplies(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
