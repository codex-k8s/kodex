package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ArchiveAssistantConversation executes the dedicated archive command and rejects a successful
// result lacking the conversation projection.
// ArchiveAssistantConversation выполняет отдельную команду архивирования и отклоняет успешный
// результат без представления беседы.
func (server *Server) ArchiveAssistantConversation(ctx context.Context, request *controlplanev1.ArchiveAssistantConversationRequest) (*controlplanev1.ArchiveAssistantConversationResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_ArchiveAssistantConversation_FullMethodName, command.ArchiveAssistantConversation, request.GetMutation(), command.AssistantConversationArchiveInput{ConversationRef: request.GetConversationRef()})
	if err != nil {
		return nil, err
	}
	if result.Conversation == nil {
		return nil, status.Error(codes.Internal, "assistant archive result is missing")
	}
	return &controlplanev1.ArchiveAssistantConversationResponse{Conversation: castConversation(*result.Conversation)}, nil
}

// RestoreAssistantConversation executes conversation restoration and requires the returned
// conversation before forming the response.
// RestoreAssistantConversation выполняет восстановление беседы и требует возвращённую беседу до
// формирования ответа.
func (server *Server) RestoreAssistantConversation(ctx context.Context, request *controlplanev1.RestoreAssistantConversationRequest) (*controlplanev1.RestoreAssistantConversationResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_RestoreAssistantConversation_FullMethodName, command.RestoreAssistantConversation, request.GetMutation(), command.AssistantConversationArchiveInput{ConversationRef: request.GetConversationRef()})
	if err != nil {
		return nil, err
	}
	if result.Conversation == nil {
		return nil, status.Error(codes.Internal, "assistant restore result is missing")
	}
	return &controlplanev1.RestoreAssistantConversationResponse{Conversation: castConversation(*result.Conversation)}, nil
}

// PurgeAssistantConversation executes the dedicated purge command and returns the request locator
// only after success.
// PurgeAssistantConversation выполняет отдельную команду окончательного удаления и возвращает
// ссылку запроса только после успеха.
func (server *Server) PurgeAssistantConversation(ctx context.Context, request *controlplanev1.PurgeAssistantConversationRequest) (*controlplanev1.PurgeAssistantConversationResponse, error) {
	_, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_PurgeAssistantConversation_FullMethodName, command.PurgeAssistantConversation, request.GetMutation(), command.AssistantConversationArchiveInput{ConversationRef: request.GetConversationRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PurgeAssistantConversationResponse{ConversationRef: request.GetConversationRef()}, nil
}

// MoveAssistantConversationToProject passes conversation and project locators to the dedicated
// move command and requires its conversation result.
// MoveAssistantConversationToProject передаёт ссылки беседы и проекта отдельной команде переноса и
// требует беседу в её результате.
func (server *Server) MoveAssistantConversationToProject(ctx context.Context, request *controlplanev1.MoveAssistantConversationToProjectRequest) (*controlplanev1.MoveAssistantConversationToProjectResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_MoveAssistantConversationToProject_FullMethodName,
		command.MoveAssistantConversationToProject, request.GetMutation(), command.AssistantConversationProjectInput{
			ConversationRef: request.GetConversationRef(), ProjectRef: request.GetProjectRef(),
		})
	if err != nil {
		return nil, err
	}
	if result.Conversation == nil {
		return nil, status.Error(codes.Internal, "assistant project move result is missing")
	}
	return &controlplanev1.MoveAssistantConversationToProjectResponse{Conversation: castConversation(*result.Conversation)}, nil
}

