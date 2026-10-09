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
	taskSessionReadFailureMessage = "assistant task session read unavailable"
	taskSessionStageAttribute     = "task_session_stage"
	taskSessionBindingAttribute   = "diagnostic_binding"
	taskSessionContext            = "CONTEXT"
	taskSessionInput              = "INPUT"
	taskSessionInputCursor        = "INPUT_CURSOR"
	taskSessionOwnerRPC           = "OWNER_RPC"
	taskSessionReply              = "REPLY"
	taskSessionProjection         = "PROJECTION"
	taskSessionBinding            = "BINDING"
	taskSessionCursorShape        = "CURSOR_SHAPE"
	taskSessionCursorSource       = "CURSOR_SOURCE"
	taskSessionCursorProgress     = "CURSOR_PROGRESS"
)

// Только локальная классификация: remote message/details/cause не сохраняются.
// Это наблюдение отказа, не authority и не разрешение повторить вызов.
type taskSessionReadError struct {
	stage string
	code  codes.Code
}

func (*taskSessionReadError) Error() string        { return taskSessionReadFailureMessage }
func (*taskSessionReadError) Is(target error) bool { return target == errAssistantTaskSessionRead }
func (failure *taskSessionReadError) GRPCStatus() *status.Status {
	_, code, _ := taskSessionFailureDetails(failure)
	return status.New(code, failure.Error())
}

func taskSessionReadFailure(stage string) error {
	return &taskSessionReadError{stage: stage, code: codes.Unknown}
}

func taskSessionOwnerFailure(err error) error {
	return &taskSessionReadError{stage: taskSessionOwnerRPC, code: normalizedTaskSessionCode(status.Code(err))}
}

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

// Вызывается только existing MCP boundary с server-authorized input. Request
// locators/cursor и remote response никогда не становятся provenance логов.
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

func taskSessionFailureClass(stage string) string {
	return "assistant_task_session_" + strings.ToLower(stage)
}
