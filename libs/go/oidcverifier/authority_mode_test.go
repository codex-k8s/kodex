package oidcverifier

import "testing"

// TestAuthorityModeSelection preserves the claims default and rejects misspelled, padded or unknown mode selectors instead of falling back.
// TestAuthorityModeSelection сохраняет claims default и отклоняет ошибочные, дополненные пробелами и неизвестные selectors без fallback.
func TestAuthorityModeSelection(t *testing.T) {
	for _, raw := range []string{"", "claims", "platform-managed"} {
		mode, valid := ParseAuthorityMode(raw)
		if !valid || raw == "platform-managed" && mode != PlatformManagedAuthorityMode || raw != "platform-managed" && mode != ClaimsAuthorityMode {
			t.Fatal("supported authority mode did not retain its exact contract")
		}
	}
	for _, raw := range []string{"platform", "automatic", " claims", "platform-managed ", "CLAIMS", "\x00"} {
		if _, valid := ParseAuthorityMode(raw); valid {
			t.Fatal("unknown authority mode silently selected a supported contract")
		}
	}
}