// GetSystemAssistant reads and projects the system assistant using the principal verified for this
// exact RPC.
// GetSystemAssistant читает и преобразует системного ассистента с principal, проверенным для
// точного RPC.
func (server *Server) GetSystemAssistant(ctx context.Context, _ *controlplanev1.GetSystemAssistantRequest) (*controlplanev1.GetSystemAssistantResponse, error) {
	p, err := principal(ctx, controlplanev1.SystemAssistantService_GetSystemAssistant_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetSystemAssistant(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetSystemAssistantResponse{Assistant: castAssistant(item)}, nil
}

// CreateProjectAssistant passes project and profile fields through the dedicated create command
// and requires a project-assistant result.
// CreateProjectAssistant передаёт проект и поля профиля отдельной команде создания и требует
// результат проектного ассистента.
func (server *Server) CreateProjectAssistant(ctx context.Context, request *controlplanev1.CreateProjectAssistantRequest) (*controlplanev1.CreateProjectAssistantResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_CreateProjectAssistant_FullMethodName,
		command.CreateProjectAssistant, request.GetMutation(), command.ProjectAssistantInput{
			ProjectRef: request.GetProjectRef(), Name: request.GetName(), Purpose: request.GetPurpose(), Instructions: request.GetInstructions(),
		})
	if err != nil {
		return nil, err
	}
	if result.ProjectAssistant == nil {
		return nil, status.Error(codes.Internal, "project assistant result is missing")
	}
	return &controlplanev1.CreateProjectAssistantResponse{Profile: castProjectAssistant(*result.ProjectAssistant)}, nil
}

// GetProjectAssistant reads the project assistant with a verified principal and delegates project
// eligibility to the domain.
// GetProjectAssistant читает проектного ассистента с проверенным principal, передавая проверку
// доступности проекта домену.
func (server *Server) GetProjectAssistant(ctx context.Context, request *controlplanev1.GetProjectAssistantRequest) (*controlplanev1.GetProjectAssistantResponse, error) {
	p, err := principal(ctx, controlplanev1.SystemAssistantService_GetProjectAssistant_FullMethodName)
	if err != nil {
		return nil, err
	}
	item, err := server.service.GetProjectAssistant(ctx, p, request.GetProjectRef())
	if err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.GetProjectAssistantResponse{Profile: castProjectAssistant(item)}, nil
}

// castProjectAssistant copies the domain profile references, state, version and timestamps to its
// protobuf representation.
// castProjectAssistant копирует ссылки доменного профиля, состояние, версию и время в
// представление Protobuf.
func castProjectAssistant(item entity.ProjectAssistantProfile) *controlplanev1.ProjectAssistantProfile {
	return &controlplanev1.ProjectAssistantProfile{Ref: item.Ref, ProjectRef: item.ProjectRef, AgentRef: item.AgentRef,
		Name: item.Name, State: item.State, Version: item.Version, CreatedAt: timestamp(item.CreatedAt), UpdatedAt: timestamp(item.UpdatedAt)}
}

// conversationScope returns SYSTEM or PROJECT for those exact enum values and an empty string for
// every other value.
// conversationScope возвращает SYSTEM либо PROJECT для точных значений enum и пустую строку для
// остальных значений.
func conversationScope(scope controlplanev1.AssistantScope) string {
	if scope == controlplanev1.AssistantScope_ASSISTANT_SCOPE_SYSTEM || scope == controlplanev1.AssistantScope_ASSISTANT_SCOPE_PROJECT {
		return strings.TrimPrefix(scope.String(), "ASSISTANT_SCOPE_")
	}
	return ""
}

// ListAssistantConversations defaults an unspecified state to ACTIVE, validates an explicit scope
// and returns the authorized conversation page.
// ListAssistantConversations заменяет неопределённое состояние на ACTIVE, проверяет явную область
// и возвращает авторизованную страницу бесед.
func (server *Server) ListAssistantConversations(ctx context.Context, request *controlplanev1.ListAssistantConversationsRequest) (*controlplanev1.ListAssistantConversationsResponse, error) {
	p, err := principal(ctx, controlplanev1.SystemAssistantService_ListAssistantConversations_FullMethodName)
	if err != nil {
		return nil, err
	}
	state := strings.TrimPrefix(request.GetState().String(), "ASSISTANT_CONVERSATION_STATE_")
	if state == "UNSPECIFIED" {
		state = "ACTIVE"
	}
	if request.GetAssistantScope() != controlplanev1.AssistantScope_ASSISTANT_SCOPE_UNSPECIFIED && conversationScope(request.GetAssistantScope()) == "" {
		return nil, status.Error(codes.InvalidArgument, "assistant scope is invalid")
	}
	items, next, err := server.service.ListAssistantConversations(ctx, p, query.AssistantConversationFilter{
		Filter:         query.Filter{ProjectRef: request.GetProjectRef(), Query: request.GetQuery(), State: state, MatchAssistantLocalizedDefaultTitle: request.GetMatchLocalizedDefaultTitle(), Page: page(request.GetPage())},
		AssistantScope: conversationScope(request.GetAssistantScope()), AssistantRef: request.GetAssistantRef(),
	})
	if err != nil {
		return nil, transportError(err)
	}
	response := &controlplanev1.ListAssistantConversationsResponse{Page: &controlplanev1.PageInfo{NextPageToken: next}}
	for _, item := range items {
		response.Conversations = append(response.Conversations, castConversation(item))
	}
	return response, nil
}

// CreateAssistantConversation requires a known assistant scope before sending project and context
// descriptors to the create command.
// CreateAssistantConversation требует известную область ассистента до передачи проекта и описателя
// контекста команде создания.
func (server *Server) CreateAssistantConversation(ctx context.Context, request *controlplanev1.CreateAssistantConversationRequest) (*controlplanev1.CreateAssistantConversationResponse, error) {
	if conversationScope(request.GetAssistantScope()) == "" {
		return nil, status.Error(codes.InvalidArgument, "assistant scope is required")
	}
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_CreateAssistantConversation_FullMethodName, command.CreateAssistantConversation, request.GetMutation(), command.AssistantConversationInput{ProjectRef: request.GetProjectRef(), AssistantScope: conversationScope(request.GetAssistantScope()), Context: assistantContext(request.GetContext())})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateAssistantConversationResponse{Conversation: castConversation(*result.Conversation)}, nil
}

