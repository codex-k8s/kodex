package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	agentsv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/agents/v1"
	providersv1 "github.com/codex-k8s/kodex/proto/gen/go/kodex/providers/v1"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
	"github.com/google/uuid"
	grpcruntime "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRegisterAgentManagerService(t *testing.T) {
	t.Parallel()

	server := grpcruntime.NewServer()
	RegisterAgentManagerService(server, &fakeAgentService{})
}

func TestNewServerRequiresService(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("NewServer(nil) did not panic")
		}
	}()
	_ = NewServer(nil)
}

func TestServerKeepsDomainService(t *testing.T) {
	t.Parallel()

	service := &fakeAgentService{}
	if NewServer(service).service != service {
		t.Fatal("server did not keep composed domain service")
	}
}

func TestCreateFlowMapsRequestAndResponse(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	var captured agentservice.CreateFlowInput
	service := &fakeAgentService{
		createFlow: func(_ context.Context, input agentservice.CreateFlowInput) (entity.Flow, error) {
			captured = input
			return sampleFlow("22222222-2222-2222-2222-222222222222"), nil
		},
	}
	response, err := NewServer(service).CreateFlow(context.Background(), &agentsv1.CreateFlowRequest{
		Meta:        commandMeta(commandID.String(), "", nil),
		Scope:       scopeRef(agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT, "project-1"),
		Slug:        " delivery ",
		DisplayName: []*agentsv1.LocalizedText{{Locale: "ru", Text: "Поставка"}},
		Description: []*agentsv1.LocalizedText{{Locale: "ru", Text: "Доставка изменений"}},
	})
	if err != nil {
		t.Fatalf("CreateFlow() error = %v", err)
	}
	if captured.Meta.CommandID != commandID || captured.Meta.Actor.ID != "operator-1" {
		t.Fatalf("captured meta = %+v", captured.Meta)
	}
	if captured.Scope.Type != string(enum.AgentScopeTypeProject) || captured.Slug != "delivery" {
		t.Fatalf("captured input = %+v", captured)
	}
	if response.GetFlow().GetStatus() != agentsv1.FlowStatus_FLOW_STATUS_DRAFT {
		t.Fatalf("flow status = %s", response.GetFlow().GetStatus())
	}
}

func TestGetFlowLoadsActiveVersion(t *testing.T) {
	t.Parallel()

	flowID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	activeID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	service := &fakeAgentService{
		getFlow: func(_ context.Context, id uuid.UUID) (entity.Flow, error) {
			if id != flowID {
				t.Fatalf("flow id = %s", id)
			}
			flow := sampleFlow(flowID.String())
			flow.ActiveVersionID = &activeID
			return flow, nil
		},
		getFlowVersion: func(_ context.Context, id uuid.UUID) (entity.FlowVersion, error) {
			if id != activeID {
				t.Fatalf("active version id = %s", id)
			}
			version := sampleFlowVersion(activeID.String(), flowID.String())
			version.Status = enum.FlowVersionStatusActive
			return version, nil
		},
	}
	response, err := NewServer(service).GetFlow(context.Background(), &agentsv1.GetFlowRequest{
		Meta:   queryMeta(),
		FlowId: flowID.String(),
	})
	if err != nil {
		t.Fatalf("GetFlow() error = %v", err)
	}
	if response.GetActiveVersion().GetId() != activeID.String() {
		t.Fatalf("active version = %+v", response.GetActiveVersion())
	}
}

func TestListFlowsMapsFilterAndPage(t *testing.T) {
	t.Parallel()

	statusFilter := agentsv1.FlowStatus_FLOW_STATUS_ACTIVE
	service := &fakeAgentService{
		listFlows: func(_ context.Context, input agentservice.FlowList) ([]entity.Flow, value.PageResult, error) {
			if input.Scope.Type != string(enum.AgentScopeTypeOrganization) || input.Scope.Ref != "org-1" {
				t.Fatalf("scope = %+v", input.Scope)
			}
			if input.Status == nil || *input.Status != enum.FlowStatusActive {
				t.Fatalf("status = %+v", input.Status)
			}
			if input.Page.PageSize != 2 || input.Page.PageToken != "cursor-1" {
				t.Fatalf("page = %+v", input.Page)
			}
			return []entity.Flow{sampleFlow("22222222-2222-2222-2222-222222222222")}, value.PageResult{NextPageToken: "cursor-2"}, nil
		},
	}
	response, err := NewServer(service).ListFlows(context.Background(), &agentsv1.ListFlowsRequest{
		Meta:   queryMeta(),
		Scope:  scopeRef(agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_ORGANIZATION, "org-1"),
		Status: &statusFilter,
		Page:   &agentsv1.PageRequest{PageSize: 2, PageToken: ptr("cursor-1")},
	})
	if err != nil {
		t.Fatalf("ListFlows() error = %v", err)
	}
	if len(response.GetFlows()) != 1 || response.GetPage().GetNextPageToken() != "cursor-2" {
		t.Fatalf("response = %+v", response)
	}
}

