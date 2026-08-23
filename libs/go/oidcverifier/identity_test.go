package oidcverifier

import (
	"strings"
	"testing"
)

func TestSafeDisplayName(t *testing.T) {
	t.Parallel()

	if actual := safeDisplayName("  Анна\t Петрова\u202e  ", "ignored"); actual != "Анна Петрова" {
		t.Fatalf("unexpected normalized display name: %q", actual)
	}
	if actual := safeDisplayName("", "operator"); actual != "operator" {
		t.Fatalf("preferred username was not used: %q", actual)
	}
	if actual := safeDisplayName("", ""); actual != unknownUserName {
		t.Fatalf("unexpected fallback display name: %q", actual)
	}
	if actual := safeDisplayName(strings.Repeat("я", maximumDisplayRunes+10), ""); len([]rune(actual)) != maximumDisplayRunes {
		t.Fatalf("display name was not bounded: %d", len([]rune(actual)))
	}
}

func TestMaskedVerifiedEmail(t *testing.T) {
	t.Parallel()

	if actual := maskedVerifiedEmail("Anna.Petrova@Example.COM", true); actual != "A***@example.com" {
		t.Fatalf("unexpected email hint: %q", actual)
	}
	for _, input := range []struct {
		email    string
		verified bool
	}{
		{"private@example.com", false},
		{"two@@example.com", true},
		{"missing-domain@localhost", true},
		{"line\nbreak@example.com", true},
	} {
		if actual := maskedVerifiedEmail(input.email, input.verified); actual != "" {
			t.Fatalf("unsafe email was exposed as %q", actual)
		}
	}
}