// UpdateAssistantConversationTitle passes the conversation locator and proposed title through the
// dedicated mutation command.
// UpdateAssistantConversationTitle передаёт ссылку беседы и предлагаемый заголовок через отдельную
// команду изменения.
func (server *Server) UpdateAssistantConversationTitle(ctx context.Context, request *controlplanev1.UpdateAssistantConversationTitleRequest) (*controlplanev1.UpdateAssistantConversationTitleResponse, error) {
	payload := command.AssistantConversationTitleInput{ConversationRef: request.GetConversationRef(), Title: request.GetTitle()}
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_UpdateAssistantConversationTitle_FullMethodName, command.UpdateAssistantConversation, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateAssistantConversationTitleResponse{Conversation: castConversation(*result.Conversation)}, nil
}

// AddAssistantTurn defaults delivery to QUEUE and preserves optional context presence before
// executing the turn command.
// AddAssistantTurn выбирает доставку QUEUE по умолчанию и сохраняет наличие необязательного
// контекста до выполнения команды хода.
func (server *Server) AddAssistantTurn(ctx context.Context, request *controlplanev1.AddAssistantTurnRequest) (*controlplanev1.AddAssistantTurnResponse, error) {
	deliveryMode := strings.TrimPrefix(request.GetDeliveryMode().String(), "ASSISTANT_TURN_DELIVERY_MODE_")
	if deliveryMode == "UNSPECIFIED" {
		deliveryMode = "QUEUE"
	}
	payload := command.AssistantTurnInput{ConversationRef: request.GetConversationRef(), Content: request.GetContent(), AttachmentSetRef: request.GetAttachmentSetRef(), DeliveryMode: deliveryMode}
	if request.GetContext() != nil {
		context := assistantContext(request.GetContext())
		payload.Context = &context
	}
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_AddAssistantTurn_FullMethodName, command.AddAssistantTurn, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return castAssistantTurnResult(result), nil
}

// castAssistantTurnResult projects the command conversation and includes the assistant only when
// the result supplies one.
// castAssistantTurnResult преобразует беседу команды и добавляет ассистента только при его наличии
// в результате.
func castAssistantTurnResult(result command.Result) *controlplanev1.AddAssistantTurnResponse {
	response := &controlplanev1.AddAssistantTurnResponse{Conversation: castConversation(*result.Conversation)}
	if result.Assistant != nil {
		response.Assistant = castAssistant(*result.Assistant)
	}
	return response
}

// CancelAssistantTurn executes turn cancellation and requires the authoritative conversation
// locator in its runtime result.
// CancelAssistantTurn выполняет отмену хода и требует авторитетную ссылку беседы в
// runtime-результате.
func (server *Server) CancelAssistantTurn(ctx context.Context, request *controlplanev1.CancelAssistantTurnRequest) (*controlplanev1.CancelAssistantTurnResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_CancelAssistantTurn_FullMethodName, command.CancelAssistantTurn, request.GetMutation(), command.AssistantTurnCancellationInput{ConversationRef: request.GetConversationRef()})
	if err != nil {
		return nil, err
	}
	conversationRef, _ := result.Runtime["conversationRef"].(string)
	runRef, _ := result.Runtime["runRef"].(string)
	cancelled, _ := result.Runtime["cancelled"].(bool)
	if conversationRef == "" {
		return nil, status.Error(codes.Internal, "assistant cancellation result is missing")
	}
	return &controlplanev1.CancelAssistantTurnResponse{ConversationRef: conversationRef, RunRef: runRef, Cancelled: cancelled}, nil
}

