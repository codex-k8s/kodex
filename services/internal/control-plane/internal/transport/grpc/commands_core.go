package grpc

import (
	"context"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
)

// CompleteOnboarding executes onboarding under the exact RPC principal, then reads fresh bootstrap
// state; a later read failure does not undo the completed command.
// CompleteOnboarding исполняет onboarding под principal точного RPC, затем читает свежее bootstrap
// state; ошибка последующего чтения не отменяет выполненную команду.
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

// CreateProject maps name/purpose/language to the specialized create command and returns the
// owner-assigned project with the caller mutation context.
// CreateProject передаёт name, purpose и language специализированной create command и возвращает
// назначенный владельцем project с caller mutation context.
func (server *Server) CreateProject(ctx context.Context, request *controlplanev1.CreateProjectRequest) (*controlplanev1.CreateProjectResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_CreateProject_FullMethodName, command.CreateProject, request.GetMutation(), command.ProjectInput{Name: request.GetName(), Purpose: request.GetPurpose(), Language: request.GetLanguage()}, func(result command.Result) *controlplanev1.CreateProjectResponse {
		return &controlplanev1.CreateProjectResponse{Project: castProject(*result.Project)}
	})
}

// UpdateProject passes the project locator and editable fields through the exact update command,
// leaving ownership and optimistic version checks to the owner.
// UpdateProject передаёт project locator и редактируемые поля точной update command, оставляя
// ownership и optimistic version checks владельцу.
func (server *Server) UpdateProject(ctx context.Context, request *controlplanev1.UpdateProjectRequest) (*controlplanev1.UpdateProjectResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_UpdateProject_FullMethodName, command.UpdateProject, request.GetMutation(), command.ProjectInput{Ref: request.GetProjectRef(), Name: request.GetName(), Purpose: request.GetPurpose(), Language: request.GetLanguage()}, func(result command.Result) *controlplanev1.UpdateProjectResponse {
		return &controlplanev1.UpdateProjectResponse{Project: castProject(*result.Project)}
	})
}

// TrashProject dispatches the specialized trash transition with the project locator and mutation,
// returning the resulting owner project state.
// TrashProject передаёт project locator и mutation специализированному trash переходу и возвращает
// итоговое owner project state.
func (server *Server) TrashProject(ctx context.Context, request *controlplanev1.TrashProjectRequest) (*controlplanev1.TrashProjectResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_TrashProject_FullMethodName, command.TrashProject, request.GetMutation(), command.ProjectLifecycleInput{Ref: request.GetProjectRef()}, func(result command.Result) *controlplanev1.TrashProjectResponse {
		return &controlplanev1.TrashProjectResponse{Project: castProject(*result.Project)}
	})
}

// RestoreProject dispatches the specialized restore transition rather than assigning project
// lifecycle fields from the request.
// RestoreProject вызывает специализированный restore переход без назначения project lifecycle
// fields из запроса.
func (server *Server) RestoreProject(ctx context.Context, request *controlplanev1.RestoreProjectRequest) (*controlplanev1.RestoreProjectResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_RestoreProject_FullMethodName, command.RestoreProject, request.GetMutation(), command.ProjectLifecycleInput{Ref: request.GetProjectRef()}, func(result command.Result) *controlplanev1.RestoreProjectResponse {
		return &controlplanev1.RestoreProjectResponse{Project: castProject(*result.Project)}
	})
}

// PurgeProject passes the exact project locator to the owner purge command; transport success
// reflects its returned state rather than independently proving all external cleanup.
// PurgeProject передаёт точный project locator owner purge command; успех transport отражает
// возвращённое состояние без самостоятельного доказательства всей внешней очистки.
func (server *Server) PurgeProject(ctx context.Context, request *controlplanev1.PurgeProjectRequest) (*controlplanev1.PurgeProjectResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_PurgeProject_FullMethodName, command.PurgeProject, request.GetMutation(), command.ProjectLifecycleInput{Ref: request.GetProjectRef()}, func(result command.Result) *controlplanev1.PurgeProjectResponse {
		return &controlplanev1.PurgeProjectResponse{Project: castProject(*result.Project)}
	})
}

