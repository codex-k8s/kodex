package grpc

import (
	"context"
	"time"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) ManageAccessResource(
	ctx context.Context,
	request *controlplanev1.ManageAccessResourceRequest,
) (*controlplanev1.ManageAccessResourceResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ManageAccessResource_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	kind := fromProtoKind(request.GetKind())
	action := trimEnum(request.GetAction().String(), "ADMINISTRATIVE_ACTION_")
	var changed resourceResult
	switch action {
	case "CREATE", "UPDATE":
		spec, castErr := fromProtoSpec(request.GetSpec())
		if castErr != nil || spec.Kind() != kind {
			return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
		}
		if action == "CREATE" {
			changed.resource, err = server.service.Create(ctx, resource.CreateInput{
				Principal:      principal,
				IdempotencyKey: request.GetIdempotencyKey(),
				Kind:           kind,
				Name:           request.GetName(),
				Spec:           spec,
				Administrative: true,
			})
		} else {
			changed.resource, err = server.service.Update(ctx, resource.UpdateInput{
				Principal:       principal,
				IdempotencyKey:  request.GetIdempotencyKey(),
				ResourceID:      request.GetResourceId(),
				ExpectedVersion: request.GetExpectedVersion(),
				Name:            request.GetName(),
				Spec:            spec,
				Administrative:  true,
			})
		}
	case "ACTIVATE", "PAUSE", "ARCHIVE":
		state := map[string]enum.State{
			"ACTIVATE": enum.StateActive,
			"PAUSE":    enum.StatePaused,
			"ARCHIVE":  enum.StateArchived,
		}[action]
		changed.resource, err = server.service.Transition(ctx, resource.TransitionInput{
			Principal:       principal,
			IdempotencyKey:  request.GetIdempotencyKey(),
			ResourceID:      request.GetResourceId(),
			ExpectedVersion: request.GetExpectedVersion(),
			Target:          state,
			ReasonCode:      "administrative_action",
			Administrative:  true,
		})
	case "DELETE":
		changed.resource, err = server.service.Delete(ctx, resource.DeleteInput{
			Principal:       principal,
			IdempotencyKey:  request.GetIdempotencyKey(),
			ResourceID:      request.GetResourceId(),
			ExpectedVersion: request.GetExpectedVersion(),
			Administrative:  true,
		})
	default:
		err = errs.ErrInvalidInput
	}
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed.resource)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageAccessResourceResponse{Resource: encoded}, nil
}

type resourceResult struct {
	resource entity.Resource
}

