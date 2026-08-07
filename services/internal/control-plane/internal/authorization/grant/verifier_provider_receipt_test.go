package grant

import (
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
)

func TestProviderReceiptAudienceIsExactForMattermostAndAI(t *testing.T) {
	now := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		purpose, audience, workload string
	}{
		{"MATTERMOST_PROVIDER_READBACK_RECEIPT", "urn:mattercodex:provider-readback:mattermost", "interaction-gateway"},
		{"AI_PROVIDER_READBACK_RECEIPT", "urn:mattercodex:provider-readback:ai", "integration-gateway"},
	}
	for _, test := range tests {
		t.Run(test.purpose, func(t *testing.T) {
			verifier := &Verifier{config: Config{
				ProducerID: "provider-receipt", Purpose: test.purpose, Issuer: "https://issuer.example",
				Audience: test.audience, WorkloadID: test.workload,
				CallerSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/" + test.workload,
			}, now: func() time.Time { return now }}
			claims := validProviderReceiptClaims(verifier.config, now)
			canonical, err := internalrpcauth.CanonicalJSON(claims)
			if err != nil {
				t.Fatal(err)
			}
			first, err := verifier.authenticateProviderReceipt(canonical)
			if err != nil {
				t.Fatalf("valid audience rejected: %v", err)
			}
			// Повторная криптографическая проверка сохраняет тот же JTI; one-use
			// replay fence получает стабильную identity и может отклонить повтор.
			replay, err := verifier.authenticateProviderReceipt(canonical)
			if err != nil || replay.SessionJTI != first.SessionJTI || replay.CredentialDigest != first.CredentialDigest {
				t.Fatalf("replay identity is not stable: first=%#v replay=%#v err=%v", first, replay, err)
			}
			for _, invalidAudience := range []string{"", test.audience + ":other"} {
				claims.Audience = invalidAudience
				invalid, encodeErr := internalrpcauth.CanonicalJSON(claims)
				if encodeErr != nil {
					t.Fatal(encodeErr)
				}
				if _, err := verifier.authenticateProviderReceipt(invalid); !errors.Is(err, errs.ErrUnauthenticated) {
					t.Fatalf("audience %q was accepted: %v", invalidAudience, err)
				}
			}
		})
	}
}

func validProviderReceiptClaims(config Config, now time.Time) providerReceiptClaims {
	return providerReceiptClaims{
		ContractVersion: 1, Issuer: config.Issuer, Audience: config.Audience, Purpose: config.Purpose,
		WorkloadID: config.WorkloadID, CallerSPIFFEID: config.CallerSPIFFEID,
		FullMethod: "/controlplane.v1.ControlPlaneService/ManageWorkspaceMattermostMapping",
		ActorID:    "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", OrganizationID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		ProjectID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", WorkspaceID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc",
		Action: "bind", Effect: "workspace_mattermost_mapping", EffectVersion: 1, EffectGeneration: 1,
		EffectSHA256: "1111111111111111111111111111111111111111111111111111111111111111",
		ReceiptID:    "dddddddd-dddd-4ddd-8ddd-dddddddddddd", ReceiptRevision: 1,
		IssuedAt: now, NotBefore: now, ExpiresAt: now.Add(time.Minute), MaskedStatus: "active", Eligible: true,
		TargetKind: "workspace_mattermost_mapping", TargetStableKey: "workspace-cccccccccccc4ccc8ccccccccccccccc",
		CommandIntentSHA256: "2222222222222222222222222222222222222222222222222222222222222222",
	}
}
