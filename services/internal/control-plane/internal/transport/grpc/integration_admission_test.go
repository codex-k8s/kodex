package grpc

import (
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestCastIntegrationInvocationAdmissionPreservesOutcome covers legacy absence and blocked states
// at the real owner-to-Proto caster. Corrupt metadata is unavailable rather than reclassified.
// TestCastIntegrationInvocationAdmissionPreservesOutcome проверяет legacy absence и blocked states
// в настоящем owner-to-Proto caster. Повреждённые metadata дают unavailable без смены outcome.
func TestCastIntegrationInvocationAdmissionPreservesOutcome(t *testing.T) {
	legacy, err := castIntegrationInvocation(map[string]any{"state": "SUCCEEDED", "effectReceiptRef": "erc_fixture"})
	if err != nil || legacy.GetAdmissionBlocked() || legacy.GetAdmissionBlockReason() != cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_UNSPECIFIED || legacy.GetEffectReceiptRef() != "erc_fixture" {
		t.Fatal("legacy outcome changed")
	}
	for _, state := range []string{"READY", "UNKNOWN_OUTCOME", "FAILED", "CANCELLED"} {
		got, err := castIntegrationInvocation(map[string]any{"state": state, "safeErrorCode": "INTEGRATION_OUTCOME_UNKNOWN", "admissionBlocked": true, "admissionBlockReason": entity.IntegrationAdmissionCurrentDenied})
		if err != nil || got.GetState() != state || got.GetSafeErrorCode() != "INTEGRATION_OUTCOME_UNKNOWN" || !got.GetAdmissionBlocked() || got.GetAdmissionBlockReason() != cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_CURRENT_ADMISSION_DENIED || got.GetEffectReceiptRef() != "" {
			t.Fatal("blocked projection changed outcome")
		}
	}
	for _, values := range []map[string]any{
		{"state": "READY", "admissionBlocked": "true"},
		{"state": "READY", "admissionBlockReason": 12},
		{"state": "READY", "admissionBlocked": true},
		{"state": "READY", "admissionBlockReason": entity.IntegrationAdmissionCurrentDenied},
		{"state": "READY", "admissionBlocked": true, "admissionBlockReason": "private cause"},
		{"state": "SUCCEEDED", "admissionBlocked": true, "admissionBlockReason": entity.IntegrationAdmissionCurrentDenied},
		{"state": "UNKNOWN_OUTCOME", "admissionBlocked": true, "admissionBlockReason": entity.IntegrationAdmissionCurrentDenied, "effectReceiptRef": "erc_fixture"},
	} {
		if result, err := castIntegrationInvocation(values); result != nil || status.Code(err) != codes.Unavailable {
			t.Fatal("invalid admission projection did not fail closed")
		}
	}
}
