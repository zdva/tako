package i18n_test

import (
	"testing"
	"time"

	"github.com/wei-dong/tako/internal/i18n"
)

func TestLocale_NumbersAndDates(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 11, 11, 15, 4, 0, 0, time.UTC)
	tests := []struct {
		locale, number, date, clock string
	}{
		{"en_US.UTF-8", "1,234.5", "Nov 11, 2026", "3:04 PM"},
		{"zh_CN.UTF-8", "1,234.5", "2026年11月11日", "15:04"},
		{"en_GB", "1,234.5", "11 Nov 2026", "15:04"},
		{"de_DE@euro", "1.234,5", "11.11.2026", "15:04"},
		{"", "1,234.5", "Nov 11, 2026", "3:04 PM"},
		{"garbage!!", "1,234.5", "Nov 11, 2026", "3:04 PM"},
	}
	for _, tt := range tests {
		l := i18n.ParseLocale(tt.locale)
		if got := l.Number(1234.5, 1); got != tt.number {
			t.Errorf("%q: Number = %q, want %q", tt.locale, got, tt.number)
		}
		if got := l.Date(day); got != tt.date {
			t.Errorf("%q: Date = %q, want %q", tt.locale, got, tt.date)
		}
		if got := l.Clock(day); got != tt.clock {
			t.Errorf("%q: Clock = %q, want %q", tt.locale, got, tt.clock)
		}
	}
	var zero i18n.Locale
	if zero.Number(2.5, 0) != "3" && zero.Number(2.5, 0) != "2" {
		t.Error("zero Locale does not format")
	}
}