// AddPlatformMembership maps the selected user/role to an active platform membership command while
// actor and organization come from the verified principal.
// AddPlatformMembership преобразует выбранные user и role в команду active platform membership,
// получая actor и organization из проверенного principal.
func (server *Server) AddPlatformMembership(ctx context.Context, request *controlplanev1.AddPlatformMembershipRequest) (*controlplanev1.AddPlatformMembershipResponse, error) {
	payload := command.PlatformMembershipInput{UserRef: request.GetUserRef(), Role: enumSuffix(request.GetRole(), "PLATFORM_ROLE_"), Active: true}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_AddPlatformMembership_FullMethodName, command.AddPlatformMembership, request.GetMutation(), payload, func(result command.Result) *controlplanev1.AddPlatformMembershipResponse {
		return &controlplanev1.AddPlatformMembershipResponse{Membership: castMembership(*result.Membership)}
	})
}

// ChangePlatformMembership maps membership locator, role suffix and active flag through the
// specialized owner command with mutation/version context.
// ChangePlatformMembership передаёт membership locator, role suffix и active flag
// специализированной owner command с mutation и version context.
func (server *Server) ChangePlatformMembership(ctx context.Context, request *controlplanev1.ChangePlatformMembershipRequest) (*controlplanev1.ChangePlatformMembershipResponse, error) {
	payload := command.PlatformMembershipInput{MembershipRef: request.GetMembershipRef(), Role: enumSuffix(request.GetRole(), "PLATFORM_ROLE_"), Active: request.GetActive()}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ChangePlatformMembership_FullMethodName, command.ChangePlatformMembership, request.GetMutation(), payload, func(result command.Result) *controlplanev1.ChangePlatformMembershipResponse {
		return &controlplanev1.ChangePlatformMembershipResponse{Membership: castMembership(*result.Membership)}
	})
}

// RemovePlatformMembership passes only the membership locator and mutation to the specialized
// removal command, returning the owner result.
// RemovePlatformMembership передаёт только membership locator и mutation специализированной
// removal command и возвращает owner result.
func (server *Server) RemovePlatformMembership(ctx context.Context, request *controlplanev1.RemovePlatformMembershipRequest) (*controlplanev1.RemovePlatformMembershipResponse, error) {
	payload := command.PlatformMembershipInput{MembershipRef: request.GetMembershipRef()}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_RemovePlatformMembership_FullMethodName, command.RemovePlatformMembership, request.GetMutation(), payload, func(result command.Result) *controlplanev1.RemovePlatformMembershipResponse {
		return &controlplanev1.RemovePlatformMembershipResponse{Membership: castMembership(*result.Membership)}
	})
}

// AddProjectMembership maps project/user locators and ordered permission suffixes to an active
// membership command without treating locators as authority.
// AddProjectMembership преобразует project и user locators и упорядоченные permission suffixes в
// active membership command без признания locators полномочиями.
func (server *Server) AddProjectMembership(ctx context.Context, request *controlplanev1.AddProjectMembershipRequest) (*controlplanev1.AddProjectMembershipResponse, error) {
	payload := command.MembershipInput{ProjectRef: request.GetProjectRef(), UserRef: request.GetUserRef(), Permissions: domainProjectPermissions(request.GetPermissions()), Active: true}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_AddProjectMembership_FullMethodName, command.AddMembership, request.GetMutation(), payload, func(result command.Result) *controlplanev1.AddProjectMembershipResponse {
		return &controlplanev1.AddProjectMembershipResponse{Membership: castMembership(*result.Membership)}
	})
}

// ChangeProjectMembership maps project/membership locators, permission suffixes and active state
// to the owner command under the exact RPC principal.
// ChangeProjectMembership передаёт project и membership locators, permission suffixes и active
// state owner command под principal точного RPC.
func (server *Server) ChangeProjectMembership(ctx context.Context, request *controlplanev1.ChangeProjectMembershipRequest) (*controlplanev1.ChangeProjectMembershipResponse, error) {
	payload := command.MembershipInput{ProjectRef: request.GetProjectRef(), MembershipRef: request.GetMembershipRef(), Permissions: domainProjectPermissions(request.GetPermissions()), Active: request.GetActive()}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ChangeProjectMembership_FullMethodName, command.ChangeMembership, request.GetMutation(), payload, func(result command.Result) *controlplanev1.ChangeProjectMembershipResponse {
		return &controlplanev1.ChangeProjectMembershipResponse{Membership: castMembership(*result.Membership)}
	})
}

