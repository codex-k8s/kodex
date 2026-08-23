package usertext

import "testing"

func TestCatalogUsesConnectionLocale(t *testing.T) {
	t.Parallel()
	catalog, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Localize("ru-RU", "MATTERMOST_GATE_STALE", nil); got == "MATTERMOST_GATE_STALE" {
		t.Fatal("Russian Mattermost message was not localized")
	}
	if got := catalog.Localize("en-US", "MATTERMOST_GATE_STALE", nil); got == "MATTERMOST_GATE_STALE" {
		t.Fatal("English Mattermost message was not localized")
	}
}
