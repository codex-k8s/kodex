package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
)

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

func (server *Server) ListAssistantConversations(ctx context.Context, request *controlplanev1.ListAssistantConversationsRequest) (*controlplanev1.ListAssistantConversationsResponse, error) {
	p, err := principal(ctx, controlplanev1.SystemAssistantService_ListAssistantConversations_FullMethodName)
	if err != nil {
		return nil, err
	}
	items, next, err := server.service.ListAssistantConversations(ctx, p, query.Filter{ProjectRef: request.GetProjectRef(), Page: page(request.GetPage())})
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
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_CreateAssistantConversation_FullMethodName, command.CreateAssistantConversation, request.GetMutation(), command.AssistantConversationInput{ProjectRef: request.GetProjectRef(), Context: assistantContext(request.GetContext())})
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
	payload := command.AssistantTurnInput{ConversationRef: request.GetConversationRef(), Content: request.GetContent(), AttachmentSetRef: request.GetAttachmentSetRef()}
	result, err := execute(ctx, server.service, controlplanev1.SystemAssistantService_AddAssistantTurn_FullMethodName, command.AddAssistantTurn, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.AddAssistantTurnResponse{Conversation: castConversation(*result.Conversation), Assistant: castAssistant(*result.Assistant)}, nil
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
