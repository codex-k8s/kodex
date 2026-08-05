package grpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type memoryCursor struct {
	ID             string  `json:"id"`
	TextRank       float32 `json:"textRank"`
	VectorDistance float32 `json:"vectorDistance"`
	VectorUsed     bool    `json:"vectorUsed"`
}

func (server *Server) ManageSession(
	ctx context.Context,
	request *controlplanev1.ManageSessionRequest,
) (*controlplanev1.ManageSessionResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ManageSession_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	changed, err := server.service.ManageSession(ctx, resource.ManageSessionInput{
		Principal:                            principal,
		IdempotencyKey:                       request.GetIdempotencyKey(),
		Action:                               trimEnum(request.GetAction().String(), "SESSION_ACTION_"),
		SessionID:                            request.GetSessionId(),
		ExpectedVersion:                      request.GetExpectedVersion(),
		Name:                                 request.GetName(),
		RoleID:                               request.GetRoleId(),
		ConversationID:                       request.GetConversationId(),
		ArchiveRef:                           request.GetArchiveRef(),
		ReasonCode:                           request.GetReasonCode(),
		PreferredProviderCredentialBindingID: request.GetPreferredProviderCredentialBindingId(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageSessionResponse{Session: encoded}, nil
}

func (server *Server) BindSessionMCP(
	ctx context.Context,
	request *controlplanev1.BindSessionMCPRequest,
) (*controlplanev1.BindSessionMCPResponse, error) {
	principal, err := authorization.Principal(ctx, controlplanev1.ControlPlaneService_BindSessionMCP_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	changed, err := server.service.BindSessionMCP(ctx, resource.BindSessionMCPInput{Principal: principal,
		IdempotencyKey: request.GetIdempotencyKey(), SessionID: request.GetSessionId(),
		AgentSessionKey: request.GetAgentSessionKey(),
		AgentSessionID:  request.GetAgentSessionId(), AgentSessionVersion: request.GetAgentSessionVersion(),
		AgentSessionBindingSHA256: request.GetAgentSessionBindingSha256(),
		ImmutableSecretRef:        request.GetImmutableSecretRef(), ProviderContentVersion: request.GetProviderContentVersion(),
		ContentSHA256: request.GetContentSha256()})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.BindSessionMCPResponse{Session: encoded}, nil
}

func (server *Server) ManageConversationLifecycle(
	ctx context.Context,
	request *controlplanev1.ManageConversationLifecycleRequest,
) (*controlplanev1.ManageConversationLifecycleResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ManageConversationLifecycle_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	changed, err := server.service.ManageConversationLifecycle(ctx, resource.ManageConversationLifecycleInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), ResourceID: request.GetResourceId(),
		Kind:   trimEnum(request.GetKind().String(), "CONVERSATION_LIFECYCLE_KIND_"),
		Action: trimEnum(request.GetAction().String(), "CONVERSATION_LIFECYCLE_ACTION_"),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageConversationLifecycleResponse{Resource: encoded}, nil
}

func (server *Server) ManageMemoryRecord(
	ctx context.Context,
	request *controlplanev1.ManageMemoryRecordRequest,
) (*controlplanev1.ManageMemoryRecordResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ManageMemoryRecord_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	changed, err := server.service.ManageMemoryRecord(ctx, resource.ManageMemoryRecordInput{
		Principal:       principal,
		IdempotencyKey:  request.GetIdempotencyKey(),
		Action:          trimEnum(request.GetAction().String(), "MEMORY_ACTION_"),
		MemoryRecordID:  request.GetMemoryRecordId(),
		ExpectedVersion: request.GetExpectedVersion(),
		Scope:           request.GetScope(),
		RoleID:          request.GetRoleId(),
		Title:           request.GetTitle(),
		Content:         request.GetContent(),
		ContentSHA256:   request.GetContentSha256(),
		Provenance:      request.GetProvenance(),
		Importance:      request.GetImportance(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageMemoryRecordResponse{MemoryRecord: encoded}, nil
}

func (server *Server) ManageWorkClaim(
	ctx context.Context,
	request *controlplanev1.ManageWorkClaimRequest,
) (*controlplanev1.ManageWorkClaimResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ManageWorkClaim_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	ttl, err := optionalDuration(request.GetTtl())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	changed, err := server.service.ManageWorkClaim(ctx, resource.ManageWorkClaimInput{
		Principal:       principal,
		IdempotencyKey:  request.GetIdempotencyKey(),
		Action:          trimEnum(request.GetAction().String(), "WORK_CLAIM_ACTION_"),
		WorkClaimID:     request.GetWorkClaimId(),
		ExpectedVersion: request.GetExpectedVersion(),
		ProcessRunID:    request.GetProcessRunId(),
		TurnID:          request.GetTurnId(),
		Summary:         request.GetSummary(),
		Domains:         request.GetDomains(),
		ResourceKeys:    request.GetResourceKeys(),
		TTL:             ttl,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ManageWorkClaimResponse{WorkClaim: encoded}, nil
}

func (server *Server) RecordOwnerGateDelivery(
	ctx context.Context,
	request *controlplanev1.RecordOwnerGateDeliveryRequest,
) (*controlplanev1.RecordOwnerGateDeliveryResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_RecordOwnerGateDelivery_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	changed, err := server.service.RecordOwnerGateDelivery(
		ctx,
		resource.RecordOwnerGateDeliveryInput{
			Principal:             principal,
			IdempotencyKey:        request.GetIdempotencyKey(),
			OwnerGateID:           request.GetOwnerGateId(),
			ExpectedVersion:       request.GetExpectedVersion(),
			DeliveryID:            request.GetDeliveryId(),
			DeliveryPayloadSHA256: request.GetDeliveryPayloadSha256(),
			DeliveryClaimToken:    request.GetDeliveryClaimToken(),
			DeliveryFence:         request.GetDeliveryFence(),
			MattermostPostID:      request.GetMattermostPostId(),
			MattermostChannelID:   request.GetMattermostChannelId(),
			MattermostRootPostID:  request.GetMattermostRootPostId(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(changed)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.RecordOwnerGateDeliveryResponse{OwnerGate: encoded}, nil
}

func (server *Server) ClaimOwnerGateDelivery(
	ctx context.Context,
	request *controlplanev1.ClaimOwnerGateDeliveryRequest,
) (*controlplanev1.ClaimOwnerGateDeliveryResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ClaimOwnerGateDelivery_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	claimed, err := server.service.ClaimOwnerGateDelivery(
		ctx,
		resource.ClaimOwnerGateDeliveryInput{
			Principal:      principal,
			IdempotencyKey: request.GetIdempotencyKey(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(claimed.OwnerGate)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ClaimOwnerGateDeliveryResponse{
		OwnerGate:              encoded,
		DeliveryClaimToken:     claimed.ClaimToken,
		DeliveryClaimExpiresAt: timestamppb.New(claimed.ExpiresAt),
	}, nil
}

func (server *Server) ExpireOwnerGate(
	ctx context.Context,
	request *controlplanev1.ExpireOwnerGateRequest,
) (*controlplanev1.ExpireOwnerGateResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_ExpireOwnerGate_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	expired, err := server.service.ExpireOwnerGate(
		ctx,
		resource.ExpireOwnerGateInput{
			Principal:      principal,
			IdempotencyKey: request.GetIdempotencyKey(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	gate, err := toProtoResource(expired.OwnerGate)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	process, err := toProtoResource(expired.Process)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.ExpireOwnerGateResponse{
		OwnerGate:  gate,
		ProcessRun: process,
	}, nil
}

func (server *Server) GetRuntimeRevision(
	ctx context.Context,
	request *controlplanev1.GetRuntimeRevisionRequest,
) (*controlplanev1.GetRuntimeRevisionResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_GetRuntimeRevision_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	found, err := server.service.GetRuntimeRevision(ctx, resource.GetRuntimeRevisionInput{
		Principal:         principal,
		RuntimeRevisionID: request.GetRuntimeRevisionId(),
		ExpectedVersion:   request.GetExpectedVersion(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	encoded, err := toProtoResource(found)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
	}
	return &controlplanev1.GetRuntimeRevisionResponse{RuntimeRevision: encoded}, nil
}

func (server *Server) RecordMemoryEmbedding(
	ctx context.Context,
	request *controlplanev1.RecordMemoryEmbeddingRequest,
) (*controlplanev1.RecordMemoryEmbeddingResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_RecordMemoryEmbedding_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	recorded, err := server.service.RecordMemoryEmbedding(
		ctx,
		resource.RecordMemoryEmbeddingInput{
			Principal:               principal,
			IdempotencyKey:          request.GetIdempotencyKey(),
			MemoryRecordID:          request.GetMemoryRecordId(),
			ExpectedResourceVersion: request.GetExpectedResourceVersion(),
			ContentSHA256:           request.GetContentSha256(),
		},
	)
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.RecordMemoryEmbeddingResponse{
		MemoryRecordId:   recorded.MemoryRecordID,
		ResourceVersion:  recorded.ResourceVersion,
		ProjectionSha256: recorded.ProjectionSHA256,
	}, nil
}

func (server *Server) SearchMemoryRecords(
	ctx context.Context,
	request *controlplanev1.SearchMemoryRecordsRequest,
) (*controlplanev1.SearchMemoryRecordsResponse, error) {
	principal, err := authorization.Principal(
		ctx,
		controlplanev1.ControlPlaneService_SearchMemoryRecords_FullMethodName,
	)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	cursor, err := decodeMemoryCursor(request.GetPageToken())
	if err != nil {
		return nil, rpcError(principal.CorrelationID, errs.ErrInvalidInput)
	}
	limit := pageSize(request.GetPageSize())
	found, err := server.service.SearchMemory(ctx, resource.SearchMemoryInput{
		Principal:           principal,
		Query:               request.GetQuery(),
		Scope:               request.GetScope(),
		RoleID:              request.GetRoleId(),
		AfterID:             cursor.ID,
		AfterTextRank:       cursor.TextRank,
		AfterVectorDistance: cursor.VectorDistance,
		AfterVectorUsed:     cursor.VectorUsed,
		Limit:               limit,
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	response := &controlplanev1.SearchMemoryRecordsResponse{
		Hits: make([]*controlplanev1.MemorySearchHit, 0, len(found)),
	}
	for _, hit := range found {
		encoded, err := toProtoResource(hit.Resource)
		if err != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
		}
		response.Hits = append(response.Hits, &controlplanev1.MemorySearchHit{
			Resource:             encoded,
			TextRank:             hit.TextRank,
			VectorDistance:       hit.VectorDistance,
			VectorProjectionUsed: hit.VectorProjectionUsed,
		})
	}
	if len(found) == limit {
		last := found[len(found)-1]
		response.NextPageToken, err = encodeMemoryCursor(memoryCursor{
			ID:             last.Resource.ID,
			TextRank:       last.TextRank,
			VectorDistance: last.VectorDistance,
			VectorUsed:     last.VectorProjectionUsed,
		})
		if err != nil {
			return nil, rpcError(principal.CorrelationID, errs.ErrInternal)
		}
	}
	return response, nil
}

func decodeMemoryCursor(raw string) (memoryCursor, error) {
	if raw == "" {
		return memoryCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 || len(decoded) > 512 {
		return memoryCursor{}, errs.ErrInvalidInput
	}
	var cursor memoryCursor
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || cursor.ID == "" ||
		decoder.Decode(&struct{}{}) != io.EOF {
		return memoryCursor{}, errs.ErrInvalidInput
	}
	return cursor, nil
}

func encodeMemoryCursor(cursor memoryCursor) (string, error) {
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
