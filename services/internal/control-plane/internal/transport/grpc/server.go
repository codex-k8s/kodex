// Package grpc реализует строгий сгенерированный транспорт control-plane.
package grpc

import (
	"context"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/libs/go/integrationgatewayauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const readinessPermission = "controlplane.readiness.check"

// ReadinessState описывает фактические проверки зависимостей.
type ReadinessState struct {
	SchemaVersion  uint64
	AuthorityReady bool
	PostgresReady  bool
	RedisReady     bool
	OutboxReady    bool
}

// Readiness проверяет тот же промышленный путь, что используют RPC и ретранслятор.
type Readiness interface {
	Check(context.Context) (ReadinessState, error)
}

// Server реализует generated service без infrastructure orchestration.
type Server struct {
	controlplanev1.UnimplementedControlPlaneServiceServer
	service          *resource.Service
	readiness        Readiness
	transitionSigner grantSigner
}

type grantSigner interface {
	Sign(context.Context, integrationgatewayauth.Claims) (string, error)
}

// NewServer создаёт транспорт над доменным сервисом.
func NewServer(service *resource.Service, readiness Readiness, transitionSigner grantSigner) (*Server, error) {
	if service == nil || readiness == nil || transitionSigner == nil {
		return nil, errors.New("control-plane gRPC dependencies are required")
	}
	return &Server{service: service, readiness: readiness, transitionSigner: transitionSigner}, nil
}

const continuationGrantTTL = 8 * 24 * time.Hour

func (server *Server) CreateProject(
	ctx context.Context,
	request *controlplanev1.CreateProjectRequest,
) (*controlplanev1.CreateProjectResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_CreateProject_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	spec, err := fromProtoSpec(&controlplanev1.ResourceSpec{
		Value: &controlplanev1.ResourceSpec_Project{Project: request.GetSpec()},
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	created, err := server.service.Create(ctx, resource.CreateInput{
		Principal:      principal,
		IdempotencyKey: request.GetIdempotencyKey(),
		Kind:           enum.KindProject,
		Name:           request.GetName(),
		Spec:           spec,
		TenantProject:  true,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(created)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CreateProjectResponse{Project: encoded}, nil
}

func (server *Server) UpdateProject(
	ctx context.Context,
	request *controlplanev1.UpdateProjectRequest,
) (*controlplanev1.UpdateProjectResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_UpdateProject_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	spec, err := fromProtoSpec(&controlplanev1.ResourceSpec{
		Value: &controlplanev1.ResourceSpec_Project{Project: request.GetSpec()},
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	projectSpec, ok := spec.(entity.ProjectSpec)
	if !ok {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	updated, err := server.service.UpdateProject(ctx, resource.UpdateProjectInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ProjectID: request.GetProjectId(), ExpectedVersion: request.GetExpectedVersion(),
		Name: request.GetName(), Spec: projectSpec,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(updated)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.UpdateProjectResponse{Project: encoded}, nil
}

func (server *Server) DeleteProject(
	ctx context.Context,
	request *controlplanev1.DeleteProjectRequest,
) (*controlplanev1.DeleteProjectResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_DeleteProject_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	deleted, err := server.service.DeleteProject(ctx, resource.DeleteProjectInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ProjectID: request.GetProjectId(), ExpectedVersion: request.GetExpectedVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(deleted)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.DeleteProjectResponse{Project: encoded}, nil
}

func (server *Server) ListProjects(
	ctx context.Context,
	request *controlplanev1.ListProjectsRequest,
) (*controlplanev1.ListProjectsResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ListProjects_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	pageSize := int(request.GetPageSize())
	if pageSize == 0 {
		pageSize = 50
	}
	found, err := server.service.List(ctx, resource.ListInput{
		Principal: principal,
		Filter: query.ResourceFilter{
			Kind:    enum.KindProject,
			AfterID: request.GetPageToken(),
			Limit:   pageSize,
		},
		TenantProjects: true,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListProjectsResponse{
		Projects: make([]*controlplanev1.Resource, 0, len(found)),
	}
	for _, item := range found {
		encoded, err := toProtoResource(item)
		if err != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
		}
		response.Projects = append(response.Projects, encoded)
	}
	if len(found) == pageSize {
		response.NextPageToken = found[len(found)-1].ID
	}
	return response, nil
}

func (server *Server) CreateResource(
	ctx context.Context,
	request *controlplanev1.CreateResourceRequest,
) (*controlplanev1.CreateResourceResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_CreateResource_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	spec, err := fromProtoSpec(request.GetSpec())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	created, err := server.service.Create(ctx, resource.CreateInput{
		Principal:      principal,
		IdempotencyKey: request.GetIdempotencyKey(),
		Kind:           fromProtoKind(request.GetKind()),
		Name:           request.GetName(),
		ParentID:       request.GetParentId(),
		Spec:           spec,
		TenantProject:  false,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response, err := toProtoResource(created)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CreateResourceResponse{Resource: response}, nil
}

func (server *Server) UpdateResource(
	ctx context.Context,
	request *controlplanev1.UpdateResourceRequest,
) (*controlplanev1.UpdateResourceResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_UpdateResource_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	spec, err := fromProtoSpec(request.GetSpec())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	updated, err := server.service.Update(ctx, resource.UpdateInput{
		Principal:           principal,
		IdempotencyKey:      request.GetIdempotencyKey(),
		ResourceID:          request.GetResourceId(),
		ExpectedVersion:     request.GetExpectedVersion(),
		Name:                request.GetName(),
		Spec:                spec,
		DetachGitManagement: request.GetDetachGitManagement(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response, err := toProtoResource(updated)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.UpdateResourceResponse{Resource: response}, nil
}

func (server *Server) TransitionResource(
	ctx context.Context,
	request *controlplanev1.TransitionResourceRequest,
) (*controlplanev1.TransitionResourceResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_TransitionResource_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	updated, err := server.service.Transition(ctx, resource.TransitionInput{
		Principal:       principal,
		IdempotencyKey:  request.GetIdempotencyKey(),
		ResourceID:      request.GetResourceId(),
		ExpectedVersion: request.GetExpectedVersion(),
		Target:          fromProtoState(request.GetTargetState()),
		ReasonCode:      request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response, err := toProtoResource(updated)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.TransitionResourceResponse{Resource: response}, nil
}

func (server *Server) DeleteResource(
	ctx context.Context,
	request *controlplanev1.DeleteResourceRequest,
) (*controlplanev1.DeleteResourceResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_DeleteResource_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	deleted, err := server.service.Delete(ctx, resource.DeleteInput{
		Principal:       principal,
		IdempotencyKey:  request.GetIdempotencyKey(),
		ResourceID:      request.GetResourceId(),
		ExpectedVersion: request.GetExpectedVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response, err := toProtoResource(deleted)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.DeleteResourceResponse{Resource: response}, nil
}

func (server *Server) GetResource(
	ctx context.Context,
	request *controlplanev1.GetResourceRequest,
) (*controlplanev1.GetResourceResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_GetResource_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	found, err := server.service.Get(ctx, resource.GetInput{
		Principal:       principal,
		ResourceID:      request.GetResourceId(),
		Kind:            fromProtoKind(request.GetExpectedKind()),
		ExpectedVersion: request.GetExpectedVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response, err := toProtoResource(found)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.GetResourceResponse{Resource: response}, nil
}

func (server *Server) ListResources(
	ctx context.Context,
	request *controlplanev1.ListResourcesRequest,
) (*controlplanev1.ListResourcesResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ListResources_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	pageSize := int(request.GetPageSize())
	if pageSize == 0 {
		pageSize = 50
	}
	states := make([]enum.State, 0, len(request.GetStates()))
	for _, state := range request.GetStates() {
		states = append(states, fromProtoState(state))
	}
	found, err := server.service.List(ctx, resource.ListInput{
		Principal: principal,
		Filter: query.ResourceFilter{
			ParentID: request.GetParentId(),
			Kind:     fromProtoKind(request.GetKind()),
			States:   states,
			AfterID:  request.GetPageToken(),
			Limit:    pageSize,
		},
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListResourcesResponse{
		Resources: make([]*controlplanev1.Resource, 0, len(found)),
	}
	for _, item := range found {
		encoded, err := toProtoResource(item)
		if err != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
		}
		response.Resources = append(response.Resources, encoded)
	}
	if len(found) == pageSize {
		response.NextPageToken = found[len(found)-1].ID
	}
	return response, nil
}

func (server *Server) EnqueueTurn(
	ctx context.Context,
	request *controlplanev1.EnqueueTurnRequest,
) (*controlplanev1.EnqueueTurnResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_EnqueueTurn_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	turn, err := server.service.EnqueueTurn(ctx, resource.EnqueueTurnInput{
		Principal:        principal,
		IdempotencyKey:   request.GetIdempotencyKey(),
		SessionID:        request.GetSessionId(),
		SourceRef:        request.GetSourceRef(),
		PromptArtifactID: request.GetPromptArtifactId(),
		ProcessRunID:     request.GetProcessRunId(),
		InputArtifactIDs: request.GetInputArtifactIds(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(turn)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.EnqueueTurnResponse{Turn: encoded}, nil
}

func (server *Server) ClaimTurn(
	ctx context.Context,
	request *controlplanev1.ClaimTurnRequest,
) (*controlplanev1.ClaimTurnResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ClaimTurn_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	claimed, err := server.service.ClaimTurn(ctx, resource.ClaimTurnInput{
		Principal:      principal,
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(claimed.Turn)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ClaimTurnResponse{
		Turn:                encoded,
		LeaseToken:          claimed.LeaseToken,
		LeaseExpiresAt:      timestamppb.New(claimed.LeaseExpiresAt),
		Attempt:             claimed.Attempt,
		AuthorityGeneration: claimed.AuthorityGeneration,
	}, nil
}

func (server *Server) CompleteTurn(
	ctx context.Context,
	request *controlplanev1.CompleteTurnRequest,
) (*controlplanev1.CompleteTurnResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_CompleteTurn_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	turn, err := server.service.CompleteTurn(ctx, resource.CompleteTurnInput{
		Principal:           principal,
		IdempotencyKey:      request.GetIdempotencyKey(),
		TurnID:              request.GetTurnId(),
		LeaseToken:          request.GetLeaseToken(),
		ExpectedVersion:     request.GetExpectedVersion(),
		TerminalState:       fromProtoState(request.GetTerminalState()),
		Outcome:             request.GetOutcome(),
		ResultArtifactID:    request.GetResultArtifactId(),
		Attempt:             request.GetAttempt(),
		AuthorityGeneration: request.GetAuthorityGeneration(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(turn)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CompleteTurnResponse{Turn: encoded}, nil
}

func (server *Server) ClaimDueSchedules(
	ctx context.Context,
	request *controlplanev1.ClaimDueSchedulesRequest,
) (*controlplanev1.ClaimDueSchedulesResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ClaimDueSchedules_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	claimed, err := server.service.ClaimDueSchedules(
		ctx,
		resource.ClaimDueSchedulesInput{
			Principal:      principal,
			IdempotencyKey: request.GetIdempotencyKey(),
			Limit:          int(request.GetLimit()),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ClaimDueSchedulesResponse{
		Occurrences: make(
			[]*controlplanev1.ScheduleOccurrence,
			0,
			len(claimed.Occurrences),
		),
	}
	for _, occurrence := range claimed.Occurrences {
		response.Occurrences = append(
			response.Occurrences,
			&controlplanev1.ScheduleOccurrence{
				ScheduleId:           occurrence.ScheduleID,
				ScheduledFor:         timestamppb.New(occurrence.ScheduledFor),
				OccurrenceId:         occurrence.OccurrenceID,
				TargetResourceId:     occurrence.TargetResourceID,
				TargetKind:           toProtoKind(occurrence.TargetKind),
				TargetVersion:        occurrence.TargetVersion,
				EffectiveInputSha256: occurrence.EffectiveInputSHA256,
				PromptProfileId:      occurrence.PromptProfileID,
				PromptRevision:       occurrence.PromptRevision,
				RuntimeRevisionId:    occurrence.RuntimeRevisionID,
				SessionPolicy:        scheduleSessionPolicy(occurrence.SessionPolicy),
				RoomId:               occurrence.RoomID,
				NotificationPolicy:   scheduleNotificationPolicy(occurrence.NotificationPolicy),
				MaximumExecutionDuration: durationpb.New(
					occurrence.MaximumExecution,
				),
				Coalesce:    occurrence.Coalesce,
				State:       occurrenceState(occurrence.State),
				Attempt:     occurrence.Attempt,
				AvailableAt: timestamppb.New(occurrence.AvailableAt),
				Outcome:     occurrence.Outcome,
			},
		)
	}
	return response, nil
}

func (server *Server) ResolveOwnerGate(
	ctx context.Context,
	request *controlplanev1.ResolveOwnerGateRequest,
) (*controlplanev1.ResolveOwnerGateResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ResolveOwnerGate_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	resolved, err := server.service.ResolveOwnerGate(ctx, resource.ResolveOwnerGateInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		OwnerGateID: request.GetOwnerGateId(), ExpectedVersion: request.GetExpectedVersion(),
		Decision: ownerDecisionString(request.GetDecision()), Reason: request.GetReason(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encodedGate, err := toProtoResource(resolved.OwnerGate)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	encodedProcess, err := toProtoResource(resolved.Process)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ResolveOwnerGateResponse{
		OwnerGate:  encodedGate,
		ProcessRun: encodedProcess,
	}, nil
}

func (server *Server) CheckReadiness(
	ctx context.Context,
	_ *controlplanev1.CheckReadinessRequest,
) (*controlplanev1.CheckReadinessResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_CheckReadiness_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	if principal.Permission != readinessPermission {
		return nil, rpcError(principal.CorrelationID, errs.ErrPermissionDenied)
	}
	state, err := server.readiness.Check(ctx)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrUnavailable)
	}
	ready := state.AuthorityReady && state.PostgresReady &&
		state.RedisReady && state.OutboxReady
	return &controlplanev1.CheckReadinessResponse{
		Ready:          ready,
		SchemaVersion:  state.SchemaVersion,
		AuthorityReady: state.AuthorityReady,
		PostgresReady:  state.PostgresReady,
		RedisReady:     state.RedisReady,
		OutboxReady:    state.OutboxReady,
	}, nil
}

func rpcError(correlationID string, err error) error {
	code := codes.Internal
	reason := controlplanev1.ErrorReason_ERROR_REASON_INTERNAL
	message := "internal control-plane failure"
	safeCode := "INTERNAL"
	retryable := false
	switch {
	case errors.Is(err, errs.ErrInvalidInput):
		code, reason = codes.InvalidArgument, controlplanev1.ErrorReason_ERROR_REASON_INVALID_REQUEST
		message, safeCode = "control-plane request is invalid", "INVALID_REQUEST"
	case errors.Is(err, errs.ErrUnauthenticated):
		code, reason = codes.Unauthenticated, controlplanev1.ErrorReason_ERROR_REASON_UNAUTHENTICATED
		message, safeCode = "control-plane authentication required", "UNAUTHENTICATED"
	case errors.Is(err, errs.ErrPermissionDenied):
		code, reason = codes.PermissionDenied, controlplanev1.ErrorReason_ERROR_REASON_PERMISSION_DENIED
		message, safeCode = "control-plane permission denied", "PERMISSION_DENIED"
	case errors.Is(err, errs.ErrNotFound):
		code, reason = codes.NotFound, controlplanev1.ErrorReason_ERROR_REASON_NOT_FOUND
		message, safeCode = "control-plane resource not found", "NOT_FOUND"
	case errors.Is(err, errs.ErrVersionMismatch):
		code, reason = codes.Aborted, controlplanev1.ErrorReason_ERROR_REASON_VERSION_MISMATCH
		message, safeCode, retryable = "control-plane version mismatch", "VERSION_MISMATCH", true
	case errors.Is(err, errs.ErrIdempotencyConflict):
		code, reason = codes.AlreadyExists, controlplanev1.ErrorReason_ERROR_REASON_IDEMPOTENCY_CONFLICT
		message, safeCode = "control-plane idempotency conflict", "IDEMPOTENCY_CONFLICT"
	case errors.Is(err, errs.ErrStateConflict):
		code, reason = codes.FailedPrecondition, controlplanev1.ErrorReason_ERROR_REASON_STATE_CONFLICT
		message, safeCode = "control-plane state conflict", "STATE_CONFLICT"
	case errors.Is(err, errs.ErrUnavailable):
		code, reason = codes.Unavailable, controlplanev1.ErrorReason_ERROR_REASON_UNAVAILABLE
		message, safeCode, retryable = "control-plane dependency unavailable", "UNAVAILABLE", true
	}
	current := status.New(code, message)
	withDetail, detailErr := current.WithDetails(&controlplanev1.ErrorDetail{
		Reason:        reason,
		Code:          safeCode,
		CorrelationId: correlationID,
		Retryable:     retryable,
	})
	if detailErr != nil {
		return current.Err()
	}
	return withDetail.Err()
}

func ownerDecisionString(decision controlplanev1.OwnerGateDecision) string {
	return trimEnum(decision.String(), "OWNER_GATE_DECISION_")
}
