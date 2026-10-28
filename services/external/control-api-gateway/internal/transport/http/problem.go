package httptransport

import (
	"errors"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
	"github.com/codex-k8s/kodex/libs/go/sttapi/errorprofile"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/availability"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/problemresponse"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// controlPlaneErrorDomain identifies the trusted control-plane ErrorInfo namespace; a matching reason from another domain must not select a specialized public problem code.
	// controlPlaneErrorDomain задаёт доверенное пространство ErrorInfo control-plane; совпадающая причина другого домена не выбирает специальный код ошибки.
	controlPlaneErrorDomain = "kodex.control-plane"
	// freshAuthenticationRequiredReason preserves the trusted fresh-login requirement only for PermissionDenied responses with the exact control-plane domain.
	// freshAuthenticationRequiredReason сохраняет требование свежего входа только для PermissionDenied с точным доменом control-plane.
	freshAuthenticationRequiredReason = "FRESH_AUTHENTICATION_REQUIRED"
	// resourceInUseReason preserves the trusted resource-in-use reason only for FailedPrecondition from the exact control-plane domain.
	// resourceInUseReason сохраняет доверенную причину занятости ресурса только для FailedPrecondition с точным доменом control-plane.
	resourceInUseReason = "RESOURCE_IN_USE"
)

// writeRPCProblem maps gRPC status into closed HTTP status/code/retryability values without exposing upstream text. Typed local-authority cancellation becomes retryable unavailable; bare cancellation remains a request timeout.
// writeRPCProblem отображает gRPC status в закрытые HTTP status/code/retryability без раскрытия внешнего текста. Типизированная отмена локального запроса полномочий становится повторяемой недоступностью; обычная отмена остаётся timeout запроса.
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
		if rpcErrorHasReason(err, resourceInUseReason) {
			name = resourceInUseReason
		}
		for _, reason := range []string{"LEGACY_REWORK_SCOPE_UNSUPPORTED", "LEGACY_REWORK_WRITE_REQUIRES_RECONCILIATION", "LEGACY_REWORK_PARENT_BUSY", "LEGACY_REWORK_DELIVERY_BLOCKED", "LEGACY_REWORK_READ_LIMIT"} {
			if rpcErrorHasClosedReason(err, reason) {
				name = reason
				break
			}
		}
	case codes.ResourceExhausted:
		statusCode, name, retryable = http.StatusTooManyRequests, "RATE_LIMITED", true
	case codes.Canceled:
		var localAuthorityFailure *authorityclient.LocalAuthorityError
		if errors.As(err, &localAuthorityFailure) {
			statusCode, name, retryable = http.StatusServiceUnavailable, "UNAVAILABLE", true
		} else {
			statusCode, name = http.StatusRequestTimeout, "REQUEST_CANCELED"
		}
	case codes.Unavailable:
		statusCode, name, retryable = http.StatusServiceUnavailable, "UNAVAILABLE", true
	case codes.DeadlineExceeded:
		statusCode, name, retryable = http.StatusGatewayTimeout, "DEADLINE_EXCEEDED", true
	}
	if statusCode == http.StatusServiceUnavailable {
		problemresponse.MarkFailure(writer, availability.Fixed("upstream_rpc", "unavailable", err))
	}
	writeLocalProblem(writer, statusCode, name, retryable)
}

// rpcErrorHasReason accepts a typed ErrorInfo only when both control-plane domain and requested reason match; arbitrary status text is never parsed as authority.
// rpcErrorHasReason принимает типизированный ErrorInfo только при совпадении домена control-plane и запрошенной причины; произвольный текст статуса полномочий не даёт.
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

// rpcErrorHasClosedReason rejects metadata-bearing or foreign details for the bounded owner-rework errors; status text never selects a public code.
// rpcErrorHasClosedReason отклоняет metadata и чужой domain в bounded owner-rework errors; текст status не выбирает публичный код.
func rpcErrorHasClosedReason(err error, reason string) bool {
	rpcStatus, ok := status.FromError(err)
	if !ok {
		return false
	}
	for _, detail := range rpcStatus.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if ok && info.GetDomain() == controlPlaneErrorDomain && info.GetReason() == reason && len(info.GetMetadata()) == 0 {
			return true
		}
	}
	return false
}

// writeLocalProblem sets no-store problem headers and defaults Retry-After to one second only for ordinary 429 responses, preserving the distinct STT hint policy. The shared writer emits the strict body without upstream details.
// writeLocalProblem задаёт no-store заголовки ошибки и Retry-After одну секунду только для обычного 429, сохраняя отдельную политику подсказок STT. Общий writer выдаёт строгое тело без внешних подробностей.
func writeLocalProblem(writer http.ResponseWriter, statusCode int, code string, retryable bool) {
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.Header().Set("Cache-Control", "no-store")
	if statusCode == http.StatusTooManyRequests && code != errorprofile.TranscriptionRateLimited {
		writer.Header().Set("Retry-After", "1")
	}
	problemresponse.Write(writer, statusCode, code, retryable)
}

// WriteLocalProblem exposes the same unit-local HTTP problem projection to sibling callers; it does not classify arbitrary errors or make retry decisions itself.
// WriteLocalProblem предоставляет соседним вызывающим ту же локальную HTTP-проекцию ошибки, не классифицируя произвольные ошибки и не выбирая повтор самостоятельно.
func WriteLocalProblem(writer http.ResponseWriter, statusCode int, code string, retryable bool) {
	writeLocalProblem(writer, statusCode, code, retryable)
}
