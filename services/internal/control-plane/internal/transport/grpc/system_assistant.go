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

func (server *Server) PurgeAssistantConversation(ctx context.Context, request *controlplanev1.PurgeAssistantConversationRequest) (*controlplanev1.PurgeAssistantConversationResponse, error) {
	_, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_PurgeAssistantConversation_FullMethodName, command.PurgeAssistantConversation, request.GetMutation(), command.AssistantConversationArchiveInput{ConversationRef: request.GetConversationRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PurgeAssistantConversationResponse{ConversationRef: request.GetConversationRef()}, nil
}

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

func castProjectAssistant(item entity.ProjectAssistantProfile) *controlplanev1.ProjectAssistantProfile {
	return &controlplanev1.ProjectAssistantProfile{Ref: item.Ref, ProjectRef: item.ProjectRef, AgentRef: item.AgentRef,
		Name: item.Name, State: item.State, Version: item.Version, CreatedAt: timestamp(item.CreatedAt), UpdatedAt: timestamp(item.UpdatedAt)}
}

func conversationScope(scope controlplanev1.AssistantScope) string {
	if scope == controlplanev1.AssistantScope_ASSISTANT_SCOPE_SYSTEM || scope == controlplanev1.AssistantScope_ASSISTANT_SCOPE_PROJECT {
		return strings.TrimPrefix(scope.String(), "ASSISTANT_SCOPE_")
	}
	return ""
}

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

func (server *Server) UpdateAssistantConversationTitle(ctx context.Context, request *controlplanev1.UpdateAssistantConversationTitleRequest) (*controlplanev1.UpdateAssistantConversationTitleResponse, error) {
	payload := command.AssistantConversationTitleInput{ConversationRef: request.GetConversationRef(), Title: request.GetTitle()}
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_UpdateAssistantConversationTitle_FullMethodName, command.UpdateAssistantConversation, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateAssistantConversationTitleResponse{Conversation: castConversation(*result.Conversation)}, nil
}

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
	return &controlplanev1.AddAssistantTurnResponse{Conversation: castConversation(*result.Conversation), Assistant: castAssistant(*result.Assistant)}, nil
}

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

func (server *Server) ApplyAssistantPlan(ctx context.Context, request *controlplanev1.ApplyAssistantPlanRequest) (*controlplanev1.ApplyAssistantPlanResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_ApplyAssistantPlan_FullMethodName, command.ApplyAssistantPlan, request.GetMutation(), command.AssistantPlanInput{PlanRef: request.GetPlanRef(), Revision: request.GetRevision()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ApplyAssistantPlanResponse{Conversation: castConversation(*result.Conversation), Plan: castPlan(result.Plan), CreatedResourceRefs: result.CreatedRefs, Receipt: castPlanReceipt(result.PlanReceipt)}, nil
}

func (server *Server) UpdateAssistantPlanDraft(ctx context.Context, request *controlplanev1.UpdateAssistantPlanDraftRequest) (*controlplanev1.UpdateAssistantPlanDraftResponse, error) {
	payload := command.AssistantPlanDraftInput{PlanRef: request.GetPlanRef(), Summary: request.GetSummary(), Operations: assistantOperations(request.GetOperations())}
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_UpdateAssistantPlanDraft_FullMethodName, command.UpdateAssistantPlan, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateAssistantPlanDraftResponse{Plan: castPlan(result.Plan)}, nil
}

func (server *Server) ValidateAssistantPlan(ctx context.Context, request *controlplanev1.ValidateAssistantPlanRequest) (*controlplanev1.ValidateAssistantPlanResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_ValidateAssistantPlan_FullMethodName, command.ValidateAssistantPlan, request.GetMutation(), command.AssistantPlanInput{PlanRef: request.GetPlanRef(), Revision: request.GetRevision()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ValidateAssistantPlanResponse{Plan: castPlan(result.Plan)}, nil
}

func (server *Server) RejectAssistantPlan(ctx context.Context, request *controlplanev1.RejectAssistantPlanRequest) (*controlplanev1.RejectAssistantPlanResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_RejectAssistantPlan_FullMethodName, command.RejectAssistantPlan, request.GetMutation(), command.AssistantPlanInput{PlanRef: request.GetPlanRef(), Revision: request.GetRevision()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RejectAssistantPlanResponse{Plan: castPlan(result.Plan), Receipt: castPlanReceipt(result.PlanReceipt)}, nil
}

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

func (server *Server) UpdateAssistantOwnerInstructions(ctx context.Context, request *controlplanev1.UpdateAssistantOwnerInstructionsRequest) (*controlplanev1.UpdateAssistantOwnerInstructionsResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_UpdateAssistantOwnerInstructions_FullMethodName, command.UpdateAssistantInstructions, request.GetMutation(), command.AssistantInstructionsInput{Instructions: request.GetInstructions()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateAssistantOwnerInstructionsResponse{Assistant: castAssistant(*result.Assistant)}, nil
}

func (server *Server) RecoverSystemAssistant(ctx context.Context, request *controlplanev1.RecoverSystemAssistantRequest) (*controlplanev1.RecoverSystemAssistantResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_RecoverSystemAssistant_FullMethodName, command.RecoverAssistant, request.GetMutation(), struct{}{})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RecoverSystemAssistantResponse{Assistant: castAssistant(*result.Assistant)}, nil
}
