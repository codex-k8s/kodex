package grpc

import (
	"context"
	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// castIntegrationApprovalConsent maps the owner-selected self projection; no credential or request-provided identity is added.
// castIntegrationApprovalConsent преобразует owner-selected self projection без credential либо request-provided identity.
func castIntegrationApprovalConsent(item entity.IntegrationApprovalConsent) *cp.IntegrationApprovalConsent {
	life := cp.IntegrationApprovalScopeLifetime_value["INTEGRATION_APPROVAL_SCOPE_LIFETIME_"+item.Lifetime]
	state := cp.IntegrationApprovalConsentState_value["INTEGRATION_APPROVAL_CONSENT_STATE_"+item.State]
	result := &cp.IntegrationApprovalConsent{Ref: item.Ref, Version: item.Version, ConnectionRef: item.ConnectionRef, ConnectionName: item.ConnectionName, CapabilityKey: item.CapabilityKey, AgentRef: item.AgentRef, RootRunRef: item.RootRunRef, Lifetime: cp.IntegrationApprovalScopeLifetime(life), State: cp.IntegrationApprovalConsentState(state), ScopePaths: item.ScopePaths, ScopeValuesJson: item.ScopeValuesJSON, MaxEffects: item.MaxEffects, ReservedEffects: item.ReservedEffects, CreatedAt: timestamppb.New(item.CreatedAt), ExpiresAt: timestamppb.New(item.ExpiresAt)}
	if item.RevokedAt != nil {
		result.RevokedAt = timestamppb.New(*item.RevokedAt)
	}
	return result
}

// ListIntegrationApprovalConsents binds the exact query method to the authenticated actor before the self-only owner catalog is read.
// ListIntegrationApprovalConsents связывает exact query method с authenticated actor до чтения self-only owner catalog.
func (server *Server) ListIntegrationApprovalConsents(ctx context.Context, request *cp.ListIntegrationApprovalConsentsRequest) (*cp.ListIntegrationApprovalConsentsResponse, error) {
	p, err := principal(ctx, cp.PlatformQueryService_ListIntegrationApprovalConsents_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListIntegrationApprovalConsents(ctx, p, query.Filter{Page: page(request.GetPage())})
	if err != nil {
		return nil, transportError(err)
	}
	result := &cp.ListIntegrationApprovalConsentsResponse{Page: &cp.PageInfo{NextPageToken: next}}
	for _, item := range items {
		result.Consents = append(result.Consents, castIntegrationApprovalConsent(item))
	}
	return result, nil
}

// RevokeIntegrationApprovalConsent dispatches only the dedicated owner command with mutation version and idempotency; the caller cannot select a different initiator.
// RevokeIntegrationApprovalConsent направляет только dedicated owner command с mutation version и idempotency; caller не выбирает другого initiator.
func (server *Server) RevokeIntegrationApprovalConsent(ctx context.Context, request *cp.RevokeIntegrationApprovalConsentRequest) (*cp.RevokeIntegrationApprovalConsentResponse, error) {
	p, err := principal(ctx, cp.PlatformCommandService_RevokeIntegrationApprovalConsent_FullMethodName)
	if err != nil {
		return nil, err
	}
	result, err := server.service.Execute(ctx, command.Command{Kind: command.RevokeIntegrationApprovalConsent, Principal: p, Mutation: mutation(request.GetMutation()), Payload: command.IntegrationApprovalConsentInput{ConsentRef: request.GetConsentRef()}})
	if err != nil {
		return nil, transportError(err)
	}
	if result.IntegrationApprovalConsent == nil {
		return nil, transportError(errs.ErrUnavailable)
	}
	return &cp.RevokeIntegrationApprovalConsentResponse{Consent: castIntegrationApprovalConsent(*result.IntegrationApprovalConsent)}, nil
}
