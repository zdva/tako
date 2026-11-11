// Package settings holds the user's preferences (settings.json): language and
// appearance. It holds no secrets.
package settings

import (
	"cmp"
	"errors"
	"fmt"
	"sync"

	"github.com/wei-dong/tako/internal/i18n"
	"github.com/wei-dong/tako/internal/scheme"
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
	// CompactBlocks lets blocks shrink to about 48x16 columns before the
	// grid scrolls; the default keeps them at 80x24.
	CompactBlocks bool `json:"compactBlocks,omitempty"`
	// Monitor and Transfer tune the footer readings and file transfers; the
	// zero values are the defaults.
	Monitor  Monitor  `json:"monitor,omitzero"`
	Transfer Transfer `json:"transfer,omitzero"`
	// Terminal and Connection are the Terminal and Connection pages of the
	// settings window.
	Terminal   Terminal   `json:"terminal,omitzero"`
	Connection Connection `json:"connection,omitzero"`
}

// Terminal settings.
type Terminal struct {
	// Scheme is the ID of the color scheme; "" follows the appearance.
	Scheme string `json:"scheme,omitempty"`
	// Custom holds the schemes the user imported (iTerm2 presets).
	Custom []scheme.Scheme `json:"custom,omitempty"`
	// FontSize is in points; 0 means 12.
	FontSize int `json:"fontSize,omitempty"`
	// CopyOnSelect copies the selection to the clipboard as it is made.
	CopyOnSelect bool `json:"copyOnSelect,omitempty"`
	// KeepContrast raises the contrast of text that is hard to read on its
	// background (WCAG ratio 3).
	KeepContrast bool `json:"keepContrast,omitempty"`
}

// Terminal bounds and defaults.
const (
	DefaultFontSize = 12
	MinFontSize     = 9
	MaxFontSize     = 24
)

// Font is the font size in points, clamped to the supported range.
func (t Terminal) Font() int {
	return min(max(cmp.Or(t.FontSize, DefaultFontSize), MinFontSize), MaxFontSize)
}

// Connection settings.
type Connection struct {
	// KeepAliveSec is the SSH keepalive for hosts that do not set their own;
	// 0 means 30.
	KeepAliveSec int `json:"keepAliveSec,omitempty"`
}

// DefaultKeepAlive is the keepalive interval, in seconds, when none is set.
const DefaultKeepAlive = 30

// KeepAlive is the default keepalive interval in seconds.
func (c Connection) KeepAlive() int { return cmp.Or(c.KeepAliveSec, DefaultKeepAlive) }

// Monitor settings.
type Monitor struct {
	// Off stops all monitoring.
	Off bool `json:"off,omitempty"`
	// IntervalSec is the time between samples; 0 means 2.
	IntervalSec int `json:"intervalSec,omitempty"`
	// WarnPct and CritPct color a reading amber and red; 0 means 70 and 90.
	WarnPct int `json:"warnPct,omitempty"`
	CritPct int `json:"critPct,omitempty"`
}

// Monitor defaults.
const (
	DefaultMonitorInterval = 2
	DefaultWarnPct         = 70
	DefaultCritPct         = 90
)

// Interval is the sampling interval in seconds.
func (m Monitor) Interval() int { return cmp.Or(m.IntervalSec, DefaultMonitorInterval) }

// Warn is the percentage from which a reading turns amber.
func (m Monitor) Warn() int { return cmp.Or(m.WarnPct, DefaultWarnPct) }

// Crit is the percentage from which a reading turns red.
func (m Monitor) Crit() int { return cmp.Or(m.CritPct, DefaultCritPct) }

// Conflict is what a transfer does with a same-name file.
type Conflict string

// Conflict policies.
const (
	ConflictAsk       Conflict = "ask"
	ConflictOverwrite Conflict = "overwrite"
	ConflictSkip      Conflict = "skip"
)

// Transfer settings.
type Transfer struct {
	// Concurrency is how many transfers run at once; 0 means 3.
	Concurrency int `json:"concurrency,omitempty"`
	// Conflict is the same-name policy; "" means ask.
	Conflict Conflict `json:"conflict,omitempty"`
	// Verify is "auto" (default), "hash" or "size".
	Verify string `json:"verify,omitempty"`
	// AllowDirect enables the direct scp/rsync mode between servers. It is
	// off by default because it forwards the SSH agent to the destination
	// for the length of a copy.
	AllowDirect bool `json:"allowDirect,omitempty"`
}

// Parallel is the number of simultaneous transfers.
func (t Transfer) Parallel() int { return cmp.Or(t.Concurrency, 3) }

// Policy is the same-name policy, ask by default.
func (t Transfer) Policy() Conflict { return cmp.Or(t.Conflict, ConflictAsk) }

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
