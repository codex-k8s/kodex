package httptransport

import (
	"encoding/json"
	"errors"
	"log/slog"
	stdhttp "net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/transport/http/generated"
)

// ErrorCode is the stable public error code from the OpenAPI contract.
type ErrorCode string

const (
	CodeInvalidRequest        ErrorCode = "invalid_request"
	CodeSourceNotAllowed      ErrorCode = "source_not_allowed"
	CodeSignatureInvalid      ErrorCode = "signature_invalid"
	CodePayloadTooLarge       ErrorCode = "payload_too_large"
	CodeRateLimited           ErrorCode = "rate_limited"
	CodeDownstreamUnavailable ErrorCode = "downstream_unavailable"
	CodeBackpressure          ErrorCode = "backpressure"
)

// SafeError is a redaction-safe HTTP error boundary object.
type SafeError struct {
	Status            int
	Code              ErrorCode
	Message           string
	Retryable         bool
	RetryAfterSeconds int
	Cause             error
}

// Error returns the safe public message.
func (e *SafeError) Error() string {
	return e.Message
}

// Unwrap returns the original error for internal classification.
func (e *SafeError) Unwrap() error {
	return e.Cause
}

// NewSafeError creates a safe HTTP error.
func NewSafeError(status int, code ErrorCode, message string, retryable bool) *SafeError {
	return &SafeError{Status: status, Code: code, Message: message, Retryable: retryable}
}

// WrapSafeError creates a safe HTTP error with an internal cause.
func WrapSafeError(status int, code ErrorCode, message string, retryable bool, cause error) *SafeError {
	return &SafeError{Status: status, Code: code, Message: message, Retryable: retryable, Cause: cause}
}

// WithRetryAfter sets a bounded Retry-After header value for retryable edge errors.
func (e *SafeError) WithRetryAfter(delay time.Duration) *SafeError {
	if e == nil {
		return nil
	}
	if delay <= 0 {
		e.RetryAfterSeconds = 1
		return e
	}
	seconds := int((delay + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	e.RetryAfterSeconds = seconds
	return e
}

// ErrorHandler is Echo's central error boundary.
func ErrorHandler(logger *slog.Logger) echo.HTTPErrorHandler {
	return func(c *echo.Context, err error) {
		response := c.Response()
		if echoResponse, unwrapErr := echo.UnwrapResponse(response); unwrapErr == nil && echoResponse.Committed {
			return
		}
		req := c.Request()
		safeErr := normalizeError(err)
		if safeErr.Status >= 500 {
			logger.WarnContext(req.Context(), "integration-gateway request failed",
				"request_id", requestIDFromContext(req.Context()),
				"error_code", safeErr.Code,
				"status", safeErr.Status,
			)
		}
		WriteSafeError(response, req, safeErr)
	}
}

// WriteSafeError writes a stable OpenAPI error response.
func WriteSafeError(w stdhttp.ResponseWriter, req *stdhttp.Request, safeErr *SafeError) {
	setRejectReason(req, safeErr.Code)
	if safeErr.RetryAfterSeconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(safeErr.RetryAfterSeconds))
	}
	correlationID := requestIDFromContext(req.Context())
	body := generated.SafeError{
		RequestId:     requestIDFromContext(req.Context()),
		CorrelationId: &correlationID,
		Code:          generated.SafeErrorCode(safeErr.Code),
		Message:       safeErr.Message,
		Retryable:     safeErr.Retryable,
	}
	writeJSON(w, safeErr.Status, body)
}

func normalizeError(err error) *SafeError {
	var safeErr *SafeError
	if errors.As(err, &safeErr) {
		return safeErr
	}
	var echoErr *echo.HTTPError
	if errors.As(err, &echoErr) {
		return NewSafeError(echoErr.StatusCode(), CodeInvalidRequest, echoErr.Message, false)
	}
	return WrapSafeError(stdhttp.StatusInternalServerError, CodeDownstreamUnavailable, "internal gateway error", true, err)
}

func writeJSON(w stdhttp.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
