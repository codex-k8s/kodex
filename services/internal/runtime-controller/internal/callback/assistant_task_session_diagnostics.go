package callback

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// taskSessionReadFailureMessage is shared safe error text independent of failure
	// stage, selected session and private owner cause.
	// taskSessionReadFailureMessage — общий safe error text независимо от failure stage,
	// selected session и private owner cause.
	taskSessionReadFailureMessage = "assistant task session read unavailable"
	// taskSessionStageAttribute names the structured log field for a closed local
	// read-failure stage, not arbitrary caller-supplied diagnostic prose.
	// taskSessionStageAttribute задаёт structured log field закрытого local read-failure
	// stage,не произвольного caller-supplied diagnostic prose.
	taskSessionStageAttribute = "task_session_stage"
	// taskSessionBindingAttribute marks whether diagnostic provenance came from
	// already-authorized execution input or remains unavailable.
	// taskSessionBindingAttribute отмечает diagnostic provenance из already-authorized
	// execution input либо её недоступность.
	taskSessionBindingAttribute = "diagnostic_binding"
	// taskSessionContext marks locally rejected missing assistant/lease execution
	// context before any owner read.
	// taskSessionContext отмечает local rejection отсутствующего assistant/lease execution
	// context до owner read.
	taskSessionContext = "CONTEXT"
	// taskSessionInput marks rejected closed input shape or unsafe selected run locator.
	// taskSessionInput обозначает rejected closed input shape и unsafe selected run locator.
	taskSessionInput = "INPUT"
	// taskSessionInputCursor marks a rejected cursor type, empty value or byte limit
	// before RPC; encoded cursor/source integrity is checked later.
	// taskSessionInputCursor отмечает отказ cursor type,empty value/byte limit до RPC;
	// encoded cursor/source integrity проверяются далее.
	taskSessionInputCursor = "INPUT_CURSOR"
	// taskSessionOwnerRPC marks a failed owner RPC whose registered gRPC code may be
	// retained after private message/details/cause are discarded.
	// taskSessionOwnerRPC обозначает failed owner RPC с сохранением registered gRPC code
	// после удаления private message/details/cause.
	taskSessionOwnerRPC = "OWNER_RPC"
	// taskSessionReply marks missing, unknown-field or mixed-mode owner reply envelope.
	// taskSessionReply обозначает missing,unknown-field и mixed-mode owner reply envelope.
	taskSessionReply = "REPLY"
	// taskSessionProjection marks rejection by the shared public task-session projection
	// validator rather than a failure to launch or resume the session.
	// taskSessionProjection отмечает отказ shared public task-session projection validator,
	// не ошибку запуска и не resume session.
	taskSessionProjection = "PROJECTION"
	// taskSessionBinding marks mismatched selected run or project in an otherwise
	// projectable owner response.
	// taskSessionBinding отмечает mismatch selected run/project в иначе допустимом owner response.
	taskSessionBinding = "BINDING"
	// taskSessionCursorShape marks an invalid encoded continuation cursor after owner
	// read; it does not label the remote owner error as locally correctable input.
	// taskSessionCursorShape отмечает invalid encoded continuation cursor после owner read,
	// не превращая remote owner error в locally correctable input.
	taskSessionCursorShape = "CURSOR_SHAPE"
	// taskSessionCursorSource marks continuation against a different source commitment,
	// requiring a fresh source read rather than combining version-inconsistent pages.
	// taskSessionCursorSource отмечает continuation с другим source commitment,требуя
	// fresh source read вместо объединения version-inconsistent pages.
	taskSessionCursorSource = "CURSOR_SOURCE"
	// taskSessionCursorProgress marks invalid next offset or changed continuation binding
	// for a truncated page; a cursor locator alone never authorizes broader discovery.
	// taskSessionCursorProgress отмечает invalid next offset/changed continuation binding
	// truncated page; один cursor locator не авторизует broader discovery.
	taskSessionCursorProgress = "CURSOR_PROGRESS"
)

// taskSessionReadError stores only a closed local stage and normalized gRPC code,
// without retaining raw owner error chains, payloads or permission grants.
// taskSessionReadError хранит лишь closed local stage/normalized gRPC code без raw owner
// error chains,payloads и permission grants.
type taskSessionReadError struct {
	stage string
	code  codes.Code
}

// Error returns fixed safe read-unavailable text without stage or remote details.
// Error возвращает fixed safe read-unavailable text без stage и remote details.
func (*taskSessionReadError) Error() string { return taskSessionReadFailureMessage }

// Is supports comparison only with the shared task-session-read sentinel; it does
// not expose or match a discarded remote error cause.
// Is допускает сравнение лишь с общим task-session-read sentinel,не раскрывая
// и не сопоставляя отброшенный remote error cause.
func (*taskSessionReadError) Is(target error) bool { return target == errAssistantTaskSessionRead }

// GRPCStatus returns a new sanitized status using validated stage/code classification
// and fixed text; arbitrary remote details are never attached.
// GRPCStatus возвращает новый sanitized status из validated stage/code classification
// и fixed text без arbitrary remote details.
func (failure *taskSessionReadError) GRPCStatus() *status.Status {
	_, code, _ := taskSessionFailureDetails(failure)
	return status.New(code, failure.Error())
}

