package i18n

import "testing"

func TestDefaultLocaleIsEnglish(t *testing.T) {
	localizer, err := New("")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if localizer.Locale() != DefaultLocale {
		t.Fatalf("Locale() = %q", localizer.Locale())
	}
	if got := localizer.T("label.configured", nil); got != "configured" {
		t.Fatalf("T(label.configured) = %q", got)
	}
}

func TestSetLocaleSupportsRussianTags(t *testing.T) {
	localizer, err := New(DefaultLocale)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	locale, err := localizer.SetLocale("ru-RU")
	if err != nil {
		t.Fatalf("SetLocale() error = %v", err)
	}
	if locale != RussianLocale || localizer.Locale() != RussianLocale {
		t.Fatalf("locale = %q current = %q", locale, localizer.Locale())
	}
	if got := localizer.T("label.configured", nil); got != "настроен" {
		t.Fatalf("T(label.configured) = %q", got)
	}
}

func TestSetLocaleRejectsUnsupportedLocale(t *testing.T) {
	localizer, err := New(DefaultLocale)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := localizer.SetLocale("fr"); err == nil {
		t.Fatal("SetLocale() error = nil")
	}
}

func TestTemplateData(t *testing.T) {
	localizer, err := New(DefaultLocale)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	got := localizer.T("locale.set", map[string]any{"Locale": "ru"})
	if got != "matter-codex: locale changed to `ru`." {
		t.Fatalf("T(locale.set) = %q", got)
	}
}
