package usertext

import "testing"

func TestAssistantPlanCatalogSupportsRussianAndEnglish(t *testing.T) {
	localizer, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for locale, expected := range map[string]string{
		"ru": "Создать проект «Продажи»",
		"en": "Create project “Sales”",
	} {
		name := "Продажи"
		if locale == "en" {
			name = "Sales"
		}
		if actual := localizer.Localize(locale, "ASSISTANT_PLAN_CREATE_PROJECT", map[string]any{"Name": name}); actual != expected {
			t.Fatalf("locale %s: got %q, want %q", locale, actual, expected)
		}
	}
}
