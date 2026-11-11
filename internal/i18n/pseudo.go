package i18n

import (
	"strings"
	"unicode/utf8"
)

// Pseudo is a test-only language: English with accents, about 40% longer,
// wrapped in ⟦ ⟧. A string that shows without the brackets did not come from
// a table; one that loses its closing bracket was cut off. It is not offered
// in the settings window (see Languages).
const Pseudo Language = "en-XA"

var accents = map[rune]rune{
	'a': 'á', 'b': 'ƀ', 'c': 'ç', 'd': 'ð', 'e': 'é', 'f': 'ƒ', 'g': 'ĝ', 'h': 'ĥ', 'i': 'í', 'j': 'ĵ', 'k': 'ķ',
	'l': 'ļ', 'm': 'ḿ', 'n': 'ñ', 'o': 'ó', 'p': 'þ', 'q': 'ǫ', 'r': 'ŕ', 's': 'š', 't': 'ţ', 'u': 'ú', 'v': 'ṽ',
	'w': 'ŵ', 'x': 'ẋ', 'y': 'ý', 'z': 'ž',
	'A': 'Á', 'B': 'Ɓ', 'C': 'Ç', 'D': 'Ð', 'E': 'É', 'F': 'Ƒ', 'G': 'Ĝ', 'H': 'Ĥ', 'I': 'Í', 'J': 'Ĵ', 'K': 'Ķ',
	'L': 'Ļ', 'M': 'Ḿ', 'N': 'Ñ', 'O': 'Ó', 'P': 'Þ', 'Q': 'Ǫ', 'R': 'Ŕ', 'S': 'Š', 'T': 'Ţ', 'U': 'Ú', 'V': 'Ṽ',
	'W': 'Ŵ', 'X': 'Ẋ', 'Y': 'Ý', 'Z': 'Ž',
}

// pseudoize rewrites s, leaving printf verbs (%s, %d, %.1f, %[2]s, %%) alone
// so the result can still be formatted with the same arguments.
func pseudoize(s string) string {
	var b strings.Builder
	b.WriteString("⟦")
	letters := 0
	for i := 0; i < len(s); {
		if s[i] == '%' {
			j := i + 1
			for j < len(s) && strings.IndexByte("+-# 0123456789.[]*", s[j]) >= 0 {
				j++
			}
			if j < len(s) {
				j++ // the verb itself
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if a, ok := accents[r]; ok {
			r = a
			letters++
		} else if r > 127 {
			letters += 2 // wide characters need no more room, but count them
		}
		b.WriteRune(r)
		i += size
	}
	b.WriteString(strings.Repeat("~", letters*4/10))
	b.WriteString("⟧")
	return b.String()
}

func pseudoTable(en map[string]string) map[string]string {
	out := make(map[string]string, len(en))
	for k, v := range en {
		out[k] = pseudoize(v)
	}
	return out
}
