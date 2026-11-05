package oidcidentity

import (
	"strings"
	"testing"
)

// TestCanonicalIdentity requires deterministic opaque mapping and distinct
// derived UUIDs for another issuer or identity kind, without external OIDC calls.
// TestCanonicalIdentity требует устойчивого opaque отображения и разных UUID
// при другом issuer или виде identity, без внешних OIDC-запросов.
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

// TestCanonicalIdentityPreservesUUID checks that uppercase UUID spelling is
// normalized while its identity is preserved instead of being namespace-hashed.
// TestCanonicalIdentityPreservesUUID проверяет нормализацию регистра UUID
// с сохранением идентичности вместо вычисления нового namespace-хеша.
func TestCanonicalIdentityPreservesUUID(t *testing.T) {
	t.Parallel()

	const identity = "8C5211F0-018D-4A9A-B973-4F36B75B564A"
	canonical, err := Subject("https://sso.example.test/realms/example", identity)
	if err != nil || canonical != "8c5211f0-018d-4a9a-b973-4f36b75b564a" {
		t.Fatalf("preserve UUID: value=%q err=%v", canonical, err)
	}
}

// TestCanonicalIdentityRejectsInvalidInput rejects missing, short, whitespace and
// slash-bearing opaque values before any identity can be returned.
// TestCanonicalIdentityRejectsInvalidInput запрещает пустой, короткий, содержащий
// пробелы или slash opaque ввод до возврата идентификатора.
func TestCanonicalIdentityRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "short", "contains whitespace", "opaque/session/identifier"} {
		if _, err := SessionID("https://sso.example.test/realms/example", value); err == nil {
			t.Fatalf("invalid value %q was accepted", value)
		}
	}
}

// TestScopedSubjectPreservesExactIssuerAndSubject isolates UUID subjects across issuers and preserves standard subject case and spaces.
// TestScopedSubjectPreservesExactIssuerAndSubject разделяет UUID subjects по issuer и сохраняет регистр и пробелы standard subject.
func TestScopedSubjectPreservesExactIssuerAndSubject(t *testing.T) {
	const issuer = "https://identity.example.invalid/realm/test"
	const subject = "20000000-0000-4000-8000-000000000001"
	first, err := ScopedSubject(issuer, subject)
	second, otherErr := ScopedSubject(issuer+"-other", subject)
	if err != nil || otherErr != nil || first == subject || first == second {
		t.Fatal("UUID subject escaped exact issuer binding")
	}
	for _, raw := range []string{"alice", "Alice", " alice ", strings.Repeat("x", 255)} {
		a, err := ScopedSubject(issuer, raw)
		b, repeatErr := ScopedSubject(issuer, raw)
		if err != nil || repeatErr != nil || a != b || a == first {
			t.Fatal("bounded standard subject mapping was not stable")
		}
		if raw != "alice" {
			plain, _ := ScopedSubject(issuer, "alice")
			if plain == a {
				t.Fatal("distinct exact subjects were normalized into one identity")
			}
		}
	}
	for _, raw := range []string{"", "line\nbreak", "zero\x00byte", "non-ascii-☃", strings.Repeat("x", 256)} {
		if _, err := ScopedSubject(issuer, raw); err == nil {
			t.Fatal("invalid scoped subject was accepted")
		}
	}
}