func (server *Server) SearchResources(
	ctx context.Context,
	request *controlplanev1.SearchResourcesRequest,
) (*controlplanev1.SearchResourcesResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_SearchResources_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := pageSize(request.GetPageSize())
	states := make([]enum.State, 0, len(request.GetStates()))
	for _, state := range request.GetStates() {
		states = append(states, fromProtoState(state))
	}
	found, err := server.service.Search(ctx, resource.SearchInput{
		Principal: principal,
		Filter: query.ResourceSearch{
			Kind:    fromProtoKind(request.GetKind()),
			Query:   request.GetQuery(),
			States:  states,
			AfterID: request.GetPageToken(),
			Limit:   limit,
		},
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.SearchResourcesResponse{
		Resources: make([]*controlplanev1.Resource, 0, len(found)),
	}
	for _, item := range found {
		encoded, err := toProtoResource(item)
		if err != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
		}
		response.Resources = append(response.Resources, encoded)
	}
	if len(found) == limit {
		response.NextPageToken = found[len(found)-1].ID
	}
	return response, nil
}

func (server *Server) ListAuditEvents(
	ctx context.Context,
	request *controlplanev1.ListAuditEventsRequest,
) (*controlplanev1.ListAuditEventsResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ListAuditEvents_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := pageSize(request.GetPageSize())
	found, err := server.service.ListAudit(ctx, resource.ListAuditInput{
		Principal: principal,
		Filter: query.AuditFilter{
			ResourceKind: fromProtoKind(request.GetResourceKind()),
			ResourceID:   request.GetResourceId(),
			Action:       request.GetAction(),
			AfterID:      request.GetPageToken(),
			Limit:        limit,
		},
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListAuditEventsResponse{
		Events: make([]*controlplanev1.AuditEvent, 0, len(found)),
	}
	for _, item := range found {
		response.Events = append(response.Events, &controlplanev1.AuditEvent{
			Id:              item.ID,
			Action:          item.Action,
			ResourceId:      item.ResourceID,
			ResourceKind:    toProtoKind(enum.Kind(item.ResourceKind)),
			ResourceVersion: item.ResourceVersion,
			Outcome:         item.Outcome,
			ActorId:         item.ActorID,
			CorrelationId:   item.CorrelationID,
			PolicyRevision:  item.PolicyRevision,
			OccurredAt:      timestamppb.New(item.OccurredAt),
		})
	}
	if len(found) == limit {
		response.NextPageToken = found[len(found)-1].ID
	}
	return response, nil
}

func (server *Server) ListTombstones(
	ctx context.Context,
	request *controlplanev1.ListTombstonesRequest,
) (*controlplanev1.ListTombstonesResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ListTombstones_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	limit := pageSize(request.GetPageSize())
	found, err := server.service.ListTombstones(ctx, resource.ListTombstonesInput{
		Principal: principal,
		Filter: query.TombstoneFilter{
			Kind:    fromProtoKind(request.GetKind()),
			AfterID: request.GetPageToken(),
			Limit:   limit,
		},
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListTombstonesResponse{
		Tombstones: make([]*controlplanev1.ResourceTombstone, 0, len(found)),
	}
	for _, item := range found {
		response.Tombstones = append(
			response.Tombstones,
			&controlplanev1.ResourceTombstone{
				ResourceId:       item.ResourceID,
				Kind:             toProtoKind(item.Kind),
				Version:          item.Version,
				ProjectionSha256: item.ProjectionSHA256,
				DeletedAt:        timestamppb.New(item.DeletedAt),
			},
		)
	}
	if len(found) == limit {
		response.NextPageToken = found[len(found)-1].ResourceID
	}
	return response, nil
}

func (server *Server) GetDiagnostics(
	ctx context.Context,
	_ *controlplanev1.GetDiagnosticsRequest,
) (*controlplanev1.GetDiagnosticsResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_GetDiagnostics_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	found, err := server.service.Diagnostics(ctx, resource.DiagnosticsInput{
		Principal: principal,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.GetDiagnosticsResponse{
		SchemaVersion:              found.SchemaVersion,
		PendingOutboxEvents:        found.PendingOutboxEvents,
		TerminalOutboxEvents:       found.TerminalOutboxEvents,
		OldestPendingAge:           durationpb.New(found.OldestPendingAge),
		ActiveTurnLeases:           found.ActiveTurnLeases,
		QueuedScheduleOccurrences:  found.QueuedScheduleOccurrences,
		RuntimePrincipalStatus:     found.RuntimePrincipalStatus,
		RuntimePrincipalGeneration: found.RuntimePrincipalGeneration,
	}, nil
}

func (server *Server) RenewTurn(
	ctx context.Context,
	request *controlplanev1.RenewTurnRequest,
) (*controlplanev1.RenewTurnResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_RenewTurn_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	renewed, err := server.service.RenewTurn(ctx, resource.RenewTurnInput{
		Principal:           principal,
		IdempotencyKey:      request.GetIdempotencyKey(),
		TurnID:              request.GetTurnId(),
		LeaseToken:          request.GetLeaseToken(),
		ExpectedVersion:     request.GetExpectedVersion(),
		Attempt:             request.GetAttempt(),
		AuthorityGeneration: request.GetAuthorityGeneration(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(renewed.Turn)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RenewTurnResponse{
		Turn:                encoded,
		LeaseToken:          renewed.LeaseToken,
		LeaseExpiresAt:      timestamppb.New(renewed.LeaseExpiresAt),
		Attempt:             renewed.Attempt,
		AuthorityGeneration: renewed.AuthorityGeneration,
	}, nil
}

func (server *Server) RetryTurn(
	ctx context.Context,
	request *controlplanev1.RetryTurnRequest,
) (*controlplanev1.RetryTurnResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_RetryTurn_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	turn, err := server.service.RetryTurn(ctx, resource.RetryTurnInput{
		Principal:       principal,
		IdempotencyKey:  request.GetIdempotencyKey(),
		TurnID:          request.GetTurnId(),
		ExpectedVersion: request.GetExpectedVersion(),
		ReasonCode:      request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(turn)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RetryTurnResponse{Turn: encoded}, nil
}

func (server *Server) CancelTurn(
	ctx context.Context,
	request *controlplanev1.CancelTurnRequest,
) (*controlplanev1.CancelTurnResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_CancelTurn_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	turn, err := server.service.CancelTurn(ctx, resource.CancelTurnInput{
		Principal:       principal,
		IdempotencyKey:  request.GetIdempotencyKey(),
		TurnID:          request.GetTurnId(),
		ExpectedVersion: request.GetExpectedVersion(),
		ReasonCode:      request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(turn)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CancelTurnResponse{Turn: encoded}, nil
}

func (server *Server) ManageSchedule(
	ctx context.Context,
	request *controlplanev1.ManageScheduleRequest,
) (*controlplanev1.ManageScheduleResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ManageSchedule_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	var spec entity.ScheduleSpec
	if request.GetSpec() != nil {
		decoded, castErr := fromProtoSpec(&controlplanev1.ResourceSpec{
			Value: &controlplanev1.ResourceSpec_Schedule{Schedule: request.GetSpec()},
		})
		var ok bool
		spec, ok = decoded.(entity.ScheduleSpec)
		if castErr != nil || !ok {
			return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
		}
	}
	changed, err := server.service.ManageSchedule(ctx, resource.ManageScheduleInput{
		Principal:           principal,
		IdempotencyKey:      request.GetIdempotencyKey(),
		Action:              trimEnum(request.GetAction().String(), "ADMINISTRATIVE_ACTION_"),
		ScheduleID:          request.GetScheduleId(),
		ExpectedVersion:     request.GetExpectedVersion(),
		Name:                request.GetName(),
		Spec:                spec,
		DetachGitManagement: request.GetDetachGitManagement(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageScheduleResponse{Schedule: encoded}, nil
}

func (server *Server) RunScheduleNow(
	ctx context.Context,
	request *controlplanev1.RunScheduleNowRequest,
) (*controlplanev1.RunScheduleNowResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_RunScheduleNow_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	occurrence, err := server.service.RunScheduleNow(ctx, resource.RunScheduleNowInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
		ScheduleID: request.GetScheduleId(), ExpectedVersion: request.GetExpectedVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.RunScheduleNowResponse{Occurrence: toProtoOccurrence(occurrence)}, nil
}

func (server *Server) ClaimScheduleOccurrence(
	ctx context.Context,
	request *controlplanev1.ClaimScheduleOccurrenceRequest,
) (*controlplanev1.ClaimScheduleOccurrenceResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ClaimScheduleOccurrence_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	claimed, err := server.service.ClaimScheduleOccurrence(
		ctx,
		resource.ClaimScheduleOccurrenceInput{
			Principal:      principal,
			IdempotencyKey: request.GetIdempotencyKey(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	disposition := toProtoScheduleOccurrenceClaimDisposition(claimed.Disposition)
	if disposition == controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_UNSPECIFIED {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	response := &controlplanev1.ClaimScheduleOccurrenceResponse{
		ProjectId: claimed.ProjectID, Disposition: disposition,
	}
	if claimed.Disposition != resource.ScheduleOccurrenceClaimRetired {
		response.Occurrence = toProtoOccurrence(claimed.Occurrence)
		response.MaterializationCapability = claimed.MaterializationCapability
		response.MaterializationIdempotencyKey = claimed.MaterializationIdempotencyKey
		response.CapabilityExpiresAt = timestamppb.New(claimed.CapabilityExpiresAt)
	}
	return response, nil
}

func toProtoScheduleOccurrenceClaimDisposition(
	disposition resource.ScheduleOccurrenceClaimDisposition,
) controlplanev1.ScheduleOccurrenceClaimDisposition {
	switch disposition {
	case resource.ScheduleOccurrenceClaimReserved:
		return controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_RESERVED
	case resource.ScheduleOccurrenceClaimMaterialized:
		return controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_MATERIALIZED
	case resource.ScheduleOccurrenceClaimRetired:
		return controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_RETIRED
	default:
		return controlplanev1.ScheduleOccurrenceClaimDisposition_SCHEDULE_OCCURRENCE_CLAIM_DISPOSITION_UNSPECIFIED
	}
}

func (server *Server) MaterializeScheduleOccurrence(
	ctx context.Context,
	request *controlplanev1.MaterializeScheduleOccurrenceRequest,
) (*controlplanev1.MaterializeScheduleOccurrenceResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_MaterializeScheduleOccurrence_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	materialized, err := server.service.MaterializeScheduleOccurrence(ctx,
		resource.MaterializeScheduleOccurrenceInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			OccurrenceID: request.GetOccurrenceId(), ProjectID: request.GetProjectId(),
			ExpectedAttempt:           request.GetExpectedAttempt(),
			MaterializationCapability: request.GetMaterializationCapability(),
		})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.MaterializeScheduleOccurrenceResponse{
		Occurrence:           toProtoOccurrence(materialized.Occurrence),
		CompletionCapability: materialized.CompletionCapability,
	}, nil
}

func (server *Server) CompleteScheduleOccurrence(
	ctx context.Context,
	request *controlplanev1.CompleteScheduleOccurrenceRequest,
) (*controlplanev1.CompleteScheduleOccurrenceResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_CompleteScheduleOccurrence_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	completed, err := server.service.CompleteScheduleOccurrence(
		ctx,
		resource.CompleteScheduleOccurrenceInput{
			Principal:            principal,
			IdempotencyKey:       request.GetIdempotencyKey(),
			OccurrenceID:         request.GetOccurrenceId(),
			CompletionCapability: request.GetCompletionCapability(),
			ExpectedAttempt:      request.GetExpectedAttempt(),
			ProjectID:            request.GetProjectId(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.CompleteScheduleOccurrenceResponse{
		Occurrence: toProtoOccurrence(completed),
	}, nil
}

func (server *Server) ResolveScheduleRecovery(
	ctx context.Context,
	request *controlplanev1.ResolveScheduleRecoveryRequest,
) (*controlplanev1.ResolveScheduleRecoveryResponse, error) {
	principal, err := authorization.Principal(
		ctx, controlplanev1.ControlPlaneService_ResolveScheduleRecovery_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	action := map[controlplanev1.ScheduleRecoveryAction]string{
		controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_REPAIR: "REPAIR",
		controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_CANCEL: "CANCEL",
		controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_SKIP:   "SKIP",
	}[request.GetAction()]
	recovered, err := server.service.ResolveScheduleRecovery(ctx,
		resource.ResolveScheduleRecoveryInput{
			Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
			ScheduleID: request.GetScheduleId(), OccurrenceID: request.GetOccurrenceId(), ExpectedVersion: request.GetExpectedVersion(),
			ExpectedAttempt: request.GetExpectedAttempt(), Action: action,
			EvidenceSHA256: request.GetEvidenceSha256(), ReasonCode: request.GetReasonCode(),
		})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ResolveScheduleRecoveryResponse{Occurrence: toProtoOccurrence(recovered)}, nil
}

func (server *Server) CancelScheduleOccurrence(
	ctx context.Context,
	request *controlplanev1.CancelScheduleOccurrenceRequest,
) (*controlplanev1.CancelScheduleOccurrenceResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_CancelScheduleOccurrence_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	cancelled, err := server.service.CancelScheduleOccurrence(
		ctx,
		resource.CancelScheduleOccurrenceInput{
			Principal:       principal,
			IdempotencyKey:  request.GetIdempotencyKey(),
			OccurrenceID:    request.GetOccurrenceId(),
			ExpectedAttempt: request.GetExpectedAttempt(),
			ReasonCode:      request.GetReasonCode(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.CancelScheduleOccurrenceResponse{
		Occurrence: toProtoOccurrence(cancelled),
	}, nil
}

func (server *Server) ListScheduleOccurrences(
	ctx context.Context,
	request *controlplanev1.ListScheduleOccurrencesRequest,
) (*controlplanev1.ListScheduleOccurrencesResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ListScheduleOccurrences_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	states := make([]string, 0, len(request.GetStates()))
	for _, state := range request.GetStates() {
		states = append(states, occurrenceStateString(state))
	}
	limit := pageSize(request.GetPageSize())
	found, err := server.service.ListScheduleOccurrences(
		ctx,
		resource.ListScheduleOccurrencesInput{
			Principal: principal,
			Filter: query.ScheduleOccurrenceFilter{
				ScheduleID: request.GetScheduleId(),
				States:     states,
				AfterID:    request.GetPageToken(),
				Limit:      limit,
			},
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.ListScheduleOccurrencesResponse{
		Occurrences: make([]*controlplanev1.ScheduleOccurrence, 0, len(found)),
	}
	for _, occurrence := range found {
		response.Occurrences = append(
			response.Occurrences,
			toProtoOccurrence(occurrence),
		)
	}
	if len(found) == limit {
		response.NextPageToken = found[len(found)-1].ID
	}
	return response, nil
}

func (server *Server) StartProcess(
	ctx context.Context,
	request *controlplanev1.StartProcessRequest,
) (*controlplanev1.StartProcessResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_StartProcess_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	started, err := server.service.StartProcess(ctx, resource.StartProcessInput{
		Principal:        principal,
		IdempotencyKey:   request.GetIdempotencyKey(),
		Name:             request.GetName(),
		ParentProcessID:  request.GetParentProcessRunId(),
		PlaybookRef:      request.GetPlaybookRef(),
		PolicyRevision:   request.GetPolicyRevision(),
		RootTriggerRef:   request.GetRootTriggerRef(),
		RootSessionID:    request.GetRootSessionId(),
		RootTurnID:       request.GetRootTurnId(),
		RootAttempt:      request.GetRootAttempt(),
		InputArtifactID:  request.GetInputArtifactId(),
		LaunchingTurnID:  request.GetLaunchingTurnId(),
		LaunchingAttempt: request.GetLaunchingAttempt(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(started)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.StartProcessResponse{ProcessRun: encoded}, nil
}

func (server *Server) CancelProcess(
	ctx context.Context,
	request *controlplanev1.CancelProcessRequest,
) (*controlplanev1.CancelProcessResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_CancelProcess_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	cancelled, err := server.service.CancelProcess(ctx, resource.CancelProcessInput{
		Principal:       principal,
		IdempotencyKey:  request.GetIdempotencyKey(),
		ProcessRunID:    request.GetProcessRunId(),
		ExpectedVersion: request.GetExpectedVersion(),
		ReasonCode:      request.GetReasonCode(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(cancelled)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.CancelProcessResponse{ProcessRun: encoded}, nil
}

func (server *Server) RequestOwnerGate(
	ctx context.Context,
	request *controlplanev1.RequestOwnerGateRequest,
) (*controlplanev1.RequestOwnerGateResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_RequestOwnerGate_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	expiresAt, err := requiredTime(request.GetExpiresAt())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	created, err := server.service.RequestOwnerGate(ctx, resource.RequestOwnerGateInput{
		Principal:              principal,
		IdempotencyKey:         request.GetIdempotencyKey(),
		ProcessRunID:           request.GetProcessRunId(),
		ProcessExpectedVersion: request.GetProcessExpectedVersion(),
		SessionID:              request.GetSessionId(),
		TurnID:                 request.GetTurnId(),
		Attempt:                request.GetAttempt(),
		ResultArtifactID:       request.GetResultArtifactId(),
		ExpiresAt:              expiresAt,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	gate, err := toProtoResource(created.OwnerGate)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	process, err := toProtoResource(created.Process)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RequestOwnerGateResponse{
		OwnerGate:  gate,
		ProcessRun: process,
	}, nil
}

func (server *Server) RegisterArtifact(
	ctx context.Context,
	request *controlplanev1.RegisterArtifactRequest,
) (*controlplanev1.RegisterArtifactResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_RegisterArtifact_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	decoded, err := fromProtoSpec(&controlplanev1.ResourceSpec{
		Value: &controlplanev1.ResourceSpec_Artifact{Artifact: request.GetSpec()},
	})
	spec, ok := decoded.(entity.ArtifactSpec)
	if err != nil || !ok {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	artifact, err := server.service.RegisterArtifact(ctx, resource.RegisterArtifactInput{
		Principal:      principal,
		IdempotencyKey: request.GetIdempotencyKey(),
		Name:           request.GetName(),
		ParentID:       request.GetParentId(),
		Spec:           spec,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(artifact)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RegisterArtifactResponse{Artifact: encoded}, nil
}

func (server *Server) RecordArtifactScan(
	ctx context.Context,
	request *controlplanev1.RecordArtifactScanRequest,
) (*controlplanev1.RecordArtifactScanResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_RecordArtifactScan_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	artifact, err := server.service.RecordArtifactScan(
		ctx,
		resource.RecordArtifactScanInput{
			Principal:       principal,
			IdempotencyKey:  request.GetIdempotencyKey(),
			ArtifactID:      request.GetArtifactId(),
			ExpectedVersion: request.GetExpectedVersion(),
			TargetState: occurrenceOrArtifactState(
				request.GetTargetState().String(),
				"ARTIFACT_SCAN_STATE_",
			),
			ScanPolicyRevision: request.GetScanPolicyRevision(),
			EvidenceSHA256:     request.GetScanEvidenceSha256(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(artifact)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RecordArtifactScanResponse{Artifact: encoded}, nil
}

func pageSize(input uint32) int {
	if input == 0 {
		return 50
	}
	return int(input)
}

func toProtoOccurrence(
	occurrence domainrepo.ScheduleOccurrence,
) *controlplanev1.ScheduleOccurrence {
	result := &controlplanev1.ScheduleOccurrence{
		ScheduleId:           occurrence.ScheduleID,
		ScheduledFor:         timestamppb.New(occurrence.ScheduledFor),
		OccurrenceId:         occurrence.ID,
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
		Coalesce:                        occurrence.Coalesce,
		State:                           occurrenceState(occurrence.State),
		Attempt:                         occurrence.Attempt,
		ClaimantWorkloadId:              occurrence.ClaimantWorkloadID,
		AuthorityGeneration:             occurrence.AuthorityGeneration,
		AvailableAt:                     timestamppb.New(occurrence.AvailableAt),
		Outcome:                         occurrence.Outcome,
		ExecutionSessionId:              occurrence.ExecutionSessionID,
		ExecutionSessionVersion:         occurrence.ExecutionSessionVersion,
		ExecutionTurnId:                 occurrence.ExecutionTurnID,
		ExecutionTurnVersion:            occurrence.ExecutionTurnVersion,
		ExecutionProcessRunId:           occurrence.ExecutionProcessRunID,
		ExecutionProcessVersion:         occurrence.ExecutionProcessVersion,
		ExecutionRuntimeRevisionId:      occurrence.ExecutionRuntimeRevisionID,
		ExecutionRuntimeRevisionVersion: occurrence.ExecutionRuntimeRevisionVersion,
		Version:                         occurrence.Version,
		RecoveryEvidenceSha256:          occurrence.RecoveryEvidenceSHA256,
	}
	if !occurrence.LeaseExpiresAt.IsZero() &&
		!occurrence.LeaseExpiresAt.Equal(time.Unix(0, 0)) {
		result.LeaseExpiresAt = timestamppb.New(occurrence.LeaseExpiresAt)
	}
	if occurrence.State == "RECOVERY_BLOCKED" && occurrence.RecoveryEvidenceSHA256 != "" {
		result.RecoveryActions = []controlplanev1.ScheduleRecoveryAction{
			controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_REPAIR,
			controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_CANCEL,
			controlplanev1.ScheduleRecoveryAction_SCHEDULE_RECOVERY_ACTION_SKIP,
		}
	}
	return result
}

func occurrenceState(value string) controlplanev1.ScheduleOccurrenceState {
	return controlplanev1.ScheduleOccurrenceState(
		controlplanev1.ScheduleOccurrenceState_value["SCHEDULE_OCCURRENCE_STATE_"+value],
	)
}

func occurrenceStateString(value controlplanev1.ScheduleOccurrenceState) string {
	return occurrenceOrArtifactState(
		value.String(),
		"SCHEDULE_OCCURRENCE_STATE_",
	)
}

func occurrenceOrArtifactState(value, prefix string) string {
	return trimEnum(value, prefix)
}