// RemoveProjectMembership dispatches the specialized project membership removal using exact
// resource locators and mutation context.
// RemoveProjectMembership вызывает специализированное удаление project membership по точным
// resource locators и mutation context.
func (server *Server) RemoveProjectMembership(ctx context.Context, request *controlplanev1.RemoveProjectMembershipRequest) (*controlplanev1.RemoveProjectMembershipResponse, error) {
	payload := command.MembershipInput{ProjectRef: request.GetProjectRef(), MembershipRef: request.GetMembershipRef()}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_RemoveProjectMembership_FullMethodName, command.RemoveMembership, request.GetMutation(), payload, func(result command.Result) *controlplanev1.RemoveProjectMembershipResponse {
		return &controlplanev1.RemoveProjectMembershipResponse{Membership: castMembership(*result.Membership)}
	})
}

// CreateAgent maps the initial agent fields and instructions into the owner create command with
// enabled=true, returning the assigned agent.
// CreateAgent передаёт начальные agent fields и instructions owner create command с enabled=true и
// возвращает назначенного агента.
func (server *Server) CreateAgent(ctx context.Context, request *controlplanev1.CreateAgentRequest) (*controlplanev1.CreateAgentResponse, error) {
	payload := command.AgentInput{ProjectRef: request.GetProjectRef(), RoleDefinitionRef: request.GetRoleDefinitionRef(), Name: request.GetName(), Purpose: request.GetPurpose(), RoleDescription: request.GetRoleDescription(), AvatarURL: request.GetAvatarUrl(), RuntimeRef: request.GetRuntimeRef(), Instructions: request.GetInitialInstructions(), Enabled: true}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_CreateAgent_FullMethodName, command.CreateAgent, request.GetMutation(), payload, func(result command.Result) *controlplanev1.CreateAgentResponse {
		return &controlplanev1.CreateAgentResponse{Agent: castAgent(*result.Agent)}
	})
}

// UpdateAgent maps editable agent presentation/runtime fields without implicitly replacing
// instructions or enabling the agent.
// UpdateAgent преобразует редактируемые agent presentation и runtime fields без неявной замены
// instructions либо включения агента.
func (server *Server) UpdateAgent(ctx context.Context, request *controlplanev1.UpdateAgentRequest) (*controlplanev1.UpdateAgentResponse, error) {
	payload := command.AgentInput{Ref: request.GetAgentRef(), RoleDefinitionRef: request.GetRoleDefinitionRef(), Name: request.GetName(), Purpose: request.GetPurpose(), RoleDescription: request.GetRoleDescription(), AvatarURL: request.GetAvatarUrl(), RuntimeRef: request.GetRuntimeRef()}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_UpdateAgent_FullMethodName, command.UpdateAgent, request.GetMutation(), payload, func(result command.Result) *controlplanev1.UpdateAgentResponse {
		return &controlplanev1.UpdateAgentResponse{Agent: castAgent(*result.Agent)}
	})
}

// SetAgentEnabled passes the explicit enabled flag to the specialized agent command and returns
// its owner state.
// SetAgentEnabled передаёт явный enabled flag специализированной agent command и возвращает owner
// state.
func (server *Server) SetAgentEnabled(ctx context.Context, request *controlplanev1.SetAgentEnabledRequest) (*controlplanev1.SetAgentEnabledResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_SetAgentEnabled_FullMethodName, command.SetAgentEnabled, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef(), Enabled: request.GetEnabled()}, func(result command.Result) *controlplanev1.SetAgentEnabledResponse {
		return &controlplanev1.SetAgentEnabledResponse{Agent: castAgent(*result.Agent)}
	})
}

