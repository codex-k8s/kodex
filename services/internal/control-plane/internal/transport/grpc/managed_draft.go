package grpc

import (
	"context"
	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// SavePromptTemplateDraft copies exact revision, content, and prompt-scope inputs into the dedicated owner command and returns its typed revision view.
// SavePromptTemplateDraft передаёт точные данные ревизии, содержимого и области промпта в отдельную команду владельца и возвращает её типизированное представление ревизии.
func (server *Server) SavePromptTemplateDraft(ctx context.Context, request *controlplanev1.SavePromptTemplateDraftRequest) (*controlplanev1.SavePromptTemplateDraftResponse, error) {
	input := managedRevisionInput(request)
	input.ContentFormat, input.Content = request.GetContentFormat(), request.GetContent()
	input.PromptScope = castPromptScopeInput(request.GetPromptScope())
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_SavePromptTemplateDraft_FullMethodName, command.SavePromptTemplateDraft, request.GetMutation(), input)
	return &controlplanev1.SavePromptTemplateDraftResponse{Configuration: configuration, Revision: revision}, err
}

// DiscardPromptTemplateDraft passes the pinned revision and mutation to the separate discard command without manufacturing a replacement draft locally.
// DiscardPromptTemplateDraft передаёт закреплённую ревизию и контекст изменения отдельной команде отказа, не создавая локально новый черновик.
func (server *Server) DiscardPromptTemplateDraft(ctx context.Context, request *controlplanev1.DiscardPromptTemplateDraftRequest) (*controlplanev1.DiscardPromptTemplateDraftResponse, error) {
	input := managedRevisionInput(request)
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DiscardPromptTemplateDraft_FullMethodName, command.DiscardPromptTemplateDraft, request.GetMutation(), input)
	return &controlplanev1.DiscardPromptTemplateDraftResponse{Configuration: configuration, Revision: revision}, err
}

// SaveRoleImageRevisionDraft forwards role-image content and exact revision references through the specialized managed mutation path.
// SaveRoleImageRevisionDraft передаёт содержимое образа роли и точные ссылки на ревизию через специализированный путь управляемого изменения.
func (server *Server) SaveRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.SaveRoleImageRevisionDraftRequest) (*controlplanev1.SaveRoleImageRevisionDraftResponse, error) {
	input := managedRevisionInput(request)
	input.ContentFormat, input.Content = request.GetContentFormat(), request.GetContent()
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_SaveRoleImageRevisionDraft_FullMethodName, command.SaveRoleImageRevisionDraft, request.GetMutation(), input)
	return &controlplanev1.SaveRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// DiscardRoleImageRevisionDraft selects the role-image discard command by its full RPC method and returns only the owner's typed result.
// DiscardRoleImageRevisionDraft выбирает команду отказа от черновика образа по полному RPC-методу и возвращает только типизированный результат владельца.
func (server *Server) DiscardRoleImageRevisionDraft(ctx context.Context, request *controlplanev1.DiscardRoleImageRevisionDraftRequest) (*controlplanev1.DiscardRoleImageRevisionDraftResponse, error) {
	input := managedRevisionInput(request)
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DiscardRoleImageRevisionDraft_FullMethodName, command.DiscardRoleImageRevisionDraft, request.GetMutation(), input)
	return &controlplanev1.DiscardRoleImageRevisionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// SaveIntegrationDefinitionDraft passes integration-definition content and revision pins to the dedicated owner command without interpreting the definition here.
// SaveIntegrationDefinitionDraft передаёт содержимое определения интеграции и закрепления ревизии отдельной команде владельца, не интерпретируя определение в этом слое.
func (server *Server) SaveIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.SaveIntegrationDefinitionDraftRequest) (*controlplanev1.SaveIntegrationDefinitionDraftResponse, error) {
	input := managedRevisionInput(request)
	input.ContentFormat, input.Content = request.GetContentFormat(), request.GetContent()
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_SaveIntegrationDefinitionDraft_FullMethodName, command.SaveIntegrationDefinitionDraft, request.GetMutation(), input)
	return &controlplanev1.SaveIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// DiscardIntegrationDefinitionDraft delegates the exact integration-definition revision to its own discard command and preserves the typed response.
// DiscardIntegrationDefinitionDraft передаёт точную ревизию определения интеграции собственной команде отказа и сохраняет типизированный ответ.
func (server *Server) DiscardIntegrationDefinitionDraft(ctx context.Context, request *controlplanev1.DiscardIntegrationDefinitionDraftRequest) (*controlplanev1.DiscardIntegrationDefinitionDraftResponse, error) {
	input := managedRevisionInput(request)
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DiscardIntegrationDefinitionDraft_FullMethodName, command.DiscardIntegrationDefinitionDraft, request.GetMutation(), input)
	return &controlplanev1.DiscardIntegrationDefinitionDraftResponse{Configuration: configuration, Revision: revision}, err
}

// SaveSystemSTTConfigurationDraft forwards the proposed STT configuration bytes and revision pins to the dedicated managed command.
// SaveSystemSTTConfigurationDraft передаёт предлагаемые байты конфигурации STT и закрепления ревизии отдельной управляемой команде.
func (server *Server) SaveSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.SaveSystemSTTConfigurationDraftRequest) (*controlplanev1.SaveSystemSTTConfigurationDraftResponse, error) {
	input := managedRevisionInput(request)
	input.ContentFormat, input.Content = request.GetContentFormat(), request.GetContent()
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_SaveSystemSTTConfigurationDraft_FullMethodName, command.SaveSystemSTTConfigurationDraft, request.GetMutation(), input)
	return &controlplanev1.SaveSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}

// DiscardSystemSTTConfigurationDraft selects the distinct STT discard command and returns its owner-generated configuration and revision.
// DiscardSystemSTTConfigurationDraft выбирает отдельную команду отказа от черновика STT и возвращает созданные владельцем конфигурацию и ревизию.
func (server *Server) DiscardSystemSTTConfigurationDraft(ctx context.Context, request *controlplanev1.DiscardSystemSTTConfigurationDraftRequest) (*controlplanev1.DiscardSystemSTTConfigurationDraftResponse, error) {
	input := managedRevisionInput(request)
	configuration, revision, err := server.managedMutation(ctx, controlplanev1.PlatformCommandService_DiscardSystemSTTConfigurationDraft_FullMethodName, command.DiscardSystemSTTConfigurationDraft, request.GetMutation(), input)
	return &controlplanev1.DiscardSystemSTTConfigurationDraftResponse{Configuration: configuration, Revision: revision}, err
}
