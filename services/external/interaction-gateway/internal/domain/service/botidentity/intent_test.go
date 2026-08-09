package botidentity

import "testing"

func TestBotUsernameNormalizationIsBoundToOperation(t *testing.T) {
	t.Parallel()
	first, display, err := normalizeBotIntent("  Агент Primary  ", " Agent primary ",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	if err != nil || display != "Agent primary" || len(first) > 22 {
		t.Fatalf("normalized bot intent mismatch: %q %q %v", first, display, err)
	}
	same, _, err := normalizeBotIntent("  Агент Primary  ", " Agent primary ",
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")
	other, _, otherErr := normalizeBotIntent("  Агент Primary  ", " Agent primary ",
		"bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb")
	if err != nil || otherErr != nil || same != first || other == first {
		t.Fatalf("operation-bound normalization is not deterministic: %q %q %q", first, same, other)
	}
}