// ArchiveAgent passes only the agent locator through the archive command and returns the
// owner-managed lifecycle result.
// ArchiveAgent передаёт только agent locator archive command и возвращает управляемый владельцем
// lifecycle result.
func (server *Server) ArchiveAgent(ctx context.Context, request *controlplanev1.ArchiveAgentRequest) (*controlplanev1.ArchiveAgentResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ArchiveAgent_FullMethodName, command.ArchiveAgent, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef()}, func(result command.Result) *controlplanev1.ArchiveAgentResponse {
		return &controlplanev1.ArchiveAgentResponse{Agent: castAgent(*result.Agent)}
	})
}

// SetAgentAvatar binds the agent/artifact locators to the dedicated avatar command, leaving
// artifact ownership and eligibility checks with the domain.
// SetAgentAvatar связывает agent и artifact locators с отдельной avatar command, оставляя artifact
// ownership и eligibility checks домену.
func (server *Server) SetAgentAvatar(ctx context.Context, request *controlplanev1.SetAgentAvatarRequest) (*controlplanev1.SetAgentAvatarResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_SetAgentAvatar_FullMethodName, command.SetAgentAvatar, request.GetMutation(), command.AgentAvatarInput{AgentRef: request.GetAgentRef(), ArtifactRef: request.GetArtifactRef()}, func(result command.Result) *controlplanev1.SetAgentAvatarResponse {
		return &controlplanev1.SetAgentAvatarResponse{Agent: castAgent(*result.Agent)}
	})
}

// RemoveAgentAvatar dispatches the dedicated avatar removal command without accepting a
// caller-defined image path or value.
// RemoveAgentAvatar вызывает отдельную avatar removal command без приёма произвольного image path
// либо value от caller.
func (server *Server) RemoveAgentAvatar(ctx context.Context, request *controlplanev1.RemoveAgentAvatarRequest) (*controlplanev1.RemoveAgentAvatarResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_RemoveAgentAvatar_FullMethodName, command.RemoveAgentAvatar, request.GetMutation(), command.AgentAvatarInput{AgentRef: request.GetAgentRef()}, func(result command.Result) *controlplanev1.RemoveAgentAvatarResponse {
		return &controlplanev1.RemoveAgentAvatarResponse{Agent: castAgent(*result.Agent)}
	})
}

// CreateInstructionDraft maps agent locator and full content to the instruction draft command
// without treating draft text as runtime authority.
// CreateInstructionDraft передаёт agent locator и полный content instruction draft command без
// признания draft text runtime authority.
func (server *Server) CreateInstructionDraft(ctx context.Context, request *controlplanev1.CreateInstructionDraftRequest) (*controlplanev1.CreateInstructionDraftResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_CreateInstructionDraft_FullMethodName, command.CreateInstructions, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef(), Instructions: request.GetContent()}, func(result command.Result) *controlplanev1.CreateInstructionDraftResponse {
		return &controlplanev1.CreateInstructionDraftResponse{Agent: castAgent(*result.Agent)}
	})
}

// ValidateInstructionDraft runs the owner instruction-validation command for the agent and returns
// the resulting agent projection.
// ValidateInstructionDraft вызывает owner instruction-validation command для агента и возвращает
// итоговую agent projection.
func (server *Server) ValidateInstructionDraft(ctx context.Context, request *controlplanev1.ValidateInstructionDraftRequest) (*controlplanev1.ValidateInstructionDraftResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ValidateInstructionDraft_FullMethodName, command.ValidateInstructions, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef()}, func(result command.Result) *controlplanev1.ValidateInstructionDraftResponse {
		return &controlplanev1.ValidateInstructionDraftResponse{Agent: castAgent(*result.Agent)}
	})
}

// PublishInstructionDraft passes plan/selected-item pins to instruction publication and checks the
// returned AGENT_INSTRUCTIONS impact result before exposing agent and plan.
// PublishInstructionDraft передаёт plan и selected-item pins публикации instructions и проверяет
// возвращённый AGENT_INSTRUCTIONS impact result до выдачи agent и plan.
func (server *Server) PublishInstructionDraft(ctx context.Context, request *controlplanev1.PublishInstructionDraftRequest) (*controlplanev1.PublishInstructionDraftResponse, error) {
	result, err := execute(ctx, server.service, controlplanev1.PlatformCommandService_PublishInstructionDraft_FullMethodName, command.PublishInstructions, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef(), PlanRef: request.GetPlanRef(), SelectedItemRefs: append([]string(nil), request.GetSelectedItemRefs()...)})
	if err != nil {
		return nil, err
	}
	if err := validatePublishedImpactResult(result, "AGENT_INSTRUCTIONS", request.GetPlanRef(), request.GetAgentRef()); err != nil {
		return nil, transportError(err)
	}
	return &controlplanev1.PublishInstructionDraftResponse{Agent: castAgent(*result.Agent), Plan: castRevisionImpactPlan(result.RevisionImpactPlan)}, nil
}

