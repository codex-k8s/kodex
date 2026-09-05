package session

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEmailElevationSealedBindingAndKeyRotation(t *testing.T) {
	first, second := filepath.Join(t.TempDir(), "first.hex"), filepath.Join(t.TempDir(), "second.hex")
	writeKey(t, first, strings.Repeat("11", 32))
	writeKey(t, second, strings.Repeat("22", 32))
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store, err := New(Config{CurrentKeyFile: first, TTL: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now }
	expected := Elevation{Kind: ElevationKindEmailReconciliation, ReceiptRef: "erc_fixture01", ReceiptVersion: 3, ReceiptDigest: strings.Repeat("a", 64), ExpiresAt: now.Add(90 * time.Second).Unix()}
	purpose := expected
	claims, encoded, csrf, err := store.IssueWithElevation(uuid.NewString(), uuid.NewString(), uuid.NewString(), 2, "fixture-bearer", now.Add(time.Hour), &purpose)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Elevation == nil || *claims.Elevation != expected {
		t.Fatal("issued email binding changed")
	}
	purpose.ReceiptVersion = 9
	rotated, err := New(Config{CurrentKeyFile: second, PreviousKeyFile: first, TTL: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	rotated.now = func() time.Time { return now.Add(time.Minute) }
	opened, err := rotated.Open(encoded)
	if err != nil || opened.Elevation == nil || *opened.Elevation != expected || !VerifyCSRF(opened, csrf) {
		t.Fatalf("sealed email binding changed: err=%v", err)
	}
	changed := "A"
	if encoded[len(encoded)-1] == 'A' {
		changed = "B"
	}
	if _, err := rotated.Open(encoded[:len(encoded)-1] + changed); err == nil {
		t.Fatal("modified ciphertext accepted")
	}
	rotated.now = func() time.Time { return now.Add(130 * time.Second) }
	renewed, renewal, ok, err := rotated.Renew(opened, now.Add(time.Hour))
	if err != nil || !ok || renewed.Elevation != nil {
		t.Fatalf("expired elevation renewed: changed=%t err=%v", ok, err)
	}
	if final, err := rotated.Open(renewal); err != nil || final.Elevation != nil {
		t.Fatal("renewal restored email elevation")
	}
}

func TestEmailElevationRejectsInvalidBindings(t *testing.T) {
	key := filepath.Join(t.TempDir(), "key.hex")
	writeKey(t, key, strings.Repeat("33", 32))
	store, err := New(Config{CurrentKeyFile: key, TTL: 3 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	for _, tc := range []struct {
		name   string
		mutate func(*Elevation)
	}{
		{"missing-ref", func(e *Elevation) { e.ReceiptRef = "" }},
		{"bad-ref", func(e *Elevation) { e.ReceiptRef = "bad/ref" }},
		{"large-ref", func(e *Elevation) { e.ReceiptRef = strings.Repeat("a", 129) }},
		{"missing-version", func(e *Elevation) { e.ReceiptVersion = 0 }},
		{"unsafe-version", func(e *Elevation) { e.ReceiptVersion = 1 << 53 }},
		{"negative-version", func(e *Elevation) { e.ReceiptVersion = -1 }},
		{"short-digest", func(e *Elevation) { e.ReceiptDigest = "a" }},
		{"uppercase-digest", func(e *Elevation) { e.ReceiptDigest = strings.Repeat("A", 64) }},
		{"nonhex-digest", func(e *Elevation) { e.ReceiptDigest = strings.Repeat("g", 64) }},
		{"project", func(e *Elevation) { e.ProjectRef = "prj_fixture01" }},
		{"secret", func(e *Elevation) { e.SecretRef = "sec_fixture01" }},
		{"secret-purpose", func(e *Elevation) {
			e.Kind = ElevationKindRuntimeSecretReveal
			e.ProjectRef = "prj_fixture01"
			e.SecretRef = "sec_fixture01"
		}},
		{"expired", func(e *Elevation) { e.ExpiresAt = now.Unix() }},
		{"too-long", func(e *Elevation) { e.ExpiresAt = now.Add(MaximumElevationLifetime + time.Second).Unix() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := Elevation{Kind: ElevationKindEmailReconciliation, ReceiptRef: "erc_fixture01", ReceiptVersion: 3, ReceiptDigest: strings.Repeat("a", 64), ExpiresAt: now.Add(time.Minute).Unix()}
			tc.mutate(&e)
			if _, _, _, err := store.IssueWithElevation(uuid.NewString(), uuid.NewString(), uuid.NewString(), 1, "fixture-bearer", now.Add(time.Hour), &e); err == nil {
				t.Fatal("invalid elevation issued")
			}
		})
	}
	if !ValidEmailReceiptBinding("erc_fixture01", maximumReceiptVersion, strings.Repeat("f", 64)) {
		t.Fatal("safe maximum version rejected")
	}
}
