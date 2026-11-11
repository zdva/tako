// Package i18n looks up the UI's strings in the language tables embedded in
// the binary (locales/*.json). Keys are constants (keys.go), so a typo is a
// compile error and a test checks every language has every key.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"
)

//go:embed locales/*.json
var localeFS embed.FS

// Language is a supported UI language tag.
type Language string

// Supported languages.
const (
	English Language = "en"
	Chinese Language = "zh-Hans"
)

// Languages lists the supported languages.
func Languages() []Language { return []Language{Chinese, English} }

// Bundle is the strings of one language, with English behind it.
type Bundle struct {
	lang     Language
	table    map[string]string
	fallback map[string]string
}

var (
	loadOnce sync.Once
	tables   map[Language]map[string]string
	loadErr  error
)

func load() {
	tables = map[Language]map[string]string{}
	for _, l := range Languages() {
		data, err := fs.ReadFile(localeFS, "locales/"+string(l)+".json")
		if err != nil {
			loadErr = fmt.Errorf("reading locale %s: %w", l, err)
			return
		}
		var m map[string]string
		if err := json.Unmarshal(data, &m); err != nil {
			loadErr = fmt.Errorf("parsing locale %s: %w", l, err)
			return
		}
		tables[l] = m
	}
}

// New returns the bundle for a language. An unknown language gets English.
func New(lang Language) (*Bundle, error) {
	loadOnce.Do(load)
	if loadErr != nil {
		return nil, loadErr
	}
	t, ok := tables[lang]
	if !ok {
		lang, t = English, tables[English]
	}
	return &Bundle{lang: lang, table: t, fallback: tables[English]}, nil
}

// Must is New for tables that are known good (they are embedded and tested).
func Must(lang Language) *Bundle {
	b, err := New(lang)
	if err != nil {
		panic(err)
	}
	return b
}

// Detect maps a locale such as "zh_CN.UTF-8" or "en-US" to a language.
func Detect(locale string) Language {
	l := strings.ToLower(locale)
	if strings.HasPrefix(l, "zh") {
		return Chinese
	}
	return English
}

// Language returns the bundle's language.
func (b *Bundle) Language() Language { return b.lang }

// T returns the string for key with fmt.Sprintf arguments applied. A missing
// key returns the key itself, so a gap shows in the UI instead of vanishing.
func (b *Bundle) T(key string, args ...any) string {
	s, ok := b.table[key]
	if !ok {
		if s, ok = b.fallback[key]; !ok {
			return key
		}
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Keys returns every key of a language's table, for tests.
func Keys(lang Language) []string {
	loadOnce.Do(load)
	out := make([]string, 0, len(tables[lang]))
	for k := range tables[lang] {
		out = append(out, k)
	}
	return out
}
