package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

func (server *Server) BindInteractionIdentity(ctx context.Context, request *controlplanev1.BindInteractionIdentityRequest) (*controlplanev1.BindInteractionIdentityResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_BindInteractionIdentity_FullMethodName, command.BindInteractionIdentity, request.GetMutation(),
		command.InteractionIdentityInput{ConnectionRef: request.GetConnectionRef(), ExternalTeamRef: request.GetExternalTeamRef(), ExternalChannelRef: request.GetExternalChannelRef(), ExternalUserDigest: request.GetExternalUserDigest(), SubjectRef: request.GetSubjectRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.BindInteractionIdentityResponse{Identity: castInteractionIdentity(result.InteractionIdentity)}, nil
}
func (server *Server) RevokeInteractionIdentity(ctx context.Context, request *controlplanev1.RevokeInteractionIdentityRequest) (*controlplanev1.RevokeInteractionIdentityResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RevokeInteractionIdentity_FullMethodName, command.RevokeInteractionIdentity, request.GetMutation(), command.InteractionIdentityInput{IdentityRef: request.GetIdentityRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RevokeInteractionIdentityResponse{Identity: castInteractionIdentity(result.InteractionIdentity)}, nil
}
func (server *Server) ListInteractionIdentities(ctx context.Context, request *controlplanev1.ListInteractionIdentitiesRequest) (*controlplanev1.ListInteractionIdentitiesResponse, error) {
	p, err := principal(ctx, controlplanev1.PlatformQueryService_ListInteractionIdentities_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListInteractionIdentities(ctx, p, request.GetConnectionRef(), page(request.GetPage()))
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListInteractionIdentitiesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Identities = append(response.Identities, castInteractionIdentity(&item))
	}
	return response, nil
}
func castInteractionIdentity(input *entity.InteractionIdentity) *controlplanev1.InteractionIdentity {
	if input == nil {
		return nil
	}
	return &controlplanev1.InteractionIdentity{Ref: input.Ref, Version: input.Version, ConnectionRef: input.ConnectionRef, ConnectionVersion: input.ConnectionVersion,
		ExternalTeamRef: input.ExternalTeamRef, ExternalChannelRef: input.ExternalChannelRef, ExternalUserDigest: input.ExternalUserDigest, SubjectRef: input.SubjectRef, State: input.State}
}
