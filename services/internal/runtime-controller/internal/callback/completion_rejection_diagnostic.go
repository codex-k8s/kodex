package callback

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
)

const (
	// completionRejectionLog identifies rejected completion diagnostics, not a
	// committed terminal transition or a successful cleanup.
	// completionRejectionLog обозначает диагностику отвергнутого завершения,
	// а не подтверждённый terminal-переход или успешную очистку.
	completionRejectionLog = "runtime completion rejected"
	// completionStageKey names the bounded payload-versus-owner-RPC log dimension.
	// completionStageKey именует закрытый признак стадии payload либо owner RPC в логе.
	completionStageKey = "completion_stage"
	// completionReasonKey names a closed rejection reason, never raw error details.
	// completionReasonKey именует закрытую причину отказа, не сырые подробности ошибки.
	completionReasonKey = "completion_reason"
	// completionCodeKey names the normalized canonical gRPC rejection code.
	// completionCodeKey именует нормализованный канонический gRPC-код отказа.
	completionCodeKey = "grpc_code"
	// completionPayloadStage classifies local decoding, validation or binding
	// rejection and forces InvalidArgument regardless of a supplied gRPC code.
	// completionPayloadStage обозначает локальный отказ декодирования, проверки
	// или привязки и задаёт InvalidArgument независимо от переданного gRPC-кода.
	completionPayloadStage = "PAYLOAD"
	// completionOwnerRPCStage classifies a rejecting owner RPC without asserting
	// that its remote state changed or exposing remote error text.
	// completionOwnerRPCStage обозначает отказ owner RPC, не утверждая изменение
	// удалённого состояния и не раскрывая текст удалённой ошибки.
	completionOwnerRPCStage = "OWNER_RPC"
	// completionDecodeReason identifies a payload rejected by the decoder.
	// completionDecodeReason обозначает payload, отвергнутый декодером.
	completionDecodeReason = "DECODE"
	// completionValidateReason identifies a decoded payload rejected semantically.
	// completionValidateReason обозначает смысловой отказ для декодированного payload.
	completionValidateReason = "VALIDATE"
	// completionProvenanceReason identifies a mismatch with the trusted execution.
	// completionProvenanceReason обозначает несовпадение с доверенным выполнением.
	completionProvenanceReason = "PROVENANCE"
	// completionProviderDiagnosticReason identifies a rejected provider diagnostic
	// binding, separately from the provider's reported failure itself.
	// completionProviderDiagnosticReason обозначает отказ привязки диагностики
	// провайдера, отдельно от самой сообщённой ошибки провайдера.
	completionProviderDiagnosticReason = "PROVIDER_DIAGNOSTIC"
	// completionRPCRejectedReason is the only reason accepted for the owner-RPC stage.
	// completionRPCRejectedReason является единственной причиной стадии owner RPC.
	completionRPCRejectedReason = "RPC_REJECTED"
)

// logCompletionRejection emits only allowed stage/reason combinations, normalized
// gRPC codes and execution pins already resolved by authorization. It never logs
// the payload or remote error text and performs no completion or cleanup itself.
// logCompletionRejection пишет только допустимые пары стадии и причины,
// нормализованные gRPC-коды и привязки, уже полученные при авторизации. Payload
// и удалённая ошибка не логируются; завершение и очистка здесь не выполняются.
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
