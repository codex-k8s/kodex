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
	return downstreamError(err, downstreamErrorMessages{
		InvalidRequest:        "request is invalid",
		Unauthenticated:       "actor context is not authenticated",
		PermissionDenied:      "access is denied",
		NotFound:              "owner inbox item is not found",
		StaleVersion:          "owner inbox item version is stale",
		Conflict:              "owner inbox item cannot accept this action",
		RateLimited:           "interaction-hub rate limit is active",
		DownstreamUnavailable: "interaction-hub is unavailable",
	})
}

func agentManagerError(err error) *SafeError {
	return downstreamError(err, downstreamErrorMessages{
		InvalidRequest:        "request is invalid",
		Unauthenticated:       "actor context is not authenticated",
		PermissionDenied:      "access is denied",
		NotFound:              "agent run is not found",
		StaleVersion:          "agent run status version is stale",
		Conflict:              "agent run runtime status is conflicted",
		RateLimited:           "agent-manager rate limit is active",
		DownstreamUnavailable: "agent-manager is unavailable",
	})
}

func governanceManagerError(err error) *SafeError {
	return downstreamError(err, downstreamErrorMessages{
		InvalidRequest:        "request is invalid",
		Unauthenticated:       "actor context is not authenticated",
		PermissionDenied:      "access is denied",
		NotFound:              "governance summary is not found",
		StaleVersion:          "governance summary version is stale",
		Conflict:              "governance summary scope is conflicted",
		RateLimited:           "governance-manager rate limit is active",
		DownstreamUnavailable: "governance-manager is unavailable",
	})
}

func projectCatalogError(err error) *SafeError {
	return downstreamError(err, downstreamErrorMessages{
		InvalidRequest:        "project-catalog request is invalid",
		Unauthenticated:       "actor context is not authenticated",
		PermissionDenied:      "access is denied",
		NotFound:              "project-catalog resource is not found",
		StaleVersion:          "project-catalog read version is stale",
		Conflict:              "project-catalog read is conflicted",
		RateLimited:           "project-catalog rate limit is active",
		DownstreamUnavailable: "project-catalog is unavailable",
	})
}

func projectCatalogSelfDeploySignalError(err error) *SafeError {
	return projectCatalogError(err)
}

func downstreamNotFound(err error) bool {
	return status.Code(err) == codes.NotFound
}

type downstreamErrorMessages struct {
	InvalidRequest        string
	Unauthenticated       string
	PermissionDenied      string
	NotFound              string
	StaleVersion          string
	Conflict              string
	RateLimited           string
	DownstreamUnavailable string
}

func downstreamError(err error, messages downstreamErrorMessages) *SafeError {
	switch status.Code(err) {
	case codes.InvalidArgument:
		return WrapSafeError(http.StatusBadRequest, CodeInvalidRequest, messages.InvalidRequest, false, err)
	case codes.Unauthenticated:
		return WrapSafeError(http.StatusUnauthorized, CodeUnauthenticated, messages.Unauthenticated, false, err)
	case codes.PermissionDenied:
		return WrapSafeError(http.StatusForbidden, CodePermissionDenied, messages.PermissionDenied, false, err)
	case codes.NotFound:
		return WrapSafeError(http.StatusNotFound, CodeNotFound, messages.NotFound, false, err)
	case codes.Aborted:
		return WrapSafeError(http.StatusConflict, CodeStaleVersion, messages.StaleVersion, false, err)
	case codes.FailedPrecondition, codes.AlreadyExists:
		return WrapSafeError(http.StatusConflict, CodeConflict, messages.Conflict, false, err)
	case codes.ResourceExhausted:
		return WrapSafeError(http.StatusTooManyRequests, CodeRateLimited, messages.RateLimited, true, err)
	case codes.DeadlineExceeded, codes.Unavailable:
		return WrapSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, messages.DownstreamUnavailable, true, err)
	default:
		return WrapSafeError(http.StatusServiceUnavailable, CodeDownstreamUnavailable, messages.DownstreamUnavailable, true, err)
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
