package grpc

import (
	"context"
	"slices"

	controlplanev1 "github.com/codex-k8s/matter-codex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/authorization"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/service/resource"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (server *Server) ClaimInteractionDelivery(ctx context.Context,
	request *controlplanev1.ClaimInteractionDeliveryRequest) (*controlplanev1.ClaimInteractionDeliveryResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_ClaimInteractionDelivery_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	claimed, err := server.service.ClaimInteractionDelivery(ctx, resource.ClaimInteractionDeliveryInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	work := claimed.Work
	if work.ID == "" {
		return &controlplanev1.ClaimInteractionDeliveryResponse{}, nil
	}
	return &controlplanev1.ClaimInteractionDeliveryResponse{
		DeliveryId: work.ID, ActorId: work.ActorID, SessionId: work.SessionID,
		SessionVersion: work.SessionVersion, TurnId: work.TurnID, TurnVersion: work.TurnVersion,
		Attempt: work.Attempt, RuntimeRevisionId: work.RuntimeRevisionID,
		RuntimeRevisionVersion: work.RuntimeRevisionVersion, ImmutableInputSha256: work.ImmutableInputSHA256,
		Kind: work.Kind, LifecycleState: toProtoState(enum.State(work.LifecycleState)), Outcome: work.Outcome,
		ArtifactId: work.ArtifactID, ArtifactVersion: work.ArtifactVersion, ArtifactSha256: work.ArtifactSHA256,
		DeliveryFence: work.Fence, DeliveryLeaseToken: claimed.LeaseToken,
		DeliveryLeaseExpiresAt:     timestamppb.New(work.LeaseExpiresAt),
		DeliveryReadbackCredential: claimed.ReadbackCredential,
		OrganizationId:             work.OrganizationID, ProjectId: work.ProjectID,
		ArtifactName: work.ArtifactName, ArtifactStorageRef: work.ArtifactStorageRef,
		ArtifactSizeBytes: work.ArtifactSizeBytes, ArtifactMediaType: work.ArtifactMediaType,
		InlinePayload: slices.Clone(work.InlinePayload),
	}, nil
}

func (server *Server) IssueInteractionDeliveryReadbackGrant(ctx context.Context,
	request *controlplanev1.IssueInteractionDeliveryReadbackGrantRequest) (*controlplanev1.IssueInteractionDeliveryReadbackGrantResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_IssueInteractionDeliveryReadbackGrant_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	issued, err := server.service.IssueInteractionDeliveryReadback(ctx, resource.IssueInteractionDeliveryReadbackInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), DeliveryID: request.GetDeliveryId(),
		Readiness: request.GetReadiness(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.IssueInteractionDeliveryReadbackGrantResponse{DeliveryId: issued.DeliveryID,
		Credential: issued.Credential, ExpiresAt: timestamppb.New(issued.ExpiresAt)}, nil
}

func (server *Server) ValidateInteractionDeliveryReadbackGrant(ctx context.Context,
	request *controlplanev1.ValidateInteractionDeliveryReadbackGrantRequest) (*controlplanev1.ValidateInteractionDeliveryReadbackGrantResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_ValidateInteractionDeliveryReadbackGrant_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	active, err := server.service.ValidateInteractionDeliveryReadback(ctx, resource.ValidateInteractionDeliveryReadbackInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), GrantID: request.GetGrantId(),
		DeliveryID: request.GetDeliveryId(), OrganizationID: request.GetOrganizationId(), ProjectID: request.GetProjectId(),
		CredentialSHA256: request.GetCredentialSha256(), Generation: request.GetGeneration(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.ValidateInteractionDeliveryReadbackGrantResponse{GrantId: request.GetGrantId(),
		DeliveryId: request.GetDeliveryId(), Active: active}, nil
}

func (server *Server) RecordInteractionDelivery(ctx context.Context,
	request *controlplanev1.RecordInteractionDeliveryRequest) (*controlplanev1.RecordInteractionDeliveryResponse, error) {
	principal, err := authorization.Principal(ctx,
		controlplanev1.ControlPlaneService_RecordInteractionDelivery_FullMethodName)
	if err != nil {
		return nil, rpcError("", errs.ErrUnauthenticated)
	}
	err = server.service.RecordInteractionDelivery(ctx, resource.RecordInteractionDeliveryInput{
		Principal: principal, IdempotencyKey: request.GetIdempotencyKey(), DeliveryID: request.GetDeliveryId(),
		Fence: request.GetDeliveryFence(), LeaseToken: request.GetDeliveryLeaseToken(),
		ProviderReceiptSHA256: request.GetProviderReceiptSha256(),
	})
	if err != nil {
		return nil, rpcError(principal.CorrelationID, err)
	}
	return &controlplanev1.RecordInteractionDeliveryResponse{DeliveryId: request.GetDeliveryId(),
		ProviderReceiptSha256: request.GetProviderReceiptSha256()}, nil
}
