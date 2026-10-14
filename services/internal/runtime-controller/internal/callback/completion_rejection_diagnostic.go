package callback

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
)

const (
	completionRejectionLog             = "runtime completion rejected"
	completionStageKey                 = "completion_stage"
	completionReasonKey                = "completion_reason"
	completionCodeKey                  = "grpc_code"
	completionPayloadStage             = "PAYLOAD"
	completionOwnerRPCStage            = "OWNER_RPC"
	completionDecodeReason             = "DECODE"
	completionValidateReason           = "VALIDATE"
	completionProvenanceReason         = "PROVENANCE"
	completionProviderDiagnosticReason = "PROVIDER_DIAGNOSTIC"
	completionRPCRejectedReason        = "RPC_REJECTED"
)

// Tuple берётся только после authorize из server-owned workload mapping.
// Ни payload, ни текст/details удалённой ошибки не передаются в logger.
func (server *Server) logCompletionRejection(ctx context.Context, input runtimecontract.RunnerInput, stage, reason string, code codes.Code) {
	if server.logger == nil {
		return
	}
	switch stage {
	case completionPayloadStage:
		switch reason {
		case completionDecodeReason, completionValidateReason, completionProvenanceReason, completionProviderDiagnosticReason:
		default:
			return
		}
		code = codes.InvalidArgument
	case completionOwnerRPCStage:
		if reason != completionRPCRejectedReason {
			return
		}
	default:
		return
	}
	// Code.String для неизвестного numeric code содержит произвольное число;
	// закрытая проекция оставляет только канонические gRPC-коды отказа.
	if code < codes.Canceled || code > codes.Unauthenticated {
		code = codes.Unknown
	}
	server.logger.WarnContext(ctx, completionRejectionLog,
		completionStageKey, stage, completionReasonKey, reason, completionCodeKey, code.String(),
		"run_ref", input.RunRef, "node_ref", input.NodeRef, "session_ref", input.SessionRef,
		"turn_ref", input.TurnRef, "attempt", input.Attempt,
		"runtime_revision_digest", input.RuntimeRevisionDigest, "input_digest", input.InputDigest,
		"execution_binding_digest", input.ExecutionBindingDigest)
}
