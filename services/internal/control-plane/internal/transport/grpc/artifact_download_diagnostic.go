package grpc

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/observability/artifactread"
	"google.golang.org/grpc/status"
)

// GRPCStatus возвращает прежние code/text/details. Диагностика остаётся только
// внутри процесса для существующего server error observer, не в wire response.
type artifactDownloadFailure struct {
	rpcStatus *status.Status
	cause     error
}

func (err *artifactDownloadFailure) Error() string              { return err.rpcStatus.Err().Error() }
func (err *artifactDownloadFailure) GRPCStatus() *status.Status { return err.rpcStatus }
func (err *artifactDownloadFailure) Unwrap() error              { return err.cause }

func artifactDownloadTransportError(err error) error {
	mapped := transportError(err)
	diagnostic, ok := artifactread.From(err)
	if !ok {
		return mapped
	}
	return &artifactDownloadFailure{rpcStatus: status.Convert(mapped), cause: artifactread.WithFailure(err, diagnostic)}
}
