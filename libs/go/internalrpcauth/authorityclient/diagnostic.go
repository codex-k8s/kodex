package authorityclient

import (
	"errors"
	"fmt"

	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DiagnosticStage обозначает локальную стадию, а не полномочие или результат RPC.
type DiagnosticStage string

const (
	StageUnknown           DiagnosticStage = "unknown"
	StageProofOperation    DiagnosticStage = "proof_operation"
	StageGrantMissing      DiagnosticStage = "grant_missing"
	StageGrantRead         DiagnosticStage = "grant_read"
	StageProofResolve      DiagnosticStage = "proof_resolve"
	StageProofResponse     DiagnosticStage = "proof_response"
	StageContextIssue      DiagnosticStage = "context_issue"
	StageContinuationIssue DiagnosticStage = "continuation_issue"
)

// Diagnostic содержит только закрытые классы. Удостоверения, сообщения внешних
// ошибок, correlation и protobuf unknown fields сюда не копируются.
type Diagnostic struct {
	Stage          DiagnosticStage
	Code           codes.Code
	Reason         api.AuthorizationErrorReason
	AuthorityStage api.AuthorizationFailureStage
}

func (value Diagnostic) String() string {
	value = normalizeDiagnostic(value)
	return fmt.Sprintf("authority_stage=%s dependency_code=%s authority_reason=%s authority_failure_stage=%s", value.Stage, value.Code, value.Reason, value.AuthorityStage)
}

// Diagnostic возвращает классификацию для локальной process boundary.
// GRPCStatus намеренно сохраняет прежний публичный status без этих подробностей.
func (failure *LocalAuthorityError) Diagnostic() Diagnostic {
	return normalizeDiagnostic(failure.diagnostic)
}

type proofFailure struct{ diagnostic Diagnostic }

func (failure *proofFailure) Error() string {
	return "authority proof provider failed [" + failure.diagnostic.String() + "]"
}
func (failure *proofFailure) GRPCStatus() *status.Status {
	return status.New(failure.diagnostic.Code, "authority proof provider failed")
}

// NewProofFailure сохраняет стадию provider без исходной ошибки. Исходный code
// сохраняется для прежней bounded retry policy; новый retry здесь не создаётся.
func NewProofFailure(stage DiagnosticStage, cause error) error {
	return &proofFailure{diagnostic: diagnosticFrom(cause, stage)}
}

func diagnosticFrom(cause error, stage DiagnosticStage) Diagnostic {
	var provider *proofFailure
	if errors.As(cause, &provider) {
		return normalizeDiagnostic(provider.diagnostic)
	}
	value := Diagnostic{Stage: stage, Code: authorityFailureCode(cause)}
	// Только issuer/continuation возвращает утверждённый AuthorizationErrorDetail.
	// Любые details resolver, произвольные сообщения и correlation игнорируются.
	if stage == StageContextIssue || stage == StageContinuationIssue {
		if parsed, ok := status.FromError(cause); ok {
			details := parsed.Details()
			if len(details) == 1 {
				if detail, ok := details[0].(*api.AuthorizationErrorDetail); ok && len(detail.ProtoReflect().GetUnknown()) == 0 {
					value.Reason, value.AuthorityStage = detail.GetReason(), detail.GetStage()
				}
			}
		}
	}
	return normalizeDiagnostic(value)
}

func normalizeDiagnostic(value Diagnostic) Diagnostic {
	switch value.Stage {
	case StageProofOperation, StageGrantMissing, StageGrantRead, StageProofResolve, StageProofResponse, StageContextIssue, StageContinuationIssue:
	default:
		value.Stage = StageUnknown
	}
	if value.Code < codes.Canceled || value.Code > codes.Unauthenticated {
		value.Code = codes.Unknown
	}
	if _, ok := api.AuthorizationErrorReason_name[int32(value.Reason)]; !ok {
		value.Reason = 0
	}
	if _, ok := api.AuthorizationFailureStage_name[int32(value.AuthorityStage)]; !ok {
		value.AuthorityStage = 0
	}
	return value
}
