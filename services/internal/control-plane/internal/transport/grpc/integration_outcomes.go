package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetExecutionIntegrationOutcomes rejects unknown wire fields and projects only a
// successful owner-committed read. No actor or project can be assigned by this request.
// GetExecutionIntegrationOutcomes отвергает unknown wire fields и выдаёт лишь успешно
// завершённое owner-чтение; запрос не может назначить actor либо project.
func (server *Server) GetExecutionIntegrationOutcomes(ctx context.Context, request *cp.GetExecutionIntegrationOutcomesRequest) (*cp.GetExecutionIntegrationOutcomesResponse, error) {
	return queryResponse(ctx, cp.RuntimeWorkService_GetExecutionIntegrationOutcomes_FullMethodName, func(p value.Principal) (*cp.GetExecutionIntegrationOutcomesResponse, error) {
		if request == nil || len(request.ProtoReflect().GetUnknown()) != 0 {
			return nil, errs.ErrInvalid
		}
		result, err := server.service.GetExecutionIntegrationOutcomes(ctx, p, query.ExecutionIntegrationOutcomes{LeaseRef: request.LeaseRef, Fence: request.Fence, Generation: request.Generation, SourceInvocationRef: request.SourceInvocationRef, SourceSHA256: request.SourceSha256, OffsetBytes: request.OffsetBytes, MaximumBytes: request.MaximumBytes})
		if err != nil {
			return nil, err
		}
		return &cp.GetExecutionIntegrationOutcomesResponse{SourceInvocationRef: result.SourceInvocationRef, SourceSha256: result.SourceSHA256, SizeBytes: result.SizeBytes, OutcomesCount: result.OutcomesCount, OffsetBytes: result.OffsetBytes, NextOffsetBytes: result.NextOffsetBytes, PageSha256: result.PageSHA256, Text: result.Text, Eof: result.EOF}, nil
	})
}
