package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
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
	response, err := server.control.Assistant.CreateAssistantConversation(r.Context(), &controlplanev1.CreateAssistantConversationRequest{Mutation: m, Title: body.Title, ProjectRef: stringValue(body.ProjectRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "conversation", "")
}
func (server *Server) AddAssistantTurn(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.AddAssistantTurnParams) {
	body, ok := decodeJSON[generated.AddAssistantTurnJSONBody](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Assistant.AddAssistantTurn(r.Context(), &controlplanev1.AddAssistantTurnRequest{Mutation: m, ConversationRef: ref, Content: body.Content, ArtifactRefs: sliceOrEmpty(body.ArtifactRefs)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "conversation", "")
}
func (server *Server) ApplyAssistantPlan(w http.ResponseWriter, r *http.Request, ref generated.PlanRef, p generated.ApplyAssistantPlanParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.ApplyAssistantPlan(r.Context(), &controlplanev1.ApplyAssistantPlanRequest{Mutation: m, PlanRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
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
