package oidcidentity

import "testing"

func TestCanonicalIdentity(t *testing.T) {
	t.Parallel()

	const issuer = "https://sso.example.test/realms/example"
	const opaque = "opaque-session-identifier"

	first, err := SessionID(issuer, opaque)
	if err != nil {
		t.Fatalf("canonicalize session: %v", err)
	}
	second, err := SessionID(issuer, opaque)
	if err != nil || first != second {
		t.Fatalf("session canonicalization is not stable: %q %q %v", first, second, err)
	}
	token, err := TokenID(issuer, opaque)
	if err != nil || token == first {
		t.Fatalf("identity kinds are not isolated: session=%q token=%q err=%v", first, token, err)
	}
	otherIssuer, err := SessionID(issuer+"-other", opaque)
	if err != nil || otherIssuer == first {
		t.Fatalf("issuers are not isolated: first=%q other=%q err=%v", first, otherIssuer, err)
	}
}

func TestCanonicalIdentityPreservesUUID(t *testing.T) {
	t.Parallel()

	const identity = "8C5211F0-018D-4A9A-B973-4F36B75B564A"
	canonical, err := Subject("https://sso.example.test/realms/example", identity)
	if err != nil || canonical != "8c5211f0-018d-4a9a-b973-4f36b75b564a" {
		t.Fatalf("preserve UUID: value=%q err=%v", canonical, err)
	}
}

func TestCanonicalIdentityRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "short", "contains whitespace", "opaque/session/identifier"} {
		if _, err := SessionID("https://sso.example.test/realms/example", value); err == nil {
			t.Fatalf("invalid value %q was accepted", value)
		}
	}
}