// taskSessionReadFailure creates local stage failure with Unknown status; the later
// closed-stage classifier rejects unregistered stages instead of logging them.
// taskSessionReadFailure создаёт local stage failure с Unknown status; последующий
// closed-stage classifier отвергает unregistered stages вместо их логирования.
func taskSessionReadFailure(stage string) error {
	return &taskSessionReadError{stage: stage, code: codes.Unknown}
}

// taskSessionOwnerFailure preserves only a normalized owner status code and OWNER_RPC
// stage, discarding the original error message, details and unwrap chain.
// taskSessionOwnerFailure сохраняет лишь normalized owner status code/OWNER_RPC stage,
// отбрасывая исходные error message,details и unwrap chain.
func taskSessionOwnerFailure(err error) error {
	return &taskSessionReadError{stage: taskSessionOwnerRPC, code: normalizedTaskSessionCode(status.Code(err))}
}

// normalizedTaskSessionCode retains the sixteen registered non-OK gRPC codes and
// maps OK or unknown numeric values to Unknown for failure diagnostics.
// normalizedTaskSessionCode сохраняет шестнадцать registered non-OK gRPC codes,
// заменяя OK и unknown numeric values на Unknown в failure diagnostics.
func normalizedTaskSessionCode(code codes.Code) codes.Code {
	switch code {
	case codes.Canceled, codes.Unknown, codes.InvalidArgument, codes.DeadlineExceeded,
		codes.NotFound, codes.AlreadyExists, codes.PermissionDenied, codes.ResourceExhausted,
		codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unimplemented,
		codes.Internal, codes.Unavailable, codes.DataLoss, codes.Unauthenticated:
		return code
	default:
		return codes.Unknown
	}
}

// taskSessionFailureDetails recognizes a nonnil typed failure through wrapping,
// accepts only registered stages and exposes the normalized code solely for OWNER_RPC.
// Local stages always remain Unknown; untyped text and invalid markers have no details.
// taskSessionFailureDetails распознаёт nonnil typed failure через wrapping,допуская лишь
// registered stages и normalized code только OWNER_RPC. Local stages всегда Unknown;
// untyped text и invalid markers не имеют details.
func taskSessionFailureDetails(err error) (string, codes.Code, bool) {
	var failure *taskSessionReadError
	if !errors.As(err, &failure) || failure == nil {
		return "", codes.Unknown, false
	}
	switch failure.stage {
	case taskSessionContext, taskSessionInput, taskSessionInputCursor, taskSessionOwnerRPC,
		taskSessionReply, taskSessionProjection, taskSessionBinding, taskSessionCursorShape,
		taskSessionCursorSource, taskSessionCursorProgress:
	default:
		return "", codes.Unknown, false
	}
	code := normalizedTaskSessionCode(failure.code)
	if failure.stage != taskSessionOwnerRPC {
		code = codes.Unknown
	}
	return failure.stage, code, true
}

// taskSessionFailureAttributes is called only at an authenticated MCP boundary and
// emits safe execution provenance only when refs/attempt/digests are structurally valid.
// Otherwise it returns stage plus UNAVAILABLE, never request locator/cursor or remote
// content. Caller authorization is assumed, not established by these syntax checks.
// taskSessionFailureAttributes вызывается лишь authenticated MCP boundary и выдаёт safe
// execution provenance при structurally valid refs/attempt/digests,иначе stage/UNAVAILABLE
// без request locator/cursor и remote content. Caller authorization предполагается,
// а не устанавливается этими syntax checks.
func taskSessionFailureAttributes(input runtimecontract.RunnerInput, callID json.RawMessage, stage string) []any {
	attributes := []any{taskSessionStageAttribute, stage, taskSessionBindingAttribute, "UNAVAILABLE"}
	for _, ref := range []string{input.RunRef, input.NodeRef, input.SessionRef, input.TurnRef} {
		if !validAssistantResourceRef(ref) {
			return attributes
		}
	}
	if input.Attempt < 1 || input.LeaseRef == "" || !validAssistantCatalogDigest(input.RuntimeRevisionDigest) ||
		!validAssistantCatalogDigest(input.InputDigest) || !validAssistantCatalogDigest(input.ExecutionBindingDigest) {
		return attributes
	}
	digest := sha256.Sum256([]byte(stableKey(input.LeaseRef, string(callID))))
	return []any{taskSessionStageAttribute, stage, taskSessionBindingAttribute, "AUTHENTICATED_INPUT",
		"run_ref", input.RunRef, "node_ref", input.NodeRef, "session_ref", input.SessionRef,
		"turn_ref", input.TurnRef, "attempt", input.Attempt,
		"runtime_revision_digest", input.RuntimeRevisionDigest, "input_digest", input.InputDigest,
		"execution_binding_digest", input.ExecutionBindingDigest, "tool_call_ref", "tcl_" + hex.EncodeToString(digest[:16])}
}

// taskSessionFailureClass formats an already-validated closed stage as a bounded
// diagnostic label; callers must not pass arbitrary request or remote text.
// taskSessionFailureClass преобразует уже validated closed stage в bounded diagnostic
// label; caller не передаёт arbitrary request/remote text.
func taskSessionFailureClass(stage string) string {
	return "assistant_task_session_" + strings.ToLower(stage)
}
