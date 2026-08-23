package i18n

import (
	"testing"
	"testing/fstest"
)

func TestYAMLCatalogUsesRequestLocale(t *testing.T) {
	catalog, err := New(Config{
		Locale:       DefaultLocale,
		MessageFS:    fstest.MapFS{"active.en.yaml": {Data: []byte("ACTIVE:\n  other: Active\n")}, "active.ru.yaml": {Data: []byte("ACTIVE:\n  other: Активен\n")}},
		MessageFiles: []string{"active.en.yaml", "active.ru.yaml"},
	})
	if err != nil {
		t.Fatalf("создать каталог: %v", err)
	}
	if got := catalog.Localize("ru-RU", "ACTIVE", nil); got != "Активен" {
		t.Fatalf("ожидался русский текст, получено %q", got)
	}
	if got := catalog.Localize("en-US", "ACTIVE", nil); got != "Active" {
		t.Fatalf("ожидался английский текст, получено %q", got)
	}
}

func TestResolveAcceptLanguage(t *testing.T) {
	if got := ResolveAcceptLanguage("de;q=0.9, ru;q=0.8, en;q=0.5"); got != RussianLocale {
		t.Fatalf("ожидалась поддерживаемая русская локаль, получено %q", got)
	}
}
