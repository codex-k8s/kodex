package grpc

import (
	"context"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

func (server *Server) CompleteOnboarding(ctx context.Context, request *controlplanev1.CompleteOnboardingRequest) (*controlplanev1.CompleteOnboardingResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CompleteOnboarding_FullMethodName, command.CompleteOnboarding, request.GetMutation(), struct{}{})
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx, controlplanev1.PlatformCommandService_CompleteOnboarding_FullMethodName)
	if err != nil {
		return nil, err
	}
	state, err := server.service.GetBootstrapState(ctx, p)
	if err != nil {
		return nil, transportError(err)
	}
	_ = result
	return &controlplanev1.CompleteOnboardingResponse{State: castBootstrap(state)}, nil
}

func (server *Server) CreateProject(ctx context.Context, request *controlplanev1.CreateProjectRequest) (*controlplanev1.CreateProjectResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateProject_FullMethodName, command.CreateProject, request.GetMutation(), command.ProjectInput{Name: request.GetName(), Purpose: request.GetPurpose(), Language: request.GetLanguage()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateProjectResponse{Project: castProject(*result.Project)}, nil
}

func (server *Server) UpdateProject(ctx context.Context, request *controlplanev1.UpdateProjectRequest) (*controlplanev1.UpdateProjectResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UpdateProject_FullMethodName, command.UpdateProject, request.GetMutation(), command.ProjectInput{Ref: request.GetProjectRef(), Name: request.GetName(), Purpose: request.GetPurpose(), Language: request.GetLanguage()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateProjectResponse{Project: castProject(*result.Project)}, nil
}

func (server *Server) AddPlatformMembership(ctx context.Context, request *controlplanev1.AddPlatformMembershipRequest) (*controlplanev1.AddPlatformMembershipResponse, error) {
	payload := command.PlatformMembershipInput{UserRef: request.GetUserRef(), Role: enumSuffix(request.GetRole(), "PLATFORM_ROLE_"), Active: true}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_AddPlatformMembership_FullMethodName, command.AddPlatformMembership, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.AddPlatformMembershipResponse{Membership: castMembership(*result.Membership)}, nil
}

func (server *Server) ChangePlatformMembership(ctx context.Context, request *controlplanev1.ChangePlatformMembershipRequest) (*controlplanev1.ChangePlatformMembershipResponse, error) {
	payload := command.PlatformMembershipInput{MembershipRef: request.GetMembershipRef(), Role: enumSuffix(request.GetRole(), "PLATFORM_ROLE_"), Active: request.GetActive()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ChangePlatformMembership_FullMethodName, command.ChangePlatformMembership, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangePlatformMembershipResponse{Membership: castMembership(*result.Membership)}, nil
}

func (server *Server) RemovePlatformMembership(ctx context.Context, request *controlplanev1.RemovePlatformMembershipRequest) (*controlplanev1.RemovePlatformMembershipResponse, error) {
	payload := command.PlatformMembershipInput{MembershipRef: request.GetMembershipRef()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RemovePlatformMembership_FullMethodName, command.RemovePlatformMembership, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RemovePlatformMembershipResponse{Membership: castMembership(*result.Membership)}, nil
}

func (server *Server) AddProjectMembership(ctx context.Context, request *controlplanev1.AddProjectMembershipRequest) (*controlplanev1.AddProjectMembershipResponse, error) {
	payload := command.MembershipInput{ProjectRef: request.GetProjectRef(), UserRef: request.GetUserRef(), Permissions: domainProjectPermissions(request.GetPermissions()), Active: true}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_AddProjectMembership_FullMethodName, command.AddMembership, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.AddProjectMembershipResponse{Membership: castMembership(*result.Membership)}, nil
}

func (server *Server) ChangeProjectMembership(ctx context.Context, request *controlplanev1.ChangeProjectMembershipRequest) (*controlplanev1.ChangeProjectMembershipResponse, error) {
	payload := command.MembershipInput{ProjectRef: request.GetProjectRef(), MembershipRef: request.GetMembershipRef(), Permissions: domainProjectPermissions(request.GetPermissions()), Active: request.GetActive()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ChangeProjectMembership_FullMethodName, command.ChangeMembership, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangeProjectMembershipResponse{Membership: castMembership(*result.Membership)}, nil
}

func (server *Server) RemoveProjectMembership(ctx context.Context, request *controlplanev1.RemoveProjectMembershipRequest) (*controlplanev1.RemoveProjectMembershipResponse, error) {
	payload := command.MembershipInput{ProjectRef: request.GetProjectRef(), MembershipRef: request.GetMembershipRef()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RemoveProjectMembership_FullMethodName, command.RemoveMembership, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RemoveProjectMembershipResponse{Membership: castMembership(*result.Membership)}, nil
}

func (server *Server) CreateAgent(ctx context.Context, request *controlplanev1.CreateAgentRequest) (*controlplanev1.CreateAgentResponse, error) {
	payload := command.AgentInput{ProjectRef: request.GetProjectRef(), RoleDefinitionRef: request.GetRoleDefinitionRef(), Name: request.GetName(), Purpose: request.GetPurpose(), RoleDescription: request.GetRoleDescription(), AvatarURL: request.GetAvatarUrl(), RuntimeRef: request.GetRuntimeRef(), Instructions: request.GetInitialInstructions(), Enabled: true}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateAgent_FullMethodName, command.CreateAgent, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateAgentResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) UpdateAgent(ctx context.Context, request *controlplanev1.UpdateAgentRequest) (*controlplanev1.UpdateAgentResponse, error) {
	payload := command.AgentInput{Ref: request.GetAgentRef(), RoleDefinitionRef: request.GetRoleDefinitionRef(), Name: request.GetName(), Purpose: request.GetPurpose(), RoleDescription: request.GetRoleDescription(), AvatarURL: request.GetAvatarUrl(), RuntimeRef: request.GetRuntimeRef()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UpdateAgent_FullMethodName, command.UpdateAgent, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateAgentResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) SetAgentEnabled(ctx context.Context, request *controlplanev1.SetAgentEnabledRequest) (*controlplanev1.SetAgentEnabledResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_SetAgentEnabled_FullMethodName, command.SetAgentEnabled, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef(), Enabled: request.GetEnabled()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.SetAgentEnabledResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) ArchiveAgent(ctx context.Context, request *controlplanev1.ArchiveAgentRequest) (*controlplanev1.ArchiveAgentResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ArchiveAgent_FullMethodName, command.ArchiveAgent, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ArchiveAgentResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) CreateInstructionDraft(ctx context.Context, request *controlplanev1.CreateInstructionDraftRequest) (*controlplanev1.CreateInstructionDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateInstructionDraft_FullMethodName, command.CreateInstructions, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef(), Instructions: request.GetContent()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateInstructionDraftResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) ValidateInstructionDraft(ctx context.Context, request *controlplanev1.ValidateInstructionDraftRequest) (*controlplanev1.ValidateInstructionDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ValidateInstructionDraft_FullMethodName, command.ValidateInstructions, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ValidateInstructionDraftResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) PublishInstructionDraft(ctx context.Context, request *controlplanev1.PublishInstructionDraftRequest) (*controlplanev1.PublishInstructionDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishInstructionDraft_FullMethodName, command.PublishInstructions, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PublishInstructionDraftResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) RollbackInstructions(ctx context.Context, request *controlplanev1.RollbackInstructionsRequest) (*controlplanev1.RollbackInstructionsResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RollbackInstructions_FullMethodName, command.RollbackInstructions, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef(), Instructions: request.GetPublishedInstructionRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RollbackInstructionsResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) ChangeAgentCapability(ctx context.Context, request *controlplanev1.ChangeAgentCapabilityRequest) (*controlplanev1.ChangeAgentCapabilityResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ChangeAgentCapability_FullMethodName, command.ChangeAgentCapability, request.GetMutation(), command.AgentBindingInput{AgentRef: request.GetAgentRef(), BindingRef: request.GetCapabilityKey(), Enabled: request.GetEnabled()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangeAgentCapabilityResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) ChangeAgentIntegrationGrant(ctx context.Context, request *controlplanev1.ChangeAgentIntegrationGrantRequest) (*controlplanev1.ChangeAgentIntegrationGrantResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ChangeAgentIntegrationGrant_FullMethodName, command.ChangeAgentGrant, request.GetMutation(), command.AgentBindingInput{AgentRef: request.GetAgentRef(), BindingRef: request.GetGrantRef(), Enabled: request.GetEnabled()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ChangeAgentIntegrationGrantResponse{Agent: castAgent(*result.Agent)}, nil
}

func (server *Server) CreateWorkflow(ctx context.Context, request *controlplanev1.CreateWorkflowRequest) (*controlplanev1.CreateWorkflowResponse, error) {
	payload := command.WorkflowInput{ProjectRef: request.GetProjectRef(), Name: request.GetName(), Purpose: request.GetPurpose(), CoordinatorAgentRef: request.GetCoordinatorAgentRef(), Draft: domainWorkflowVersion(request.GetDraft())}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CreateWorkflow_FullMethodName, command.CreateWorkflow, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CreateWorkflowResponse{Workflow: castWorkflow(*result.Workflow)}, nil
}

func (server *Server) UpdateWorkflowDraft(ctx context.Context, request *controlplanev1.UpdateWorkflowDraftRequest) (*controlplanev1.UpdateWorkflowDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_UpdateWorkflowDraft_FullMethodName, command.UpdateWorkflow, request.GetMutation(), command.WorkflowInput{Ref: request.GetWorkflowRef(), Draft: domainWorkflowVersion(request.GetDraft())})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.UpdateWorkflowDraftResponse{Workflow: castWorkflow(*result.Workflow)}, nil
}

func (server *Server) ValidateWorkflowDraft(ctx context.Context, request *controlplanev1.ValidateWorkflowDraftRequest) (*controlplanev1.ValidateWorkflowDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ValidateWorkflowDraft_FullMethodName, command.ValidateWorkflow, request.GetMutation(), command.WorkflowInput{Ref: request.GetWorkflowRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ValidateWorkflowDraftResponse{Workflow: castWorkflow(*result.Workflow)}, nil
}

func (server *Server) PublishWorkflowDraft(ctx context.Context, request *controlplanev1.PublishWorkflowDraftRequest) (*controlplanev1.PublishWorkflowDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishWorkflowDraft_FullMethodName, command.PublishWorkflow, request.GetMutation(), command.WorkflowInput{Ref: request.GetWorkflowRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.PublishWorkflowDraftResponse{Workflow: castWorkflow(*result.Workflow)}, nil
}

func (server *Server) ArchiveWorkflow(ctx context.Context, request *controlplanev1.ArchiveWorkflowRequest) (*controlplanev1.ArchiveWorkflowResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ArchiveWorkflow_FullMethodName, command.ArchiveWorkflow, request.GetMutation(), command.WorkflowInput{Ref: request.GetWorkflowRef()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ArchiveWorkflowResponse{Workflow: castWorkflow(*result.Workflow)}, nil
}

func (server *Server) LaunchRun(ctx context.Context, request *controlplanev1.LaunchRunRequest) (*controlplanev1.LaunchRunResponse, error) {
	payload := command.LaunchRunInput{ProjectRef: request.GetProjectRef(), Title: request.GetTitle(), Task: request.GetTask(), SessionRef: request.GetSessionRef(), Source: enumSuffix(request.GetSource(), "RUN_SOURCE_"), Target: runTarget(request.GetTarget()), Input: asMap(request.GetInput()), ArtifactRefs: request.GetArtifactRefs()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_LaunchRun_FullMethodName, command.LaunchRun, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.LaunchRunResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}, nil
}

func (server *Server) AddSessionTurn(ctx context.Context, request *controlplanev1.AddSessionTurnRequest) (*controlplanev1.AddSessionTurnResponse, error) {
	payload := command.SessionTurnInput{SessionRef: request.GetSessionRef(), RunRef: request.GetRunRef(), NodeRef: request.GetNodeRef(), Task: request.GetTask(), ArtifactRefs: request.GetArtifactRefs()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_AddSessionTurn_FullMethodName, command.AddSessionTurn, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.AddSessionTurnResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}, nil
}

func (server *Server) CancelRun(ctx context.Context, request *controlplanev1.CancelRunRequest) (*controlplanev1.CancelRunResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_CancelRun_FullMethodName, command.CancelRun, request.GetMutation(), command.RunCommandInput{RunRef: request.GetRunRef(), Reason: request.GetReason()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.CancelRunResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}, nil
}

func (server *Server) RetryRun(ctx context.Context, request *controlplanev1.RetryRunRequest) (*controlplanev1.RetryRunResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_RetryRun_FullMethodName, command.RetryRun, request.GetMutation(), command.RunCommandInput{RunRef: request.GetRunRef(), Reason: request.GetReason()})
	if err != nil {
		return nil, err
	}
	return &controlplanev1.RetryRunResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}, nil
}

func (server *Server) ResolveOwnerGate(ctx context.Context, request *controlplanev1.ResolveOwnerGateRequest) (*controlplanev1.ResolveOwnerGateResponse, error) {
	payload := command.GateResolutionInput{GateRef: request.GetGateRef(), Decision: enumSuffix(request.GetDecision(), "OWNER_GATE_DECISION_"), Comment: request.GetComment(), ArtifactRefs: request.GetArtifactRefs()}
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_ResolveOwnerGate_FullMethodName, command.ResolveOwnerGate, request.GetMutation(), payload)
	if err != nil {
		return nil, err
	}
	return &controlplanev1.ResolveOwnerGateResponse{Gate: castGate(*result.Gate), Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}, nil
}
