package settings_test

import (
	"path/filepath"
	"testing"

	"github.com/wei-dong/tako/internal/i18n"
	"github.com/wei-dong/tako/internal/scheme"
	"github.com/wei-dong/tako/internal/settings"
)

func TestSettings_DefaultsSaveAndReload(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v := s.Values()
	if v.ResolveAppearance() != settings.AppearanceSystem {
		t.Errorf("default appearance = %q", v.ResolveAppearance())
	}
	if got := v.ResolveLanguage("zh_CN.UTF-8"); got != i18n.Chinese {
		t.Errorf("language from zh_CN = %q", got)
	}
	if got := v.ResolveLanguage("en_US"); got != i18n.English {
		t.Errorf("language from en_US = %q", got)
	}
	if err := s.Update(func(v *settings.Values) {
		v.Language = i18n.English
		v.Appearance = settings.AppearanceDark
	}); err != nil {
		t.Fatal(err)
	}
	again, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Values()
	if got.Language != i18n.English || got.Appearance != settings.AppearanceDark {
		t.Errorf("reloaded = %+v", got)
	}
	if got.ResolveLanguage("zh_CN") != i18n.English {
		t.Error("a saved language must beat the system locale")
	}
}

func TestSettings_MonitorAndTransferDefaultsAndRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v := s.Values()
	if v.Monitor.Interval() != 2 || v.Monitor.Warn() != 70 || v.Monitor.Crit() != 90 || v.Monitor.Off {
		t.Errorf("monitor defaults %+v", v.Monitor)
	}
	if v.Transfer.Parallel() != 3 || v.Transfer.Policy() != settings.ConflictAsk || v.Transfer.AllowDirect {
		t.Errorf("transfer defaults %+v", v.Transfer)
	}
	if err := s.Update(func(v *settings.Values) {
		v.Monitor = settings.Monitor{IntervalSec: 5, WarnPct: 60, CritPct: 80}
		v.Transfer = settings.Transfer{Concurrency: 2, Conflict: settings.ConflictSkip, AllowDirect: true, Verify: "hash"}
	}); err != nil {
		t.Fatal(err)
	}
	again, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v = again.Values()
	if v.Monitor.Interval() != 5 || v.Monitor.Warn() != 60 || v.Monitor.Crit() != 80 ||
		v.Transfer.Parallel() != 2 || v.Transfer.Policy() != settings.ConflictSkip || !v.Transfer.AllowDirect || v.Transfer.Verify != "hash" {
		t.Errorf("round trip lost values: %+v", v)
	}
}

func TestSettings_TerminalAndConnectionDefaultsAndClamps(t *testing.T) {
	t.Parallel()
	var v settings.Values
	if v.Terminal.Font() != settings.DefaultFontSize || v.Connection.KeepAlive() != settings.DefaultKeepAlive {
		t.Errorf("defaults: font %d, keepalive %d", v.Terminal.Font(), v.Connection.KeepAlive())
	}
	for in, want := range map[int]int{1: settings.MinFontSize, 99: settings.MaxFontSize, 15: 15} {
		if got := (settings.Terminal{FontSize: in}).Font(); got != want {
			t.Errorf("Font(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestSettings_CustomSchemesSurviveReload(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "settings.json")
	s, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mine := scheme.Builtin[2]
	mine.ID, mine.Name = "custom:mine", "Mine"
	if err := s.Update(func(v *settings.Values) {
		v.Terminal = settings.Terminal{Scheme: mine.ID, Custom: []scheme.Scheme{mine}, FontSize: 14, CopyOnSelect: true}
		v.Connection.KeepAliveSec = 10
	}); err != nil {
		t.Fatal(err)
	}
	again, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Values()
	if got.Terminal.Scheme != mine.ID || len(got.Terminal.Custom) != 1 || got.Terminal.Custom[0] != mine ||
		got.Terminal.Font() != 14 || !got.Terminal.CopyOnSelect || got.Connection.KeepAlive() != 10 {
		t.Errorf("reloaded: %+v", got)
	}
}