func TestCreateFlowVersionMapsDefinition(t *testing.T) {
	t.Parallel()

	roleID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	service := &fakeAgentService{
		createFlowVersion: func(_ context.Context, input agentservice.CreateFlowVersionInput) (entity.FlowVersion, error) {
			if len(input.Stages) != 1 || input.Stages[0].StageType != enum.StageTypeWork {
				t.Fatalf("stages = %+v", input.Stages)
			}
			if len(input.Transitions) != 1 || input.Transitions[0].FromStageSlug == nil || *input.Transitions[0].FromStageSlug != "intake" {
				t.Fatalf("transitions = %+v", input.Transitions)
			}
			if len(input.RoleBindings) != 1 || input.RoleBindings[0].RoleProfileID != roleID {
				t.Fatalf("role bindings = %+v", input.RoleBindings)
			}
			version := sampleFlowVersion("33333333-3333-3333-3333-333333333333", input.FlowID.String())
			version.Status = enum.FlowVersionStatusSuperseded
			return version, nil
		},
	}
	response, err := NewServer(service).CreateFlowVersion(context.Background(), &agentsv1.CreateFlowVersionRequest{
		Meta:   commandMeta("11111111-1111-1111-1111-111111111111", "", nil),
		FlowId: "22222222-2222-2222-2222-222222222222",
		Definition: &agentsv1.FlowDefinitionInput{
			DefinitionDigest: "sha256:flow",
			Stages: []*agentsv1.StageInput{{
				Slug:                  "dev",
				StageType:             agentsv1.StageType_STAGE_TYPE_WORK,
				RequiredArtifactsJson: "{}",
				AcceptancePolicyJson:  "{}",
			}},
			Transitions: []*agentsv1.StageTransitionInput{{
				FromStageSlug: ptr("intake"),
				ToStageSlug:   "dev",
				ConditionJson: "{}",
			}},
			StageRoleBindings: []*agentsv1.StageRoleBindingInput{{
				StageSlug:             "dev",
				RoleProfileId:         roleID.String(),
				BindingKind:           agentsv1.StageRoleBindingKind_STAGE_ROLE_BINDING_KIND_EXECUTOR,
				LaunchPolicyJson:      "{}",
				RequiredForAcceptance: true,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateFlowVersion() error = %v", err)
	}
	if response.GetFlowVersion().GetStatus() != agentsv1.FlowVersionStatus_FLOW_VERSION_STATUS_SUPERSEDED {
		t.Fatalf("flow version status = %s", response.GetFlowVersion().GetStatus())
	}
}

func TestRoleHandlersMapRequests(t *testing.T) {
	t.Parallel()

	expectedVersion := int64(7)
	service := &fakeAgentService{
		createRoleProfile: func(_ context.Context, input agentservice.CreateRoleProfileInput) (entity.RoleProfile, error) {
			if input.RoleKind != enum.RoleKindReviewer || input.RuntimeProfile != "code" {
				t.Fatalf("create input = %+v", input)
			}
			return sampleRole("55555555-5555-5555-5555-555555555555"), nil
		},
		updateRoleProfile: func(_ context.Context, input agentservice.UpdateRoleProfileInput) (entity.RoleProfile, error) {
			if input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion || input.Status != enum.RoleStatusActive {
				t.Fatalf("update input = %+v", input)
			}
			role := sampleRole(input.RoleProfileID.String())
			role.Status = enum.RoleStatusActive
			return role, nil
		},
	}
	server := NewServer(service)
	if _, err := server.CreateRoleProfile(context.Background(), &agentsv1.CreateRoleProfileRequest{
		Meta:           commandMeta("11111111-1111-1111-1111-111111111111", "", nil),
		Scope:          scopeRef(agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT, "project-1"),
		Slug:           "reviewer",
		RoleKind:       agentsv1.RoleKind_ROLE_KIND_REVIEWER,
		RuntimeProfile: "code",
	}); err != nil {
		t.Fatalf("CreateRoleProfile() error = %v", err)
	}
	active := agentsv1.RoleStatus_ROLE_STATUS_ACTIVE
	if _, err := server.UpdateRoleProfile(context.Background(), &agentsv1.UpdateRoleProfileRequest{
		Meta:          commandMeta("11111111-1111-1111-1111-111111111112", "", &expectedVersion),
		RoleProfileId: "55555555-5555-5555-5555-555555555555",
		Status:        &active,
	}); err != nil {
		t.Fatalf("UpdateRoleProfile() error = %v", err)
	}
}

func TestRoleReadHandlersMapRequests(t *testing.T) {
	t.Parallel()

	roleID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	kind := agentsv1.RoleKind_ROLE_KIND_REVIEWER
	statusFilter := agentsv1.RoleStatus_ROLE_STATUS_ACTIVE
	service := &fakeAgentService{
		getRoleProfile: func(_ context.Context, id uuid.UUID) (entity.RoleProfile, error) {
			if id != roleID {
				t.Fatalf("role id = %s", id)
			}
			return sampleRole(roleID.String()), nil
		},
		listRoleProfiles: func(_ context.Context, input agentservice.RoleProfileList) ([]entity.RoleProfile, value.PageResult, error) {
			if input.Kind == nil || *input.Kind != enum.RoleKindReviewer {
				t.Fatalf("kind = %+v", input.Kind)
			}
			if input.Status == nil || *input.Status != enum.RoleStatusActive {
				t.Fatalf("status = %+v", input.Status)
			}
			return []entity.RoleProfile{sampleRole(roleID.String())}, value.PageResult{NextPageToken: "roles-next"}, nil
		},
	}
	server := NewServer(service)
	if _, err := server.GetRoleProfile(context.Background(), &agentsv1.GetRoleProfileRequest{Meta: queryMeta(), RoleProfileId: roleID.String()}); err != nil {
		t.Fatalf("GetRoleProfile() error = %v", err)
	}
	response, err := server.ListRoleProfiles(context.Background(), &agentsv1.ListRoleProfilesRequest{
		Meta:     queryMeta(),
		Scope:    scopeRef(agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT, "project-1"),
		RoleKind: &kind,
		Status:   &statusFilter,
		Page:     &agentsv1.PageRequest{PageSize: 3},
	})
	if err != nil {
		t.Fatalf("ListRoleProfiles() error = %v", err)
	}
	if len(response.GetRoleProfiles()) != 1 || response.GetPage().GetNextPageToken() != "roles-next" {
		t.Fatalf("response = %+v", response)
	}
}

func TestPromptTemplateHandlersMapRequests(t *testing.T) {
	t.Parallel()

	templateID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	activeID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	service := &fakeAgentService{
		getPromptTemplate: func(_ context.Context, id uuid.UUID) (entity.PromptTemplate, error) {
			if id != templateID {
				t.Fatalf("template id = %s", id)
			}
			template := samplePromptTemplate(templateID.String(), "55555555-5555-5555-5555-555555555555")
			template.ActiveVersionID = &activeID
			return template, nil
		},
		getPromptTemplateVersion: func(_ context.Context, id uuid.UUID) (entity.PromptTemplateVersion, error) {
			if id != activeID {
				t.Fatalf("template version id = %s", id)
			}
			version := samplePromptVersion(activeID.String(), templateID.String(), "55555555-5555-5555-5555-555555555555")
			version.Status = enum.PromptVersionStatusActive
			return version, nil
		},
		createPromptTemplateVersion: func(_ context.Context, input agentservice.CreatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error) {
			if input.PromptKind != enum.PromptKindReview || input.TemplateObject.ObjectURI != "s3://bucket/prompt.md" {
				t.Fatalf("create prompt version input = %+v", input)
			}
			return samplePromptVersion(activeID.String(), templateID.String(), input.RoleProfileID.String()), nil
		},
		listPromptTemplates: func(_ context.Context, input agentservice.PromptTemplateList) ([]entity.PromptTemplate, value.PageResult, error) {
			if input.RoleProfileID.String() != "55555555-5555-5555-5555-555555555555" || input.Kind == nil || *input.Kind != enum.PromptKindReview {
				t.Fatalf("list prompt template input = %+v", input)
			}
			return []entity.PromptTemplate{samplePromptTemplate(templateID.String(), input.RoleProfileID.String())}, value.PageResult{NextPageToken: "templates-next"}, nil
		},
	}
	server := NewServer(service)
	response, err := server.GetPromptTemplate(context.Background(), &agentsv1.GetPromptTemplateRequest{
		Meta:             queryMeta(),
		PromptTemplateId: templateID.String(),
	})
	if err != nil {
		t.Fatalf("GetPromptTemplate() error = %v", err)
	}
	if response.GetActiveVersion().GetStatus() != agentsv1.PromptVersionStatus_PROMPT_VERSION_STATUS_ACTIVE {
		t.Fatalf("active version = %+v", response.GetActiveVersion())
	}
	if _, err := server.CreatePromptTemplateVersion(context.Background(), &agentsv1.CreatePromptTemplateVersionRequest{
		Meta:          commandMeta("11111111-1111-1111-1111-111111111111", "prompt-review-v1", nil),
		RoleProfileId: "55555555-5555-5555-5555-555555555555",
		PromptKind:    agentsv1.PromptKind_PROMPT_KIND_REVIEW,
		TemplateObject: &agentsv1.ObjectRef{
			ObjectUri:    "s3://bucket/prompt.md",
			ObjectDigest: "sha256:prompt",
		},
		TemplateDigest: "sha256:prompt",
	}); err != nil {
		t.Fatalf("CreatePromptTemplateVersion() error = %v", err)
	}
	promptKind := agentsv1.PromptKind_PROMPT_KIND_REVIEW
	list, err := server.ListPromptTemplates(context.Background(), &agentsv1.ListPromptTemplatesRequest{
		Meta:          queryMeta(),
		RoleProfileId: "55555555-5555-5555-5555-555555555555",
		PromptKind:    &promptKind,
		Page:          &agentsv1.PageRequest{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("ListPromptTemplates() error = %v", err)
	}
	if len(list.GetPromptTemplates()) != 1 || list.GetPage().GetNextPageToken() != "templates-next" {
		t.Fatalf("list = %+v", list)
	}
}

func TestPromptVersionHandlersMapRequests(t *testing.T) {
	t.Parallel()

	templateID := uuid.MustParse("66666666-6666-6666-6666-666666666666")
	roleID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	versionID := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	expectedVersion := int64(3)
	promptKind := agentsv1.PromptKind_PROMPT_KIND_REVIEW
	versionStatus := agentsv1.PromptVersionStatus_PROMPT_VERSION_STATUS_SUPERSEDED
	service := &fakeAgentService{
		activatePromptVersion: func(_ context.Context, input agentservice.ActivatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error) {
			if input.PromptTemplateVersionID != versionID || input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion {
				t.Fatalf("activate input = %+v", input)
			}
			version := samplePromptVersion(versionID.String(), templateID.String(), roleID.String())
			version.Status = enum.PromptVersionStatusActive
			return version, nil
		},
		getPromptTemplateVersion: func(_ context.Context, id uuid.UUID) (entity.PromptTemplateVersion, error) {
			if id != versionID {
				t.Fatalf("version id = %s", id)
			}
			version := samplePromptVersion(versionID.String(), templateID.String(), roleID.String())
			version.Status = enum.PromptVersionStatusSuperseded
			return version, nil
		},
		listPromptTemplateVersions: func(_ context.Context, input agentservice.PromptTemplateVersionList) ([]entity.PromptTemplateVersion, value.PageResult, error) {
			if input.RoleProfileID != roleID || input.Kind == nil || *input.Kind != enum.PromptKindReview {
				t.Fatalf("list input = %+v", input)
			}
			if input.Status == nil || *input.Status != enum.PromptVersionStatusSuperseded {
				t.Fatalf("status = %+v", input.Status)
			}
			return []entity.PromptTemplateVersion{samplePromptVersion(versionID.String(), templateID.String(), roleID.String())}, value.PageResult{NextPageToken: "prompt-next"}, nil
		},
	}
	server := NewServer(service)
	activated, err := server.ActivatePromptTemplateVersion(context.Background(), &agentsv1.ActivatePromptTemplateVersionRequest{
		Meta:                    commandMeta("11111111-1111-1111-1111-111111111113", "", &expectedVersion),
		PromptTemplateVersionId: versionID.String(),
	})
	if err != nil {
		t.Fatalf("ActivatePromptTemplateVersion() error = %v", err)
	}
	if activated.GetPromptTemplateVersion().GetStatus() != agentsv1.PromptVersionStatus_PROMPT_VERSION_STATUS_ACTIVE {
		t.Fatalf("activated = %+v", activated)
	}
	read, err := server.GetPromptTemplateVersion(context.Background(), &agentsv1.GetPromptTemplateVersionRequest{Meta: queryMeta(), PromptTemplateVersionId: versionID.String()})
	if err != nil {
		t.Fatalf("GetPromptTemplateVersion() error = %v", err)
	}
	if read.GetPromptTemplateVersion().GetStatus() != agentsv1.PromptVersionStatus_PROMPT_VERSION_STATUS_SUPERSEDED {
		t.Fatalf("read = %+v", read)
	}
	list, err := server.ListPromptTemplateVersions(context.Background(), &agentsv1.ListPromptTemplateVersionsRequest{
		Meta:          queryMeta(),
		RoleProfileId: roleID.String(),
		PromptKind:    &promptKind,
		Status:        &versionStatus,
		Page:          &agentsv1.PageRequest{PageSize: 4},
	})
	if err != nil {
		t.Fatalf("ListPromptTemplateVersions() error = %v", err)
	}
	if len(list.GetPromptTemplateVersions()) != 1 || list.GetPage().GetNextPageToken() != "prompt-next" {
		t.Fatalf("list = %+v", list)
	}
}

func TestActivateFlowVersionMapsExpectedVersion(t *testing.T) {
	t.Parallel()

	versionID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	flowID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	expectedVersion := int64(5)
	service := &fakeAgentService{
		activateFlowVersion: func(_ context.Context, input agentservice.ActivateFlowVersionInput) (entity.FlowVersion, error) {
			if input.FlowVersionID != versionID || input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion {
				t.Fatalf("activate input = %+v", input)
			}
			version := sampleFlowVersion(versionID.String(), flowID.String())
			version.Status = enum.FlowVersionStatusActive
			return version, nil
		},
	}
	response, err := NewServer(service).ActivateFlowVersion(context.Background(), &agentsv1.ActivateFlowVersionRequest{
		Meta:          commandMeta("11111111-1111-1111-1111-111111111114", "", &expectedVersion),
		FlowVersionId: versionID.String(),
	})
	if err != nil {
		t.Fatalf("ActivateFlowVersion() error = %v", err)
	}
	if response.GetFlowVersion().GetStatus() != agentsv1.FlowVersionStatus_FLOW_VERSION_STATUS_ACTIVE {
		t.Fatalf("response = %+v", response)
	}
}

func TestSessionRunHandlersMapRequests(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("99999999-1111-2222-3333-444444444444")
	runID := uuid.MustParse("aaaaaaaa-1111-2222-3333-444444444444")
	roleID := uuid.MustParse("bbbbbbbb-1111-2222-3333-444444444444")
	promptVersionID := uuid.MustParse("cccccccc-1111-2222-3333-444444444444")
	expectedVersion := int64(3)
	runStatus := agentsv1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING
	service := &fakeAgentService{
		startAgentSession: func(_ context.Context, input agentservice.StartAgentSessionInput) (entity.AgentSession, error) {
			if input.Scope.Type != string(enum.AgentScopeTypeProject) || input.ProviderWorkItemRef != "github:issue:42" || input.CreatedByActorRef != "user:owner" {
				t.Fatalf("start session input = %+v", input)
			}
			return entity.AgentSession{
				VersionedBase:       entity.VersionedBase{ID: sessionID, Version: 1, CreatedAt: sampleTime(), UpdatedAt: sampleTime()},
				Scope:               input.Scope,
				ProviderWorkItemRef: input.ProviderWorkItemRef,
				Status:              enum.AgentSessionStatusOpen,
				CreatedByActorRef:   input.CreatedByActorRef,
			}, nil
		},
		startAgentRun: func(_ context.Context, input agentservice.StartAgentRunInput) (entity.AgentRun, error) {
			if input.SessionID != sessionID || input.RoleProfileID != roleID || input.PromptTemplateVersionID != promptVersionID {
				t.Fatalf("start run input = %+v", input)
			}
			if input.ProviderTarget.WorkItemRef != "github:issue:42" {
				t.Fatalf("provider target = %+v", input.ProviderTarget)
			}
			return entity.AgentRun{
				VersionedBase:           entity.VersionedBase{ID: runID, Version: 1, CreatedAt: sampleTime(), UpdatedAt: sampleTime()},
				SessionID:               sessionID,
				RoleProfileID:           roleID,
				RoleProfileVersion:      1,
				RoleProfileDigest:       "sha256:role",
				PromptTemplateVersionID: promptVersionID,
				PromptTemplateDigest:    "sha256:prompt",
				ProviderTarget:          input.ProviderTarget,
				Status:                  enum.AgentRunStatusRequested,
			}, nil
		},
		recordRunState: func(_ context.Context, input agentservice.RecordRunStateInput) (entity.AgentRun, error) {
			if input.RunID != runID || input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion {
				t.Fatalf("record run input = %+v", input)
			}
			if input.RuntimeContext == nil || input.RuntimeContext.SlotRef != "slot-1" {
				t.Fatalf("runtime context = %+v", input.RuntimeContext)
			}
			run := entity.AgentRun{
				VersionedBase:           entity.VersionedBase{ID: runID, Version: expectedVersion + 1, CreatedAt: sampleTime(), UpdatedAt: sampleTime()},
				SessionID:               sessionID,
				RoleProfileID:           roleID,
				RoleProfileVersion:      1,
				RoleProfileDigest:       "sha256:role",
				PromptTemplateVersionID: promptVersionID,
				PromptTemplateDigest:    "sha256:prompt",
				RuntimeContext:          *input.RuntimeContext,
				ProviderTarget:          value.ProviderTargetRef{WorkItemRef: "github:issue:42"},
				Status:                  enum.AgentRunStatusRunning,
			}
			return run, nil
		},
		listAgentRuns: func(_ context.Context, input agentservice.AgentRunList) ([]entity.AgentRun, value.PageResult, error) {
			if input.SessionID != sessionID || input.Status == nil || *input.Status != enum.AgentRunStatusRunning {
				t.Fatalf("list runs input = %+v", input)
			}
			return []entity.AgentRun{{
				VersionedBase:           entity.VersionedBase{ID: runID, Version: 4, CreatedAt: sampleTime(), UpdatedAt: sampleTime()},
				SessionID:               sessionID,
				RoleProfileID:           roleID,
				RoleProfileVersion:      1,
				RoleProfileDigest:       "sha256:role",
				PromptTemplateVersionID: promptVersionID,
				PromptTemplateDigest:    "sha256:prompt",
				Status:                  enum.AgentRunStatusRunning,
			}}, value.PageResult{NextPageToken: "runs-next"}, nil
		},
	}
	server := NewServer(service)
	session, err := server.StartAgentSession(context.Background(), &agentsv1.StartAgentSessionRequest{
		Meta:                commandMeta("11111111-2222-3333-4444-555555555555", "", nil),
		Scope:               scopeRef(agentsv1.AgentScopeType_AGENT_SCOPE_TYPE_PROJECT, "project-1"),
		ProviderWorkItemRef: ptr("github:issue:42"),
		CreatedByActorRef:   "user:owner",
	})
	if err != nil {
		t.Fatalf("StartAgentSession() error = %v", err)
	}
	if session.GetSession().GetStatus() != agentsv1.AgentSessionStatus_AGENT_SESSION_STATUS_OPEN {
		t.Fatalf("session = %+v", session.GetSession())
	}
	startedRun, err := server.StartAgentRun(context.Background(), &agentsv1.StartAgentRunRequest{
		Meta:                    commandMeta("11111111-2222-3333-4444-555555555556", "", nil),
		SessionId:               sessionID.String(),
		RoleProfileId:           roleID.String(),
		PromptTemplateVersionId: promptVersionID.String(),
		ProviderTarget:          &agentsv1.ProviderTargetRef{WorkItemRef: ptr("github:issue:42")},
	})
	if err != nil {
		t.Fatalf("StartAgentRun() error = %v", err)
	}
	if startedRun.GetRun().GetStatus() != agentsv1.AgentRunStatus_AGENT_RUN_STATUS_REQUESTED {
		t.Fatalf("started run = %+v", startedRun.GetRun())
	}
	recordedRun, err := server.RecordRunState(context.Background(), &agentsv1.RecordRunStateRequest{
		Meta:           commandMeta("11111111-2222-3333-4444-555555555557", "", &expectedVersion),
		RunId:          runID.String(),
		Status:         agentsv1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING,
		RuntimeContext: &agentsv1.RuntimeContextRef{SlotRef: ptr("slot-1")},
		ResultSummary:  ptr("agent started"),
	})
	if err != nil {
		t.Fatalf("RecordRunState() error = %v", err)
	}
	if recordedRun.GetRun().GetRuntimeContext().GetSlotRef() != "slot-1" {
		t.Fatalf("recorded run = %+v", recordedRun.GetRun())
	}
	list, err := server.ListAgentRuns(context.Background(), &agentsv1.ListAgentRunsRequest{
		Meta:      queryMeta(),
		SessionId: ptr(sessionID.String()),
		Status:    &runStatus,
		Page:      &agentsv1.PageRequest{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("ListAgentRuns() error = %v", err)
	}
	if len(list.GetRuns()) != 1 || list.GetPage().GetNextPageToken() != "runs-next" {
		t.Fatalf("list = %+v", list)
	}
}

func TestAcceptanceHandlersMapRequests(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("dddddddd-1111-2222-3333-444444444444")
	runID := uuid.MustParse("dddddddd-2222-3333-4444-555555555555")
	stageID := uuid.MustParse("dddddddd-3333-4444-5555-666666666666")
	acceptanceID := uuid.MustParse("dddddddd-4444-5555-6666-777777777777")
	expectedVersion := int64(2)
	statusFilter := agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_PASSED
	service := &fakeAgentService{
		requestAcceptance: func(_ context.Context, input agentservice.RequestAcceptanceInput) (entity.AcceptanceResult, error) {
			if input.SessionID != sessionID || input.RunID == nil || *input.RunID != runID || len(input.CheckKinds) != 1 || input.CheckKinds[0] != enum.AcceptanceCheckKindRoleResult {
				t.Fatalf("request acceptance input = %+v", input)
			}
			return sampleAcceptanceResult(acceptanceID, sessionID, &runID, &stageID, enum.AcceptanceCheckKindRoleResult, enum.AcceptanceStatusPending), nil
		},
		recordAcceptanceResult: func(_ context.Context, input agentservice.RecordAcceptanceResultInput) (entity.AcceptanceResult, error) {
			if input.AcceptanceResultID != acceptanceID || input.Meta.ExpectedVersion == nil || *input.Meta.ExpectedVersion != expectedVersion || input.Status != enum.AcceptanceStatusPassed {
				t.Fatalf("record acceptance input = %+v", input)
			}
			if string(input.DetailsJSON) != `{"summary":"ok"}` {
				t.Fatalf("details_json = %s", input.DetailsJSON)
			}
			return sampleAcceptanceResult(acceptanceID, sessionID, &runID, &stageID, enum.AcceptanceCheckKindRoleResult, enum.AcceptanceStatusPassed), nil
		},
		getAcceptanceResult: func(_ context.Context, id uuid.UUID) (entity.AcceptanceResult, error) {
			if id != acceptanceID {
				t.Fatalf("acceptance id = %s", id)
			}
			return sampleAcceptanceResult(acceptanceID, sessionID, &runID, &stageID, enum.AcceptanceCheckKindRoleResult, enum.AcceptanceStatusPassed), nil
		},
		listAcceptanceResults: func(_ context.Context, input agentservice.AcceptanceResultList) ([]entity.AcceptanceResult, value.PageResult, error) {
			if input.SessionID != sessionID || input.RunID != runID || input.StageID != stageID || input.Status == nil || *input.Status != enum.AcceptanceStatusPassed {
				t.Fatalf("list acceptance input = %+v", input)
			}
			return []entity.AcceptanceResult{sampleAcceptanceResult(acceptanceID, sessionID, &runID, &stageID, enum.AcceptanceCheckKindRoleResult, enum.AcceptanceStatusPassed)}, value.PageResult{NextPageToken: "acceptance-next"}, nil
		},
	}
	server := NewServer(service)
	requested, err := server.RequestAcceptance(context.Background(), &agentsv1.RequestAcceptanceRequest{
		Meta:       commandMeta("eeeeeeee-1111-2222-3333-444444444444", "", nil),
		SessionId:  sessionID.String(),
		RunId:      ptr(runID.String()),
		StageId:    ptr(stageID.String()),
		CheckKinds: []agentsv1.AcceptanceCheckKind{agentsv1.AcceptanceCheckKind_ACCEPTANCE_CHECK_KIND_ROLE_RESULT},
		TargetRef:  ptr("artifact:run-summary"),
	})
	if err != nil {
		t.Fatalf("RequestAcceptance() error = %v", err)
	}
	if requested.GetAcceptanceResult().GetStatus() != agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_PENDING {
		t.Fatalf("requested = %+v", requested.GetAcceptanceResult())
	}
	recorded, err := server.RecordAcceptanceResult(context.Background(), &agentsv1.RecordAcceptanceResultRequest{
		Meta:               commandMeta("eeeeeeee-2222-3333-4444-555555555555", "", &expectedVersion),
		AcceptanceResultId: acceptanceID.String(),
		Status:             agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_PASSED,
		DetailsJson:        `{"summary":"ok"}`,
	})
	if err != nil {
		t.Fatalf("RecordAcceptanceResult() error = %v", err)
	}
	if recorded.GetAcceptanceResult().GetStatus() != agentsv1.AcceptanceStatus_ACCEPTANCE_STATUS_PASSED {
		t.Fatalf("recorded = %+v", recorded.GetAcceptanceResult())
	}
	read, err := server.GetAcceptanceResult(context.Background(), &agentsv1.GetAcceptanceResultRequest{
		Meta:               queryMeta(),
		AcceptanceResultId: acceptanceID.String(),
	})
	if err != nil {
		t.Fatalf("GetAcceptanceResult() error = %v", err)
	}
	if read.GetAcceptanceResult().GetId() != acceptanceID.String() {
		t.Fatalf("read = %+v", read.GetAcceptanceResult())
	}
	list, err := server.ListAcceptanceResults(context.Background(), &agentsv1.ListAcceptanceResultsRequest{
		Meta:      queryMeta(),
		SessionId: ptr(sessionID.String()),
		RunId:     ptr(runID.String()),
		StageId:   ptr(stageID.String()),
		Status:    &statusFilter,
		Page:      &agentsv1.PageRequest{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("ListAcceptanceResults() error = %v", err)
	}
	if len(list.GetAcceptanceResults()) != 1 || list.GetPage().GetNextPageToken() != "acceptance-next" {
		t.Fatalf("list = %+v", list)
	}
}

func TestCreateFollowUpIntentHandlerMapsRequest(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("85858585-1111-2222-3333-444444444444")
	runID := uuid.MustParse("85858585-2222-3333-4444-555555555555")
	fromStageID := uuid.MustParse("85858585-3333-4444-5555-666666666666")
	toStageID := uuid.MustParse("85858585-4444-5555-6666-777777777777")
	acceptanceID := uuid.MustParse("85858585-5555-6666-7777-888888888888")
	intentID := uuid.MustParse("85858585-6666-7777-8888-999999999999")
	service := &fakeAgentService{
		createFollowUpIntent: func(_ context.Context, input agentservice.CreateFollowUpIntentInput) (entity.FollowUpIntent, error) {
			if input.SessionID != sessionID || input.RunID == nil || *input.RunID != runID || input.FromStageID == nil || *input.FromStageID != fromStageID || input.ToStageID == nil || *input.ToStageID != toStageID {
				t.Fatalf("stage/run input = %+v", input)
			}
			if input.AcceptanceResultID == nil || *input.AcceptanceResultID != acceptanceID {
				t.Fatalf("acceptance input = %+v", input.AcceptanceResultID)
			}
			if input.ProviderTarget.WorkItemRef != "issue:123" || input.ProviderWorkItemType != "task" || input.SafeTitle != "Follow-up" || input.SafeSummary != "Summary" {
				t.Fatalf("payload input = %+v", input)
			}
			now := sampleTime()
			return entity.FollowUpIntent{
				VersionedBase:        entity.VersionedBase{ID: intentID, Version: 1, CreatedAt: now, UpdatedAt: now},
				SessionID:            sessionID,
				RunID:                &runID,
				FromStageID:          &fromStageID,
				ToStageID:            &toStageID,
				AcceptanceResultID:   &acceptanceID,
				ProviderTarget:       input.ProviderTarget,
				ProviderWorkItemType: input.ProviderWorkItemType,
				SafeTitle:            input.SafeTitle,
				SafeSummary:          input.SafeSummary,
				RoleHint:             input.RoleHint,
				StageHint:            input.StageHint,
				IdempotencyKey:       "domain.Service.CreateFollowUpIntent:user:operator-1:follow-up",
				Status:               enum.FollowUpIntentStatusRequested,
			}, nil
		},
	}
	server := NewServer(service)

	response, err := server.CreateFollowUpIntent(context.Background(), &agentsv1.CreateFollowUpIntentRequest{
		Meta:                 commandMeta("", "follow-up", nil),
		SessionId:            sessionID.String(),
		RunId:                ptr(runID.String()),
		FromStageId:          ptr(fromStageID.String()),
		ToStageId:            ptr(toStageID.String()),
		AcceptanceResultId:   ptr(acceptanceID.String()),
		ProviderTarget:       &agentsv1.ProviderTargetRef{WorkItemRef: ptr("issue:123")},
		ProviderWorkItemType: "task",
		SafeTitle:            ptr("Follow-up"),
		SafeSummary:          ptr("Summary"),
		RoleHint:             ptr("worker"),
		StageHint:            ptr("review"),
	})
	if err != nil {
		t.Fatalf("CreateFollowUpIntent() error = %v", err)
	}
	intent := response.GetFollowUpIntent()
	if intent.GetId() != intentID.String() || intent.GetStatus() != agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_REQUESTED {
		t.Fatalf("intent = %+v", intent)
	}
	if intent.GetProviderTarget().GetWorkItemRef() != "issue:123" || intent.GetSafeSummary() != "Summary" {
		t.Fatalf("intent target = %+v", intent)
	}
}

func TestDispatchFollowUpIntentHandlerMapsRequest(t *testing.T) {
	t.Parallel()

	intentID := uuid.MustParse("85858585-7777-8888-9999-aaaaaaaaaaaa")
	projectID := uuid.MustParse("85858585-8888-9999-aaaa-bbbbbbbbbbbb")
	repositoryID := uuid.MustParse("85858585-9999-aaaa-bbbb-cccccccccccc")
	accountID := uuid.MustParse("85858585-aaaa-bbbb-cccc-dddddddddddd")
	expectedVersion := int64(4)
	service := &fakeAgentService{
		dispatchFollowUpIntent: func(_ context.Context, input agentservice.DispatchFollowUpIntentInput) (entity.FollowUpIntent, error) {
			if input.FollowUpIntentID != intentID || input.ProjectID != projectID || input.RepositoryID != repositoryID || input.ExternalAccountID != accountID {
				t.Fatalf("refs input = %+v", input)
			}
			if input.ProviderSlug != "github" || input.RepositoryTarget.RepositoryFullName != "codex-k8s/kodex" {
				t.Fatalf("provider target = %+v", input.RepositoryTarget)
			}
			if input.OperationPolicyContext.RiskLevel != agentservice.ProviderRiskLevelLow || input.OperationPolicyContext.OperationType != agentservice.ProviderOperationTypeCreateIssue {
				t.Fatalf("policy = %+v", input.OperationPolicyContext)
			}
			if input.ApprovalGateRef.ApprovalID != "gate:123" || input.SafeBodyHint != "Follow-up body" {
				t.Fatalf("approval/body = %+v/%q", input.ApprovalGateRef, input.SafeBodyHint)
			}
			now := sampleTime()
			return entity.FollowUpIntent{
				VersionedBase:        entity.VersionedBase{ID: intentID, Version: expectedVersion + 1, CreatedAt: now, UpdatedAt: now},
				SessionID:            uuid.MustParse("85858585-bbbb-cccc-dddd-eeeeeeeeeeee"),
				ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "github:repo:codex-k8s/kodex:issue:123"},
				ProviderWorkItemType: "task",
				ProviderOperationRef: "provider_operation:op-1",
				SafeTitle:            "Follow-up",
				Status:               enum.FollowUpIntentStatusCreated,
			}, nil
		},
	}
	response, err := NewServer(service).DispatchFollowUpIntent(context.Background(), &agentsv1.DispatchFollowUpIntentRequest{
		Meta:              commandMeta("85858585-cccc-dddd-eeee-ffffffffffff", "", &expectedVersion),
		FollowUpIntentId:  intentID.String(),
		ProjectId:         projectID.String(),
		RepositoryId:      repositoryID.String(),
		ProviderSlug:      "github",
		ExternalAccountId: accountID.String(),
		RepositoryTarget: &providersv1.ProviderTarget{
			ProviderSlug:       "github",
			RepositoryFullName: ptr("codex-k8s/kodex"),
		},
		Labels:                 []string{"agent", "next-stage"},
		AssigneeProviderLogins: []string{"kodex-agent"},
		SafeBodyHint:           ptr("Follow-up body"),
		OperationPolicyContext: &providersv1.ProviderOperationPolicyContext{
			OperationType: providersv1.ProviderOperationType_PROVIDER_OPERATION_TYPE_CREATE_ISSUE,
			RiskLevel:     providersv1.ProviderOperationRiskLevel_PROVIDER_OPERATION_RISK_LEVEL_LOW,
		},
		ApprovalGateRef: &providersv1.ApprovalGateReference{
			ApprovalId: "gate:123",
			GateType:   "owner_approval",
			Decision:   "approved",
		},
	})
	if err != nil {
		t.Fatalf("DispatchFollowUpIntent() error = %v", err)
	}
	if response.GetFollowUpIntent().GetStatus() != agentsv1.FollowUpIntentStatus_FOLLOW_UP_INTENT_STATUS_CREATED {
		t.Fatalf("intent = %+v", response.GetFollowUpIntent())
	}
	if response.GetFollowUpIntent().GetProviderOperationRef() != "provider_operation:op-1" {
		t.Fatalf("provider operation ref = %q", response.GetFollowUpIntent().GetProviderOperationRef())
	}
}

func TestAgentActivityHandlersMapRequests(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("86868686-1111-2222-3333-444444444444")
	runID := uuid.MustParse("86868686-2222-3333-4444-555555555555")
	activityID := uuid.MustParse("86868686-3333-4444-5555-666666666666")
	startedAt := sampleTime()
	finishedAt := startedAt.Add(time.Second)
	durationMs := int64(1000)
	statusFilter := agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_SUCCEEDED
	kindFilter := agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_RESULT
	service := &fakeAgentService{
		recordAgentActivity: func(_ context.Context, input agentservice.RecordAgentActivityInput) (entity.AgentActivity, error) {
			if input.SessionID != sessionID || input.RunID == nil || *input.RunID != runID {
				t.Fatalf("activity refs = %+v", input)
			}
			if input.ActivityKind != enum.AgentActivityKindToolResult || input.Status != enum.AgentActivityStatusSucceeded ||
				input.ToolName != "functions.exec_command" || input.ToolCategory != "shell" {
				t.Fatalf("activity kind/status/tool = %+v", input)
			}
			if input.StartedAt == nil || !input.StartedAt.Equal(startedAt) || input.FinishedAt == nil || !input.FinishedAt.Equal(finishedAt) {
				t.Fatalf("activity time = %+v/%+v", input.StartedAt, input.FinishedAt)
			}
			if string(input.SafeRefsJSON) != `{"artifact_ref":"artifact:1"}` || string(input.SafeDetailsJSON) != `{"summary":"ok"}` {
				t.Fatalf("activity json = %s/%s", input.SafeRefsJSON, input.SafeDetailsJSON)
			}
			return entity.AgentActivity{
				VersionedBase:   entity.VersionedBase{ID: activityID, Version: 1, CreatedAt: sampleTime(), UpdatedAt: sampleTime()},
				SessionID:       sessionID,
				RunID:           &runID,
				TurnID:          input.TurnID,
				ToolUseID:       input.ToolUseID,
				ActivityKind:    input.ActivityKind,
				ToolName:        input.ToolName,
				ToolCategory:    input.ToolCategory,
				Status:          input.Status,
				StartedAt:       *input.StartedAt,
				FinishedAt:      input.FinishedAt,
				DurationMs:      input.DurationMs,
				SafeSummary:     input.SafeSummary,
				SafeRefsJSON:    input.SafeRefsJSON,
				SafeDetailsJSON: input.SafeDetailsJSON,
				CorrelationID:   input.CorrelationID,
				IdempotencyKey:  "domain.Service.RecordAgentActivity:user:operator-1:activity-1",
			}, nil
		},
		listAgentActivities: func(_ context.Context, input agentservice.AgentActivityList) ([]entity.AgentActivity, value.PageResult, error) {
			if input.SessionID != sessionID || input.RunID != runID {
				t.Fatalf("list refs = %+v", input)
			}
			if input.ActivityKind == nil || *input.ActivityKind != enum.AgentActivityKindToolResult ||
				input.Status == nil || *input.Status != enum.AgentActivityStatusSucceeded {
				t.Fatalf("list filters = %+v", input)
			}
			return []entity.AgentActivity{{
				VersionedBase:   entity.VersionedBase{ID: activityID, Version: 1, CreatedAt: sampleTime(), UpdatedAt: sampleTime()},
				SessionID:       sessionID,
				RunID:           &runID,
				ActivityKind:    enum.AgentActivityKindToolResult,
				ToolName:        "functions.exec_command",
				Status:          enum.AgentActivityStatusSucceeded,
				StartedAt:       startedAt,
				SafeRefsJSON:    []byte("{}"),
				SafeDetailsJSON: []byte("{}"),
				IdempotencyKey:  "domain.Service.RecordAgentActivity:user:operator-1:activity-1",
			}}, value.PageResult{NextPageToken: "activities-next"}, nil
		},
	}
	server := NewServer(service)

	recorded, err := server.RecordAgentActivity(context.Background(), &agentsv1.RecordAgentActivityRequest{
		Meta:            commandMeta("", "activity-1", nil),
		SessionId:       sessionID.String(),
		RunId:           ptr(runID.String()),
		TurnId:          ptr("turn:1"),
		ToolUseId:       ptr("tool:1"),
		ActivityKind:    agentsv1.AgentActivityKind_AGENT_ACTIVITY_KIND_TOOL_RESULT,
		ToolName:        ptr("functions.exec_command"),
		ToolCategory:    ptr("shell"),
		Status:          agentsv1.AgentActivityStatus_AGENT_ACTIVITY_STATUS_SUCCEEDED,
		StartedAt:       ptr(startedAt.Format(time.RFC3339Nano)),
		FinishedAt:      ptr(finishedAt.Format(time.RFC3339Nano)),
		DurationMs:      &durationMs,
		SafeSummary:     ptr("Tool completed."),
		SafeRefsJson:    `{"artifact_ref":"artifact:1"}`,
		SafeDetailsJson: `{"summary":"ok"}`,
		CorrelationId:   ptr("trace:1"),
	})
	if err != nil {
		t.Fatalf("RecordAgentActivity() error = %v", err)
	}
	if recorded.GetActivity().GetId() != activityID.String() || recorded.GetActivity().GetToolName() != "functions.exec_command" {
		t.Fatalf("recorded = %+v", recorded.GetActivity())
	}
	list, err := server.ListAgentActivities(context.Background(), &agentsv1.ListAgentActivitiesRequest{
		Meta:         queryMeta(),
		SessionId:    ptr(sessionID.String()),
		RunId:        ptr(runID.String()),
		ActivityKind: &kindFilter,
		Status:       &statusFilter,
		Page:         &agentsv1.PageRequest{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("ListAgentActivities() error = %v", err)
	}
	if len(list.GetActivities()) != 1 || list.GetPage().GetNextPageToken() != "activities-next" {
		t.Fatalf("list = %+v", list)
	}
}

func TestTransportRejectsValidationErrorsBeforeDomainCall(t *testing.T) {
	t.Parallel()

	called := false
	service := &fakeAgentService{
		getFlow: func(context.Context, uuid.UUID) (entity.Flow, error) {
			called = true
			return entity.Flow{}, nil
		},
	}
	_, err := NewServer(service).GetFlow(context.Background(), &agentsv1.GetFlowRequest{Meta: queryMeta(), FlowId: "not-a-uuid"})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("GetFlow() error = %v, want invalid argument", err)
	}
	if called {
		t.Fatal("domain service was called after invalid transport input")
	}
}

func TestUnaryErrorInterceptorMapsDomainErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		code codes.Code
	}{
		{name: "validation", err: errs.ErrInvalidArgument, code: codes.InvalidArgument},
		{name: "not found", err: errs.ErrNotFound, code: codes.NotFound},
		{name: "conflict", err: errs.ErrConflict, code: codes.Aborted},
		{name: "precondition", err: errs.ErrPreconditionFailed, code: codes.FailedPrecondition},
		{name: "dependency unavailable", err: errs.ErrDependencyUnavailable, code: codes.Unavailable},
	}
	interceptor := UnaryErrorInterceptor(slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := interceptor(context.Background(), nil, &grpcruntime.UnaryServerInfo{FullMethod: "/test"}, func(context.Context, any) (any, error) {
				return nil, tt.err
			})
			if status.Code(err) != tt.code {
				t.Fatalf("code = %s, want %s", status.Code(err), tt.code)
			}
		})
	}
}

type fakeAgentService struct {
	createFlow                  func(context.Context, agentservice.CreateFlowInput) (entity.Flow, error)
	updateFlow                  func(context.Context, agentservice.UpdateFlowInput) (entity.Flow, error)
	getFlow                     func(context.Context, uuid.UUID) (entity.Flow, error)
	listFlows                   func(context.Context, agentservice.FlowList) ([]entity.Flow, value.PageResult, error)
	createFlowVersion           func(context.Context, agentservice.CreateFlowVersionInput) (entity.FlowVersion, error)
	activateFlowVersion         func(context.Context, agentservice.ActivateFlowVersionInput) (entity.FlowVersion, error)
	getFlowVersion              func(context.Context, uuid.UUID) (entity.FlowVersion, error)
	createRoleProfile           func(context.Context, agentservice.CreateRoleProfileInput) (entity.RoleProfile, error)
	updateRoleProfile           func(context.Context, agentservice.UpdateRoleProfileInput) (entity.RoleProfile, error)
	getRoleProfile              func(context.Context, uuid.UUID) (entity.RoleProfile, error)
	listRoleProfiles            func(context.Context, agentservice.RoleProfileList) ([]entity.RoleProfile, value.PageResult, error)
	getPromptTemplate           func(context.Context, uuid.UUID) (entity.PromptTemplate, error)
	listPromptTemplates         func(context.Context, agentservice.PromptTemplateList) ([]entity.PromptTemplate, value.PageResult, error)
	createPromptTemplateVersion func(context.Context, agentservice.CreatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error)
	activatePromptVersion       func(context.Context, agentservice.ActivatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error)
	getPromptTemplateVersion    func(context.Context, uuid.UUID) (entity.PromptTemplateVersion, error)
	listPromptTemplateVersions  func(context.Context, agentservice.PromptTemplateVersionList) ([]entity.PromptTemplateVersion, value.PageResult, error)
	startAgentSession           func(context.Context, agentservice.StartAgentSessionInput) (entity.AgentSession, error)
	getAgentSession             func(context.Context, uuid.UUID) (entity.AgentSession, error)
	startAgentRun               func(context.Context, agentservice.StartAgentRunInput) (entity.AgentRun, error)
	recordRunState              func(context.Context, agentservice.RecordRunStateInput) (entity.AgentRun, error)
	recordSessionSnapshot       func(context.Context, agentservice.RecordSessionStateSnapshotInput) (agentservice.SessionSnapshotResult, error)
	listAgentRuns               func(context.Context, agentservice.AgentRunList) ([]entity.AgentRun, value.PageResult, error)
	getSessionStateSnapshot     func(context.Context, uuid.UUID) (entity.AgentSessionStateSnapshot, error)
	requestAcceptance           func(context.Context, agentservice.RequestAcceptanceInput) (entity.AcceptanceResult, error)
	recordAcceptanceResult      func(context.Context, agentservice.RecordAcceptanceResultInput) (entity.AcceptanceResult, error)
	getAcceptanceResult         func(context.Context, uuid.UUID) (entity.AcceptanceResult, error)
	listAcceptanceResults       func(context.Context, agentservice.AcceptanceResultList) ([]entity.AcceptanceResult, value.PageResult, error)
	createFollowUpIntent        func(context.Context, agentservice.CreateFollowUpIntentInput) (entity.FollowUpIntent, error)
	dispatchFollowUpIntent      func(context.Context, agentservice.DispatchFollowUpIntentInput) (entity.FollowUpIntent, error)
	recordAgentActivity         func(context.Context, agentservice.RecordAgentActivityInput) (entity.AgentActivity, error)
	listAgentActivities         func(context.Context, agentservice.AgentActivityList) ([]entity.AgentActivity, value.PageResult, error)
}

func (f *fakeAgentService) CreateFlow(ctx context.Context, input agentservice.CreateFlowInput) (entity.Flow, error) {
	if f.createFlow == nil {
		return entity.Flow{}, errs.ErrPreconditionFailed
	}
	return f.createFlow(ctx, input)
}

func (f *fakeAgentService) UpdateFlow(ctx context.Context, input agentservice.UpdateFlowInput) (entity.Flow, error) {
	if f.updateFlow == nil {
		return entity.Flow{}, errs.ErrPreconditionFailed
	}
	return f.updateFlow(ctx, input)
}

func (f *fakeAgentService) GetFlow(ctx context.Context, id uuid.UUID) (entity.Flow, error) {
	if f.getFlow == nil {
		return entity.Flow{}, errs.ErrPreconditionFailed
	}
	return f.getFlow(ctx, id)
}

func (f *fakeAgentService) ListFlows(ctx context.Context, input agentservice.FlowList) ([]entity.Flow, value.PageResult, error) {
	if f.listFlows == nil {
		return nil, value.PageResult{}, errs.ErrPreconditionFailed
	}
	return f.listFlows(ctx, input)
}

func (f *fakeAgentService) CreateFlowVersion(ctx context.Context, input agentservice.CreateFlowVersionInput) (entity.FlowVersion, error) {
	if f.createFlowVersion == nil {
		return entity.FlowVersion{}, errs.ErrPreconditionFailed
	}
	return f.createFlowVersion(ctx, input)
}

func (f *fakeAgentService) ActivateFlowVersion(ctx context.Context, input agentservice.ActivateFlowVersionInput) (entity.FlowVersion, error) {
	if f.activateFlowVersion == nil {
		return entity.FlowVersion{}, errs.ErrPreconditionFailed
	}
	return f.activateFlowVersion(ctx, input)
}

func (f *fakeAgentService) GetFlowVersion(ctx context.Context, id uuid.UUID) (entity.FlowVersion, error) {
	if f.getFlowVersion == nil {
		return entity.FlowVersion{}, errs.ErrPreconditionFailed
	}
	return f.getFlowVersion(ctx, id)
}

func (f *fakeAgentService) CreateRoleProfile(ctx context.Context, input agentservice.CreateRoleProfileInput) (entity.RoleProfile, error) {
	if f.createRoleProfile == nil {
		return entity.RoleProfile{}, errs.ErrPreconditionFailed
	}
	return f.createRoleProfile(ctx, input)
}

func (f *fakeAgentService) UpdateRoleProfile(ctx context.Context, input agentservice.UpdateRoleProfileInput) (entity.RoleProfile, error) {
	if f.updateRoleProfile == nil {
		return entity.RoleProfile{}, errs.ErrPreconditionFailed
	}
	return f.updateRoleProfile(ctx, input)
}

func (f *fakeAgentService) GetRoleProfile(ctx context.Context, id uuid.UUID) (entity.RoleProfile, error) {
	if f.getRoleProfile == nil {
		return entity.RoleProfile{}, errs.ErrPreconditionFailed
	}
	return f.getRoleProfile(ctx, id)
}

func (f *fakeAgentService) ListRoleProfiles(ctx context.Context, input agentservice.RoleProfileList) ([]entity.RoleProfile, value.PageResult, error) {
	if f.listRoleProfiles == nil {
		return nil, value.PageResult{}, errs.ErrPreconditionFailed
	}
	return f.listRoleProfiles(ctx, input)
}

func (f *fakeAgentService) GetPromptTemplate(ctx context.Context, id uuid.UUID) (entity.PromptTemplate, error) {
	if f.getPromptTemplate == nil {
		return entity.PromptTemplate{}, errs.ErrPreconditionFailed
	}
	return f.getPromptTemplate(ctx, id)
}

func (f *fakeAgentService) ListPromptTemplates(ctx context.Context, input agentservice.PromptTemplateList) ([]entity.PromptTemplate, value.PageResult, error) {
	if f.listPromptTemplates == nil {
		return nil, value.PageResult{}, errs.ErrPreconditionFailed
	}
	return f.listPromptTemplates(ctx, input)
}

func (f *fakeAgentService) CreatePromptTemplateVersion(ctx context.Context, input agentservice.CreatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error) {
	if f.createPromptTemplateVersion == nil {
		return entity.PromptTemplateVersion{}, errs.ErrPreconditionFailed
	}
	return f.createPromptTemplateVersion(ctx, input)
}

func (f *fakeAgentService) ActivatePromptTemplateVersion(ctx context.Context, input agentservice.ActivatePromptTemplateVersionInput) (entity.PromptTemplateVersion, error) {
	if f.activatePromptVersion == nil {
		return entity.PromptTemplateVersion{}, errs.ErrPreconditionFailed
	}
	return f.activatePromptVersion(ctx, input)
}

func (f *fakeAgentService) GetPromptTemplateVersion(ctx context.Context, id uuid.UUID) (entity.PromptTemplateVersion, error) {
	if f.getPromptTemplateVersion == nil {
		return entity.PromptTemplateVersion{}, errs.ErrPreconditionFailed
	}
	return f.getPromptTemplateVersion(ctx, id)
}

func (f *fakeAgentService) ListPromptTemplateVersions(ctx context.Context, input agentservice.PromptTemplateVersionList) ([]entity.PromptTemplateVersion, value.PageResult, error) {
	if f.listPromptTemplateVersions == nil {
		return nil, value.PageResult{}, errs.ErrPreconditionFailed
	}
	return f.listPromptTemplateVersions(ctx, input)
}

func (f *fakeAgentService) StartAgentSession(ctx context.Context, input agentservice.StartAgentSessionInput) (entity.AgentSession, error) {
	if f.startAgentSession == nil {
		return entity.AgentSession{}, errs.ErrPreconditionFailed
	}
	return f.startAgentSession(ctx, input)
}

func (f *fakeAgentService) GetAgentSession(ctx context.Context, id uuid.UUID) (entity.AgentSession, error) {
	if f.getAgentSession == nil {
		return entity.AgentSession{}, errs.ErrPreconditionFailed
	}
	return f.getAgentSession(ctx, id)
}

func (f *fakeAgentService) StartAgentRun(ctx context.Context, input agentservice.StartAgentRunInput) (entity.AgentRun, error) {
	if f.startAgentRun == nil {
		return entity.AgentRun{}, errs.ErrPreconditionFailed
	}
	return f.startAgentRun(ctx, input)
}

func (f *fakeAgentService) RecordRunState(ctx context.Context, input agentservice.RecordRunStateInput) (entity.AgentRun, error) {
	if f.recordRunState == nil {
		return entity.AgentRun{}, errs.ErrPreconditionFailed
	}
	return f.recordRunState(ctx, input)
}

func (f *fakeAgentService) RecordSessionStateSnapshot(ctx context.Context, input agentservice.RecordSessionStateSnapshotInput) (agentservice.SessionSnapshotResult, error) {
	if f.recordSessionSnapshot == nil {
		return agentservice.SessionSnapshotResult{}, errs.ErrPreconditionFailed
	}
	return f.recordSessionSnapshot(ctx, input)
}

func (f *fakeAgentService) ListAgentRuns(ctx context.Context, input agentservice.AgentRunList) ([]entity.AgentRun, value.PageResult, error) {
	if f.listAgentRuns == nil {
		return nil, value.PageResult{}, errs.ErrPreconditionFailed
	}
	return f.listAgentRuns(ctx, input)
}

func (f *fakeAgentService) GetSessionStateSnapshot(ctx context.Context, id uuid.UUID) (entity.AgentSessionStateSnapshot, error) {
	if f.getSessionStateSnapshot == nil {
		return entity.AgentSessionStateSnapshot{}, errs.ErrPreconditionFailed
	}
	return f.getSessionStateSnapshot(ctx, id)
}

func (f *fakeAgentService) RequestAcceptance(ctx context.Context, input agentservice.RequestAcceptanceInput) (entity.AcceptanceResult, error) {
	if f.requestAcceptance == nil {
		return entity.AcceptanceResult{}, errs.ErrPreconditionFailed
	}
	return f.requestAcceptance(ctx, input)
}

func (f *fakeAgentService) RecordAcceptanceResult(ctx context.Context, input agentservice.RecordAcceptanceResultInput) (entity.AcceptanceResult, error) {
	if f.recordAcceptanceResult == nil {
		return entity.AcceptanceResult{}, errs.ErrPreconditionFailed
	}
	return f.recordAcceptanceResult(ctx, input)
}

func (f *fakeAgentService) GetAcceptanceResult(ctx context.Context, id uuid.UUID) (entity.AcceptanceResult, error) {
	if f.getAcceptanceResult == nil {
		return entity.AcceptanceResult{}, errs.ErrPreconditionFailed
	}
	return f.getAcceptanceResult(ctx, id)
}

func (f *fakeAgentService) ListAcceptanceResults(ctx context.Context, input agentservice.AcceptanceResultList) ([]entity.AcceptanceResult, value.PageResult, error) {
	if f.listAcceptanceResults == nil {
		return nil, value.PageResult{}, errs.ErrPreconditionFailed
	}
	return f.listAcceptanceResults(ctx, input)
}

func (f *fakeAgentService) CreateFollowUpIntent(ctx context.Context, input agentservice.CreateFollowUpIntentInput) (entity.FollowUpIntent, error) {
	if f.createFollowUpIntent == nil {
		return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
	}
	return f.createFollowUpIntent(ctx, input)
}

func (f *fakeAgentService) DispatchFollowUpIntent(ctx context.Context, input agentservice.DispatchFollowUpIntentInput) (entity.FollowUpIntent, error) {
	if f.dispatchFollowUpIntent == nil {
		return entity.FollowUpIntent{}, errs.ErrPreconditionFailed
	}
	return f.dispatchFollowUpIntent(ctx, input)
}

func (f *fakeAgentService) RecordAgentActivity(ctx context.Context, input agentservice.RecordAgentActivityInput) (entity.AgentActivity, error) {
	if f.recordAgentActivity == nil {
		return entity.AgentActivity{}, errs.ErrPreconditionFailed
	}
	return f.recordAgentActivity(ctx, input)
}

func (f *fakeAgentService) ListAgentActivities(ctx context.Context, input agentservice.AgentActivityList) ([]entity.AgentActivity, value.PageResult, error) {
	if f.listAgentActivities == nil {
		return nil, value.PageResult{}, errs.ErrPreconditionFailed
	}
	return f.listAgentActivities(ctx, input)
}

func commandMeta(commandID string, idempotencyKey string, expectedVersion *int64) *agentsv1.CommandMeta {
	return &agentsv1.CommandMeta{
		CommandId:       optional(commandID),
		IdempotencyKey:  optional(idempotencyKey),
		ExpectedVersion: expectedVersion,
		Actor:           &agentsv1.Actor{Type: "user", Id: "operator-1"},
		Reason:          "test",
		RequestId:       "request-1",
	}
}

func queryMeta() *agentsv1.QueryMeta {
	return &agentsv1.QueryMeta{Actor: &agentsv1.Actor{Type: "user", Id: "operator-1"}, RequestId: "request-1"}
}

func scopeRef(scopeType agentsv1.AgentScopeType, ref string) *agentsv1.ScopeRef {
	return &agentsv1.ScopeRef{Type: scopeType, Ref: ref}
}

func sampleFlow(id string) entity.Flow {
	now := sampleTime()
	return entity.Flow{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse(id), Version: 1, CreatedAt: now, UpdatedAt: now},
		Scope:         value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		Slug:          "delivery",
		DisplayName:   []value.LocalizedText{{Locale: "ru", Text: "Поставка"}},
		Status:        enum.FlowStatusDraft,
	}
}

func sampleFlowVersion(id string, flowID string) entity.FlowVersion {
	now := sampleTime()
	stageID := uuid.MustParse("88888888-8888-8888-8888-888888888888")
	return entity.FlowVersion{
		ID:               uuid.MustParse(id),
		FlowID:           uuid.MustParse(flowID),
		Version:          1,
		DefinitionDigest: "sha256:flow",
		Status:           enum.FlowVersionStatusDraft,
		CreatedAt:        now,
		Stages: []entity.Stage{{
			ID:                    stageID,
			FlowVersionID:         uuid.MustParse(id),
			Slug:                  "dev",
			StageType:             enum.StageTypeWork,
			RequiredArtifactsJSON: []byte("{}"),
			AcceptancePolicyJSON:  []byte("{}"),
			Position:              1,
		}},
	}
}

func sampleRole(id string) entity.RoleProfile {
	now := sampleTime()
	return entity.RoleProfile{
		VersionedBase:   entity.VersionedBase{ID: uuid.MustParse(id), Version: 1, CreatedAt: now, UpdatedAt: now},
		Scope:           value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		Slug:            "reviewer",
		DisplayName:     []value.LocalizedText{{Locale: "ru", Text: "Ревьюер"}},
		RoleKind:        enum.RoleKindReviewer,
		RuntimeProfile:  "code",
		AllowedMCPTools: []string{"provider.github.comment"},
		Status:          enum.RoleStatusDraft,
	}
}

func samplePromptTemplate(id string, roleID string) entity.PromptTemplate {
	now := sampleTime()
	return entity.PromptTemplate{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse(id), Version: 1, CreatedAt: now, UpdatedAt: now},
		RoleProfileID: uuid.MustParse(roleID),
		PromptKind:    enum.PromptKindReview,
	}
}

func samplePromptVersion(id string, templateID string, roleID string) entity.PromptTemplateVersion {
	return entity.PromptTemplateVersion{
		ID:               uuid.MustParse(id),
		PromptTemplateID: uuid.MustParse(templateID),
		RoleProfileID:    uuid.MustParse(roleID),
		PromptKind:       enum.PromptKindReview,
		Version:          1,
		TemplateObject:   value.ObjectRef{ObjectURI: "s3://bucket/prompt.md", ObjectDigest: "sha256:prompt"},
		TemplateDigest:   "sha256:prompt",
		Status:           enum.PromptVersionStatusDraft,
		CreatedAt:        sampleTime(),
	}
}

func sampleAcceptanceResult(id uuid.UUID, sessionID uuid.UUID, runID *uuid.UUID, stageID *uuid.UUID, checkKind enum.AcceptanceCheckKind, status enum.AcceptanceStatus) entity.AcceptanceResult {
	now := sampleTime()
	return entity.AcceptanceResult{
		VersionedBase: entity.VersionedBase{ID: id, Version: 1, CreatedAt: now, UpdatedAt: now},
		SessionID:     sessionID,
		RunID:         runID,
		StageID:       stageID,
		CheckKind:     checkKind,
		Status:        status,
		TargetRef:     "artifact:run-summary",
		DetailsJSON:   []byte(`{"summary":"ok"}`),
	}
}

func sampleTime() time.Time {
	return time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC)
}

func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func ptr(value string) *string {
	return &value
}

var _ grpcserver.UnaryInterceptor = UnaryErrorInterceptor(nil)
