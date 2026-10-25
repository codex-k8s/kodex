package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// BindInteractionIdentity passes connection, external team/channel/user digest and subject
// locators through the dedicated identity-bind command.
// BindInteractionIdentity передаёт подключение, внешние team/channel/user digest и ссылку subject
// отдельной команде привязки identity.
func (server *Server) BindInteractionIdentity(ctx context.Context, request *controlplanev1.BindInteractionIdentityRequest) (*controlplanev1.BindInteractionIdentityResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_BindInteractionIdentity_FullMethodName, command.BindInteractionIdentity, request.GetMutation(), command.InteractionIdentityInput{ConnectionRef: request.GetConnectionRef(), ExternalTeamRef: request.GetExternalTeamRef(), ExternalChannelRef: request.GetExternalChannelRef(), ExternalUserDigest: request.GetExternalUserDigest(), SubjectRef: request.GetSubjectRef()}, func(result command.Result) *controlplanev1.BindInteractionIdentityResponse {
		return &controlplanev1.BindInteractionIdentityResponse{Identity: castInteractionIdentity(result.InteractionIdentity)}
	})
}

// RevokeInteractionIdentity passes the identity locator through the dedicated revoke command and
// preserves optional identity-result presence.
// RevokeInteractionIdentity передаёт ссылку identity отдельной команде отзыва и сохраняет наличие
// необязательного результата identity.
func (server *Server) RevokeInteractionIdentity(ctx context.Context, request *controlplanev1.RevokeInteractionIdentityRequest) (*controlplanev1.RevokeInteractionIdentityResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_RevokeInteractionIdentity_FullMethodName, command.RevokeInteractionIdentity, request.GetMutation(), command.InteractionIdentityInput{IdentityRef: request.GetIdentityRef()}, func(result command.Result) *controlplanev1.RevokeInteractionIdentityResponse {
		return &controlplanev1.RevokeInteractionIdentityResponse{Identity: castInteractionIdentity(result.InteractionIdentity)}
	})
}

// ListInteractionIdentities reads the authorized connection identity page and projects each
// identity together with the continuation token.
// ListInteractionIdentities читает авторизованную страницу identity подключения и преобразует
// каждую identity вместе с токеном продолжения.
func (server *Server) ListInteractionIdentities(ctx context.Context, request *controlplanev1.ListInteractionIdentitiesRequest) (*controlplanev1.ListInteractionIdentitiesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_ListInteractionIdentities_FullMethodName, func(p value.Principal) (*controlplanev1.ListInteractionIdentitiesResponse, error) {
		items, next, err := server.service.ListInteractionIdentities(ctx, p, request.GetConnectionRef(), page(request.GetPage()))
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.ListInteractionIdentitiesResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
		for _, item := range items {
			response.Identities = append(response.Identities, castInteractionIdentity(&item))
		}
		return response, nil
	})
}

// castInteractionIdentity preserves absent identity and copies owner-resolved connection, external
// scope, subject, version and state fields.
// castInteractionIdentity сохраняет отсутствие identity и копирует разрешённые владельцем
// подключение, внешнюю область, subject, версию и состояние.
func castInteractionIdentity(input *entity.InteractionIdentity) *controlplanev1.InteractionIdentity {
	if input == nil {
		return nil
	}
	return &controlplanev1.InteractionIdentity{Ref: input.Ref, Version: input.Version, ConnectionRef: input.ConnectionRef, ConnectionVersion: input.ConnectionVersion,
		ExternalTeamRef: input.ExternalTeamRef, ExternalChannelRef: input.ExternalChannelRef, ExternalUserDigest: input.ExternalUserDigest, SubjectRef: input.SubjectRef, State: input.State}
}
