package value

import (
	"strings"
	"testing"
	"time"
)

func TestProviderEffectReceiptRequiresExactBoundedTuple(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	receipt := ProviderEffectReceipt{
		ContractVersion:     1,
		Issuer:              "mattercodex-interaction-gateway-provider-readback",
		Purpose:             "MATTERMOST_PROVIDER_READBACK_RECEIPT",
		WorkloadID:          "interaction-gateway",
		CallerSPIFFEID:      "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
		FullMethod:          "/controlplane.v1.ControlPlaneService/ManageWorkspaceMattermostMapping",
		ActorID:             "11111111-1111-4111-8111-111111111111",
		OrganizationID:      "22222222-2222-4222-8222-222222222222",
		ProjectID:           "33333333-3333-4333-8333-333333333333",
		WorkspaceID:         "33333333-3333-4333-8333-333333333333",
		ProviderTeamRef:     "team/server-owned",
		Action:              "bind",
		Effect:              "workspace_mattermost_mapping",
		EffectVersion:       7,
		EffectGeneration:    9,
		EffectSHA256:        strings.Repeat("a", 64),
		ReceiptID:           "44444444-4444-4444-8444-444444444444",
		ReceiptRevision:     11,
		IssuedAt:            now.Add(-time.Minute),
		NotBefore:           now.Add(-time.Minute),
		ExpiresAt:           now.Add(time.Minute),
		MaskedStatus:        "AVAILABLE",
		TargetKind:          "workspace_mattermost_mapping",
		TargetResourceID:    "33333333-3333-4333-8333-333333333333",
		TargetStableKey:     "workspace-33333333333343338333333333333333",
		CommandIntentSHA256: strings.Repeat("b", 64),
	}
	if err := receipt.Validate(now); err != nil {
		t.Fatalf("valid provider receipt: %v", err)
	}

	expired := receipt
	expired.ExpiresAt = now.Add(-6 * time.Second)
	if err := expired.Validate(now); err == nil {
		t.Fatal("expired provider receipt was accepted")
	}

	futureIssued := receipt
	futureIssued.IssuedAt = now.Add(6 * time.Second)
	futureIssued.NotBefore = futureIssued.IssuedAt
	if err := futureIssued.Validate(now); err == nil {
		t.Fatal("future-issued provider receipt was accepted")
	}

	partialCredential := receipt
	partialCredential.CredentialBindingID = "55555555-5555-4555-8555-555555555555"
	if err := partialCredential.Validate(now); err == nil {
		t.Fatal("partial credential binding was accepted")
	}

	duplicatedCapability := receipt
	duplicatedCapability.Capabilities = []string{"chat_read", "chat_read"}
	if err := duplicatedCapability.Validate(now); err == nil {
		t.Fatal("duplicated receipt capability was accepted")
	}
}
