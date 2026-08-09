package providerreceipt

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	domaincontrol "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/controlplane"
)

func TestSignerProducesVersionedAgentReceiptWithoutSecretValue(t *testing.T) {
	key, err := internalrpcauth.GenerateES256Key("provider-readback-agent-g1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private.jwk")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := New(Config{Issuer: "https://interaction-gateway.example.invalid",
		PrivateJWKFile: path, MaximumTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := signer.Sign(domaincontrol.ProviderEffectReceipt{
		FullMethod: "/controlplane.v1.ControlPlaneService/ManageAgentMattermostBotIdentity",
		ActorID:    "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", OrganizationID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ProjectID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", WorkspaceID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		ProviderTeamRef: "provider-team", ProviderObjectRef: "dddddddd-dddd-4ddd-8ddd-dddddddddddd",
		ProviderUsername: "agent-bot", Action: "rebind", Effect: "agent_bot_identity",
		EffectVersion: 2, EffectGeneration: 3,
		EffectSHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		ReceiptID:    "ffffffff-ffff-4fff-8fff-ffffffffffff", ReceiptRevision: 3,
		CredentialBindingID: "11111111-1111-4111-8111-111111111111", CredentialBindingVersion: 4,
		CredentialBindingSHA256: "2222222222222222222222222222222222222222222222222222222222222222",
		Provider:                "mattermost", MaskedLabel: "Agent bot", MaskedStatus: "AVAILABLE", Eligible: true,
		Capabilities: []string{"mattermost.post", "mattermost.readback"},
		TargetKind:   "agent_bot_identity", TargetResourceID: "33333333-3333-4333-8333-333333333333",
		TargetStableKey:     "agent-primary",
		CommandIntentSHA256: "4444444444444444444444444444444444444444444444444444444444444444",
	})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(credential.CompactJWS, key.PublicOnly(),
		internalrpcauth.ProtectedHeaderExpectation{Type: credentialType, KeyID: key.KeyID})
	if err != nil {
		t.Fatal(err)
	}
	var payload claims
	if err := json.Unmarshal(verified.CanonicalPayload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.ContractVersion != 1 || payload.TargetKind != "agent_bot_identity" ||
		payload.EffectGeneration != 3 || payload.CredentialBindingVersion != 4 ||
		payload.CommandIntentSHA256 != "4444444444444444444444444444444444444444444444444444444444444444" {
		t.Fatalf("Agent receipt exact fields mismatch: %#v", payload)
	}
	if bytes.Contains(verified.CanonicalPayload, []byte("secret-token-value")) {
		t.Fatal("Agent receipt contains secret value")
	}
}

func TestSignerProducesCanonicalExactBoundReceipt(t *testing.T) {
	key, err := internalrpcauth.GenerateES256Key("provider-readback-g1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private.jwk")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := New(Config{
		Issuer:         "https://interaction-gateway.mattercodex-system.svc.cluster.local/authority/provider-readback",
		PrivateJWKFile: path, MaximumTTL: 2 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	signer.now = func() time.Time { return now }
	credential, err := signer.Sign(domaincontrol.ProviderEffectReceipt{
		FullMethod:      "/controlplane.v1.ControlPlaneService/ManageWorkspaceMattermostMapping",
		ActorID:         "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		OrganizationID:  "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ProjectID:       "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		WorkspaceID:     "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		ProviderTeamRef: "provider-team-one", ProviderObjectRef: "provider-team-one",
		Action: "bind", Effect: "workspace_mattermost_mapping", EffectVersion: 7, EffectGeneration: 7,
		EffectSHA256: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		ReceiptID:    "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee", ReceiptRevision: 7,
		MaskedStatus: "active", Eligible: true, TargetKind: "workspace_mattermost_mapping",
		TargetStableKey:     "workspace-cccccccccccc4ccc8ccccccccccccccc",
		CommandIntentSHA256: "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	})
	if err != nil {
		t.Fatal(err)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(credential.CompactJWS, key.PublicOnly(),
		internalrpcauth.ProtectedHeaderExpectation{Type: credentialType, KeyID: key.KeyID})
	if err != nil {
		t.Fatal(err)
	}
	var payload claims
	if err := json.Unmarshal(verified.CanonicalPayload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Purpose != Purpose || payload.Audience != Audience || payload.WorkloadID != WorkloadID || payload.CallerSPIFFEID != CallerSPIFFEID ||
		payload.FullMethod != credential.Receipt.FullMethod || payload.ReceiptRevision != 7 ||
		!payload.ExpiresAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("receipt binding mismatch: %#v", payload)
	}
	wantDigest, err := internalrpcauth.CanonicalJSONSHA256(payload)
	if err != nil {
		t.Fatal(err)
	}
	gotDigest, err := internalrpcauth.CanonicalJSONSHA256(credential.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("receipt authority digest mismatch: got %s want %s", gotDigest, wantDigest)
	}
}

func TestSignerRejectsUnboundTarget(t *testing.T) {
	key, err := internalrpcauth.GenerateES256Key("provider-readback-g1")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private.jwk")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := New(Config{
		Issuer:         "https://interaction-gateway.example.invalid",
		PrivateJWKFile: path, MaximumTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Sign(domaincontrol.ProviderEffectReceipt{}); err == nil {
		t.Fatal("unbound provider receipt was signed")
	}
}
