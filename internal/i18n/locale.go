package i18n

import (
	"strings"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Locale formats numbers and dates as the system's region does, whatever
// language the text is in: an English interface on a German system still
// writes 1.234,5. The zero value formats as en-US.
type Locale struct {
	tag language.Tag
	p   *message.Printer
}

// ParseLocale reads a locale name such as "zh_CN.UTF-8", "en-GB" or "de_DE@euro".
// An unknown or empty name gives en-US.
func ParseLocale(name string) Locale {
	name, _, _ = strings.Cut(name, ".")
	name, _, _ = strings.Cut(name, "@")
	tag, err := language.Parse(strings.ReplaceAll(name, "_", "-"))
	if err != nil || tag == language.Und {
		tag = language.AmericanEnglish
	}
	return Locale{tag: tag, p: message.NewPrinter(tag)}
}

func (l Locale) printer() *message.Printer {
	if l.p == nil {
		return message.NewPrinter(language.AmericanEnglish)
	}
	return l.p
}

// Number writes v with the given number of decimals and the locale's
// separators ("1,234.5" or "1.234,5").
func (l Locale) Number(v float64, decimals int) string {
	return l.printer().Sprintf("%.*f", decimals, v)
}

func (l Locale) base() string {
	b, _ := l.tag.Base()
	return b.String()
}

func (l Locale) region() string {
	r, _ := l.tag.Region()
	return r.String()
}

func (l Locale) twelveHour() bool {
	switch l.base() {
	case "en":
		return l.region() == "US" || l.region() == "ZZ" || l.region() == "CA" || l.region() == "AU" || l.region() == "IN"
	default:
		return false
	}
}

// Date writes a day the way the locale does: 2026年11月11日, Nov 11, 2026,
// 11 Nov 2026 or 11.11.2026.
func (l Locale) Date(t time.Time) string {
	switch l.base() {
	case "zh", "ja":
		return t.Format("2006年1月2日")
	case "en":
		if r := l.region(); r == "US" || r == "ZZ" {
			return t.Format("Jan 2, 2006")
		}
		return t.Format("2 Jan 2006")
	case "de", "ru", "pl", "tr":
		return t.Format("02.01.2006")
	case "fr", "es", "it", "pt":
		return t.Format("02/01/2006")
	}
	return t.Format("2006-01-02")
}

// Clock writes a time of day, in 12 hours where the locale uses them.
func (l Locale) Clock(t time.Time) string {
	if l.twelveHour() {
		return t.Format("3:04 PM")
	}
	return t.Format("15:04")
}
