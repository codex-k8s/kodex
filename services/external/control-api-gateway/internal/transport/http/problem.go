package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	interactiongatewayv1 "github.com/codex-k8s/matter-codex/libs/go/interactiongatewayapi/gen/interactiongateway/v1"
	ownerclient "github.com/codex-k8s/matter-codex/services/external/control-api-gateway/internal/clients/owner"
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
	controlplanev1.ErrorReason_ERROR_REASON_RATE_LIMITED:         {http.StatusTooManyRequests, "RATE_LIMITED", true, true},
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
	controlplanev1.ErrorReason_ERROR_REASON_RATE_LIMITED:         codes.ResourceExhausted,
}

func mapRPCError(err error, mutation bool) (generated.Problem, bool) {
	fallback := newProblem(http.StatusInternalServerError, "INTERNAL", false, uuid.NewString())
	if err == nil {
		return fallback, false
	}
	currentError := err
	var downstream *ownerclient.DownstreamError
	if errors.As(err, &downstream) {
		if !downstream.Valid() {
			return fallback, false
		}
		currentError = downstream.Err
	}
	current, ok := status.FromError(currentError)
	if !ok {
		return fallback, false
	}
	var detail *controlplanev1.ErrorDetail
	var interactionDetail *interactiongatewayv1.ErrorDetail
	for _, candidate := range current.Details() {
		if typed, match := candidate.(*controlplanev1.ErrorDetail); match {
			if detail != nil || interactionDetail != nil {
				return fallback, false
			}
			detail = typed
		}
		if typed, match := candidate.(*interactiongatewayv1.ErrorDetail); match {
			if detail != nil || interactionDetail != nil {
				return fallback, false
			}
			interactionDetail = typed
		}
	}
	if downstream != nil && downstream.NormalizedDetail != nil {
		if detail != nil || interactionDetail != nil {
			return fallback, false
		}
		detail = downstream.NormalizedDetail
	}
	if downstream != nil && downstream.Source == ownerclient.RPCSourceIntegration && downstream.NormalizedDetail == nil {
		return fallback, false
	}
	if downstream != nil && downstream.Source == ownerclient.RPCSourceInteraction && downstream.NormalizedDetail == nil {
		if interactionDetail == nil {
			return fallback, false
		}
		reason, known := interactionReason(interactionDetail.GetReason())
		mapping := reasonMatrix[reason]
		if !known || (!mutation && mutationOnlyReason(reason)) || uuid.Validate(interactionDetail.GetCorrelationId()) != nil || grpcMatrix[reason] != current.Code() ||
			!interactionProblemCodeAllowed(interactionDetail.GetReason(), interactionDetail.GetCode()) || mapping.Retryable != interactionDetail.GetRetryable() {
			return fallback, false
		}
		return newProblem(mapping.Status, interactionDetail.GetCode(), mapping.Retryable, interactionDetail.GetCorrelationId()), true
	}
	if detail == nil || uuid.Validate(detail.GetCorrelationId()) != nil {
		return fallback, false
	}
	mapping, known := reasonMatrix[detail.GetReason()]
	if detail.GetReason() == controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE && current.Code() == codes.DeadlineExceeded && detail.GetCode() == "DEADLINE_EXCEEDED" && detail.GetRetryable() {
		return newProblem(http.StatusGatewayTimeout, detail.GetCode(), true, detail.GetCorrelationId()), true
	}
	if !known || (!mutation && mutationOnlyReason(detail.GetReason())) || grpcMatrix[detail.GetReason()] != current.Code() || mapping.Code != detail.GetCode() ||
		mapping.Retryable != detail.GetRetryable() {
		return fallback, false
	}
	return newProblem(mapping.Status, mapping.Code, mapping.Retryable, detail.GetCorrelationId()), true
}

func mutationOnlyReason(reason controlplanev1.ErrorReason) bool {
	return reason == controlplanev1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT || reason == controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH
}

func interactionReason(value interactiongatewayv1.ErrorReason) (controlplanev1.ErrorReason, bool) {
	mapping := map[interactiongatewayv1.ErrorReason]controlplanev1.ErrorReason{
		interactiongatewayv1.ErrorReason_ERROR_REASON_INVALID_REQUEST:      controlplanev1.ErrorReason_ERROR_REASON_INVALID_REQUEST,
		interactiongatewayv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED:      controlplanev1.ErrorReason_ERROR_REASON_UNAUTHENTICATED,
		interactiongatewayv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED:    controlplanev1.ErrorReason_ERROR_REASON_PERMISSION_DENIED,
		interactiongatewayv1.ErrorReason_ERROR_REASON_NOT_FOUND:            controlplanev1.ErrorReason_ERROR_REASON_NOT_FOUND,
		interactiongatewayv1.ErrorReason_ERROR_REASON_STATE_CONFLICT:       controlplanev1.ErrorReason_ERROR_REASON_STATE_CONFLICT,
		interactiongatewayv1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT: controlplanev1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT,
		interactiongatewayv1.ErrorReason_ERROR_REASON_VERSION_MISMATCH:     controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH,
		interactiongatewayv1.ErrorReason_ERROR_REASON_UNAVAILABLE:          controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE,
		interactiongatewayv1.ErrorReason_ERROR_REASON_INTERNAL:             controlplanev1.ErrorReason_ERROR_REASON_INTERNAL,
	}
	result, ok := mapping[value]
	return result, ok
}

func interactionProblemCodeAllowed(reason interactiongatewayv1.ErrorReason, code string) bool {
	allowed := map[interactiongatewayv1.ErrorReason]map[string]struct{}{
		interactiongatewayv1.ErrorReason_ERROR_REASON_INVALID_REQUEST:      {"INVALID_REQUEST": {}},
		interactiongatewayv1.ErrorReason_ERROR_REASON_UNAUTHENTICATED:      {"UNAUTHENTICATED": {}},
		interactiongatewayv1.ErrorReason_ERROR_REASON_PERMISSION_DENIED:    {"PERMISSION_DENIED": {}},
		interactiongatewayv1.ErrorReason_ERROR_REASON_NOT_FOUND:            {"NOT_FOUND": {}},
		interactiongatewayv1.ErrorReason_ERROR_REASON_STATE_CONFLICT:       {"STATE_CONFLICT": {}, "MATTERMOST_BOT_CONFLICT": {}, "MATTERMOST_BOT_DELETED": {}, "MATTERMOST_BOT_REPAIR_REQUIRED": {}},
		interactiongatewayv1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT: {"IDEMPOTENCY_CONFLICT": {}},
		interactiongatewayv1.ErrorReason_ERROR_REASON_VERSION_MISMATCH:     {"VERSION_MISMATCH": {}},
		interactiongatewayv1.ErrorReason_ERROR_REASON_UNAVAILABLE:          {"UNAVAILABLE": {}, "MATTERMOST_BOT_OPERATION_BUSY": {}, "MATTERMOST_BOT_EFFECT_AMBIGUOUS": {}},
		interactiongatewayv1.ErrorReason_ERROR_REASON_INTERNAL:             {"INTERNAL": {}},
	}
	_, ok := allowed[reason][code]
	return ok
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
