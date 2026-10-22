package app

import (
	"context"
	"errors"
	"regexp"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// renewalPhase identifies the closed diagnostic stage of lease renewal, not a persisted lease state.
// renewalPhase задаёт закрытую диагностическую фазу продления, а не сохранённое состояние аренды.
type renewalPhase string

const (
	// renewalPhaseTrack labels renewal while the controller tracks an executing turn.
	// renewalPhaseTrack обозначает продление при отслеживании контроллером выполняющегося хода.
	renewalPhaseTrack renewalPhase = "TRACK"
	// renewalPhaseDrain labels renewal during bounded turn shutdown and callback draining.
	// renewalPhaseDrain обозначает продление при ограниченном завершении хода и ожидании обратного вызова.
	renewalPhaseDrain renewalPhase = "DRAIN"
	// renewalPhaseMaterialization labels renewal while workload materialization is still guarded by a claim.
	// renewalPhaseMaterialization обозначает продление во время размещения нагрузки, ещё защищённого заявкой.
	renewalPhaseMaterialization renewalPhase = "MATERIALIZATION"
	// renewalFailureMessage fixes the English diagnostic for a failed renewal observed before cleanup cancellation.
	// renewalFailureMessage задаёт английскую диагностику ошибки продления, зафиксированной до отмены при очистке.
	renewalFailureMessage = "runtime lease renewal failed before cleanup cancellation"
	// renewalPhaseKey names the structured field containing only a normalized renewal phase.
	// renewalPhaseKey задаёт имя структурированного поля только для нормализованной фазы продления.
	renewalPhaseKey = "phase"
	// renewalCodeKey names the structured field for a closed gRPC status code, never the raw error text.
	// renewalCodeKey задаёт имя структурированного поля закрытого кода gRPC, но не исходного текста ошибки.
	renewalCodeKey = "grpc_code"
	// renewalRequestContextKey identifies the request-context snapshot taken before local cancellation.
	// renewalRequestContextKey обозначает снимок контекста запроса до его локальной отмены.
	renewalRequestContextKey = "request_context_state"
	// renewalParentContextKey separates the parent-context state from the shorter-lived request context.
	// renewalParentContextKey отделяет состояние родительского контекста от более короткоживущего контекста запроса.
	renewalParentContextKey = "parent_context_state"
	// observationContextActive records that Context.Err currently returns nil; it is not a separate deadline check.
	// observationContextActive означает текущий nil от Context.Err, а не отдельную проверку истечения срока.
	observationContextActive = "ACTIVE"
	// observationContextCanceled represents a context cancellation without exposing its private cause.
	// observationContextCanceled обозначает отмену контекста без раскрытия её частной причины.
	observationContextCanceled = "CANCELED"
	// observationContextDeadline records a deadline-exceeded context error as a bounded diagnostic value.
	// observationContextDeadline фиксирует ошибку истечения срока контекста ограниченным диагностическим значением.
	observationContextDeadline = "DEADLINE_EXCEEDED"
	// observationUnknown is the closed fallback for an unrecognized renewal phase or context state.
	// observationUnknown является закрытым запасным значением для неизвестной фазы продления или состояния контекста.
	observationUnknown = "UNKNOWN"
)

var (
	// renewalReferencePattern permits only 8..128 ASCII letters, digits, underscores and hyphens in logged locator fields.
	// The filter bounds syntax, not provenance or owner authority.
	// renewalReferencePattern допускает в полях указателей журнала только от 8 до 128 букв ASCII, цифр, подчёркиваний и дефисов.
	// Фильтр ограничивает синтаксис, а не проверяет происхождение или полномочия владельца.
	renewalReferencePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)
	// renewalDigestPattern permits only 64 lowercase hexadecimal characters in logged digest fields.
	// Matching a value does not verify the underlying content.
	// renewalDigestPattern допускает в полях дайджестов журнала только 64 строчных шестнадцатеричных символа.
	// Совпадение значения не проверяет соответствующее содержимое.
	renewalDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// logRenewalFailure emits one warning for a nonnil failure when a logger exists, snapshotting contexts before caller cleanup.
// Only closed status/phase/context values and syntax-filtered pins are recorded; raw errors, fences and input are omitted. The observation is not proof of lease revocation or an owner commit.
// logRenewalFailure выводит одно предупреждение для ненулевой ошибки при наличии журнала, фиксируя контексты до очистки вызывающей стороной.
// Записываются только закрытые значения статуса/фазы/контекста и синтаксически отфильтрованные привязки; исходные ошибки, ограждения и вход не выводятся. Наблюдение не доказывает отзыв аренды или фиксацию владельцем.
func (runtime *runtime) logRenewalFailure(request, parent context.Context, input runtimecontract.RunnerInput, phase renewalPhase, err error) {
	if err == nil || runtime.logger == nil {
		return
	}
	requestState, parentState := renewalContextState(request), renewalContextState(parent)
	lease, run, node := renewalReference(input.LeaseRef), renewalReference(input.RunRef), renewalReference(input.NodeRef)
	session, turn := renewalReference(input.SessionRef), renewalReference(input.TurnRef)
	inputDigest, revisionDigest := renewalDigest(input.InputDigest), renewalDigest(input.RuntimeRevisionDigest)
	executionDigest := renewalDigest(input.ExecutionBindingDigest)
	generation, attempt := input.LeaseGeneration, input.Attempt
	if generation < 1 {
		generation = 0
	}
	if attempt < 1 {
		attempt = 0
	}
	binding := "INCOMPLETE_PINS"
	if lease != "" && run != "" && node != "" && session != "" && turn != "" && generation > 0 && attempt > 0 && inputDigest != "" && revisionDigest != "" {
		binding = "CLAIM_PINS"
		if executionDigest != "" {
			binding = "EXECUTION_PINS"
		}
	}
	runtime.logger.WarnContext(parent, renewalFailureMessage,
		renewalPhaseKey, closedRenewalPhase(phase), renewalCodeKey, closedRenewalCode(err),
		renewalRequestContextKey, requestState, renewalParentContextKey, parentState,
		"binding_status", binding, "lease_ref", lease, "lease_generation", generation,
		"run_ref", run, "node_ref", node, "session_ref", session, "turn_ref", turn, "attempt", attempt,
		"input_digest", inputDigest, "runtime_revision_digest", revisionDigest, "execution_binding_digest", executionDigest)
}

// closedRenewalPhase returns a supported diagnostic phase verbatim and normalizes every other value to UNKNOWN.
// closedRenewalPhase возвращает поддержанную диагностическую фазу без изменения, а остальные значения нормализует в UNKNOWN.
func closedRenewalPhase(phase renewalPhase) string {
	switch phase {
	case renewalPhaseTrack, renewalPhaseDrain, renewalPhaseMaterialization:
		return string(phase)
	default:
		return observationUnknown
	}
}

// closedRenewalCode extracts the gRPC code, returns only a canonical status name and maps out-of-range codes to Unknown.
// It does not include status messages, details or joined error strings.
// closedRenewalCode извлекает код gRPC, возвращает только каноническое имя статуса и заменяет внештатные коды на Unknown.
// Сообщения статуса, детали и строки объединённых ошибок не включаются.
func closedRenewalCode(err error) string {
	code := status.Code(err)
	switch code {
	case codes.OK, codes.Canceled, codes.Unknown, codes.InvalidArgument, codes.DeadlineExceeded,
		codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.ResourceExhausted,
		codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unimplemented,
		codes.Internal, codes.Unavailable, codes.DataLoss, codes.Unauthenticated:
		return code.String()
	default:
		return codes.Unknown.String()
	}
}

// renewalContextState classifies Context.Err as deadline, canceled, active or unknown using errors.Is.
// It reads neither a cancellation cause nor the deadline clock; callers must provide a nonnil context.
// renewalContextState классифицирует Context.Err как истечение срока, отмену, активность или неизвестность через errors.Is.
// Причина отмены и часы срока не читаются; вызывающая сторона должна передать ненулевой контекст.
func renewalContextState(ctx context.Context) string {
	err := ctx.Err()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return observationContextDeadline
	case errors.Is(err, context.Canceled):
		return observationContextCanceled
	case err == nil:
		return observationContextActive
	default:
		return observationUnknown
	}
}

// renewalReference keeps a locator only when it matches the bounded logging grammar; otherwise it returns an empty string.
// It does not resolve the resource or establish that the locator is trusted.
// renewalReference сохраняет указатель только при соответствии ограниченной грамматике журнала, иначе возвращает пустую строку.
// Ресурс не разрешается, доверенность указателя не устанавливается.
func renewalReference(value string) string {
	if !renewalReferencePattern.MatchString(value) {
		return ""
	}
	return value
}

// renewalDigest keeps only a canonical lowercase SHA-256 representation for logging, returning empty on mismatch.
// No hashing, credential access or content validation is performed.
// renewalDigest сохраняет для журнала только каноническую строчную запись SHA-256, возвращая пустую строку при несоответствии.
// Хеширование, доступ к учётным данным и проверка содержимого не выполняются.
func renewalDigest(value string) string {
	if !renewalDigestPattern.MatchString(value) {
		return ""
	}
	return value
}
