package i18n_test

import (
	"regexp"
	"slices"
	"testing"

	"github.com/wei-dong/tako/internal/i18n"
)

var verb = regexp.MustCompile(`%[-+# 0]*[0-9.]*[a-zA-Z]`)

func TestLocales_HaveTheSameKeysAndFormatVerbs(t *testing.T) {
	en, zh := i18n.Must(i18n.English), i18n.Must(i18n.Chinese)
	enKeys, zhKeys := i18n.Keys(i18n.English), i18n.Keys(i18n.Chinese)
	slices.Sort(enKeys)
	slices.Sort(zhKeys)
	if !slices.Equal(enKeys, zhKeys) {
		t.Fatalf("key sets differ:\nen: %v\nzh: %v", enKeys, zhKeys)
	}
	for _, k := range enKeys {
		e, z := verb.FindAllString(en.T(k), -1), verb.FindAllString(zh.T(k), -1)
		if len(e) != len(z) {
			t.Errorf("%s: en takes %v, zh takes %v", k, e, z)
		}
	}
}

func TestDetect(t *testing.T) {
	for locale, want := range map[string]i18n.Language{
		"zh_CN.UTF-8": i18n.Chinese, "zh-Hans": i18n.Chinese, "en_US.UTF-8": i18n.English, "": i18n.English, "de_DE": i18n.English,
	} {
		if got := i18n.Detect(locale); got != want {
			t.Errorf("Detect(%q) = %q, want %q", locale, got, want)
		}
	}
}

func TestT_MissingKeyReturnsKeyAndFormats(t *testing.T) {
	b := i18n.Must(i18n.English)
	if got := b.T("no.such.key"); got != "no.such.key" {
		t.Fatalf("missing key = %q", got)
	}
	if got := b.T("hosts.count", 3); got != "3 hosts" {
		t.Fatalf("formatted = %q", got)
	}
}