// RollbackInstructions routes the published instruction locator through the specialized rollback
// command rather than accepting replacement instruction bytes.
// RollbackInstructions передаёт published instruction locator специализированной rollback command
// вместо приёма replacement instruction bytes.
func (server *Server) RollbackInstructions(ctx context.Context, request *controlplanev1.RollbackInstructionsRequest) (*controlplanev1.RollbackInstructionsResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_RollbackInstructions_FullMethodName, command.RollbackInstructions, request.GetMutation(), command.AgentInput{Ref: request.GetAgentRef(), Instructions: request.GetPublishedInstructionRef()}, func(result command.Result) *controlplanev1.RollbackInstructionsResponse {
		return &controlplanev1.RollbackInstructionsResponse{Agent: castAgent(*result.Agent)}
	})
}

// ChangeAgentCapability maps agent/capability locators and enabled state into the dedicated
// capability command with exact-method principal and mutation.
// ChangeAgentCapability преобразует agent и capability locators и enabled state в отдельную
// capability command с principal точного метода и mutation.
func (server *Server) ChangeAgentCapability(ctx context.Context, request *controlplanev1.ChangeAgentCapabilityRequest) (*controlplanev1.ChangeAgentCapabilityResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ChangeAgentCapability_FullMethodName, command.ChangeAgentCapability, request.GetMutation(), command.AgentBindingInput{AgentRef: request.GetAgentRef(), BindingRef: request.GetCapabilityKey(), Enabled: request.GetEnabled()}, func(result command.Result) *controlplanev1.ChangeAgentCapabilityResponse {
		return &controlplanev1.ChangeAgentCapabilityResponse{Agent: castAgent(*result.Agent)}
	})
}

// ChangeAgentIntegrationGrant passes the existing grant locator and enabled flag to the owner
// command without granting arbitrary capability authority from the payload.
// ChangeAgentIntegrationGrant передаёт existing grant locator и enabled flag owner command без
// выдачи произвольных capability полномочий из payload.
func (server *Server) ChangeAgentIntegrationGrant(ctx context.Context, request *controlplanev1.ChangeAgentIntegrationGrantRequest) (*controlplanev1.ChangeAgentIntegrationGrantResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ChangeAgentIntegrationGrant_FullMethodName, command.ChangeAgentGrant, request.GetMutation(), command.AgentBindingInput{AgentRef: request.GetAgentRef(), BindingRef: request.GetGrantRef(), Enabled: request.GetEnabled()}, func(result command.Result) *controlplanev1.ChangeAgentIntegrationGrantResponse {
		return &controlplanev1.ChangeAgentIntegrationGrantResponse{Agent: castAgent(*result.Agent)}
	})
}

// CreateWorkflow maps the draft graph, coordinator and project presentation fields through the
// owner workflow create command.
// CreateWorkflow передаёт draft graph, coordinator и project presentation fields owner workflow
// create command.
func (server *Server) CreateWorkflow(ctx context.Context, request *controlplanev1.CreateWorkflowRequest) (*controlplanev1.CreateWorkflowResponse, error) {
	payload := command.WorkflowInput{ProjectRef: request.GetProjectRef(), Name: request.GetName(), Purpose: request.GetPurpose(), CoordinatorAgentRef: request.GetCoordinatorAgentRef(), Draft: domainWorkflowVersion(request.GetDraft())}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_CreateWorkflow_FullMethodName, command.CreateWorkflow, request.GetMutation(), payload, func(result command.Result) *controlplanev1.CreateWorkflowResponse {
		return &controlplanev1.CreateWorkflowResponse{Workflow: castWorkflow(*result.Workflow)}
	})
}

