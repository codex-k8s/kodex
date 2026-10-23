package httptransport

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
)

// providerDeviceAuthorizationUnavailable is the public retryable code for transient device-authorization backend failures, without exposing provider response text.
// providerDeviceAuthorizationUnavailable — публичный повторяемый код временного отказа backend авторизации устройства без раскрытия ответа провайдера.
const providerDeviceAuthorizationUnavailable = "PROVIDER_DEVICE_AUTHORIZATION_UNAVAILABLE"

// writeProviderDeviceProblem maps Unavailable/DeadlineExceeded to a redacted retryable 503 with a one-second hint, delegating other errors to ordinary RPC mapping without initiating authorization.
// writeProviderDeviceProblem отображает Unavailable/DeadlineExceeded в скрывающий подробности повторяемый 503 с подсказкой одна секунда; остальные ошибки передаются обычному RPC-отображению без запуска авторизации.
func writeProviderDeviceProblem(writer http.ResponseWriter, err error) {
	if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
		writer.Header().Set("Retry-After", "1")
		writeLocalProblem(writer, http.StatusServiceUnavailable, providerDeviceAuthorizationUnavailable, true)
		return
	}
	writeRPCProblem(writer, err)
}
