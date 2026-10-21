package i18n

import (
	"testing"
	"testing/fstest"
)

// TestYAMLCatalogUsesRequestLocale loads in-memory YAML catalogs and verifies
// explicit Russian/English requests without changing the shared default.
// TestYAMLCatalogUsesRequestLocale загружает YAML в памяти и проверяет явные
// русские/английские запросы без изменения общей локали по умолчанию.
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

// TestResolveAcceptLanguage selects a supported weighted preference even when
// an unsupported language has a higher quality value.
// TestResolveAcceptLanguage выбирает поддерживаемое предпочтение с весом,
// даже если неподдерживаемый язык имеет больший quality.
func TestResolveAcceptLanguage(t *testing.T) {
	if got := ResolveAcceptLanguage("de;q=0.9, ru;q=0.8, en;q=0.5"); got != RussianLocale {
		t.Fatalf("ожидалась поддерживаемая русская локаль, получено %q", got)
	}
}
