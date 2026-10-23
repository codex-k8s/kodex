package authorityclient

import (
	"errors"
	"fmt"

	api "github.com/codex-k8s/kodex/libs/go/internalrpcauth/gen/internalrpcauthority/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// DiagnosticStage identifies the local failure stage without granting authority
// or describing the outcome of a remote RPC.
// RU: DiagnosticStage обозначает локальную стадию отказа, не выдавая полномочий
// и не описывая результат удалённого RPC.
type DiagnosticStage string

const (
	// StageUnknown marks a stage that is absent or outside the closed allowlist.
	// RU: StageUnknown обозначает отсутствующую стадию или значение вне закрытого списка.
	StageUnknown DiagnosticStage = "unknown"
	// StageProofOperation marks failure while selecting the operation bound to proof.
	// RU: StageProofOperation обозначает отказ при выборе операции, привязанной к proof.
	StageProofOperation DiagnosticStage = "proof_operation"
	// StageGrantMissing marks the absence of an exact local grant.
	// RU: StageGrantMissing обозначает отсутствие точного локального grant.
	StageGrantMissing DiagnosticStage = "grant_missing"
	// StageGrantRead marks a failed read of the grant backing the proof.
	// RU: StageGrantRead обозначает неудачное чтение grant, на котором основан proof.
	StageGrantRead DiagnosticStage = "grant_read"
	// StageProofResolve marks failure to resolve the proof provider input.
	// RU: StageProofResolve обозначает отказ при разрешении входа поставщика proof.
	StageProofResolve DiagnosticStage = "proof_resolve"
	// StageProofResponse marks an unusable proof provider response.
	// RU: StageProofResponse обозначает непригодный ответ поставщика proof.
	StageProofResponse DiagnosticStage = "proof_response"
	// StageContextIssue marks failure to issue an authorization context.
	// RU: StageContextIssue обозначает отказ при выпуске authorization context.
	StageContextIssue DiagnosticStage = "context_issue"
	// StageContinuationIssue marks failure to issue a continuation context.
	// RU: StageContinuationIssue обозначает отказ при выпуске continuation context.
	StageContinuationIssue DiagnosticStage = "continuation_issue"
)

// Diagnostic contains only closed classifications for a local failure. It
// never copies credentials, external error text, correlation, or unknown
// protobuf fields from an untrusted response.
// RU: Diagnostic содержит только закрытые классы локального отказа. Удостоверения,
// текст внешней ошибки, correlation и неизвестные поля protobuf не копируются.
type Diagnostic struct {
	Stage          DiagnosticStage
	Code           codes.Code
	Reason         api.AuthorizationErrorReason
	AuthorityStage api.AuthorizationFailureStage
}

// String formats the normalized diagnostic using only bounded enum values.
// RU: String форматирует нормализованную диагностику только из ограниченных enum-значений.
func (value Diagnostic) String() string {
	value = normalizeDiagnostic(value)
	return fmt.Sprintf("authority_stage=%s dependency_code=%s authority_reason=%s authority_failure_stage=%s", value.Stage, value.Code, value.Reason, value.AuthorityStage)
}

// Diagnostic returns the sanitized classification at the local process
// boundary; GRPCStatus intentionally omits these details from the public status.
// RU: Diagnostic возвращает очищенную классификацию на локальной границе процесса;
// GRPCStatus намеренно исключает эти подробности из публичного статуса.
func (failure *LocalAuthorityError) Diagnostic() Diagnostic {
	return normalizeDiagnostic(failure.diagnostic)
}

// proofFailure carries a sanitized provider failure across local retry logic.
// RU: proofFailure передаёт очищенный отказ поставщика локальной логике повтора.
type proofFailure struct{ diagnostic Diagnostic }

// Error exposes only the normalized diagnostic, never the provider's raw error.
// RU: Error раскрывает только нормализованную диагностику, но не исходную ошибку поставщика.
func (failure *proofFailure) Error() string {
	return "authority proof provider failed [" + failure.diagnostic.String() + "]"
}

// GRPCStatus keeps the provider failure's code while redacting its details.
// RU: GRPCStatus сохраняет код отказа поставщика, удаляя подробности.
func (failure *proofFailure) GRPCStatus() *status.Status {
	return status.New(failure.diagnostic.Code, "authority proof provider failed")
}

// NewProofFailure retains the provider stage and status code for the existing
// bounded retry policy, but does not retain the original error or add a retry.
// RU: NewProofFailure сохраняет стадию и код поставщика для прежней ограниченной
// политики повтора, не сохраняя исходную ошибку и не добавляя новый повтор.
func NewProofFailure(stage DiagnosticStage, cause error) error {
	return &proofFailure{diagnostic: diagnosticFrom(cause, stage)}
}

// diagnosticFrom classifies a cause and accepts typed authorization details
// only from the issuer stages and only when the detail shape is unambiguous.
// RU: diagnosticFrom классифицирует причину и принимает типизированные подробности
// авторизации только от стадий issuer и только при однозначной форме detail.
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

// normalizeDiagnostic confines every classification to the declared enums
// before it can enter local diagnostics or error formatting.
// RU: normalizeDiagnostic ограничивает каждую классификацию объявленными enum
// до её попадания в локальную диагностику или форматирование ошибки.
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
