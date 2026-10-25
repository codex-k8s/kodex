package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ChangeSystemAssistantIntegrationGrant passes connection, capability, enabled and approval fields
// through the dedicated system-assistant grant command, leaving eligibility to the owner.
// ChangeSystemAssistantIntegrationGrant передаёт подключение, возможность, enabled и поля
// одобрения отдельной команде grant системного ассистента, оставляя eligibility владельцу.
func (server *Server) ChangeSystemAssistantIntegrationGrant(ctx context.Context, request *controlplanev1.ChangeSystemAssistantIntegrationGrantRequest) (*controlplanev1.ChangeSystemAssistantIntegrationGrantResponse, error) {
	payload := command.SystemAssistantIntegrationGrantInput{ConnectionRef: request.GetConnectionRef(), CapabilityKey: request.GetCapabilityKey(), Enabled: request.GetEnabled(), ApprovalPolicy: strings.TrimPrefix(request.GetApprovalPolicy().String(), "INTEGRATION_APPROVAL_POLICY_"), ApprovalScopePaths: request.GetApprovalScopePaths()}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ChangeSystemAssistantIntegrationGrant_FullMethodName, command.ChangeSystemAssistantIntegrationGrant, request.GetMutation(), payload, func(result command.Result) *controlplanev1.ChangeSystemAssistantIntegrationGrantResponse {
		return &controlplanev1.ChangeSystemAssistantIntegrationGrantResponse{Connection: castConnection(*result.Connection)}
	})
}

// GetSystemAssistantIntegrationGrantCandidates reads system-assistant candidates, requires closed
// reason values and projects current grant metadata with owner/version pins and pagination.
// GetSystemAssistantIntegrationGrantCandidates читает кандидатов системного ассистента, требует
// закрытые значения причин и преобразует текущий grant с привязками владельца, версий и
// пагинацией.
func (server *Server) GetSystemAssistantIntegrationGrantCandidates(ctx context.Context, request *controlplanev1.GetSystemAssistantIntegrationGrantCandidatesRequest) (*controlplanev1.GetSystemAssistantIntegrationGrantCandidatesResponse, error) {
	return queryResponse(ctx, controlplanev1.PlatformQueryService_GetSystemAssistantIntegrationGrantCandidates_FullMethodName, func(p value.Principal) (*controlplanev1.GetSystemAssistantIntegrationGrantCandidatesResponse, error) {
		result, err := server.service.GetSystemAssistantIntegrationGrantCandidates(ctx, p, request.GetConnectionRef(), request.GetQuery(), page(request.GetPage()))
		if err != nil {
			return nil, err
		}
		response := &controlplanev1.GetSystemAssistantIntegrationGrantCandidatesResponse{ScopeKind: runtimeSecretScopeKind(result.ScopeKind), OrganizationRef: result.OrganizationRef, AssistantRef: result.AssistantRef, AssistantVersion: result.AssistantVersion, ConnectionRef: result.ConnectionRef, ConnectionVersion: result.ConnectionVersion, DefinitionVersion: result.DefinitionVersion, DefinitionDigest: result.DefinitionDigest, Items: []*controlplanev1.SystemAssistantIntegrationGrantCandidate{}, Total: result.Total, Page: &controlplanev1.PageInfo{NextPageToken: result.NextPageToken}}
		for _, item := range result.Items {
			reason, err := integrationCandidateReason(item.Reason)
			if err != nil {
				return nil, err
			}
			response.Items = append(response.Items, &controlplanev1.SystemAssistantIntegrationGrantCandidate{Capability: castIntegrationCapability(item.Capability), Grantable: item.Grantable, Reason: reason, CurrentGrantRef: item.CurrentGrantRef, CurrentGrantVersion: item.CurrentGrantVersion, CurrentApprovalPolicy: integrationApprovalPolicy(item.CurrentApprovalPolicy), CurrentApprovalScopePaths: append([]string{}, item.CurrentApprovalScopePaths...), CurrentGrantEnabled: item.CurrentGrantEnabled})
		}
		return response, nil

	})
}
