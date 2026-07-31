package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CompleteProcess завершает процесс только по авторитетному terminal-ходу.
func (server *Server) CompleteProcess(
	ctx context.Context,
	request *controlplanev1.CompleteProcessRequest,
) (*controlplanev1.CompleteProcessResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_CompleteProcess_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	changed, err := server.service.CompleteProcess(ctx, resource.CompleteProcessInput{
		Principal:        principal,
		IdempotencyKey:   request.GetIdempotencyKey(),
		ProcessRunID:     request.GetProcessRunId(),
		ExpectedVersion:  request.GetExpectedVersion(),
		TerminalState:    fromProtoState(request.GetTerminalState()),
		Outcome:          request.GetOutcome(),
		ResultArtifactID: request.GetResultArtifactId(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CompleteProcessResponse{ProcessRun: encoded}, nil
}

// ListOutboxFailures возвращает bounded terminal metadata без payload.
func (server *Server) ListOutboxFailures(
	ctx context.Context,
	request *controlplanev1.ListOutboxFailuresRequest,
) (*controlplanev1.ListOutboxFailuresResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ListOutboxFailures_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := pageSize(request.GetPageSize())
	failures, err := server.service.ListOutboxFailures(ctx, resource.ListOutboxFailuresInput{
		Principal: principal, AfterEventID: request.GetPageToken(), Limit: limit,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListOutboxFailuresResponse{
		Failures: make([]*controlplanev1.OutboxFailure, 0, len(failures)),
	}
	for _, failure := range failures {
		response.Failures = append(response.Failures, toProtoOutboxFailure(failure))
	}
	if len(failures) == limit {
		response.NextPageToken = failures[len(failures)-1].EventID
	}
	return response, nil
}

// RepairOutboxEvent повторно открывает только exact terminal predecessor.
func (server *Server) RepairOutboxEvent(
	ctx context.Context,
	request *controlplanev1.RepairOutboxEventRequest,
) (*controlplanev1.RepairOutboxEventResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_RepairOutboxEvent_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	failure, err := server.service.RepairOutboxEvent(ctx, resource.RepairOutboxEventInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		EventID: request.GetEventId(), ExpectedSequence: request.GetExpectedSequence(),
		ExpectedAttempts: request.GetExpectedAttempts(), ReasonCode: request.GetReasonCode(),
		EvidenceSHA256: request.GetEvidenceSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.RepairOutboxEventResponse{
		Failure: toProtoOutboxFailure(failure),
	}, nil
}

func toProtoOutboxFailure(failure domainrepo.OutboxFailure) *controlplanev1.OutboxFailure {
	return &controlplanev1.OutboxFailure{
		EventId: failure.EventID, OrderingKey: failure.OrderingKey,
		EventSequence: failure.EventSequence, EventName: failure.EventName,
		AggregateId: failure.AggregateID, Attempts: failure.Attempts,
		RepairCount: failure.RepairCount, LastErrorClass: failure.LastErrorClass,
		OccurredAt: timestamppb.New(failure.OccurredAt),
		UpdatedAt:  timestamppb.New(failure.UpdatedAt),
	}
}
