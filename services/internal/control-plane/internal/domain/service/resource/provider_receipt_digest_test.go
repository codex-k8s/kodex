package resource

import (
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

func TestProviderReceiptAuthorityDigestUsesExactMattermostSignedProjection(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 18, 9, 0, 0, 0, time.UTC)
	receipt := value.ProviderEffectReceipt{
		ContractVersion: 1, Issuer: "https://interaction-gateway.example.invalid",
		Audience: "urn:mattercodex:provider-readback:mattermost", Purpose: mattermostProviderReceiptPurpose,
		WorkloadID: "interaction-gateway", CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
		FullMethod: "/controlplane.v1.ControlPlaneService/ManageWorkspaceMattermostMapping",
		ActorID:    "11111111-1111-4111-8111-111111111111", OrganizationID: "22222222-2222-4222-8222-222222222222",
		ProjectID: "33333333-3333-4333-8333-333333333333", WorkspaceID: "33333333-3333-4333-8333-333333333333",
		ProviderTeamRef: "team-one", ProviderObjectRef: "team-one", Action: "bind",
		Effect: "workspace_mattermost_mapping", EffectVersion: 7, EffectGeneration: 7,
		EffectSHA256: strings.Repeat("a", 64), ReceiptID: "44444444-4444-4444-8444-444444444444",
		ReceiptRevision: 7, IssuedAt: now, NotBefore: now, ExpiresAt: now.Add(time.Minute),
		MaskedStatus: "active", Eligible: true, TargetKind: "workspace_mattermost_mapping",
		TargetResourceID: "33333333-3333-4333-8333-333333333333",
		TargetStableKey:  "workspace-33333333333343338333333333333333", CommandIntentSHA256: strings.Repeat("b", 64),
	}

	got, err := providerReceiptAuthorityDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	want, err := internalrpcauth.CanonicalJSONSHA256(mattermostProviderReceiptClaims{
		ContractVersion: receipt.ContractVersion, Issuer: receipt.Issuer, Audience: receipt.Audience,
		Purpose: receipt.Purpose, WorkloadID: receipt.WorkloadID, CallerSPIFFEID: receipt.CallerSPIFFEID,
		FullMethod: receipt.FullMethod, ActorID: receipt.ActorID, OrganizationID: receipt.OrganizationID,
		ProjectID: receipt.ProjectID, WorkspaceID: receipt.WorkspaceID, ProviderTeamRef: receipt.ProviderTeamRef,
		ProviderObjectRef: receipt.ProviderObjectRef, Action: receipt.Action, Effect: receipt.Effect,
		EffectVersion: receipt.EffectVersion, EffectGeneration: receipt.EffectGeneration,
		EffectSHA256: receipt.EffectSHA256, ReceiptID: receipt.ReceiptID, ReceiptRevision: receipt.ReceiptRevision,
		IssuedAt: receipt.IssuedAt, NotBefore: receipt.NotBefore, ExpiresAt: receipt.ExpiresAt,
		MaskedStatus: receipt.MaskedStatus, Eligible: receipt.Eligible, TargetKind: receipt.TargetKind,
		TargetResourceID: receipt.TargetResourceID, TargetStableKey: receipt.TargetStableKey,
		CommandIntentSHA256: receipt.CommandIntentSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	commonDigest, err := internalrpcauth.CanonicalJSONSHA256(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Mattermost receipt digest mismatch: got %s want %s", got, want)
	}
	if got == commonDigest {
		t.Fatal("Mattermost receipt digest unexpectedly includes unrelated AI timestamp fields")
	}
}

func TestProviderReceiptAuthorityDigestKeepsFullAIProjection(t *testing.T) {
	t.Parallel()

	receipt := value.ProviderEffectReceipt{Purpose: aiProviderReceiptPurpose}
	got, err := providerReceiptAuthorityDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	want, err := internalrpcauth.CanonicalJSONSHA256(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("AI receipt digest mismatch: got %s want %s", got, want)
	}
}

func TestProviderReceiptAuthorityDigestRejectsUnknownPurpose(t *testing.T) {
	t.Parallel()

	if _, err := providerReceiptAuthorityDigest(value.ProviderEffectReceipt{Purpose: "UNKNOWN"}); err == nil {
		t.Fatal("unknown provider receipt purpose was accepted")
	}
}
