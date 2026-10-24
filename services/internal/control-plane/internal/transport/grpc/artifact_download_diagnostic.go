package grpc

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"google.golang.org/grpc/status"
)

// artifactDownloadFailure keeps the public gRPC status unchanged while carrying an unwrap-visible
// artifact-read diagnostic only inside the process.
// artifactDownloadFailure сохраняет публичный gRPC status, передавая доступную через unwrap
// диагностику чтения артефакта только внутри процесса.
type artifactDownloadFailure struct {
	rpcStatus *status.Status
	cause     error
}

// Error returns the existing gRPC error text rather than serializing the internal diagnostic
// cause.
// Error возвращает прежний текст gRPC-ошибки без сериализации внутренней диагностической причины.
func (err *artifactDownloadFailure) Error() string { return err.rpcStatus.Err().Error() }

// GRPCStatus returns the original status including code and wire details without adding internal
// observer metadata.
// GRPCStatus возвращает исходный status с кодом и wire details без добавления внутренних
// метаданных observer.
func (err *artifactDownloadFailure) GRPCStatus() *status.Status { return err.rpcStatus }

// Unwrap exposes the internal diagnostic cause to in-process error inspection without changing the
// wire status.
// Unwrap открывает внутреннюю диагностическую причину для анализа ошибок внутри процесса без
// изменения wire status.
func (err *artifactDownloadFailure) Unwrap() error { return err.cause }

// artifactDownloadTransportError applies ordinary error mapping and wraps only recognized
// artifact-read diagnostics for the existing server observer.
// artifactDownloadTransportError применяет обычное преобразование ошибок и оборачивает только
// распознанную диагностику artifact-read для существующего server observer.
func artifactDownloadTransportError(err error) error {
	mapped := transportError(err)
	diagnostic, ok := artifactread.From(err)
	if !ok {
		return mapped
	}
	return &artifactDownloadFailure{rpcStatus: status.Convert(mapped), cause: artifactread.WithFailure(err, diagnostic)}
}
