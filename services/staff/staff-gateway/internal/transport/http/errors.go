package httptransport

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/transport/http/generated"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type ErrorCode string

const (
	CodeInvalidRequest        ErrorCode = "invalid_request"
	CodeUnauthenticated       ErrorCode = "unauthenticated"
	CodePermissionDenied      ErrorCode = "permission_denied"
	CodeNotFound              ErrorCode = "not_found"
	CodeConflict              ErrorCode = "conflict"
	CodeStaleVersion          ErrorCode = "stale_version"
	CodeDownstreamUnavailable ErrorCode = "downstream_unavailable"
	CodeRateLimited           ErrorCode = "rate_limited"
)

type SafeError struct {
	Status            int
	Code              ErrorCode
	Message           string
	Retryable         bool
	RetryAfterSeconds int
	Cause             error
}

func (e *SafeError) Error() string {
	return e.Message
}

func (e *SafeError) Unwrap() error {
	return e.Cause
}

func NewSafeError(status int, code ErrorCode, message string, retryable bool) *SafeError {
	return &SafeError{Status: status, Code: code, Message: message, Retryable: retryable}
}

func WrapSafeError(status int, code ErrorCode, message string, retryable bool, cause error) *SafeError {
	return &SafeError{Status: status, Code: code, Message: message, Retryable: retryable, Cause: cause}
}

func (e *SafeError) WithRetryAfter(delay time.Duration) *SafeError {
	if e == nil {
		return nil
	}
	e.RetryAfterSeconds = retryAfterSeconds(delay)
	return e
}

func retryAfterSeconds(delay time.Duration) int {
	if delay <= 0 {
		return 1
	}
	rounded := int((delay + time.Second - 1) / time.Second)
	if rounded < 1 {
		return 1
	}
	return rounded
}

func errorBoundary(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		recorder := &panicRecorder{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.WarnContext(req.Context(), "staff-gateway panic recovered", "request_id", requestIDFromContext(req.Context()))
				WriteSafeError(w, req, NewSafeError(http.StatusInternalServerError, CodeDownstreamUnavailable, "internal gateway error", true))
			}
		}()
		next.ServeHTTP(recorder, req)
	})
}

func interactionHubError(err error) *SafeError {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return WrapSafeError(http.StatusBadRequest, CodeInvalidRequest, "request is invalid", false, err)
	case codes.Unauthenticated:
		return WrapSafeError(http.StatusUnauthorized, CodeUnauthenticated, "actor context is not authenticated", false, err)
	case codes.PermissionDenied:
		return WrapSafeError(http.StatusForbidden, CodePermissionDenied, "access is denied", false, err)
	case codes.NotFound:
		return WrapSafeError(http.StatusNotFound, CodeNotFound, "owner inbox item is not found", false, err)
	case codes.Aborted:
		return WrapSafeError(http.StatusConflict, CodeStaleVersion, "owner inbox item version is stale", false, err)
	case codes.FailedPrecondition, codes.AlreadyExists:
		return WrapSafeError(http.StatusConflict, CodeConflict, "owner inbox item cannot accept this action", false, err)
	case codes.ResourceExhausted:
		return WrapSafeError(http.StatusTooManyRequests, CodeRateLimited, "interaction-hub rate limit is active", true, err)
	case codes.DeadlineExceeded, codes.Unavailable:
		return WrapSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub is unavailable", true, err)
	default:
		return WrapSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, "interaction-hub is unavailable", true, err)
	}
}

func WriteSafeError(w http.ResponseWriter, req *http.Request, safeErr *SafeError) {
	if safeErr.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(safeErr.RetryAfterSeconds))
	}
	correlationID := requestIDFromContext(req.Context())
	body := generated.SafeError{
		RequestId:     requestIDFromContext(req.Context()),
		CorrelationId: optionalString(correlationID),
		Code:          generated.SafeErrorCode(safeErr.Code),
		Message:       safeErr.Message,
		Retryable:     safeErr.Retryable,
	}
	writeJSON(w, safeErr.Status, body)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

type panicRecorder struct {
	http.ResponseWriter
}
