// Package settings holds the user's preferences (settings.json): language and
// appearance. It holds no secrets.
package settings

import (
	"cmp"
	"errors"
	"fmt"
	"sync"

	"github.com/wei-dong/tako/internal/i18n"
	"github.com/wei-dong/tako/internal/store"
)

// Version is the settings file version.
const Version = 1

// Appearance chooses the color scheme.
type Appearance string

// Appearances.
const (
	AppearanceSystem Appearance = "system"
	AppearanceLight  Appearance = "light"
	AppearanceDark   Appearance = "dark"
)

// Values is the content of settings.json.
type Values struct {
	Version int `json:"version"`
	// Language is "" (follow the system), "en" or "zh-Hans".
	Language   i18n.Language `json:"language,omitempty"`
	Appearance Appearance    `json:"appearance,omitempty"`
}

// Settings is the live, saved preferences.
type Settings struct {
	mu   sync.Mutex
	doc  store.Doc
	vals Values
}

// Open loads the settings at path, using defaults when it does not exist.
func Open(path string) (*Settings, error) {
	s := &Settings{doc: store.Doc{Path: path, Version: Version}}
	err := s.doc.Load(&s.vals)
	switch {
	case err == nil:
	case isMissing(err):
		s.vals = Values{Version: Version}
	default:
		return nil, fmt.Errorf("loading settings: %w", err)
	}
	return s, nil
}

func isMissing(err error) bool { return errors.Is(err, store.ErrNotExist) }

// Values returns a copy of the current values.
func (s *Settings) Values() Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vals
}

// Update changes the values and saves them.
func (s *Settings) Update(fn func(*Values)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.vals)
	s.vals.Version = Version
	if err := s.doc.Save(s.vals); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}
	return nil
}

// ResolveLanguage resolves the UI language: the saved one, else from the locale.
func (v Values) ResolveLanguage(locale string) i18n.Language {
	if v.Language != "" {
		return v.Language
	}
	return i18n.Detect(locale)
}

// ResolveAppearance returns the saved appearance, system by default.
func (v Values) ResolveAppearance() Appearance { return cmp.Or(v.Appearance, AppearanceSystem) }
