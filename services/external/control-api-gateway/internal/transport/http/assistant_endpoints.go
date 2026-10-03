package httptransport

import (
	"net/http"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/structpb"
)

const maximumAssistantPlanDraftJSONBody = 2 << 20

func (server *Server) GetSystemAssistant(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Assistant.GetSystemAssistant(r.Context(), &controlplanev1.GetSystemAssistantRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "assistant", "")
}

func (server *Server) GetProjectAssistant(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef) {
	if !opaqueHTTPReference.MatchString(projectRef) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	response, err := server.control.Assistant.GetProjectAssistant(r.Context(), &controlplanev1.GetProjectAssistantRequest{ProjectRef: projectRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validProjectAssistantProfile(response.GetProfile(), projectRef) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "profile", "")
}

func (server *Server) CreateProjectAssistant(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateProjectAssistantParams) {
	body, ok := decodeJSON[generated.CreateProjectAssistantJSONBody](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(projectRef) || !validSearchText(body.Name, 1, 160) ||
		!validSearchText(body.Purpose, 1, 2000) || !utf8.ValidString(body.Instructions) ||
		strings.TrimSpace(body.Instructions) == "" || utf8.RuneCountInString(body.Instructions) > 32768 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	r, ok = withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	response, err := server.control.Assistant.CreateProjectAssistant(r.Context(), &controlplanev1.CreateProjectAssistantRequest{
		Mutation: mutation, ProjectRef: projectRef, Name: body.Name, Purpose: body.Purpose, Instructions: body.Instructions,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validProjectAssistantProfile(response.GetProfile(), projectRef) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusCreated, response, "profile", "")
}

func validProjectAssistantProfile(profile *controlplanev1.ProjectAssistantProfile, projectRef string) bool {
	return profile != nil && strings.HasPrefix(profile.Ref, "asstp_") && opaqueHTTPReference.MatchString(profile.Ref) &&
		profile.ProjectRef == projectRef && strings.HasPrefix(profile.AgentRef, "agt_") && opaqueHTTPReference.MatchString(profile.AgentRef) &&
		validManagedVersion(profile.Version) && validSearchText(profile.Name, 1, 160) && profile.CreatedAt.CheckValid() == nil && profile.UpdatedAt.CheckValid() == nil &&
		(profile.State == "ACTIVE" || profile.State == "DISABLED" || profile.State == "ARCHIVED")
}

func assistantScopeInput(scope generated.AssistantScope) controlplanev1.AssistantScope {
	return controlplanev1.AssistantScope(controlplanev1.AssistantScope_value["ASSISTANT_SCOPE_"+string(scope)])
}

func (server *Server) ListAssistantConversations(w http.ResponseWriter, r *http.Request, p generated.ListAssistantConversationsParams) {
	state := controlplanev1.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE
	if !validSearchText(stringValue(p.Query), 0, 200) || p.State != nil && !p.State.Valid() ||
		p.AssistantScope != nil && !p.AssistantScope.Valid() || p.AssistantRef != nil && !opaqueHTTPReference.MatchString(*p.AssistantRef) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if p.State != nil {
		state = controlplanev1.AssistantConversationState(controlplanev1.AssistantConversationState_value["ASSISTANT_CONVERSATION_STATE_"+string(*p.State)])
	}
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	query := stringValue(p.Query)
	matchLocalizedDefaultTitle := false
	if localizer, ok := w.(interface{ Localize(string) string }); ok && strings.TrimSpace(query) != "" {
		localized := localizer.Localize("NEW_ASSISTANT_CONVERSATION")
		matchLocalizedDefaultTitle = strings.Contains(strings.ToLower(localized), strings.ToLower(strings.TrimSpace(query)))
	}
	request := &controlplanev1.ListAssistantConversationsRequest{ProjectRef: stringValue(p.ProjectRef), Query: query, State: state, MatchLocalizedDefaultTitle: matchLocalizedDefaultTitle, Page: page(p.PageSize, p.PageToken), AssistantRef: stringValue(p.AssistantRef)}
	if p.AssistantScope != nil {
		request.AssistantScope = assistantScopeInput(*p.AssistantScope)
	}
	response, err := server.control.Assistant.ListAssistantConversations(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.Conversations) > int(page(p.PageSize, p.PageToken).PageSize) ||
		len(response.GetPage().GetNextPageToken()) > 512 || !utf8.ValidString(response.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	for _, conversation := range response.Conversations {
		if conversation == nil || !opaqueHTTPReference.MatchString(conversation.Ref) || conversation.State != state ||
			p.ProjectRef != nil && conversation.GetProjectRef() != *p.ProjectRef ||
			p.AssistantScope != nil && conversation.GetAssistantScope() != request.AssistantScope ||
			p.AssistantRef != nil && conversation.GetAssistantRef() != *p.AssistantRef {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
	}
	writeMessage(w, http.StatusOK, response, "", "conversations")
}

func (server *Server) ArchiveAssistantConversation(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.ArchiveAssistantConversationParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.ArchiveAssistantConversation(r.Context(), &controlplanev1.ArchiveAssistantConversationRequest{Mutation: mutation, ConversationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	conversation := response.GetConversation()
	if conversation == nil || conversation.Ref != ref || !validManagedVersion(conversation.Version) || conversation.State != controlplanev1.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ARCHIVED {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "conversation", "")
}
func (server *Server) RestoreAssistantConversation(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.RestoreAssistantConversationParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.RestoreAssistantConversation(r.Context(), &controlplanev1.RestoreAssistantConversationRequest{Mutation: mutation, ConversationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	conversation := response.GetConversation()
	if conversation == nil || conversation.Ref != ref || !validManagedVersion(conversation.Version) || conversation.State != controlplanev1.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "conversation", "")
}
func (server *Server) PurgeAssistantConversation(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.PurgeAssistantConversationParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.PurgeAssistantConversation(r.Context(), &controlplanev1.PurgeAssistantConversationRequest{Mutation: mutation, ConversationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetConversationRef() != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (server *Server) MoveAssistantConversationToProject(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.MoveAssistantConversationToProjectParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	body, ok := decodeJSON[generated.MoveAssistantConversationToProjectJSONBody](w, r)
	if !ok {
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.MoveAssistantConversationToProject(r.Context(), &controlplanev1.MoveAssistantConversationToProjectRequest{
		Mutation: mutation, ConversationRef: ref, ProjectRef: body.ProjectRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	conversation := response.GetConversation()
	if conversation == nil || conversation.Ref != ref || conversation.ProjectRef != body.ProjectRef || !validManagedVersion(conversation.Version) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "conversation", "")
}
func (server *Server) CreateAssistantConversation(w http.ResponseWriter, r *http.Request, p generated.CreateAssistantConversationParams) {
	body, ok := decodeJSON[generated.CreateAssistantConversationJSONBody](w, r)
	if !ok {
		return
	}
	if !body.AssistantScope.Valid() || body.AssistantScope == generated.AssistantScopePROJECT && (body.ProjectRef == nil || !opaqueHTTPReference.MatchString(*body.ProjectRef)) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Assistant.CreateAssistantConversation(r.Context(), &controlplanev1.CreateAssistantConversationRequest{Mutation: m, ProjectRef: stringValue(body.ProjectRef), Context: assistantContextInput(body.Context), AssistantScope: assistantScopeInput(body.AssistantScope)})
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
	deliveryMode := controlplanev1.AssistantTurnDeliveryMode_ASSISTANT_TURN_DELIVERY_MODE_QUEUE
	if body.DeliveryMode != nil && string(*body.DeliveryMode) == "INTERRUPT_ACTIVE" {
		deliveryMode = controlplanev1.AssistantTurnDeliveryMode_ASSISTANT_TURN_DELIVERY_MODE_INTERRUPT_ACTIVE
	}
	response, err := server.control.Assistant.AddAssistantTurn(r.Context(), &controlplanev1.AddAssistantTurnRequest{Mutation: m, ConversationRef: ref, Content: body.Content, AttachmentSetRef: stringValue(body.AttachmentSetRef), Context: assistantContextInput(body.Context), DeliveryMode: deliveryMode})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "conversation", "")
}
func (server *Server) CancelAssistantTurn(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.CancelAssistantTurnParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	m, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.CancelAssistantTurn(r.Context(), &controlplanev1.CancelAssistantTurnRequest{Mutation: m, ConversationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetConversationRef() != ref || response.GetCancelled() && !opaqueHTTPReference.MatchString(response.GetRunRef()) || !response.GetCancelled() && response.GetRunRef() != "" {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
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
	body, ok := decodeJSONWithLimit[generated.UpdateAssistantPlanDraftJSONBody](w, r, maximumAssistantPlanDraftJSONBody)
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
		targetVersion := item.Target.Version
		result = append(result, &controlplanev1.AssistantPlanOperation{Ref: string(item.Ref),
			Type:   controlplanev1.AssistantPlanOperation_Type(controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+string(item.Type)]),
			Action: controlplanev1.AssistantPlanOperation_Action(controlplanev1.AssistantPlanOperation_Action_value["ACTION_"+string(item.Action)]),
			Title:  item.Title, Summary: item.Summary, TargetKind: item.Target.Kind, TargetRef: targetRef,
			TargetName: item.Target.Name, TargetVersion: targetVersion, ExpectedVersion: item.ExpectedVersion, Parameters: parameters, Before: before,
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
