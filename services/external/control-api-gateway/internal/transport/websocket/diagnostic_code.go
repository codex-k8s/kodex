package websockettransport

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// snapshotDiagnosticGRPCCode preserves a canonical gRPC status and clamps out-of-range codes to Unknown. It returns only a closed code, never error text, and does not change the error returned to the caller.
// snapshotDiagnosticGRPCCode сохраняет канонический gRPC-статус, заменяя код вне диапазона на Unknown. Возвращается только закрытый код без текста ошибки; исходная ошибка для вызывающего не меняется.
func snapshotDiagnosticGRPCCode(err error) codes.Code {
	code := status.Code(err)
	if code < codes.OK || code > codes.Unauthenticated {
		code = codes.Unknown
	}
	return code
}
