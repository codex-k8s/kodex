package mattermostevent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

type countingFence struct{ admissions int }

func (fence *countingFence) AdmitMattermostEventKeyset(
	context.Context, string, uint64, uint64, uint64, string, []int64, []int64,
) error {
	fence.admissions++
	return nil
}

func TestParsePublicKeysetClosedLifecycle(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	keyset := publicKeySet{Version: 1, Revision: 7, HighWatermark: 2, ServedGeneration: 2,
		Keys: []publicKeyRef{
			{Generation: 1, Status: "PREVIOUS", AcceptUntil: now.Add(time.Minute).Unix(), JWK: testPublicJWK(t, "g1")},
			{Generation: 2, Status: "CURRENT", JWK: testPublicJWK(t, "g2")},
			{Generation: 3, Status: "NEXT", JWK: testPublicJWK(t, "g3")},
		}}
	raw, err := internalrpcauth.CanonicalJSON(keyset)
	if err != nil {
		t.Fatalf("encode keyset: %v", err)
	}
	keys, state, retired, active, err := parsePublicKeyset(Config{MaximumTTL: 2 * time.Minute}, raw, now)
	if err != nil {
		t.Fatalf("parse valid keyset: %v", err)
	}
	if len(keys) != 2 || keys[1].key.KeyID != "g1" || keys[2].key.KeyID != "g2" ||
		state.revision != 7 || state.highWatermark != 2 || state.servedGeneration != 2 || len(retired) != 0 ||
		!slices.Equal(active, []int64{1, 2, 3}) {
		t.Fatal("served keyset projection mismatch")
	}

	keyset.Keys[2].Generation = 2
	raw, err = internalrpcauth.CanonicalJSON(keyset)
	if err != nil {
		t.Fatalf("encode duplicate keyset: %v", err)
	}
	if _, _, _, _, err := parsePublicKeyset(Config{MaximumTTL: 2 * time.Minute}, raw, now); err == nil {
		t.Fatal("duplicate generation accepted")
	}

	keyset.Keys = []publicKeyRef{
		{Generation: 1, Status: "PREVIOUS", AcceptUntil: now.Unix(), JWK: testPublicJWK(t, "expired-g1")},
		{Generation: 2, Status: "CURRENT", JWK: testPublicJWK(t, "current-g2")},
	}
	raw, err = internalrpcauth.CanonicalJSON(keyset)
	if err != nil {
		t.Fatalf("encode expired keyset: %v", err)
	}
	if _, _, _, _, err := parsePublicKeyset(Config{MaximumTTL: 2 * time.Minute}, raw, now); err == nil {
		t.Fatal("expired PREVIOUS key accepted")
	}
}

func TestRefreshReadsDurableFenceForUnchangedKeyset(t *testing.T) {
	keyset := publicKeySet{Version: 1, Revision: 1, HighWatermark: 1, ServedGeneration: 1,
		Keys: []publicKeyRef{{Generation: 1, Status: "CURRENT", JWK: testPublicJWK(t, "current-g1")}}}
	raw, err := internalrpcauth.CanonicalJSON(keyset)
	if err != nil {
		t.Fatalf("encode keyset: %v", err)
	}
	path := filepath.Join(t.TempDir(), "public-keyset.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write keyset: %v", err)
	}
	fence := &countingFence{}
	verifier, err := New(context.Background(), Config{
		ProducerID: "control-plane.interaction-gateway", Purpose: "MATTERMOST_SIGNED_EVENT",
		Issuer: "https://interaction.test/events", Audience: "urn:test:mattermost-event",
		WorkloadID: "interaction-gateway",
		CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
		PublicKeysetFile: path, MaximumTTL: time.Minute,
	}, fence)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	if err := verifier.refresh(context.Background()); err != nil {
		t.Fatalf("refresh unchanged keyset: %v", err)
	}
	if fence.admissions != 2 {
		t.Fatalf("durable fence admissions = %d, want 2", fence.admissions)
	}
}

func testPublicJWK(t *testing.T, keyID string) json.RawMessage {
	t.Helper()
	key, err := internalrpcauth.GenerateES256Key(keyID)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	raw, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	return raw
}
