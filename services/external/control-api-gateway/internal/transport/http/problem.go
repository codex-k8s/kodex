package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	controlPlaneErrorDomain           = "kodex.control-plane"
	freshAuthenticationRequiredReason = "FRESH_AUTHENTICATION_REQUIRED"
)

func writeRPCProblem(writer http.ResponseWriter, err error) {
	code := status.Code(err)
	statusCode, name, retryable := http.StatusInternalServerError, "INTERNAL", false
	switch code {
	case codes.InvalidArgument:
		statusCode, name = http.StatusBadRequest, "INVALID_REQUEST"
	case codes.Unauthenticated:
		statusCode, name = http.StatusUnauthorized, "UNAUTHENTICATED"
	case codes.PermissionDenied:
		statusCode, name = http.StatusForbidden, "PERMISSION_DENIED"
		if rpcErrorHasReason(err, freshAuthenticationRequiredReason) {
			name = freshAuthenticationRequiredReason
		}
	case codes.NotFound:
		statusCode, name = http.StatusNotFound, "NOT_FOUND"
	case codes.AlreadyExists:
		statusCode, name = http.StatusConflict, "IDEMPOTENCY_CONFLICT"
	case codes.Aborted:
		statusCode, name, retryable = http.StatusPreconditionFailed, "VERSION_OR_STATE_CONFLICT", true
	case codes.FailedPrecondition:
		statusCode, name = http.StatusConflict, "STATE_CONFLICT"
	case codes.ResourceExhausted:
		statusCode, name, retryable = http.StatusTooManyRequests, "RATE_LIMITED", true
	case codes.Canceled:
		var localAuthorityFailure *authorityclient.LocalAuthorityError
		if errors.As(err, &localAuthorityFailure) {
			statusCode, name, retryable = http.StatusServiceUnavailable, "UNAVAILABLE", true
		}
	case codes.Unavailable:
		statusCode, name, retryable = http.StatusServiceUnavailable, "UNAVAILABLE", true
	case codes.DeadlineExceeded:
		statusCode, name, retryable = http.StatusGatewayTimeout, "DEADLINE_EXCEEDED", true
	}
	writeLocalProblem(writer, statusCode, name, retryable)
}

func rpcErrorHasReason(err error, reason string) bool {
	rpcStatus, ok := status.FromError(err)
	if !ok {
		return false
	}
	for _, detail := range rpcStatus.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if ok && info.GetDomain() == controlPlaneErrorDomain && info.GetReason() == reason {
			return true
		}
	}
	return false
}

func writeLocalProblem(writer http.ResponseWriter, statusCode int, code string, retryable bool) {
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.Header().Set("Cache-Control", "no-store")
	if statusCode == http.StatusTooManyRequests {
		writer.Header().Set("Retry-After", "1")
	}
	writer.WriteHeader(statusCode)
	title := http.StatusText(statusCode)
	if localizer, ok := writer.(interface{ Localize(string) string }); ok {
		title = localizer.Localize(code)
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"type":  "urn:kodex:problem:" + strings.ToLower(code),
		"title": title, "status": statusCode, "code": code,
		"correlationId": uuid.NewString(), "retryable": retryable,
	})
}

func WriteLocalProblem(writer http.ResponseWriter, statusCode int, code string, retryable bool) {
	writeLocalProblem(writer, statusCode, code, retryable)
}
