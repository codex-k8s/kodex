package httptransport

import (
	"encoding/json"
	"net/http"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const problemBase = "https://mattercodex.local/problems/"

type mappedProblem struct {
	Status    int
	Code      string
	Retryable bool
	Expected  bool
}

var reasonMatrix = map[controlplanev1.ErrorReason]mappedProblem{
	controlplanev1.ErrorReason_ERROR_REASON_INVALID_REQUEST:      {http.StatusBadRequest, "INVALID_REQUEST", false, true},
	controlplanev1.ErrorReason_ERROR_REASON_UNAUTHENTICATED:      {http.StatusUnauthorized, "UNAUTHENTICATED", false, true},
	controlplanev1.ErrorReason_ERROR_REASON_PERMISSION_DENIED:    {http.StatusForbidden, "PERMISSION_DENIED", false, true},
	controlplanev1.ErrorReason_ERROR_REASON_NOT_FOUND:            {http.StatusNotFound, "NOT_FOUND", false, true},
	controlplanev1.ErrorReason_ERROR_REASON_STATE_CONFLICT:       {http.StatusConflict, "STATE_CONFLICT", false, true},
	controlplanev1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT: {http.StatusConflict, "IDEMPOTENCY_CONFLICT", false, true},
	controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH:     {http.StatusPreconditionFailed, "VERSION_MISMATCH", true, true},
	controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE:          {http.StatusServiceUnavailable, "UNAVAILABLE", true, true},
	controlplanev1.ErrorReason_ERROR_REASON_INTERNAL:             {http.StatusInternalServerError, "INTERNAL", false, true},
}

var grpcMatrix = map[controlplanev1.ErrorReason]codes.Code{
	controlplanev1.ErrorReason_ERROR_REASON_INVALID_REQUEST:      codes.InvalidArgument,
	controlplanev1.ErrorReason_ERROR_REASON_UNAUTHENTICATED:      codes.Unauthenticated,
	controlplanev1.ErrorReason_ERROR_REASON_PERMISSION_DENIED:    codes.PermissionDenied,
	controlplanev1.ErrorReason_ERROR_REASON_NOT_FOUND:            codes.NotFound,
	controlplanev1.ErrorReason_ERROR_REASON_STATE_CONFLICT:       codes.FailedPrecondition,
	controlplanev1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT: codes.AlreadyExists,
	controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH:     codes.Aborted,
	controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE:          codes.Unavailable,
	controlplanev1.ErrorReason_ERROR_REASON_INTERNAL:             codes.Internal,
}

func mapRPCError(err error, mutation bool) (generated.Problem, bool) {
	fallback := newProblem(http.StatusInternalServerError, "INTERNAL", false, uuid.NewString())
	if err == nil {
		return fallback, false
	}
	current, ok := status.FromError(err)
	if !ok {
		return fallback, false
	}
	var detail *controlplanev1.ErrorDetail
	for _, candidate := range current.Details() {
		if typed, match := candidate.(*controlplanev1.ErrorDetail); match {
			if detail != nil {
				return fallback, false
			}
			detail = typed
		}
	}
	if detail == nil || uuid.Validate(detail.GetCorrelationId()) != nil {
		return fallback, false
	}
	mapping, known := reasonMatrix[detail.GetReason()]
	if !known || grpcMatrix[detail.GetReason()] != current.Code() || mapping.Code != detail.GetCode() ||
		mapping.Retryable != detail.GetRetryable() {
		return fallback, false
	}
	if !mutation && (detail.GetReason() == controlplanev1.ErrorReason_ERROR_REASON_STATE_CONFLICT ||
		detail.GetReason() == controlplanev1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT ||
		detail.GetReason() == controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH) {
		return fallback, false
	}
	return newProblem(mapping.Status, mapping.Code, mapping.Retryable, detail.GetCorrelationId()), true
}

// MapRPCProblem предоставляет WebSocket boundary тот же fail-closed error contract.
func MapRPCProblem(err error) (string, bool, bool) {
	problem, expected := mapRPCError(err, false)
	return problem.Code, problem.Retryable, expected
}

func newProblem(statusCode int, code string, retryable bool, correlationID string) generated.Problem {
	parsed, err := uuid.Parse(correlationID)
	if err != nil {
		parsed = uuid.New()
	}
	return generated.Problem{
		Type: problemBase + code, Title: http.StatusText(statusCode), Status: statusCode,
		Code: code, CorrelationId: parsed, Retryable: retryable,
	}
}

func writeProblem(writer http.ResponseWriter, problem generated.Problem) {
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.Header().Set("Cache-Control", "no-store")
	if problem.Status == http.StatusTooManyRequests {
		writer.Header().Set("Retry-After", "1")
	}
	writer.WriteHeader(problem.Status)
	_ = json.NewEncoder(writer).Encode(problem)
}

func localProblem(statusCode int, code string, retryable bool) generated.Problem {
	return newProblem(statusCode, code, retryable, uuid.NewString())
}

// WriteLocalProblem сохраняет единый contract для ошибок WebSocket handshake.
func WriteLocalProblem(writer http.ResponseWriter, statusCode int, code string, retryable bool) {
	writeProblem(writer, localProblem(statusCode, code, retryable))
}