// UpdateWorkflowDraft converts the supplied workflow draft and passes its locator/mutation to the
// owner update command.
// UpdateWorkflowDraft преобразует переданный workflow draft и передаёт его locator и mutation
// owner update command.
func (server *Server) UpdateWorkflowDraft(ctx context.Context, request *controlplanev1.UpdateWorkflowDraftRequest) (*controlplanev1.UpdateWorkflowDraftResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_UpdateWorkflowDraft_FullMethodName, command.UpdateWorkflow, request.GetMutation(), command.WorkflowInput{Ref: request.GetWorkflowRef(), Draft: domainWorkflowVersion(request.GetDraft())}, func(result command.Result) *controlplanev1.UpdateWorkflowDraftResponse {
		return &controlplanev1.UpdateWorkflowDraftResponse{Workflow: castWorkflow(*result.Workflow)}
	})
}

// ValidateWorkflowDraft dispatches workflow validation for the exact locator while the owner
// retains graph and publication invariants.
// ValidateWorkflowDraft вызывает workflow validation для точного locator при сохранении graph и
// publication invariants у владельца.
func (server *Server) ValidateWorkflowDraft(ctx context.Context, request *controlplanev1.ValidateWorkflowDraftRequest) (*controlplanev1.ValidateWorkflowDraftResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ValidateWorkflowDraft_FullMethodName, command.ValidateWorkflow, request.GetMutation(), command.WorkflowInput{Ref: request.GetWorkflowRef()}, func(result command.Result) *controlplanev1.ValidateWorkflowDraftResponse {
		return &controlplanev1.ValidateWorkflowDraftResponse{Workflow: castWorkflow(*result.Workflow)}
	})
}

// PublishWorkflowDraft dispatches specialized workflow publication and returns the owner workflow
// projection without a transport-level lifecycle shortcut.
// PublishWorkflowDraft вызывает специализированную workflow publication и возвращает owner
// workflow projection без transport-level lifecycle shortcut.
func (server *Server) PublishWorkflowDraft(ctx context.Context, request *controlplanev1.PublishWorkflowDraftRequest) (*controlplanev1.PublishWorkflowDraftResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_PublishWorkflowDraft_FullMethodName, command.PublishWorkflow, request.GetMutation(), command.WorkflowInput{Ref: request.GetWorkflowRef()}, func(result command.Result) *controlplanev1.PublishWorkflowDraftResponse {
		return &controlplanev1.PublishWorkflowDraftResponse{Workflow: castWorkflow(*result.Workflow)}
	})
}

// ArchiveWorkflow dispatches the dedicated workflow archive command under the verified method
// principal and mutation context.
// ArchiveWorkflow вызывает отдельную workflow archive command под проверенным principal метода и
// mutation context.
func (server *Server) ArchiveWorkflow(ctx context.Context, request *controlplanev1.ArchiveWorkflowRequest) (*controlplanev1.ArchiveWorkflowResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ArchiveWorkflow_FullMethodName, command.ArchiveWorkflow, request.GetMutation(), command.WorkflowInput{Ref: request.GetWorkflowRef()}, func(result command.Result) *controlplanev1.ArchiveWorkflowResponse {
		return &controlplanev1.ArchiveWorkflowResponse{Workflow: castWorkflow(*result.Workflow)}
	})
}

// LaunchRun maps target/input/session/attachment locators and title-source intent into the
// specialized launch command; actor/root lineage remain server-owned.
// LaunchRun преобразует target, input, session и attachment locators и title-source intent в
// специализированную launch command; actor и root lineage остаются server-owned.
func (server *Server) LaunchRun(ctx context.Context, request *controlplanev1.LaunchRunRequest) (*controlplanev1.LaunchRunResponse, error) {
	titleSource := launchRunTitleSource(request.GetTitle())
	payload := command.LaunchRunInput{ProjectRef: request.GetProjectRef(), Title: request.GetTitle(), TitleSource: titleSource, Task: request.GetTask(), SessionRef: request.GetSessionRef(), Source: enumSuffix(request.GetSource(), "RUN_SOURCE_"), Target: runTarget(request.GetTarget()), Input: asMap(request.GetInput()), AttachmentSetRef: request.GetAttachmentSetRef()}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_LaunchRun_FullMethodName, command.LaunchRun, request.GetMutation(), payload, func(result command.Result) *controlplanev1.LaunchRunResponse {
		return &controlplanev1.LaunchRunResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}
	})
}

