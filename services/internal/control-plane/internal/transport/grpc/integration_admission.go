package grpc

import (
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// castIntegrationInvocation preserves the owner outcome and rejects malformed admission metadata.
// Legacy marker absence stays unspecified. A block never upgrades state or invents an effect receipt.
// castIntegrationInvocation сохраняет owner outcome и отклоняет повреждённые admission metadata.
// Legacy absence остаётся unspecified; block не повышает state и не создаёт effect receipt.
func castIntegrationInvocation(result map[string]any) (*cp.GetIntegrationInvocationResponse, error) {
	blocked, reasonText := false, ""
	if raw, exists := result["admissionBlocked"]; exists {
		var valid bool
		blocked, valid = raw.(bool)
		if !valid {
			return nil, transportError(errs.ErrUnavailable)
		}
	}
	if raw, exists := result["admissionBlockReason"]; exists {
		var valid bool
		reasonText, valid = raw.(string)
		if !valid {
			return nil, transportError(errs.ErrUnavailable)
		}
	}
	reason := cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_UNSPECIFIED
	if blocked {
		if reasonText != entity.IntegrationAdmissionCurrentDenied || mapString(result, "effectReceiptRef") != "" {
			return nil, transportError(errs.ErrUnavailable)
		}
		switch mapString(result, "state") {
		case "READY", "UNKNOWN_OUTCOME", "FAILED", "CANCELLED":
		default:
			return nil, transportError(errs.ErrUnavailable)
		}
		reason = cp.SSHComposeAdmissionBlockReason_SSH_COMPOSE_ADMISSION_BLOCK_REASON_CURRENT_ADMISSION_DENIED
	} else if reasonText != "" {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &cp.GetIntegrationInvocationResponse{State: mapString(result, "state"), ResultSummary: mapString(result, "resultSummary"), SafeErrorCode: mapString(result, "safeErrorCode"), GateRef: mapString(result, "gateRef"), EffectReceiptRef: mapString(result, "effectReceiptRef"), AdmissionBlocked: blocked, AdmissionBlockReason: reason}, nil
}
