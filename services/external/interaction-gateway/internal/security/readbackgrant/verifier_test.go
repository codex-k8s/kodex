package readbackgrant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

func TestVerifierBindsProducerWorkloadAndDeliveryScope(t *testing.T) {
	key, err := internalrpcauth.GenerateES256Key("readback-g1")
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicRaw, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	keyset, err := internalrpcauth.CanonicalJSON(map[string]any{"version": 1, "revision": 1,
		"high_watermark": 1, "served_generation": 1, "keys": []map[string]any{{
			"generation": 1, "status": "CURRENT", "jwk": json.RawMessage(publicRaw),
		}}})
	if err != nil {
		t.Fatalf("marshal public keyset: %v", err)
	}
	publicFile := filepath.Join(t.TempDir(), "public-keyset.json")
	if err := os.WriteFile(publicFile, keyset, 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	config := Config{Issuer: "https://control-plane.test/readback", Audience: "urn:test:readback",
		ProducerID: "control-plane.interaction-delivery-readback", Purpose: "INTERACTION_DELIVERY_READBACK_GRANT",
		Operation: "interaction.delivery.read", Permission: "interaction.delivery.read", PublicKeysetFile: publicFile,
		Generation: 1, MaximumTTL: 5 * time.Minute}
	fence := &memoryFence{}
	verifier, err := New(context.Background(), config, fence)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	verifier.now = func() time.Time { return now }
	base := Claims{Version: 1, Issuer: config.Issuer, Audience: config.Audience,
		Subject: "10000000-0000-4000-8000-000000000001", ProducerID: config.ProducerID, Purpose: config.Purpose,
		WorkloadID: "control-plane", CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		Operation: config.Operation, Permission: config.Permission,
		OrganizationID: "10000000-0000-4000-8000-000000000002",
		ProjectID:      "10000000-0000-4000-8000-000000000003", DeliveryID: "10000000-0000-4000-8000-000000000004",
		Generation: 1, JTI: "10000000-0000-4000-8000-000000000005",
		IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()}
	sign := func(value Claims) string {
		compact, signErr := internalrpcauth.SignCanonicalJSON(value, key,
			internalrpcauth.ProtectedHeaderExpectation{Type: credentialType, KeyID: key.KeyID})
		if signErr != nil {
			t.Fatalf("sign claims: %v", signErr)
		}
		return "Bearer " + compact
	}
	credential := sign(base)
	verified, err := verifier.Verify(context.Background(), credential)
	if err != nil {
		t.Fatalf("verify exact credential: %v", err)
	}
	digest := sha256.Sum256([]byte(strings.TrimPrefix(credential, "Bearer ")))
	if verified.CredentialSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("verified credential digest does not bind durable issuer receipt")
	}
	if fence.revision != 1 || fence.highWatermark != 1 || fence.generation != 1 || len(fence.identities) != 1 {
		t.Fatal("durable keyset fence did not receive exact served identity")
	}
	base.ProducerID = "control-plane.other-producer"
	if _, err := verifier.Verify(context.Background(), sign(base)); err == nil {
		t.Fatal("credential from another producer accepted")
	}
}

type memoryFence struct {
	revision, highWatermark, generation uint64
	digest                              string
	identities                          []KeyIdentity
}

func (fence *memoryFence) AdmitDeliveryReadbackKeyset(_ context.Context, revision, highWatermark,
	generation uint64, digest string, identities []KeyIdentity) error {
	if fence.revision > revision || fence.highWatermark > highWatermark ||
		(fence.revision == revision && fence.digest != "" && fence.digest != digest) {
		return errors.New("rollback")
	}
	fence.revision, fence.highWatermark, fence.generation, fence.digest = revision, highWatermark, generation, digest
	fence.identities = slices.Clone(identities)
	return nil
}