// launchRunTitleSource marks blank-after-trim titles as SERVER_DEFAULT and all other titles as
// USER_EDITED without changing title bytes.
// launchRunTitleSource отмечает пустой после trim title как SERVER_DEFAULT, остальные как
// USER_EDITED без изменения title bytes.
func launchRunTitleSource(title string) string {
	if strings.TrimSpace(title) == "" {
		return "SERVER_DEFAULT"
	}
	return "USER_EDITED"
}

// AddSessionTurn passes exact session/run/node/attachment locators and expected prompt-context
// digest to the owner turn command.
// AddSessionTurn передаёт точные session, run, node и attachment locators и expected
// prompt-context digest owner turn command.
func (server *Server) AddSessionTurn(ctx context.Context, request *controlplanev1.AddSessionTurnRequest) (*controlplanev1.AddSessionTurnResponse, error) {
	payload := command.SessionTurnInput{SessionRef: request.GetSessionRef(), RunRef: request.GetRunRef(), NodeRef: request.GetNodeRef(), Task: request.GetTask(), AttachmentSetRef: request.GetAttachmentSetRef(), ExpectedPromptContextDigest: request.GetExpectedPromptContextDigest()}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_AddSessionTurn_FullMethodName, command.AddSessionTurn, request.GetMutation(), payload, func(result command.Result) *controlplanev1.AddSessionTurnResponse {
		return &controlplanev1.AddSessionTurnResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}
	})
}

// CancelRun dispatches the owner cancel command with run locator and reason, returning the
// run/graph produced by the domain transition.
// CancelRun вызывает owner cancel command с run locator и reason и возвращает run и graph
// доменного перехода.
func (server *Server) CancelRun(ctx context.Context, request *controlplanev1.CancelRunRequest) (*controlplanev1.CancelRunResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_CancelRun_FullMethodName, command.CancelRun, request.GetMutation(), command.RunCommandInput{RunRef: request.GetRunRef(), Reason: request.GetReason()}, func(result command.Result) *controlplanev1.CancelRunResponse {
		return &controlplanev1.CancelRunResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}
	})
}

// RetryRun dispatches the specialized retry command with run locator/reason; fresh attempt and
// lineage are assigned by the owner.
// RetryRun вызывает специализированную retry command с run locator и reason; свежую attempt и
// lineage назначает владелец.
func (server *Server) RetryRun(ctx context.Context, request *controlplanev1.RetryRunRequest) (*controlplanev1.RetryRunResponse, error) {
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_RetryRun_FullMethodName, command.RetryRun, request.GetMutation(), command.RunCommandInput{RunRef: request.GetRunRef(), Reason: request.GetReason()}, func(result command.Result) *controlplanev1.RetryRunResponse {
		return &controlplanev1.RetryRunResponse{Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}
	})
}

// ResolveOwnerGate maps gate decision/comment/attachment locators through the dedicated owner
// command and returns its gate/run/graph result.
// ResolveOwnerGate передаёт gate decision, comment и attachment locators отдельной owner command и
// возвращает gate, run и graph результата.
func (server *Server) ResolveOwnerGate(ctx context.Context, request *controlplanev1.ResolveOwnerGateRequest) (*controlplanev1.ResolveOwnerGateResponse, error) {
	payload := command.GateResolutionInput{GateRef: request.GetGateRef(), Decision: enumSuffix(request.GetDecision(), "OWNER_GATE_DECISION_"), Comment: request.GetComment(), AttachmentSetRef: request.GetAttachmentSetRef()}
	return executeResponse(ctx, server.service, controlplanev1.PlatformCommandService_ResolveOwnerGate_FullMethodName, command.ResolveOwnerGate, request.GetMutation(), payload, func(result command.Result) *controlplanev1.ResolveOwnerGateResponse {
		return &controlplanev1.ResolveOwnerGateResponse{Gate: castGate(*result.Gate), Run: castRun(*result.Run), Graph: castGraph(*result.Graph)}
	})
}
