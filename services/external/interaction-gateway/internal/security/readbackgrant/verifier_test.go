package readbackgrant

import (
	"context"
	"os"
	"path/filepath"
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
	publicFile := filepath.Join(t.TempDir(), "public.jwk")
	if err := os.WriteFile(publicFile, publicRaw, 0o600); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	config := Config{Issuer: "https://control-plane.test/readback", Audience: "urn:test:readback",
		ProducerID: "control-plane.interaction-delivery-readback", Purpose: "INTERACTION_DELIVERY_READBACK_GRANT",
		Operation: "interaction.delivery.read", Permission: "interaction.delivery.read", PublicJWKFile: publicFile,
		Generation: 1, MaximumTTL: 5 * time.Minute}
	verifier, err := New(config)
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
	if _, err := verifier.Verify(context.Background(), sign(base)); err != nil {
		t.Fatalf("verify exact credential: %v", err)
	}
	base.ProducerID = "control-plane.other-producer"
	if _, err := verifier.Verify(context.Background(), sign(base)); err == nil {
		t.Fatal("credential from another producer accepted")
	}
}
