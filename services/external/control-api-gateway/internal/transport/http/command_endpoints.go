package httptransport

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	scheduleActionEnable  = "ENABLE"
	scheduleActionPause   = "PAUSE"
	scheduleActionArchive = "ARCHIVE"
)

func (server *Server) CompleteOnboarding(w http.ResponseWriter, r *http.Request, p generated.CompleteOnboardingParams) {
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	if m == nil {
		return
	}
	response, err := server.control.Command.CompleteOnboarding(r.Context(), &controlplanev1.CompleteOnboardingRequest{Mutation: m})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "state", "")
}
func (server *Server) CreateProject(w http.ResponseWriter, r *http.Request, p generated.CreateProjectParams) {
	body, ok := decodeJSON[generated.ProjectInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.CreateProject(r.Context(), &controlplanev1.CreateProjectRequest{Mutation: m, Name: body.Name, Purpose: body.Purpose, Language: string(body.Language)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "project", "")
}
func (server *Server) UpdateProject(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.UpdateProjectParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.ProjectInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.UpdateProject(r.Context(), &controlplanev1.UpdateProjectRequest{Mutation: m, ProjectRef: ref, Name: body.Name, Purpose: body.Purpose, Language: string(body.Language)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "project", "")
}
func (server *Server) AddPlatformMembership(w http.ResponseWriter, r *http.Request, p generated.AddPlatformMembershipParams) {
	body, ok := decodeJSON[generated.PlatformMembershipCreateInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Command.AddPlatformMembership(r.Context(), &controlplanev1.AddPlatformMembershipRequest{Mutation: m, UserRef: body.UserRef, Role: platformRole(string(body.PlatformRole))})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "membership", "")
}
func (server *Server) ChangePlatformMembership(w http.ResponseWriter, r *http.Request, membershipRef generated.MembershipRef, p generated.ChangePlatformMembershipParams) {
	body, ok := decodeJSON[generated.PlatformMembershipChangeInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ChangePlatformMembership(r.Context(), &controlplanev1.ChangePlatformMembershipRequest{Mutation: m, MembershipRef: membershipRef, Role: platformRole(string(body.PlatformRole)), Active: body.Active})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "membership", "")
}
func (server *Server) RemovePlatformMembership(w http.ResponseWriter, r *http.Request, membershipRef generated.MembershipRef, p generated.RemovePlatformMembershipParams) {
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RemovePlatformMembership(r.Context(), &controlplanev1.RemovePlatformMembershipRequest{Mutation: m, MembershipRef: membershipRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "membership", "")
}
func (server *Server) AddProjectMembership(w http.ResponseWriter, r *http.Request, ref generated.ProjectRef, p generated.AddProjectMembershipParams) {
	r, ok := withProjectReference(w, r, ref)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.ProjectMembershipCreateInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.AddProjectMembership(r.Context(), &controlplanev1.AddProjectMembershipRequest{Mutation: m, ProjectRef: ref, UserRef: body.UserRef, Permissions: permissions(body.Permissions)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "membership", "")
}
func (server *Server) ChangeProjectMembership(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, membershipRef generated.MembershipRef, p generated.ChangeProjectMembershipParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.ProjectMembershipChangeInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ChangeProjectMembership(r.Context(), &controlplanev1.ChangeProjectMembershipRequest{Mutation: m, ProjectRef: projectRef, MembershipRef: membershipRef, Permissions: permissions(body.Permissions), Active: body.Active})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "membership", "")
}
func (server *Server) RemoveProjectMembership(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, membershipRef generated.MembershipRef, p generated.RemoveProjectMembershipParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RemoveProjectMembership(r.Context(), &controlplanev1.RemoveProjectMembershipRequest{Mutation: m, ProjectRef: projectRef, MembershipRef: membershipRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "membership", "")
}

func (server *Server) CreateAgent(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateAgentParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.AgentInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.CreateAgent(r.Context(), &controlplanev1.CreateAgentRequest{Mutation: m, ProjectRef: projectRef, Name: body.Name, Purpose: body.Purpose, RoleDescription: body.RoleDescription, RoleDefinitionRef: stringValue(body.RoleDefinitionRef), AvatarUrl: stringValue(body.AvatarUrl), RuntimeRef: stringValue(body.RuntimeRef), InitialInstructions: stringValue(body.InitialInstructions)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "agent", "")
}
func (server *Server) UpdateAgent(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.UpdateAgentParams) {
	body, ok := decodeJSON[generated.AgentInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.UpdateAgent(r.Context(), &controlplanev1.UpdateAgentRequest{Mutation: m, AgentRef: ref, Name: body.Name, Purpose: body.Purpose, RoleDescription: body.RoleDescription, RoleDefinitionRef: stringValue(body.RoleDefinitionRef), AvatarUrl: stringValue(body.AvatarUrl), RuntimeRef: stringValue(body.RuntimeRef)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}
func (server *Server) CommandAgent(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.CommandAgentParams) {
	body, ok := decodeJSON[generated.AgentCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	switch body.Action {
	case generated.AgentCommandActionENABLE, generated.AgentCommandActionDISABLE:
		response, err = server.control.Command.SetAgentEnabled(r.Context(), &controlplanev1.SetAgentEnabledRequest{Mutation: m, AgentRef: ref, Enabled: body.Action == generated.AgentCommandActionENABLE})
	case generated.AgentCommandActionARCHIVE:
		response, err = server.control.Command.ArchiveAgent(r.Context(), &controlplanev1.ArchiveAgentRequest{Mutation: m, AgentRef: ref})
	case generated.AgentCommandActionGRANTCAPABILITY, generated.AgentCommandActionREVOKECAPABILITY:
		response, err = server.control.Command.ChangeAgentCapability(r.Context(), &controlplanev1.ChangeAgentCapabilityRequest{Mutation: m, AgentRef: ref, CapabilityKey: stringValue(body.CapabilityKey), Enabled: body.Action == generated.AgentCommandActionGRANTCAPABILITY})
	case generated.AgentCommandActionGRANTINTEGRATION, generated.AgentCommandActionREVOKEINTEGRATION:
		response, err = server.control.Command.ChangeAgentIntegrationGrant(r.Context(), &controlplanev1.ChangeAgentIntegrationGrantRequest{Mutation: m, AgentRef: ref, GrantRef: stringValue(body.GrantRef), Enabled: body.Action == generated.AgentCommandActionGRANTINTEGRATION})
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}

func (server *Server) CreateInstructionDraft(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.CreateInstructionDraftParams) {
	body, ok := decodeJSON[generated.CreateInstructionDraftJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.CreateInstructionDraft(r.Context(), &controlplanev1.CreateInstructionDraftRequest{Mutation: m, AgentRef: ref, Content: body.Content})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "agent", "")
}
func (server *Server) CommandAgentInstructions(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.CommandAgentInstructionsParams) {
	body, ok := decodeJSON[generated.InstructionCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	switch body.Action {
	case generated.InstructionCommandActionVALIDATE:
		response, err = server.control.Command.ValidateInstructionDraft(r.Context(), &controlplanev1.ValidateInstructionDraftRequest{Mutation: m, AgentRef: ref})
	case generated.InstructionCommandActionPUBLISH:
		response, err = server.control.Command.PublishInstructionDraft(r.Context(), &controlplanev1.PublishInstructionDraftRequest{Mutation: m, AgentRef: ref})
	case generated.InstructionCommandActionROLLBACK:
		response, err = server.control.Command.RollbackInstructions(r.Context(), &controlplanev1.RollbackInstructionsRequest{Mutation: m, AgentRef: ref, PublishedInstructionRef: stringValue(body.PublishedInstructionRef)})
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}

func workflowDraft(body generated.WorkflowInput) *controlplanev1.WorkflowVersion {
	draft := &controlplanev1.WorkflowVersion{Name: body.Name, Purpose: body.Purpose, CoordinatorAgentRef: body.CoordinatorAgentRef}
	if body.InputFields != nil {
		for _, field := range *body.InputFields {
			draft.InputFields = append(draft.InputFields, &controlplanev1.WorkflowInputField{Key: stringValue(field.Key), Label: field.Label, Description: field.Description, ValueType: string(field.ValueType), Required: field.Required, Options: append([]string(nil), field.Options...)})
		}
	}
	if body.MaxConcurrency != nil {
		draft.MaxConcurrency = int32(*body.MaxConcurrency)
	}
	if body.TimeoutSeconds != nil {
		draft.TimeoutSeconds = int32(*body.TimeoutSeconds)
	}
	if body.CompletionCriteria != nil {
		draft.CompletionCriteria = *body.CompletionCriteria
	}
	if body.Steps != nil {
		for _, step := range *body.Steps {
			current := &controlplanev1.WorkflowStep{Position: int32(step.Position), Name: step.Name, Purpose: step.Purpose, AgentRef: step.AgentRef, Parallel: step.Parallel, ParallelGroup: int32(step.ParallelGroup), TimeoutSeconds: int32(step.TimeoutSeconds), ExpectedResult: step.ExpectedResult, HumanGate: step.HumanGate, RequiredCapabilityKeys: step.RequiredCapabilityKeys}
			for _, decision := range step.GateDecisions {
				current.GateDecisions = append(current.GateDecisions, gateDecision(string(decision)))
			}
			draft.Steps = append(draft.Steps, current)
		}
	}
	return draft
}
func (server *Server) CreateWorkflow(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateWorkflowParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.WorkflowInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	request := &controlplanev1.CreateWorkflowRequest{Mutation: m, ProjectRef: projectRef, Name: body.Name, Purpose: body.Purpose, CoordinatorAgentRef: body.CoordinatorAgentRef}
	if body.Steps != nil && len(*body.Steps) > 0 {
		request.Draft = workflowDraft(body)
	}
	response, err := server.control.Command.CreateWorkflow(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "workflow", "")
}
func (server *Server) UpdateWorkflowDraft(w http.ResponseWriter, r *http.Request, ref generated.WorkflowRef, p generated.UpdateWorkflowDraftParams) {
	body, ok := decodeJSON[generated.WorkflowInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.UpdateWorkflowDraft(r.Context(), &controlplanev1.UpdateWorkflowDraftRequest{Mutation: m, WorkflowRef: ref, Draft: workflowDraft(body)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "workflow", "")
}
func (server *Server) CommandWorkflow(w http.ResponseWriter, r *http.Request, ref generated.WorkflowRef, p generated.CommandWorkflowParams) {
	body, ok := decodeJSON[generated.WorkflowCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	switch body.Action {
	case generated.WorkflowCommandActionVALIDATE:
		response, err = server.control.Command.ValidateWorkflowDraft(r.Context(), &controlplanev1.ValidateWorkflowDraftRequest{Mutation: m, WorkflowRef: ref})
	case generated.WorkflowCommandActionPUBLISH:
		response, err = server.control.Command.PublishWorkflowDraft(r.Context(), &controlplanev1.PublishWorkflowDraftRequest{Mutation: m, WorkflowRef: ref})
	case generated.WorkflowCommandActionARCHIVE:
		response, err = server.control.Command.ArchiveWorkflow(r.Context(), &controlplanev1.ArchiveWorkflowRequest{Mutation: m, WorkflowRef: ref})
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "workflow", "")
}

func (server *Server) CreateRun(w http.ResponseWriter, r *http.Request, p generated.CreateRunParams) {
	body, ok := decodeJSON[generated.RunInput](w, r)
	if !ok {
		return
	}
	input, _ := structpb.NewStruct(valueOrEmpty(body.Input))
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.LaunchRun(r.Context(), &controlplanev1.LaunchRunRequest{Mutation: m, ProjectRef: body.ProjectRef, Target: targetProto(string(body.TargetType), body.TargetRef), Title: body.Title, Task: body.Task, Input: input, ArtifactRefs: sliceOrEmpty(body.ArtifactRefs), SessionRef: stringValue(body.SessionRef), Source: controlplanev1.RunSource_RUN_SOURCE_CONTROL_CENTER})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "", "")
}
func valueOrEmpty(value *map[string]interface{}) map[string]interface{} {
	if value == nil {
		return map[string]interface{}{}
	}
	return *value
}
func sliceOrEmpty(value *[]string) []string {
	if value == nil {
		return nil
	}
	return *value
}
func (server *Server) CommandRun(w http.ResponseWriter, r *http.Request, ref generated.RunRef, p generated.CommandRunParams) {
	body, ok := decodeJSON[generated.RunCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	if body.Action == generated.RunCommandActionCANCEL {
		response, err = server.control.Command.CancelRun(r.Context(), &controlplanev1.CancelRunRequest{Mutation: m, RunRef: ref, Reason: stringValue(body.Reason)})
	} else if body.Action == generated.RunCommandActionRETRY {
		response, err = server.control.Command.RetryRun(r.Context(), &controlplanev1.RetryRunRequest{Mutation: m, RunRef: ref, Reason: stringValue(body.Reason)})
	} else {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}
func (server *Server) AddSessionTurn(w http.ResponseWriter, r *http.Request, sessionRef generated.SessionRef, p generated.AddSessionTurnParams) {
	body, ok := decodeJSON[generated.TurnInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	response, err := server.control.Command.AddSessionTurn(r.Context(), &controlplanev1.AddSessionTurnRequest{Mutation: m, SessionRef: sessionRef, RunRef: body.RunRef, NodeRef: stringValue(body.NodeRef), Task: body.Task, ArtifactRefs: sliceOrEmpty(body.ArtifactRefs)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "", "")
}
func (server *Server) ResolveOwnerGate(w http.ResponseWriter, r *http.Request, ref generated.GateRef, p generated.ResolveOwnerGateParams) {
	body, ok := decodeJSON[generated.GateResolution](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ResolveOwnerGate(r.Context(), &controlplanev1.ResolveOwnerGateRequest{Mutation: m, GateRef: ref, Decision: gateDecision(string(body.Decision)), Comment: stringValue(body.Comment)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}

func scheduleInput(body generated.ScheduleInput) (*controlplanev1.RunTarget, *structpb.Struct) {
	input, _ := structpb.NewStruct(body.Input)
	return targetProto(string(body.TargetType), body.TargetRef), input
}
func (server *Server) CreateSchedule(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateScheduleParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.ScheduleInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	target, input := scheduleInput(body)
	response, err := server.control.Command.CreateSchedule(r.Context(), &controlplanev1.CreateScheduleRequest{Mutation: m, ProjectRef: projectRef, Name: body.Name, Target: target, Preset: string(body.Preset), TimeOfDay: body.TimeOfDay, DayOfWeek: stringValue(body.DayOfWeek), Timezone: body.Timezone, Input: input, SessionPolicy: string(body.SessionPolicy), NotificationPolicy: string(body.NotificationPolicy)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "schedule", "")
}
func (server *Server) UpdateSchedule(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.UpdateScheduleParams) {
	body, ok := decodeJSON[generated.ScheduleInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	target, input := scheduleInput(body)
	response, err := server.control.Command.UpdateSchedule(r.Context(), &controlplanev1.UpdateScheduleRequest{Mutation: m, ScheduleRef: ref, Name: body.Name, Target: target, Preset: string(body.Preset), TimeOfDay: body.TimeOfDay, DayOfWeek: stringValue(body.DayOfWeek), Timezone: body.Timezone, Input: input, SessionPolicy: string(body.SessionPolicy), NotificationPolicy: string(body.NotificationPolicy)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "schedule", "")
}
func (server *Server) CommandSchedule(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.CommandScheduleParams) {
	body, ok := decodeJSON[generated.ScheduleCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	switch string(body.Action) {
	case scheduleActionEnable, scheduleActionPause:
		response, err = server.control.Command.SetScheduleEnabled(r.Context(), &controlplanev1.SetScheduleEnabledRequest{Mutation: m, ScheduleRef: ref, Enabled: string(body.Action) == scheduleActionEnable})
	case scheduleActionArchive:
		response, err = server.control.Command.ArchiveSchedule(r.Context(), &controlplanev1.ArchiveScheduleRequest{Mutation: m, ScheduleRef: ref})
	default:
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "schedule", "")
}

func (server *Server) CreateIntegrationConnection(w http.ResponseWriter, r *http.Request, p generated.CreateIntegrationConnectionParams) {
	body, ok := decodeJSON[generated.IntegrationConnectionInput](w, r)
	if !ok {
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	config := map[string]interface{}{}
	if body.PublicConfiguration != nil {
		for key, value := range *body.PublicConfiguration {
			config[key] = value
		}
	}
	public, _ := structpb.NewStruct(config)
	response, err := server.control.Command.CreateIntegrationConnection(r.Context(), &controlplanev1.CreateIntegrationConnectionRequest{Mutation: m, DefinitionKey: body.DefinitionKey, Name: body.Name, PublicConfiguration: public})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "connection", "")
}
func (server *Server) CommandIntegrationConnection(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.CommandIntegrationConnectionParams) {
	body, ok := decodeJSON[generated.IntegrationConnectionCommand](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	var response proto.Message
	var err error
	if body.Action == generated.IntegrationConnectionCommandActionTEST {
		response, err = server.control.Command.TestIntegrationConnection(r.Context(), &controlplanev1.TestIntegrationConnectionRequest{Mutation: m, ConnectionRef: ref})
	} else if body.Action == generated.IntegrationConnectionCommandActionENABLE || body.Action == generated.IntegrationConnectionCommandActionDISABLE {
		response, err = server.control.Command.SetIntegrationConnectionEnabled(r.Context(), &controlplanev1.SetIntegrationConnectionEnabledRequest{Mutation: m, ConnectionRef: ref, Enabled: body.Action == generated.IntegrationConnectionCommandActionENABLE})
	} else {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "connection", "")
}
func (server *Server) ChangeIntegrationGrant(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.ChangeIntegrationGrantParams) {
	body, ok := decodeJSON[generated.IntegrationGrantInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ChangeIntegrationGrant(r.Context(), &controlplanev1.ChangeIntegrationGrantRequest{Mutation: m, ConnectionRef: ref, CapabilityKey: body.CapabilityKey, AgentRef: stringValue(body.AgentRef), WorkflowRef: stringValue(body.WorkflowRef), Enabled: body.Enabled})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "connection", "")
}

func (server *Server) ChangeArtifactBinding(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.ChangeArtifactBindingParams) {
	body, ok := decodeJSON[generated.ArtifactBindingInput](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ChangeArtifactBinding(r.Context(), &controlplanev1.ChangeArtifactBindingRequest{Mutation: m, ArtifactRef: ref, AgentRef: body.AgentRef, Enabled: body.Enabled})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "artifact", "")
}
func (server *Server) UploadArtifact(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.UploadArtifactParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	if r.ContentLength < 0 {
		writeLocalProblem(w, http.StatusLengthRequired, "CONTENT_LENGTH_REQUIRED", false)
		return
	}
	if r.ContentLength > 16<<20 {
		writeLocalProblem(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", false)
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	stream, err := server.control.Command.UploadArtifact(r.Context())
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if err = stream.Send(&controlplanev1.UploadArtifactRequest{Part: &controlplanev1.UploadArtifactRequest_Metadata{Metadata: &controlplanev1.UploadArtifactMetadata{Mutation: m, ProjectRef: projectRef, RunRef: stringValue(p.RunRef), FileName: p.XFileName, MediaType: r.Header.Get("Content-Type"), SizeBytes: r.ContentLength}}}); err != nil {
		writeRPCProblem(w, err)
		return
	}
	buffer := make([]byte, 64<<10)
	reader := io.LimitReader(r.Body, (16<<20)+1)
	var received int64
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 {
			received += int64(count)
			if received > r.ContentLength || received > 16<<20 {
				writeLocalProblem(w, http.StatusBadRequest, "CONTENT_LENGTH_MISMATCH", false)
				return
			}
			if err := stream.Send(&controlplanev1.UploadArtifactRequest{Part: &controlplanev1.UploadArtifactRequest_Chunk{Chunk: append([]byte(nil), buffer[:count]...)}}); err != nil {
				writeRPCProblem(w, err)
				return
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			writeLocalProblem(w, http.StatusBadRequest, "REQUEST_BODY_READ_FAILED", false)
			return
		}
	}
	if received != r.ContentLength {
		writeLocalProblem(w, http.StatusBadRequest, "CONTENT_LENGTH_MISMATCH", false)
		return
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "artifact", "")
}
func (server *Server) DownloadArtifact(w http.ResponseWriter, r *http.Request, ref generated.ArtifactRef, p generated.DownloadArtifactParams) {
	purpose := controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_UNSPECIFIED
	switch p.Purpose {
	case generated.DOWNLOAD:
		purpose = controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_DOWNLOAD
	case generated.PREVIEW:
		purpose = controlplanev1.ArtifactDownloadPurpose_ARTIFACT_DOWNLOAD_PURPOSE_PREVIEW
	}
	stream, err := server.control.Command.DownloadArtifact(r.Context(), &controlplanev1.DownloadArtifactRequest{ArtifactRef: ref, Purpose: purpose})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	metadata, err := stream.Recv()
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if len(metadata.GetData()) != 0 || metadata.GetSizeBytes() < 0 || metadata.GetSizeBytes() > 16<<20 {
		writeLocalProblem(w, http.StatusBadGateway, "UPSTREAM_ARTIFACT_METADATA_INVALID", false)
		return
	}
	fileName := safeAttachmentFileName(metadata.GetFileName())
	mediaType := metadata.GetMediaType()
	if _, _, parseErr := mime.ParseMediaType(mediaType); parseErr != nil {
		mediaType = "application/octet-stream"
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": fileName})
	if disposition == "" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Type", mediaType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Length", strconv.FormatInt(metadata.GetSizeBytes(), 10))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	var written int64
	for {
		chunk, recvErr := stream.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return
		}
		if chunk.GetFileName() != "" || chunk.GetMediaType() != "" || chunk.GetSizeBytes() != 0 {
			return
		}
		written += int64(len(chunk.GetData()))
		if written > metadata.GetSizeBytes() || written > 16<<20 {
			return
		}
		if len(chunk.GetData()) > 0 {
			if _, err := w.Write(chunk.GetData()); err != nil {
				return
			}
		}
	}
	if written != metadata.GetSizeBytes() {
		return
	}
}

func safeAttachmentFileName(value string) string {
	value = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f || character == '/' || character == '\\' {
			return -1
		}
		return character
	}, value)
	value = strings.TrimSpace(value)
	if value == "" {
		return "artifact"
	}
	return value
}
