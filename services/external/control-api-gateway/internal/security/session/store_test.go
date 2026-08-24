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

func writeKey(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value+"\n"), 0o400); err != nil {
		t.Fatalf("write key: %v", err)
	}
}
