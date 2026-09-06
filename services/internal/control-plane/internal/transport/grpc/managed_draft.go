package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

func (server *Server) SavePromptTemplateDraft(ctx context.Context, request *controlplanev1.SavePromptTemplateDraftRequest) (*controlplanev1.SavePromptTemplateDraftResponse, error) {
	input := managedRevisionInput(request)
	input.ContentFormat, input.Content = request.GetContentFormat(), request.GetContent()
	input.PromptScope = castPromptScopeInput(request.GetPromptScope())
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_SavePromptTemplateDraft_FullMethodName, command.SavePromptTemplateDraft, request.GetMutation(), input)
	return &controlplanev1.SavePromptTemplateDraftResponse{Configuration: configuration, Revision: revision}, err
}

func (server *Server) DiscardPromptTemplateDraft(ctx context.Context, request *controlplanev1.DiscardPromptTemplateDraftRequest) (*controlplanev1.DiscardPromptTemplateDraftResponse, error) {
	input := managedRevisionInput(request)
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DiscardPromptTemplateDraft_FullMethodName, command.DiscardPromptTemplateDraft, request.GetMutation(), input)
	return &controlplanev1.DiscardPromptTemplateDraftResponse{Configuration: configuration, Revision: revision}, err
}

func (server *Server) SaveRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.SaveRoleImageRevisionDraftRequest) (*controlplanev1.SaveRoleImageRevisionDraftResponse, error) {
	input := managedRevisionInput(request)
	input.ContentFormat, input.Content = request.GetContentFormat(), request.GetContent()
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_SaveRoleImageRevisionDraft_FullMethodName, command.SaveRoleImageRevisionDraft, request.GetMutation(), input)
	return &controlplanev1.SaveRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}

func (server *Server) DiscardRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.DiscardRoleImageRevisionDraftRequest) (*controlplanev1.DiscardRoleImageRevisionDraftResponse, error) {
	input := managedRevisionInput(request)
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DiscardRoleImageRevisionDraft_FullMethodName, command.DiscardRoleImageRevisionDraft, request.GetMutation(), input)
	return &controlplanev1.DiscardRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}

func (server *Server) SaveIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.SaveIntegrationDefinitionDraftRequest) (*controlplanev1.SaveIntegrationDefinitionDraftResponse, error) {
	input := managedRevisionInput(request)
	input.ContentFormat, input.Content = request.GetContentFormat(), request.GetContent()
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_SaveIntegrationDefinitionDraft_FullMethodName, command.SaveIntegrationDefinitionDraft, request.GetMutation(), input)
	return &controlplanev1.SaveIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}

func (server *Server) DiscardIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.DiscardIntegrationDefinitionDraftRequest) (*controlplanev1.DiscardIntegrationDefinitionDraftResponse, error) {
	input := managedRevisionInput(request)
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DiscardIntegrationDefinitionDraft_FullMethodName, command.DiscardIntegrationDefinitionDraft, request.GetMutation(), input)
	return &controlplanev1.DiscardIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}

func (server *Server) SaveSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.SaveSystemSTTConfigurationDraftRequest) (*controlplanev1.SaveSystemSTTConfigurationDraftResponse, error) {
	input := managedRevisionInput(request)
	input.ContentFormat, input.Content = request.GetContentFormat(), request.GetContent()
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_SaveSystemSTTConfigurationDraft_FullMethodName, command.SaveSystemSTTConfigurationDraft, request.GetMutation(), input)
	return &controlplanev1.SaveSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}

func (server *Server) DiscardSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.DiscardSystemSTTConfigurationDraftRequest) (*controlplanev1.DiscardSystemSTTConfigurationDraftResponse, error) {
	input := managedRevisionInput(request)
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DiscardSystemSTTConfigurationDraft_FullMethodName, command.DiscardSystemSTTConfigurationDraft, request.GetMutation(), input)
	return &controlplanev1.DiscardSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}
