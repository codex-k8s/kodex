package i18n

import (
	"testing"
	"testing/fstest"

	"golang.org/x/text/language"
)

func TestBundleLocalizeJSONMessage(t *testing.T) {
	t.Parallel()

	bundle := NewBundle(language.Russian)
	source := fstest.MapFS{
		"active.ru.json": {Data: []byte(`[{"id":"access.actions.project_create.name","translation":"Создать проект"}]`)},
	}

	if err := bundle.LoadJSONFS(source, "*.json"); err != nil {
		t.Fatalf("LoadJSONFS(): %v", err)
	}
	got, err := bundle.Localize("access.actions.project_create.name", "ru")
	if err != nil {
		t.Fatalf("Localize(): %v", err)
	}
	if got != "Создать проект" {
		t.Fatalf("localized message = %q, want %q", got, "Создать проект")
	}
}