// ApplyAssistantPlan passes plan reference and revision through the apply command and projects
// conversation, plan, created refs and receipt.
// ApplyAssistantPlan передаёт ссылку и ревизию плана команде применения и преобразует беседу,
// план, созданные ссылки и квитанцию.
func (server *Server) ApplyAssistantPlan(ctx context.Context, request *controlplanev1.ApplyAssistantPlanRequest) (*controlplanev1.ApplyAssistantPlanResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_ApplyAssistantPlan_FullMethodName, command.ApplyAssistantPlan, request.GetMutation(), command.AssistantPlanInput{PlanRef: request.GetPlanRef(), Revision: request.GetRevision()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ApplyAssistantPlanResponse{Conversation: castConversation(*result.Conversation), Plan: castPlan(result.Plan), CreatedResourceRefs: result.CreatedRefs, Receipt: castPlanReceipt(result.PlanReceipt)}, nil
}

// UpdateAssistantPlanDraft converts draft operations and passes their summary and plan reference
// through the dedicated update command.
// UpdateAssistantPlanDraft преобразует операции черновика и передаёт их описание и ссылку плана
// отдельной команде обновления.
func (server *Server) UpdateAssistantPlanDraft(ctx context.Context, request *controlplanev1.UpdateAssistantPlanDraftRequest) (*controlplanev1.UpdateAssistantPlanDraftResponse, error) {
	payload := command.AssistantPlanDraftInput{PlanRef: request.GetPlanRef(), Summary: request.GetSummary(), Operations: assistantOperations(request.GetOperations())}
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_UpdateAssistantPlanDraft_FullMethodName, command.UpdateAssistantPlan, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateAssistantPlanDraftResponse{Plan: castPlan(result.Plan)}, nil
}

// ValidateAssistantPlan executes validation for the requested plan revision and returns the
// resulting plan projection.
// ValidateAssistantPlan выполняет проверку запрошенной ревизии плана и возвращает представление
// плана из результата.
func (server *Server) ValidateAssistantPlan(ctx context.Context, request *controlplanev1.ValidateAssistantPlanRequest) (*controlplanev1.ValidateAssistantPlanResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_ValidateAssistantPlan_FullMethodName, command.ValidateAssistantPlan, request.GetMutation(), command.AssistantPlanInput{PlanRef: request.GetPlanRef(), Revision: request.GetRevision()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ValidateAssistantPlanResponse{Plan: castPlan(result.Plan)}, nil
}

// RejectAssistantPlan executes rejection for the requested plan revision and returns its resulting
// plan and receipt.
// RejectAssistantPlan выполняет отклонение запрошенной ревизии плана и возвращает полученные план
// и квитанцию.
func (server *Server) RejectAssistantPlan(ctx context.Context, request *controlplanev1.RejectAssistantPlanRequest) (*controlplanev1.RejectAssistantPlanResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_RejectAssistantPlan_FullMethodName, command.RejectAssistantPlan, request.GetMutation(), command.AssistantPlanInput{PlanRef: request.GetPlanRef(), Revision: request.GetRevision()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RejectAssistantPlanResponse{Plan: castPlan(result.Plan), Receipt: castPlanReceipt(result.PlanReceipt)}, nil
}

// assistantContext copies context locators and optional version, omits unspecified operations and
// supplies an empty operation list for nil input.
// assistantContext копирует ссылки контекста и необязательную версию, пропускает неопределённые
// операции и создаёт пустой список для nil.
func assistantContext(input *controlplanev1.AssistantContextDescriptor) entity.AssistantContextDescriptor {
	if input == nil {
		return entity.AssistantContextDescriptor{AllowedOperations: []string{}}
	}
	result := entity.AssistantContextDescriptor{Route: input.GetRoute(), EntityKind: input.GetEntityKind(), EntityRef: input.GetEntityRef(), EntityName: input.GetEntityName(), EntityVersion: input.EntityVersion}
	for _, operation := range input.GetAllowedOperations() {
		if operation != controlplanev1.AssistantPlanOperation_TYPE_UNSPECIFIED {
			result.AllowedOperations = append(result.AllowedOperations, enumSuffix(operation, "TYPE_"))
		}
	}
	return result
}

// UpdateAssistantOwnerInstructions passes proposed owner instructions to the dedicated command and
// casts its assistant result.
// UpdateAssistantOwnerInstructions передаёт предлагаемые инструкции владельца отдельной команде и
// преобразует ассистента из её результата.
func (server *Server) UpdateAssistantOwnerInstructions(ctx context.Context, request *controlplanev1.UpdateAssistantOwnerInstructionsRequest) (*controlplanev1.UpdateAssistantOwnerInstructionsResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_UpdateAssistantOwnerInstructions_FullMethodName, command.UpdateAssistantInstructions, request.GetMutation(), command.AssistantInstructionsInput{Instructions: request.GetInstructions()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateAssistantOwnerInstructionsResponse{Assistant: castAssistant(*result.Assistant)}, nil
}

// RecoverSystemAssistant invokes the dedicated system-assistant recovery command with an empty
// payload and projects its result.
// RecoverSystemAssistant вызывает отдельную команду восстановления системного ассистента с пустыми
// данными и преобразует результат.
func (server *Server) RecoverSystemAssistant(ctx context.Context, request *controlplanev1.RecoverSystemAssistantRequest) (*controlplanev1.RecoverSystemAssistantResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_RecoverSystemAssistant_FullMethodName, command.RecoverAssistant, request.GetMutation(), struct{}{})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RecoverSystemAssistantResponse{Assistant: castAssistant(*result.Assistant)}, nil
}
