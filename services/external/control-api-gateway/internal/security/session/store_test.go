package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

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

func writeKey(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value+"\n"), 0o400); err != nil {
		t.Fatalf("write key: %v", err)
	}
}
