package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestSessionRotationAndCSRF uses temporary synthetic keys and a fixed clock to check exact CSRF, previous-key overlap, rejection without overlap and ciphertext tampering. No real OIDC issuer, browser or deployed key rotation is involved.
// TestSessionRotationAndCSRF использует временные synthetic keys и фиксированные часы для проверки exact CSRF, previous-key overlap, отказа без overlap и изменения ciphertext. Реальные OIDC issuer, browser и deployed key rotation не используются.
func TestSessionRotationAndCSRF(t *testing.T) {
	directory := t.TempDir()
	first := filepath.Join(directory, "first.hex")
	second := filepath.Join(directory, "second.hex")
	writeKey(t, first, strings.Repeat("11", 32))
	writeKey(t, second, strings.Repeat("22", 32))
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	original, err := New(Config{CurrentKeyFile: first, TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("new original store: %v", err)
	}
	original.now = func() time.Time { return now }
	claims, encoded, csrf, err := original.Issue(uuid.NewString(), uuid.NewString(), uuid.NewString(), 7, "header.payload.signature", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !VerifyCSRF(claims, csrf) || VerifyCSRF(claims, csrf+"x") {
		t.Fatal("CSRF verification is not exact")
	}
	rotated, err := New(Config{CurrentKeyFile: second, PreviousKeyFile: first, TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("new rotated store: %v", err)
	}
	rotated.now = func() time.Time { return now.Add(time.Minute) }
	opened, err := rotated.Open(encoded)
	if err != nil || opened.Subject != claims.Subject || opened.SessionRevision != 7 {
		t.Fatalf("open previous generation: %#v, %v", opened, err)
	}
	withoutOverlap, err := New(Config{CurrentKeyFile: second, TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("new current-only store: %v", err)
	}
	withoutOverlap.now = rotated.now
	if _, err := withoutOverlap.Open(encoded); err == nil {
		t.Fatal("old generation accepted without overlap")
	}
	last := "A"
	if encoded[len(encoded)-1] == 'A' {
		last = "B"
	}
	if _, err := rotated.Open(encoded[:len(encoded)-1] + last); err == nil {
		t.Fatal("modified ciphertext accepted")
	}
}

// TestSessionExpiryIsClosed advances the local store clock beyond a synthetic cookie's expiry and requires Open to reject it, without a live browser session or revocation store.
// TestSessionExpiryIsClosed продвигает локальные часы store за expiry synthetic cookie и требует отказа Open без живой browser session и revocation store.
func TestSessionExpiryIsClosed(t *testing.T) {
	directory := t.TempDir()
	key := filepath.Join(directory, "key.hex")
	writeKey(t, key, strings.Repeat("33", 32))
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	store, err := New(Config{CurrentKeyFile: key, TTL: 2 * time.Minute})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	store.now = func() time.Time { return now }
	_, encoded, _, err := store.Issue(uuid.NewString(), uuid.NewString(), uuid.NewString(), 1, "header.payload.signature", now.Add(time.Hour))
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	store.now = func() time.Time { return now.Add(3 * time.Minute) }
	if _, err := store.Open(encoded); err == nil {
		t.Fatal("expired session accepted")
	}
}

// TestSessionElevationIsSealedAndRenewalDoesNotExtendIt mutates the original purpose after sealing and checks the cookie retains its exact binding, then renews after elevation expiry and requires it to disappear. It does not consume a real secret effect.
// TestSessionElevationIsSealedAndRenewalDoesNotExtendIt меняет первоначальную purpose после seal и проверяет сохранение точной binding cookie, затем продлевает после elevation expiry и требует её исчезновения. Настоящий secret effect не потребляется.
func TestSessionElevationIsSealedAndRenewalDoesNotExtendIt(t *testing.T) {
	directory := t.TempDir()
	key := filepath.Join(directory, "key.hex")
	writeKey(t, key, strings.Repeat("44", 32))
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	store, err := New(Config{CurrentKeyFile: key, TTL: 3 * time.Minute})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	store.now = func() time.Time { return now }
	elevation := &Elevation{
		Kind: ElevationKindRuntimeSecretReveal, ScopeKind: "PROJECT", OrganizationRef: "org_fixture01", ProjectRef: "project_sales", SecretRef: "secret_main", ExpiresAt: now.Add(90 * time.Second).Unix(),
	}
	claims, encoded, _, err := store.IssueWithElevation(uuid.NewString(), uuid.NewString(), uuid.NewString(), 2, "header.payload.signature", now.Add(time.Hour), elevation)
	if err != nil {
		t.Fatalf("issue elevated session: %v", err)
	}
	elevation.SecretRef = "secret_mutated"
	opened, err := store.Open(encoded)
	if err != nil || opened.Elevation == nil || opened.Elevation.SecretRef != "secret_main" || claims.Elevation == nil {
		t.Fatalf("sealed elevation is not exact: %#v/%v", opened.Elevation, err)
	}

	store.now = func() time.Time { return now.Add(2*time.Minute + 10*time.Second) }
	renewed, renewedEncoded, changed, err := store.Renew(opened, now.Add(time.Hour))
	if err != nil || !changed || renewedEncoded == "" {
		t.Fatalf("renew after elevation expiry: changed=%t err=%v", changed, err)
	}
	if renewed.Elevation != nil {
		t.Fatalf("expired elevation survived renewal: %#v", renewed.Elevation)
	}
	if reopened, err := store.Open(renewedEncoded); err != nil || reopened.Elevation != nil {
		t.Fatalf("renewed session extended elevation: %#v/%v", reopened.Elevation, err)
	}
}

// TestSessionRenewalPreservesBindingAndBearerCeiling checks early no-op and late renewal, unchanged identity/bearer/CSRF, accepted old cookie before expiry and no renewal past the supplied bearer ceiling using a fixed clock.
// TestSessionRenewalPreservesBindingAndBearerCeiling на фиксированных часах проверяет early no-op и late renewal, неизменные identity, bearer и CSRF, допустимость old cookie до expiry и отсутствие продления за переданный bearer ceiling.
func TestSessionRenewalPreservesBindingAndBearerCeiling(t *testing.T) {
	directory := t.TempDir()
	key := filepath.Join(directory, "key.hex")
	writeKey(t, key, strings.Repeat("44", 32))
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	bearerExpiry := now.Add(time.Hour)
	store, err := New(Config{CurrentKeyFile: key, TTL: 15 * time.Minute})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	store.now = func() time.Time { return now }
	issued, encoded, csrf, err := store.Issue(uuid.NewString(), uuid.NewString(), uuid.NewString(), 9, "header.payload.signature", bearerExpiry)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	store.now = func() time.Time { return now.Add(9 * time.Minute) }
	unchanged, renewal, renewed, err := store.Renew(issued, bearerExpiry)
	if err != nil || renewed || renewal != "" || unchanged != issued {
		t.Fatalf("early renewal = %#v/%q/%t/%v", unchanged, renewal, renewed, err)
	}

	store.now = func() time.Time { return now.Add(11 * time.Minute) }
	updated, renewal, renewed, err := store.Renew(issued, bearerExpiry)
	if err != nil || !renewed || renewal == "" {
		t.Fatalf("renew = %#v/%q/%t/%v", updated, renewal, renewed, err)
	}
	if updated.Subject != issued.Subject || updated.OrganizationID != issued.OrganizationID ||
		updated.OIDCSessionID != issued.OIDCSessionID || updated.SessionRevision != issued.SessionRevision ||
		updated.SessionID != issued.SessionID || updated.Bearer != issued.Bearer || updated.CSRFHash != issued.CSRFHash {
		t.Fatalf("renewal changed session binding: before=%#v after=%#v", issued, updated)
	}
	if updated.IssuedAt != now.Add(11*time.Minute).Unix() || updated.ExpiresAt != now.Add(26*time.Minute).Unix() {
		t.Fatalf("renewal timestamps = %d/%d", updated.IssuedAt, updated.ExpiresAt)
	}
	opened, err := store.Open(renewal)
	if err != nil || opened != updated || !VerifyCSRF(opened, csrf) {
		t.Fatalf("open renewed session = %#v/%v", opened, err)
	}
	if _, err := store.Open(encoded); err != nil {
		t.Fatalf("original session stopped being valid before its expiry: %v", err)
	}

	store.now = func() time.Time { return bearerExpiry.Add(-4 * time.Minute) }
	capped := updated
	capped.IssuedAt = now.Add(41 * time.Minute).Unix()
	capped.ExpiresAt = bearerExpiry.Unix()
	unchanged, renewal, renewed, err = store.Renew(capped, bearerExpiry)
	if err != nil || renewed || renewal != "" || unchanged != capped {
		t.Fatalf("bearer ceiling renewal = %#v/%q/%t/%v", unchanged, renewal, renewed, err)
	}
}

// writeKey writes supplied synthetic hexadecimal key material with a newline and read-only mode 0400 to the test-owned path, failing the unit on filesystem error.
// writeKey записывает переданный synthetic hexadecimal key material с newline и read-only mode 0400 по принадлежащему тесту пути, останавливая unit при filesystem error.
func writeKey(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value+"\n"), 0o400); err != nil {
		t.Fatalf("write key: %v", err)
	}
}
