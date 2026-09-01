package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/structpb"
)

func (server *Server) GetSystemAssistant(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Assistant.GetSystemAssistant(r.Context(), &controlplanev1.GetSystemAssistantRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "assistant", "")
}
func (server *Server) ListAssistantConversations(w http.ResponseWriter, r *http.Request, p generated.ListAssistantConversationsParams) {
	response, err := server.control.Assistant.ListAssistantConversations(r.Context(), &controlplanev1.ListAssistantConversationsRequest{ProjectRef: stringValue(p.ProjectRef), Page: page(nil, nil)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "conversations")
}
func (server *Server) CreateAssistantConversation(w http.ResponseWriter, r *http.Request, p generated.CreateAssistantConversationParams) {
	body, ok := decodeJSON[generated.CreateAssistantConversationJSONBody](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Assistant.CreateAssistantConversation(r.Context(), &controlplanev1.CreateAssistantConversationRequest{Mutation: m, ProjectRef: stringValue(body.ProjectRef), Context: assistantContextInput(body.Context)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "conversation", "")
}
func (server *Server) UpdateAssistantConversationTitle(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.UpdateAssistantConversationTitleParams) {
	body, ok := decodeJSON[generated.UpdateAssistantConversationTitleJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.UpdateAssistantConversationTitle(r.Context(), &controlplanev1.UpdateAssistantConversationTitleRequest{Mutation: m, ConversationRef: ref, Title: body.Title})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "conversation", "")
}
func (server *Server) AddAssistantTurn(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.AddAssistantTurnParams) {
	body, ok := decodeJSON[generated.AddAssistantTurnJSONBody](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Assistant.AddAssistantTurn(r.Context(), &controlplanev1.AddAssistantTurnRequest{Mutation: m, ConversationRef: ref, Content: body.Content, AttachmentSetRef: stringValue(body.AttachmentSetRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "conversation", "")
}
func (server *Server) ApplyAssistantPlan(w http.ResponseWriter, r *http.Request, ref generated.PlanRef, p generated.ApplyAssistantPlanParams) {
	body, decoded := decodeJSON[generated.ApplyAssistantPlanJSONBody](w, r)
	if !decoded {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.ApplyAssistantPlan(r.Context(), &controlplanev1.ApplyAssistantPlanRequest{Mutation: m, PlanRef: ref, Revision: body.Revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}
func (server *Server) UpdateAssistantPlanDraft(w http.ResponseWriter, r *http.Request, ref generated.PlanRef, p generated.UpdateAssistantPlanDraftParams) {
	body, ok := decodeJSON[generated.UpdateAssistantPlanDraftJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.UpdateAssistantPlanDraft(r.Context(), &controlplanev1.UpdateAssistantPlanDraftRequest{Mutation: m, PlanRef: ref, Summary: body.Summary, Operations: assistantPlanOperationsInput(body.Operations)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "plan", "")
}
func (server *Server) ValidateAssistantPlan(w http.ResponseWriter, r *http.Request, ref generated.PlanRef, p generated.ValidateAssistantPlanParams) {
	body, ok := decodeJSON[generated.ValidateAssistantPlanJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.ValidateAssistantPlan(r.Context(), &controlplanev1.ValidateAssistantPlanRequest{Mutation: m, PlanRef: ref, Revision: body.Revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "plan", "")
}
func (server *Server) RejectAssistantPlan(w http.ResponseWriter, r *http.Request, ref generated.PlanRef, p generated.RejectAssistantPlanParams) {
	body, ok := decodeJSON[generated.RejectAssistantPlanJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.RejectAssistantPlan(r.Context(), &controlplanev1.RejectAssistantPlanRequest{Mutation: m, PlanRef: ref, Revision: body.Revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}

func assistantContextInput(input *generated.AssistantContextDescriptor) *controlplanev1.AssistantContextDescriptor {
	if input == nil {
		return nil
	}
	result := &controlplanev1.AssistantContextDescriptor{Route: input.Route, EntityKind: input.EntityKind,
		EntityRef: input.EntityRef, EntityName: input.EntityName, EntityVersion: input.EntityVersion}
	for _, operation := range input.AllowedOperations {
		result.AllowedOperations = append(result.AllowedOperations,
			controlplanev1.AssistantPlanOperation_Type(controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+string(operation)]))
	}
	return result
}

func assistantPlanOperationsInput(items []generated.AssistantPlanOperationInput) []*controlplanev1.AssistantPlanOperation {
	result := make([]*controlplanev1.AssistantPlanOperation, 0, len(items))
	for _, item := range items {
		parameters, _ := structpb.NewStruct(item.Parameters)
		before, _ := structpb.NewStruct(item.Before)
		after, _ := structpb.NewStruct(item.After)
		targetRef := ""
		if item.Target.Ref != nil {
			targetRef = string(*item.Target.Ref)
		}
		result = append(result, &controlplanev1.AssistantPlanOperation{Ref: string(item.Ref),
			Type:   controlplanev1.AssistantPlanOperation_Type(controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+string(item.Type)]),
			Action: controlplanev1.AssistantPlanOperation_Action(controlplanev1.AssistantPlanOperation_Action_value["ACTION_"+string(item.Action)]),
			Title:  item.Title, Summary: item.Summary, TargetKind: item.Target.Kind, TargetRef: targetRef,
			TargetName: item.Target.Name, ExpectedVersion: item.ExpectedVersion, Parameters: parameters, Before: before,
			After: after, Selected: item.Selected})
	}
	return result
}
func (server *Server) UpdateSystemAssistantOwnerInstructions(w http.ResponseWriter, r *http.Request, p generated.UpdateSystemAssistantOwnerInstructionsParams) {
	body, ok := decodeJSON[generated.UpdateSystemAssistantOwnerInstructionsJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.UpdateAssistantOwnerInstructions(r.Context(), &controlplanev1.UpdateAssistantOwnerInstructionsRequest{Mutation: m, Instructions: body.OwnerInstructions})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "assistant", "")
}
func (server *Server) CommandSystemAssistant(w http.ResponseWriter, r *http.Request, p generated.CommandSystemAssistantParams) {
	body, ok := decodeJSON[generated.CommandSystemAssistantJSONBody](w, r)
	if !ok {
		return
	}
	if body.Action != generated.RECOVER {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.RecoverSystemAssistant(r.Context(), &controlplanev1.RecoverSystemAssistantRequest{Mutation: m})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "assistant", "")
}
