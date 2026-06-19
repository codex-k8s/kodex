package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	agentevents "github.com/codex-k8s/kodex/libs/go/platformevents/agent"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
)

const (
	testInstructionDigest  = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testResultSchemaDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func testCodexSessionExecutionConfig() CodexSessionExecutionConfig {
	return CodexSessionExecutionConfig{
		ResultSchemaRef:    "object://schemas/codex-result-v1",
		ResultSchemaDigest: testResultSchemaDigest,
		HookEndpointRef:    "hook://codex-hook-ingress/agent-runner",
		TimeoutSeconds:     1800,
	}
}

func TestCreateFlowStoresCommandResult(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	repository := &fakeRepository{}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)},
		IDGenerator: fixedIDGenerator{ids: []uuid.UUID{uuid.MustParse("22222222-2222-2222-2222-222222222222")}},
	})

	flow, err := service.CreateFlow(context.Background(), CreateFlowInput{
		Meta:        value.CommandMeta{CommandID: commandID, Actor: testActor()},
		Scope:       value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		Slug:        "full-delivery",
		DisplayName: []value.LocalizedText{{Locale: "ru", Text: "Полный цикл"}},
	})
	if err != nil {
		t.Fatalf("CreateFlow() err = %v", err)
	}
	if flow.ID != repository.createdFlow.ID {
		t.Fatalf("created flow id = %s, stored = %s", flow.ID, repository.createdFlow.ID)
	}
	if repository.createdResult.AggregateType != enum.CommandAggregateTypeFlow {
		t.Fatalf("aggregate type = %s, want %s", repository.createdResult.AggregateType, enum.CommandAggregateTypeFlow)
	}
	if repository.createdResult.CommandID == nil || *repository.createdResult.CommandID != commandID {
		t.Fatalf("command id = %v, want %s", repository.createdResult.CommandID, commandID)
	}
}

func TestCreateFlowReplaysCommandResult(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	flow := entity.Flow{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Version: 1},
		Scope:         value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		Slug:          "hotfix",
		Status:        enum.FlowStatusDraft,
	}
	payload, err := marshalCommandPayload(flowCommandPayload{Flow: flow})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{replay: &entity.CommandResult{
		CommandID:     &commandID,
		Actor:         testActor(),
		Operation:     operationCreateFlow,
		AggregateType: enum.CommandAggregateTypeFlow,
		AggregateID:   flow.ID,
		ResultPayload: payload,
	}, flowByID: map[uuid.UUID]entity.Flow{flow.ID: flow}}
	service := New(Config{Repository: repository})

	replay, err := service.CreateFlow(context.Background(), CreateFlowInput{
		Meta:  value.CommandMeta{CommandID: commandID, Actor: testActor()},
		Scope: flow.Scope,
		Slug:  flow.Slug,
	})
	if err != nil {
		t.Fatalf("CreateFlow() err = %v", err)
	}
	if replay.ID != flow.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, flow.ID)
	}
	if repository.createFlowCalled {
		t.Fatal("CreateFlowWithResult called during replay")
	}
}

func TestCreateFlowReplayRejectsDifferentScope(t *testing.T) {
	t.Parallel()

	flow := entity.Flow{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse("45454545-4545-4545-4545-454545454545"), Version: 1},
		Scope:         value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		Slug:          "guarded",
		Status:        enum.FlowStatusDraft,
	}
	payload, err := marshalCommandPayload(flowCommandPayload{Flow: flow})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{replay: &entity.CommandResult{
		IdempotencyKey: "create-flow",
		Actor:          testActor(),
		Operation:      operationCreateFlow,
		AggregateType:  enum.CommandAggregateTypeFlow,
		AggregateID:    flow.ID,
		ResultPayload:  payload,
	}, flowByID: map[uuid.UUID]entity.Flow{flow.ID: flow}}
	service := New(Config{Repository: repository})

	_, err = service.CreateFlow(context.Background(), CreateFlowInput{
		Meta:  value.CommandMeta{IdempotencyKey: "create-flow", Actor: testActor()},
		Scope: value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-2"},
		Slug:  flow.Slug,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateFlow() err = %v, want %v", err, errs.ErrConflict)
	}
	if repository.createFlowCalled {
		t.Fatal("CreateFlowWithResult called after rejected replay")
	}
}

func TestCreateFlowSameIdempotencyDifferentActorDoesNotReplay(t *testing.T) {
	t.Parallel()

	flow := entity.Flow{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse("55555555-5555-5555-5555-555555555555"), Version: 1},
		Scope:         value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		Slug:          "actor-bound",
		Status:        enum.FlowStatusDraft,
	}
	payload, err := marshalCommandPayload(flowCommandPayload{Flow: flow})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{replay: &entity.CommandResult{
		IdempotencyKey: "create-flow",
		Actor:          testActor(),
		Operation:      operationCreateFlow,
		AggregateType:  enum.CommandAggregateTypeFlow,
		AggregateID:    flow.ID,
		ResultPayload:  payload,
	}, flowByID: map[uuid.UUID]entity.Flow{flow.ID: flow}}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)},
		IDGenerator: fixedIDGenerator{ids: []uuid.UUID{uuid.MustParse("66666666-6666-6666-6666-666666666666")}},
	})

	otherActor := value.Actor{Type: "service", ID: "agent-manager"}
	_, err = service.CreateFlow(context.Background(), CreateFlowInput{
		Meta:  value.CommandMeta{IdempotencyKey: "create-flow", Actor: otherActor},
		Scope: flow.Scope,
		Slug:  flow.Slug,
	})
	if err != nil {
		t.Fatalf("CreateFlow() err = %v", err)
	}
	if !repository.createFlowCalled {
		t.Fatal("CreateFlowWithResult was not called for another actor")
	}
	if repository.createdResult.Actor != otherActor {
		t.Fatalf("created result actor = %+v, want %+v", repository.createdResult.Actor, otherActor)
	}
}

func TestCreateFlowSameCommandIDDifferentActorReplaysExistingResult(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("67676767-6767-6767-6767-676767676767")
	flow := entity.Flow{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse("68686868-6868-6868-6868-686868686868"), Version: 1},
		Scope:         value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		Slug:          "global-command",
		Status:        enum.FlowStatusDraft,
	}
	payload, err := marshalCommandPayload(flowCommandPayload{Flow: flow})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{replay: &entity.CommandResult{
		CommandID:     &commandID,
		Actor:         testActor(),
		Operation:     operationCreateFlow,
		AggregateType: enum.CommandAggregateTypeFlow,
		AggregateID:   flow.ID,
		ResultPayload: payload,
	}, flowByID: map[uuid.UUID]entity.Flow{flow.ID: flow}}
	service := New(Config{Repository: repository})

	replay, err := service.CreateFlow(context.Background(), CreateFlowInput{
		Meta:  value.CommandMeta{CommandID: commandID, Actor: value.Actor{Type: "service", ID: "agent-manager"}},
		Scope: flow.Scope,
		Slug:  flow.Slug,
	})
	if err != nil {
		t.Fatalf("CreateFlow() err = %v", err)
	}
	if replay.ID != flow.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, flow.ID)
	}
	if repository.createFlowCalled {
		t.Fatal("CreateFlowWithResult was called for same command_id from another actor")
	}
}

func TestActivationEventPayloadsMatchAsyncAPIRequiredFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	flow := entity.Flow{VersionedBase: entity.VersionedBase{ID: uuid.MustParse("77777777-7777-7777-7777-777777777777"), Version: 4}}
	flowVersion := entity.FlowVersion{ID: uuid.MustParse("88888888-8888-8888-8888-888888888888"), FlowID: flow.ID, Version: 2}
	flowEvent, err := flowActivatedEvent(uuid.MustParse("99999999-9999-9999-9999-999999999999"), flow, flowVersion, now)
	if err != nil {
		t.Fatalf("flowActivatedEvent() err = %v", err)
	}
	flowPayload := decodeAgentPayload(t, flowEvent)
	if flowPayload.FlowID == "" || flowPayload.FlowVersionID == "" || flowPayload.ActivatedVersion == 0 || flowPayload.Version == 0 {
		t.Fatalf("flow payload = %+v, want required fields", flowPayload)
	}

	role := entity.RoleProfile{
		VersionedBase:  entity.VersionedBase{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), Version: 3},
		RoleKind:       enum.RoleKindWorker,
		RuntimeProfile: "default",
		Status:         enum.RoleStatusActive,
	}
	roleEvent, err := roleActivatedEvent(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), role, now)
	if err != nil {
		t.Fatalf("roleActivatedEvent() err = %v", err)
	}
	rolePayload := decodeAgentPayload(t, roleEvent)
	if rolePayload.RoleProfileID == "" || rolePayload.RoleProfileVersion == 0 || rolePayload.RoleProfileDigest == "" || rolePayload.Version == 0 {
		t.Fatalf("role payload = %+v, want required fields", rolePayload)
	}

	template := entity.PromptTemplate{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), Version: 5},
		RoleProfileID: role.ID,
		PromptKind:    enum.PromptKindWork,
	}
	promptVersion := entity.PromptTemplateVersion{
		ID:               uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"),
		PromptTemplateID: template.ID,
		RoleProfileID:    role.ID,
		PromptKind:       enum.PromptKindWork,
		Version:          2,
		TemplateDigest:   "sha256:prompt",
	}
	promptEvent, err := promptActivatedEvent(uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"), template, promptVersion, now)
	if err != nil {
		t.Fatalf("promptActivatedEvent() err = %v", err)
	}
	promptPayload := decodeAgentPayload(t, promptEvent)
	if promptPayload.RoleProfileID == "" || promptPayload.PromptTemplateVersionID == "" || promptPayload.PromptTemplateDigest == "" || promptPayload.ActivatedVersion == 0 || promptPayload.Version == 0 {
		t.Fatalf("prompt payload = %+v, want required fields", promptPayload)
	}
}

func TestStartAgentSessionStoresCommandResultAndEvent(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	eventID := uuid.MustParse("22222222-3333-4444-5555-666666666666")
	repository := &fakeRepository{}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{sessionID, eventID}},
	})

	session, err := service.StartAgentSession(context.Background(), StartAgentSessionInput{
		Meta:                value.CommandMeta{CommandID: uuid.MustParse("33333333-4444-5555-6666-777777777777"), Actor: testActor()},
		Scope:               value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		ProviderWorkItemRef: "github:issue:42",
		CreatedByActorRef:   "user:owner",
	})
	if err != nil {
		t.Fatalf("StartAgentSession() err = %v", err)
	}
	if session.ID != sessionID || repository.createdSession.ID != sessionID {
		t.Fatalf("session id = %s, stored = %s", session.ID, repository.createdSession.ID)
	}
	if repository.sessionResult.AggregateType != enum.CommandAggregateTypeSession {
		t.Fatalf("aggregate type = %s, want %s", repository.sessionResult.AggregateType, enum.CommandAggregateTypeSession)
	}
	if repository.sessionEvent.EventType != agentevents.EventSessionCreated {
		t.Fatalf("event type = %s, want %s", repository.sessionEvent.EventType, agentevents.EventSessionCreated)
	}
}

func TestStartAgentSessionReplaysCommandResult(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("33333333-4444-5555-6666-777777777778")
	session := entity.AgentSession{
		VersionedBase:     entity.VersionedBase{ID: uuid.MustParse("33333333-5555-6666-7777-888888888888"), Version: 1},
		Scope:             value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		Status:            enum.AgentSessionStatusOpen,
		CreatedByActorRef: "user:owner",
	}
	payload, err := marshalCommandPayload(agentSessionCommandPayload{Session: session})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			CommandID:     &commandID,
			Actor:         testActor(),
			Operation:     operationStartAgentSession,
			AggregateType: enum.CommandAggregateTypeSession,
			AggregateID:   session.ID,
			ResultPayload: payload,
		},
		sessionByID: map[uuid.UUID]entity.AgentSession{session.ID: session},
	}
	service := New(Config{Repository: repository})

	replay, err := service.StartAgentSession(context.Background(), StartAgentSessionInput{
		Meta:              value.CommandMeta{CommandID: commandID, Actor: testActor()},
		Scope:             session.Scope,
		CreatedByActorRef: session.CreatedByActorRef,
	})
	if err != nil {
		t.Fatalf("StartAgentSession() err = %v", err)
	}
	if replay.ID != session.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, session.ID)
	}
	if repository.createSessionCalled {
		t.Fatal("CreateAgentSessionWithResult called during replay")
	}
}

func TestStartAgentSessionReusesActiveProviderTarget(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("33333333-6666-7777-8888-999999999999")
	session := entity.AgentSession{
		VersionedBase:       entity.VersionedBase{ID: sessionID, Version: 3},
		Scope:               value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
		ProviderWorkItemRef: "github:issue:42",
		Status:              enum.AgentSessionStatusOpen,
		CreatedByActorRef:   "user:owner",
	}
	repository := &fakeRepository{activeSession: session, activeSessionFound: true}
	service := New(Config{
		Repository: repository,
		Clock:      fixedClock{now: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)},
	})

	reused, err := service.StartAgentSession(context.Background(), StartAgentSessionInput{
		Meta:                value.CommandMeta{CommandID: uuid.MustParse("33333333-7777-8888-9999-aaaaaaaaaaaa"), Actor: testActor()},
		Scope:               session.Scope,
		ProviderWorkItemRef: " github:issue:42 ",
		CreatedByActorRef:   "user:owner",
	})
	if err != nil {
		t.Fatalf("StartAgentSession() err = %v", err)
	}
	if reused.ID != sessionID {
		t.Fatalf("reused id = %s, want %s", reused.ID, sessionID)
	}
	if repository.createSessionCalled {
		t.Fatal("CreateAgentSessionWithResult called for active provider target")
	}
	if repository.recordedCommandResult.AggregateID != sessionID {
		t.Fatalf("command result aggregate = %s, want %s", repository.recordedCommandResult.AggregateID, sessionID)
	}
	if repository.sessionEvent.EventType != "" {
		t.Fatalf("unexpected session event = %s", repository.sessionEvent.EventType)
	}
}

func TestStartAgentRunFreezesRoleAndPrompt(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("44444444-5555-6666-7777-888888888888")
	roleID := uuid.MustParse("55555555-6666-7777-8888-999999999999")
	promptVersionID := uuid.MustParse("66666666-7777-8888-9999-aaaaaaaaaaaa")
	runID := uuid.MustParse("77777777-8888-9999-aaaa-bbbbbbbbbbbb")
	eventID := uuid.MustParse("88888888-9999-aaaa-bbbb-cccccccccccc")
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {
				VersionedBase:       entity.VersionedBase{ID: sessionID, Version: 1},
				Scope:               value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
				ProviderWorkItemRef: "github:issue:42",
				Status:              enum.AgentSessionStatusOpen,
			},
		},
		roleByID: map[uuid.UUID]entity.RoleProfile{
			roleID: {
				VersionedBase:  entity.VersionedBase{ID: roleID, Version: 3},
				RoleKind:       enum.RoleKindWorker,
				RuntimeProfile: "full",
				Status:         enum.RoleStatusActive,
			},
		},
		promptVersionByID: map[uuid.UUID]entity.PromptTemplateVersion{
			promptVersionID: {
				ID:             promptVersionID,
				RoleProfileID:  roleID,
				PromptKind:     enum.PromptKindWork,
				TemplateObject: value.ObjectRef{ObjectURI: "object://instructions/work-v1", ObjectDigest: testInstructionDigest},
				TemplateDigest: "sha256:prompt",
				Status:         enum.PromptVersionStatusActive,
			},
		},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{runID, eventID}},
	})

	run, err := service.StartAgentRun(context.Background(), StartAgentRunInput{
		Meta:                    value.CommandMeta{CommandID: uuid.MustParse("99999999-aaaa-bbbb-cccc-dddddddddddd"), Actor: testActor()},
		SessionID:               sessionID,
		RoleProfileID:           roleID,
		PromptTemplateVersionID: promptVersionID,
	})
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.ID != runID || repository.createdRun.ProviderTarget.WorkItemRef != "github:issue:42" {
		t.Fatalf("run = %+v, stored = %+v", run, repository.createdRun)
	}
	if repository.createdRun.RoleProfileVersion != 3 || repository.createdRun.RoleProfileDigest == "" {
		t.Fatalf("role freeze = version %d digest %q", repository.createdRun.RoleProfileVersion, repository.createdRun.RoleProfileDigest)
	}
	if repository.runResult.AggregateType != enum.CommandAggregateTypeRun || repository.runEvent.EventType != agentevents.EventRunRequested {
		t.Fatalf("result/event = %s/%s", repository.runResult.AggregateType, repository.runEvent.EventType)
	}
}

func TestStartAgentRunFreezesGuidanceRefs(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("11111111-aaaa-bbbb-cccc-dddddddddddd")
	roleID := uuid.MustParse("11111111-bbbb-cccc-dddd-eeeeeeeeeeee")
	promptVersionID := uuid.MustParse("11111111-cccc-dddd-eeee-ffffffffffff")
	runID := uuid.MustParse("11111111-dddd-eeee-ffff-111111111111")
	guidanceRef := value.GuidanceRef{
		PackageInstallationRef: "installation-1",
		PackageVersionRef:      "version-1",
		ManifestDigest:         "sha256:manifest",
		CapabilityRef:          "guidance:installation-1",
		CapabilityKind:         "guidance",
		PackageRef:             "package-1",
		PackageSlug:            "go-guidelines",
		PackageVersionLabel:    "v1.0.0",
		PolicySummaryJSON:      `{"package_status":"available"}`,
	}
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {
				VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1},
				Scope:         value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
				Status:        enum.AgentSessionStatusOpen,
			},
		},
		roleByID: map[uuid.UUID]entity.RoleProfile{
			roleID: {
				VersionedBase:  entity.VersionedBase{ID: roleID, Version: 1},
				RoleKind:       enum.RoleKindWorker,
				RuntimeProfile: "full",
				Status:         enum.RoleStatusActive,
			},
		},
		promptVersionByID: map[uuid.UUID]entity.PromptTemplateVersion{
			promptVersionID: {
				ID:             promptVersionID,
				RoleProfileID:  roleID,
				PromptKind:     enum.PromptKindWork,
				TemplateObject: value.ObjectRef{ObjectURI: "object://instructions/work-v1", ObjectDigest: testInstructionDigest},
				TemplateDigest: "sha256:prompt",
				Status:         enum.PromptVersionStatusActive,
			},
		},
	}
	resolver := &fakeGuidanceResolver{refs: []value.GuidanceRef{guidanceRef}}
	service := New(Config{
		Repository:       repository,
		Clock:            fixedClock{now: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)},
		IDGenerator:      &sequenceIDGenerator{ids: []uuid.UUID{runID, uuid.MustParse("11111111-eeee-ffff-1111-222222222222")}},
		GuidanceResolver: resolver,
	})

	run, err := service.StartAgentRun(context.Background(), StartAgentRunInput{
		Meta:                    value.CommandMeta{CommandID: uuid.MustParse("11111111-ffff-1111-2222-333333333333"), Actor: testActor()},
		SessionID:               sessionID,
		RoleProfileID:           roleID,
		PromptTemplateVersionID: promptVersionID,
		GuidanceSelectionHints:  []value.GuidanceSelectionHint{{PackageSlug: "go-guidelines"}},
	})
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if len(run.GuidanceRefs) != 1 || run.GuidanceRefs[0].PackageSlug != "go-guidelines" {
		t.Fatalf("guidance refs = %+v", run.GuidanceRefs)
	}
	if resolver.calls != 1 || resolver.last.Scope.Ref != "project-1" || resolver.last.Hints[0].PackageSlug != "go-guidelines" {
		t.Fatalf("resolver calls/input = %d/%+v", resolver.calls, resolver.last)
	}
	if repository.createdRun.GuidanceRefs[0].PolicySummaryJSON == "" {
		t.Fatalf("stored run guidance refs = %+v", repository.createdRun.GuidanceRefs)
	}
}

func TestStartAgentRunReplayKeepsFrozenGuidanceRefs(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("22222222-aaaa-bbbb-cccc-dddddddddddd")
	run := entity.AgentRun{
		VersionedBase:           entity.VersionedBase{ID: uuid.MustParse("22222222-bbbb-cccc-dddd-eeeeeeeeeeee"), Version: 1},
		SessionID:               uuid.MustParse("22222222-cccc-dddd-eeee-ffffffffffff"),
		RoleProfileID:           uuid.MustParse("22222222-dddd-eeee-ffff-111111111111"),
		RoleProfileVersion:      1,
		RoleProfileDigest:       "sha256:role",
		PromptTemplateVersionID: uuid.MustParse("22222222-eeee-ffff-1111-222222222222"),
		PromptTemplateDigest:    "sha256:prompt",
		GuidanceRefs: []value.GuidanceRef{{
			PackageInstallationRef: "installation-frozen",
			PackageVersionRef:      "version-frozen",
			ManifestDigest:         "sha256:frozen",
			PackageSlug:            "frozen-guidelines",
		}},
		Status: enum.AgentRunStatusRequested,
	}
	payload, err := marshalCommandPayload(agentRunCommandPayload{Run: run})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			CommandID:     &commandID,
			Actor:         testActor(),
			Operation:     operationStartAgentRun,
			AggregateType: enum.CommandAggregateTypeRun,
			AggregateID:   run.ID,
			ResultPayload: payload,
		},
		runByID: map[uuid.UUID]entity.AgentRun{run.ID: run},
	}
	resolver := &fakeGuidanceResolver{err: errs.ErrDependencyUnavailable}
	service := New(Config{Repository: repository, GuidanceResolver: resolver})

	replay, err := service.StartAgentRun(context.Background(), StartAgentRunInput{
		Meta:                    value.CommandMeta{CommandID: commandID, Actor: testActor()},
		SessionID:               run.SessionID,
		RoleProfileID:           run.RoleProfileID,
		PromptTemplateVersionID: run.PromptTemplateVersionID,
		GuidanceSelectionHints:  []value.GuidanceSelectionHint{{PackageSlug: "new-guidelines"}},
	})
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if resolver.calls != 0 {
		t.Fatalf("resolver calls = %d, want 0", resolver.calls)
	}
	if replay.GuidanceRefs[0].PackageSlug != "frozen-guidelines" {
		t.Fatalf("replayed guidance refs = %+v", replay.GuidanceRefs)
	}
}

func TestStartAgentRunPreparesRuntimeWorkspace(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                    "slot-123",
		WorkspaceRef:               "workspace-456",
		MaterializationFingerprint: "sha256:workspace",
		DiagnosticSummary:          "workspace_status=running",
	}}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimePreparationEnabled: true,
	})

	run, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.Status != enum.AgentRunStatusStarting || run.RuntimeContext.SlotRef != "slot-123" || run.RuntimeContext.WorkspaceRef != "workspace-456" {
		t.Fatalf("run runtime state = %s/%+v", run.Status, run.RuntimeContext)
	}
	if run.ResultSummary == "" || !strings.Contains(run.ResultSummary, "fingerprint=sha256:workspace") {
		t.Fatalf("result summary = %q", run.ResultSummary)
	}
	if fixture.policyResolver.calls != 1 || runtimePreparer.calls != 1 {
		t.Fatalf("resolver/preparer calls = %d/%d", fixture.policyResolver.calls, runtimePreparer.calls)
	}
	if runtimePreparer.last.AgentRunID != run.ID || runtimePreparer.last.RuntimeProfile != "go-full" {
		t.Fatalf("runtime input = %+v", runtimePreparer.last)
	}
	kinds := make(map[string]int)
	generatedContextDigest := ""
	for _, source := range runtimePreparer.last.WorkspacePolicy.Sources {
		kinds[source.Kind]++
		if source.Kind == WorkspaceSourceKindGeneratedContext {
			generatedContextDigest = source.Digest
		}
	}
	if kinds[WorkspaceSourceKindCode] != 1 || kinds[WorkspaceSourceKindDocumentation] != 1 ||
		kinds[WorkspaceSourceKindGuidancePackage] != 1 || kinds[WorkspaceSourceKindGeneratedContext] != 1 {
		t.Fatalf("workspace source kinds = %+v", kinds)
	}
	if generatedContextDigest == "" {
		t.Fatal("generated context digest is empty")
	}
	if runtimePreparer.last.WorkspacePolicy.PolicyDigest == "" {
		t.Fatal("workspace policy digest is empty")
	}
	if fixture.repository.updatedRun.Status != enum.AgentRunStatusStarting || fixture.repository.updateRunEvent == nil ||
		fixture.repository.updateRunEvent.EventType != agentevents.EventRunStarted {
		t.Fatalf("updated run/event = %+v/%+v", fixture.repository.updatedRun, fixture.repository.updateRunEvent)
	}
}

func TestStartAgentRunCreatesRuntimeJobAfterPreparation(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	slotRef := uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String()
	materializationRef := uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String()
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                        slotRef,
		SlotStatus:                     RuntimeSlotStatusReady,
		WorkspaceRef:                   materializationRef,
		WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
		DiagnosticSummary:              "workspace_status=completed",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{
		JobRef:            "runtime-job-123",
		Status:            "pending",
		DiagnosticSummary: "job_status=pending",
	}}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
		RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
		CodexSessionExecution:     testCodexSessionExecutionConfig(),
	})

	run, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.Status != enum.AgentRunStatusStarting || run.RuntimeContext.SlotRef != slotRef || run.RuntimeContext.JobRef != "runtime-job-123" {
		t.Fatalf("run runtime state = %s/%+v summary=%q", run.Status, run.RuntimeContext, run.ResultSummary)
	}
	if runtimePreparer.calls != 1 || runtimeJobCreator.calls != 1 {
		t.Fatalf("preparer/job calls = %d/%d", runtimePreparer.calls, runtimeJobCreator.calls)
	}
	if runtimeJobCreator.last.AgentRunID != run.ID || runtimeJobCreator.last.SlotRef != slotRef {
		t.Fatalf("runtime job input = %+v", runtimeJobCreator.last)
	}
	spec := runtimeJobCreator.last.ExecutionSpec
	if spec.AgentRunID != run.ID || spec.SlotID.String() != slotRef || spec.ExpectedMaterializationID.String() != materializationRef {
		t.Fatalf("runtime job spec refs = %+v", spec)
	}
	if spec.ExpectedMaterializationFingerprint != "sha256:workspace" || spec.ContextDigest != "sha256:agent-run-context" {
		t.Fatalf("runtime job spec digests = %+v", spec)
	}
	if spec.WorkspacePVCRef != "pvc://runtime-jobs/runtime-workspace-agent-run" {
		t.Fatalf("runtime job workspace pvc ref = %q", spec.WorkspacePVCRef)
	}
	if spec.RunnerMode != RuntimeJobRunnerModeCodexAgent || spec.RunnerProfileRef != "runner-profile://go-full" || spec.RunnerImageRef != "image://codex-agent@sha256:runner" {
		t.Fatalf("runtime job runner refs = %+v", spec)
	}
	if !hasAgentRunExecutionRef(spec.ReportingTargetRefs, "agent_run_state", "agent-manager://runs/"+run.ID.String()) ||
		!hasAgentRunExecutionRef(spec.ReportingTargetRefs, "agent_activity", "agent-manager://runs/"+run.ID.String()+"/activities") {
		t.Fatalf("runtime job reporting refs = %+v", spec.ReportingTargetRefs)
	}
	codexSpec := spec.CodexSessionExecutionSpec
	if codexSpec == nil {
		t.Fatal("codex session execution spec is nil")
	}
	if codexSpec.InstructionObjectRef != "object://instructions/work-v1" ||
		codexSpec.InstructionObjectDigest != testInstructionDigest ||
		codexSpec.ResultSchemaRef != "object://schemas/codex-result-v1" ||
		codexSpec.ResultSchemaDigest != testResultSchemaDigest {
		t.Fatalf("codex instruction/schema refs = %+v", codexSpec)
	}
	if codexSpec.WorkspaceSnapshotRef == "" || codexSpec.SessionSnapshotRef != "" || codexSpec.TimeoutSeconds != 1800 {
		t.Fatalf("codex snapshot/timeout = %+v", codexSpec)
	}
	if !hasAgentRunExecutionRef(codexSpec.CallbackRefs, "agent_run_state", "agent-manager://runs/"+run.ID.String()) ||
		!hasAgentRunExecutionRef(codexSpec.OutputRefs, "codex_output", "agent-manager://runs/"+run.ID.String()+"/codex-output") ||
		!hasAgentRunExecutionRef(codexSpec.ResultRefs, "codex_result", "agent-manager://runs/"+run.ID.String()+"/codex-result") {
		t.Fatalf("codex callback/output/result refs = %+v/%+v/%+v", codexSpec.CallbackRefs, codexSpec.OutputRefs, codexSpec.ResultRefs)
	}
	if runtimeJobCreator.last.Meta.CommandID == uuid.Nil || runtimeJobCreator.last.Meta.Actor != testActor() {
		t.Fatalf("runtime job meta = %+v", runtimeJobCreator.last.Meta)
	}
	payload := decodeAgentPayload(t, *fixture.repository.updateRunEvent)
	if payload.RuntimeJobRef != "runtime-job-123" || payload.RuntimeSlotRef != slotRef {
		t.Fatalf("event payload runtime refs = %+v", payload)
	}
}

func TestAgentRunEndToEndRuntimeReportingAcceptanceAndFollowUp(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	fixture.ids = []uuid.UUID{
		fixture.runID,
		uuid.MustParse("11010000-0001-4000-8000-000000000001"),
		uuid.MustParse("11010000-0002-4000-8000-000000000002"),
		uuid.MustParse("11010000-0003-4000-8000-000000000003"),
		uuid.MustParse("11010000-0004-4000-8000-000000000004"),
		uuid.MustParse("11010000-0005-4000-8000-000000000005"),
		uuid.MustParse("11010000-0006-4000-8000-000000000006"),
		uuid.MustParse("11010000-0007-4000-8000-000000000007"),
		uuid.MustParse("11010000-0008-4000-8000-000000000008"),
		uuid.MustParse("11010000-0009-4000-8000-000000000009"),
		uuid.MustParse("11010000-0010-4000-8000-000000000010"),
		uuid.MustParse("11010000-0011-4000-8000-000000000011"),
	}
	slotRef := uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String()
	materializationRef := uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String()
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                        slotRef,
		SlotStatus:                     RuntimeSlotStatusReady,
		WorkspaceRef:                   materializationRef,
		WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
		DiagnosticSummary:              "workspace_status=completed",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{
		JobRef:            "runtime-job-ordinary-run",
		Status:            "pending",
		DiagnosticSummary: "job_status=pending",
	}}
	providerDispatcher := &fakeProviderFollowUpDispatcher{
		result: ProviderCommandResult{
			ProviderOperationRef: "provider_operation:ordinary-follow-up",
			ResultRef:            "github:issue:1101",
			Target: ProviderCommandTarget{
				ProviderSlug:       "github",
				RepositoryFullName: "codex-k8s/kodex",
				WorkItemKind:       "issue",
				Number:             1101,
			},
			Status: ProviderOperationStatusSucceeded,
		},
	}
	service := New(Config{
		Repository:                 fixture.repository,
		Clock:                      fixedClock{now: fixture.now},
		IDGenerator:                &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:           fixture.guidanceResolver,
		WorkspacePolicyResolver:    fixture.policyResolver,
		RuntimePreparer:            runtimePreparer,
		RuntimeJobCreator:          runtimeJobCreator,
		RuntimePreparationEnabled:  true,
		RuntimeJobDispatchEnabled:  true,
		RuntimeJobRunnerImageRef:   "image://codex-agent@sha256:runner",
		CodexSessionExecution:      testCodexSessionExecutionConfig(),
		ProviderFollowUpDispatcher: providerDispatcher,
	})

	run, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.Status != enum.AgentRunStatusStarting || run.RuntimeContext.JobRef != "runtime-job-ordinary-run" {
		t.Fatalf("started run = %s/%+v", run.Status, run.RuntimeContext)
	}
	if runtimeJobCreator.last.ExecutionSpec.WorkspacePVCRef != "pvc://runtime-jobs/runtime-workspace-agent-run" ||
		runtimeJobCreator.last.ExecutionSpec.CodexSessionExecutionSpec == nil {
		t.Fatalf("runtime execution spec = %+v", runtimeJobCreator.last.ExecutionSpec)
	}
	fixture.repository.runByID = map[uuid.UUID]entity.AgentRun{run.ID: run}

	runningVersion := run.Version
	running, err := service.ReportAgentRunState(context.Background(), ReportAgentRunStateInput{
		Meta:             value.CommandMeta{CommandID: uuid.MustParse("11010000-1001-4000-8000-000000000001"), ExpectedVersion: &runningVersion, Actor: value.Actor{Type: "service", ID: "agent-runner"}},
		RunID:            run.ID,
		SessionID:        run.SessionID,
		RuntimeSlotRef:   run.RuntimeContext.SlotRef,
		RuntimeJobRef:    run.RuntimeContext.JobRef,
		State:            RunnerRunStateRunning,
		SafeSummary:      ptr("runner started ordinary agent run"),
		DiagnosticDigest: ptr("sha256:runner-running"),
	})
	if err != nil {
		t.Fatalf("ReportAgentRunState(running) err = %v", err)
	}
	if running.Status != enum.AgentRunStatusRunning {
		t.Fatalf("running status = %s", running.Status)
	}
	fixture.repository.runByID[run.ID] = running

	activityStartedAt := fixture.now.Add(time.Minute)
	activityFinishedAt := activityStartedAt.Add(3 * time.Second)
	activity, err := service.RecordAgentActivity(context.Background(), RecordAgentActivityInput{
		Meta:            value.CommandMeta{IdempotencyKey: "ordinary-run-tool-1", Actor: value.Actor{Type: "service", ID: "agent-runner"}},
		SessionID:       run.SessionID,
		RunID:           &run.ID,
		TurnID:          "turn:ordinary-1",
		ToolUseID:       "tool:ordinary-1",
		ActivityKind:    enum.AgentActivityKindToolResult,
		ToolName:        "functions.exec_command",
		ToolCategory:    "shell",
		Status:          enum.AgentActivityStatusSucceeded,
		StartedAt:       &activityStartedAt,
		FinishedAt:      &activityFinishedAt,
		SafeSummary:     "Executed bounded repository inspection.",
		PayloadDigest:   "sha256:" + strings.Repeat("c", 64),
		SafeRefsJSON:    []byte(`{"artifact_ref":"artifact:ordinary-run/tool-1"}`),
		SafeDetailsJSON: []byte(`{"summary":"bounded metadata","exit_code":0}`),
		CorrelationID:   "trace:ordinary-run",
	})
	if err != nil {
		t.Fatalf("RecordAgentActivity() err = %v", err)
	}
	if activity.ID == uuid.Nil || activity.RunID == nil || *activity.RunID != run.ID {
		t.Fatalf("activity = %+v", activity)
	}

	completedVersion := running.Version
	completed, err := service.ReportAgentRunState(context.Background(), ReportAgentRunStateInput{
		Meta:             value.CommandMeta{CommandID: uuid.MustParse("11010000-1002-4000-8000-000000000002"), ExpectedVersion: &completedVersion, Actor: value.Actor{Type: "service", ID: "agent-runner"}},
		RunID:            run.ID,
		SessionID:        run.SessionID,
		RuntimeSlotRef:   run.RuntimeContext.SlotRef,
		RuntimeJobRef:    run.RuntimeContext.JobRef,
		State:            RunnerRunStateCompleted,
		SafeSummary:      ptr("agent run completed with bounded result refs"),
		DiagnosticDigest: ptr("sha256:runner-completed"),
	})
	if err != nil {
		t.Fatalf("ReportAgentRunState(completed) err = %v", err)
	}
	if completed.Status != enum.AgentRunStatusCompleted {
		t.Fatalf("completed status = %s", completed.Status)
	}
	fixture.repository.runByID[run.ID] = completed

	acceptance, err := service.RequestAcceptance(context.Background(), RequestAcceptanceInput{
		Meta:       value.CommandMeta{CommandID: uuid.MustParse("11010000-1003-4000-8000-000000000003"), Actor: testActor()},
		SessionID:  run.SessionID,
		RunID:      &run.ID,
		CheckKinds: []enum.AcceptanceCheckKind{enum.AcceptanceCheckKindRoleResult},
		TargetRef:  "artifact:ordinary-run/result",
	})
	if err != nil {
		t.Fatalf("RequestAcceptance() err = %v", err)
	}
	if acceptance.Status != enum.AcceptanceStatusPending || acceptance.RunID == nil || *acceptance.RunID != run.ID {
		t.Fatalf("acceptance requested = %+v", acceptance)
	}
	fixture.repository.acceptanceByID = map[uuid.UUID]entity.AcceptanceResult{acceptance.ID: acceptance}

	acceptanceVersion := acceptance.Version
	passed, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
		Meta:               value.CommandMeta{CommandID: uuid.MustParse("11010000-1004-4000-8000-000000000004"), ExpectedVersion: &acceptanceVersion, Actor: testActor()},
		AcceptanceResultID: acceptance.ID,
		Status:             enum.AcceptanceStatusPassed,
		TargetRef:          "artifact:ordinary-run/acceptance-summary",
		DetailsJSON:        []byte(`{"summary":"ordinary run accepted","digest":"sha256:accepted","artifact_refs":["artifact:ordinary-run/result"]}`),
	})
	if err != nil {
		t.Fatalf("RecordAcceptanceResult() err = %v", err)
	}
	if passed.Status != enum.AcceptanceStatusPassed {
		t.Fatalf("acceptance status = %s", passed.Status)
	}
	fixture.repository.acceptanceByID[acceptance.ID] = passed

	intent, err := service.CreateFollowUpIntent(context.Background(), CreateFollowUpIntentInput{
		Meta:                  value.CommandMeta{IdempotencyKey: "ordinary-follow-up", Actor: testActor()},
		SessionID:             run.SessionID,
		AcceptanceResultID:    &acceptance.ID,
		ProviderWorkItemType:  "task",
		InstructionBodyDigest: "sha256:" + strings.Repeat("d", 64),
		SafeTitle:             "Prepare ordinary agent follow-up",
		SafeSummary:           "Create a bounded provider-native task after accepted agent run result.",
		RoleHint:              "worker",
		StageHint:             "follow-up",
	})
	if err != nil {
		t.Fatalf("CreateFollowUpIntent() err = %v", err)
	}
	if intent.Status != enum.FollowUpIntentStatusRequested || intent.RunID == nil || *intent.RunID != run.ID ||
		intent.ProviderTarget.WorkItemRef != "github:issue:42" {
		t.Fatalf("follow-up intent = %+v", intent)
	}
	fixture.repository.followUpByID = map[uuid.UUID]entity.FollowUpIntent{intent.ID: intent}

	intentVersion := intent.Version
	updated, err := service.DispatchFollowUpIntent(context.Background(), DispatchFollowUpIntentInput{
		Meta:             value.CommandMeta{CommandID: uuid.MustParse("11010000-1005-4000-8000-000000000005"), ExpectedVersion: &intentVersion, Actor: testActor()},
		FollowUpIntentID: intent.ID,
		DispatchKind:     FollowUpDispatchKindCreateIssue,
		CreateIssue: &FollowUpCreateIssueCommand{
			ProjectID:         fixture.projectID,
			RepositoryID:      fixture.repositoryID,
			ProviderSlug:      "github",
			ExternalAccountID: uuid.MustParse("11010000-2001-4000-8000-000000000001"),
			RepositoryTarget:  ProviderCommandTarget{ProviderSlug: "github", RepositoryFullName: "codex-k8s/kodex"},
			SafeBodyHint:      "Use only accepted safe refs and summary.",
		},
		OperationPolicyContext: ProviderOperationPolicyContext{RiskLevel: ProviderRiskLevelLow},
	})
	if err != nil {
		t.Fatalf("DispatchFollowUpIntent() err = %v", err)
	}
	if updated.Status != enum.FollowUpIntentStatusCreated || updated.ProviderOperationRef != "provider_operation:ordinary-follow-up" ||
		updated.ProviderTarget.WorkItemRef != "github:issue:1101" || providerDispatcher.calls != 1 {
		t.Fatalf("dispatched follow-up = %+v calls=%d", updated, providerDispatcher.calls)
	}
	for _, payload := range [][]byte{
		fixture.repository.updateRunResult.ResultPayload,
		fixture.repository.activityResult.ResultPayload,
		fixture.repository.updateAcceptanceResult.ResultPayload,
		fixture.repository.updateFollowUpResult.ResultPayload,
	} {
		for _, forbidden := range []string{"prompt_text", "transcript", "raw_provider_payload", "tool_input", "tool_response", "secret_value", "kubeconfig"} {
			if strings.Contains(string(payload), forbidden) {
				t.Fatalf("ordinary run e2e payload contains forbidden marker %q: %s", forbidden, payload)
			}
		}
	}
}

func TestStartAgentRunWaitsForRuntimeMaterializationBeforeJobDispatch(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	slotRef := uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String()
	materializationRef := uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String()
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                        slotRef,
		SlotStatus:                     "materializing",
		WorkspaceRef:                   materializationRef,
		WorkspaceMaterializationStatus: "pending",
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
		DiagnosticSummary:              "slot_status=materializing;workspace_status=pending",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{JobRef: "runtime-job-123", Status: "pending"}}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
		RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
		CodexSessionExecution:     testCodexSessionExecutionConfig(),
	})

	run, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.Status != enum.AgentRunStatusWaiting || run.RuntimeContext.SlotRef != slotRef || run.RuntimeContext.JobRef != "" {
		t.Fatalf("run runtime state = %s/%+v", run.Status, run.RuntimeContext)
	}
	if runtimeJobCreator.calls != 0 {
		t.Fatalf("runtime job calls = %d, want 0", runtimeJobCreator.calls)
	}
	payload := decodeAgentPayload(t, *fixture.repository.updateRunEvent)
	if payload.ReasonCode != runtimeMaterializationPending {
		t.Fatalf("reason code = %q, want %q", payload.ReasonCode, runtimeMaterializationPending)
	}
}

func TestStartAgentRunStoresRetryableRuntimePreparationFailureAsWaiting(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	runtimePreparer := &fakeRuntimePreparer{err: NewRuntimePreparationError(true, "dependency_unavailable", "runtime-manager unavailable")}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimePreparationEnabled: true,
	})

	run, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.Status != enum.AgentRunStatusWaiting || run.FailureCode != "" {
		t.Fatalf("run status/failure = %s/%q", run.Status, run.FailureCode)
	}
	if !strings.Contains(run.ResultSummary, "runtime prepare retryable") {
		t.Fatalf("result summary = %q", run.ResultSummary)
	}
	payload := decodeAgentPayload(t, *fixture.repository.updateRunEvent)
	if payload.ReasonCode != runtimePrepareReasonRetryable {
		t.Fatalf("reason code = %q, want %q", payload.ReasonCode, runtimePrepareReasonRetryable)
	}
}

func TestStartAgentRunStoresRetryableRuntimeJobFailureAsWaiting(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	slotRef := uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String()
	materializationRef := uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String()
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                        slotRef,
		SlotStatus:                     RuntimeSlotStatusReady,
		WorkspaceRef:                   materializationRef,
		WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{err: NewRuntimeJobError(true, "dependency_unavailable", "runtime-manager unavailable")}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
		RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
		CodexSessionExecution:     testCodexSessionExecutionConfig(),
	})

	run, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.Status != enum.AgentRunStatusWaiting || run.FailureCode != "" || run.RuntimeContext.JobRef != "" {
		t.Fatalf("run status/failure/runtime = %s/%q/%+v", run.Status, run.FailureCode, run.RuntimeContext)
	}
	if run.RuntimeContext.SlotRef != slotRef || !strings.Contains(run.ResultSummary, "runtime job retryable") {
		t.Fatalf("run runtime summary = %+v/%q", run.RuntimeContext, run.ResultSummary)
	}
	payload := decodeAgentPayload(t, *fixture.repository.updateRunEvent)
	if payload.ReasonCode != runtimeJobReasonRetryable {
		t.Fatalf("reason code = %q, want %q", payload.ReasonCode, runtimeJobReasonRetryable)
	}
}

func TestStartAgentRunStoresPermanentRuntimePreparationFailureAsFailed(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	runtimePreparer := &fakeRuntimePreparer{err: NewRuntimePreparationError(false, "failed_precondition", "workspace policy invalid")}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimePreparationEnabled: true,
	})

	run, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.Status != enum.AgentRunStatusFailed || run.FailureCode != runtimePrepareFailureCode || run.FinishedAt == nil {
		t.Fatalf("run failed state = %s/%q/%v", run.Status, run.FailureCode, run.FinishedAt)
	}
	if !strings.Contains(run.ResultSummary, "runtime prepare permanent") {
		t.Fatalf("result summary = %q", run.ResultSummary)
	}
	if fixture.repository.updateRunEvent == nil || fixture.repository.updateRunEvent.EventType != agentevents.EventRunFailed {
		t.Fatalf("event = %+v", fixture.repository.updateRunEvent)
	}
}

func TestStartAgentRunStoresPermanentRuntimeJobFailureAsFailed(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	slotRef := uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String()
	materializationRef := uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String()
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                        slotRef,
		SlotStatus:                     RuntimeSlotStatusReady,
		WorkspaceRef:                   materializationRef,
		WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{err: NewRuntimeJobError(false, "failed_precondition", "agent run job rejected")}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
		RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
		CodexSessionExecution:     testCodexSessionExecutionConfig(),
	})

	run, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.Status != enum.AgentRunStatusFailed || run.FailureCode != runtimeJobFailureCode || run.FinishedAt == nil {
		t.Fatalf("run failed state = %s/%q/%v", run.Status, run.FailureCode, run.FinishedAt)
	}
	if run.RuntimeContext.SlotRef != slotRef || run.RuntimeContext.JobRef != "" || !strings.Contains(run.ResultSummary, "runtime job permanent") {
		t.Fatalf("run runtime summary = %+v/%q", run.RuntimeContext, run.ResultSummary)
	}
	if fixture.repository.updateRunEvent == nil || fixture.repository.updateRunEvent.EventType != agentevents.EventRunFailed {
		t.Fatalf("event = %+v", fixture.repository.updateRunEvent)
	}
}

func TestStartAgentRunRuntimeJobDispatchRequiresExecutionSpecRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		result         RuntimePreparationResult
		runnerImageRef string
		wantStatus     enum.AgentRunStatus
	}{
		{
			name: "missing materialization ref",
			result: RuntimePreparationResult{
				SlotRef:                        uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String(),
				SlotStatus:                     RuntimeSlotStatusReady,
				WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
				ContextDigest:                  "sha256:agent-run-context",
				WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
				MaterializationFingerprint:     "sha256:workspace",
			},
			runnerImageRef: "image://codex-agent@sha256:runner",
			wantStatus:     enum.AgentRunStatusWaiting,
		},
		{
			name: "missing context digest",
			result: RuntimePreparationResult{
				SlotRef:                        uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String(),
				SlotStatus:                     RuntimeSlotStatusReady,
				WorkspaceRef:                   uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String(),
				WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
				WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
				MaterializationFingerprint:     "sha256:workspace",
			},
			runnerImageRef: "image://codex-agent@sha256:runner",
			wantStatus:     enum.AgentRunStatusWaiting,
		},
		{
			name: "missing workspace pvc ref",
			result: RuntimePreparationResult{
				SlotRef:                        uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String(),
				SlotStatus:                     RuntimeSlotStatusReady,
				WorkspaceRef:                   uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String(),
				WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
				ContextDigest:                  "sha256:agent-run-context",
				MaterializationFingerprint:     "sha256:workspace",
			},
			runnerImageRef: "image://codex-agent@sha256:runner",
			wantStatus:     enum.AgentRunStatusWaiting,
		},
		{
			name: "missing runner image ref",
			result: RuntimePreparationResult{
				SlotRef:                        uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String(),
				SlotStatus:                     RuntimeSlotStatusReady,
				WorkspaceRef:                   uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String(),
				WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
				ContextDigest:                  "sha256:agent-run-context",
				WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
				MaterializationFingerprint:     "sha256:workspace",
			},
			wantStatus: enum.AgentRunStatusFailed,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := newRuntimePreparationFixture()
			runtimePreparer := &fakeRuntimePreparer{result: tt.result}
			runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{JobRef: "runtime-job-123", Status: "pending"}}
			service := New(Config{
				Repository:                fixture.repository,
				Clock:                     fixedClock{now: fixture.now},
				IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
				GuidanceResolver:          fixture.guidanceResolver,
				WorkspacePolicyResolver:   fixture.policyResolver,
				RuntimePreparer:           runtimePreparer,
				RuntimeJobCreator:         runtimeJobCreator,
				RuntimePreparationEnabled: true,
				RuntimeJobDispatchEnabled: true,
				RuntimeJobRunnerImageRef:  tt.runnerImageRef,
				CodexSessionExecution:     testCodexSessionExecutionConfig(),
			})

			run, err := service.StartAgentRun(context.Background(), fixture.input)
			if err != nil {
				t.Fatalf("StartAgentRun() err = %v", err)
			}
			if run.Status != tt.wantStatus || runtimeJobCreator.calls != 0 || run.RuntimeContext.JobRef != "" {
				t.Fatalf("run/job state = %s/%+v, calls=%d", run.Status, run.RuntimeContext, runtimeJobCreator.calls)
			}
			if !strings.Contains(run.ResultSummary, "runtime job") {
				t.Fatalf("result summary = %q", run.ResultSummary)
			}
		})
	}
}

func TestStartAgentRunCodexSessionExecutionSpecRequiresMaterializedRefs(t *testing.T) {
	t.Parallel()

	readyResult := RuntimePreparationResult{
		SlotRef:                        uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String(),
		SlotStatus:                     RuntimeSlotStatusReady,
		WorkspaceRef:                   uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String(),
		WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
	}
	tests := []struct {
		name       string
		edit       func(*runtimePreparationFixture, *CodexSessionExecutionConfig)
		wantStatus enum.AgentRunStatus
		wantReason string
	}{
		{
			name: "missing instruction object waits",
			edit: func(fixture *runtimePreparationFixture, _ *CodexSessionExecutionConfig) {
				version := fixture.repository.promptVersionByID[fixture.promptVersionID]
				version.TemplateObject = value.ObjectRef{}
				fixture.repository.promptVersionByID[fixture.promptVersionID] = version
			},
			wantStatus: enum.AgentRunStatusWaiting,
			wantReason: "runtime job retryable",
		},
		{
			name: "missing result schema waits",
			edit: func(_ *runtimePreparationFixture, cfg *CodexSessionExecutionConfig) {
				cfg.ResultSchemaRef = ""
				cfg.ResultSchemaDigest = ""
			},
			wantStatus: enum.AgentRunStatusWaiting,
			wantReason: "runtime job retryable",
		},
		{
			name: "invalid schema digest fails",
			edit: func(_ *runtimePreparationFixture, cfg *CodexSessionExecutionConfig) {
				cfg.ResultSchemaDigest = "sha256:not-a-digest"
			},
			wantStatus: enum.AgentRunStatusFailed,
			wantReason: "failed_precondition",
		},
		{
			name: "invalid instruction ref fails",
			edit: func(fixture *runtimePreparationFixture, _ *CodexSessionExecutionConfig) {
				version := fixture.repository.promptVersionByID[fixture.promptVersionID]
				version.TemplateObject.ObjectURI = "object://prompt_body/raw"
				fixture.repository.promptVersionByID[fixture.promptVersionID] = version
			},
			wantStatus: enum.AgentRunStatusFailed,
			wantReason: "failed_precondition",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := newRuntimePreparationFixture()
			codexConfig := testCodexSessionExecutionConfig()
			tt.edit(&fixture, &codexConfig)
			runtimePreparer := &fakeRuntimePreparer{result: readyResult}
			runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{JobRef: "runtime-job-123", Status: "pending"}}
			service := New(Config{
				Repository:                fixture.repository,
				Clock:                     fixedClock{now: fixture.now},
				IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
				GuidanceResolver:          fixture.guidanceResolver,
				WorkspacePolicyResolver:   fixture.policyResolver,
				RuntimePreparer:           runtimePreparer,
				RuntimeJobCreator:         runtimeJobCreator,
				RuntimePreparationEnabled: true,
				RuntimeJobDispatchEnabled: true,
				RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
				CodexSessionExecution:     codexConfig,
			})

			run, err := service.StartAgentRun(context.Background(), fixture.input)
			if err != nil {
				t.Fatalf("StartAgentRun() err = %v", err)
			}
			if run.Status != tt.wantStatus || run.RuntimeContext.JobRef != "" || runtimeJobCreator.calls != 0 {
				t.Fatalf("run/job state = %s/%+v calls=%d", run.Status, run.RuntimeContext, runtimeJobCreator.calls)
			}
			if !strings.Contains(run.ResultSummary, tt.wantReason) {
				t.Fatalf("result summary = %q, want marker %q", run.ResultSummary, tt.wantReason)
			}
		})
	}
}

func TestStartAgentRunReplayDispatchesCodexSessionSpecAfterRefsReady(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	slotRef := uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String()
	materializationRef := uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String()
	run := entity.AgentRun{
		VersionedBase:           entity.VersionedBase{ID: fixture.runID, Version: 2, CreatedAt: fixture.now, UpdatedAt: fixture.now},
		SessionID:               fixture.sessionID,
		RoleProfileID:           fixture.roleID,
		RoleProfileVersion:      1,
		RoleProfileDigest:       "sha256:role",
		PromptTemplateVersionID: fixture.promptVersionID,
		PromptTemplateDigest:    "sha256:prompt",
		RuntimeContext:          value.RuntimeContextRef{SlotRef: slotRef, WorkspaceRef: materializationRef},
		GuidanceRefs:            fixture.guidanceResolver.refs,
		Status:                  enum.AgentRunStatusWaiting,
		ResultSummary:           "runtime job retryable: code=execution_input_unavailable; message=codex session execution input is unavailable",
	}
	startedRun := run
	startedRun.Version = 1
	startedRun.RuntimeContext = value.RuntimeContextRef{}
	startedRun.Status = enum.AgentRunStatusRequested
	startedRun.ResultSummary = ""
	payload, err := marshalCommandPayload(agentRunCommandPayload{Run: startedRun})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	commandID := fixture.input.Meta.CommandID
	fixture.repository.replay = &entity.CommandResult{
		CommandID:     &commandID,
		Actor:         testActor(),
		Operation:     operationStartAgentRun,
		AggregateType: enum.CommandAggregateTypeRun,
		AggregateID:   run.ID,
		ResultPayload: payload,
	}
	fixture.repository.runByID = map[uuid.UUID]entity.AgentRun{run.ID: run}
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                        slotRef,
		SlotStatus:                     RuntimeSlotStatusReady,
		WorkspaceRef:                   materializationRef,
		WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{JobRef: "runtime-job-123", Status: "pending"}}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
		RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
		CodexSessionExecution:     testCodexSessionExecutionConfig(),
	})

	replay, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if runtimePreparer.calls != 1 || runtimeJobCreator.calls != 1 {
		t.Fatalf("preparer/job calls = %d/%d", runtimePreparer.calls, runtimeJobCreator.calls)
	}
	if replay.RuntimeContext.JobRef != "runtime-job-123" || replay.Status != enum.AgentRunStatusStarting {
		t.Fatalf("replayed run = %s/%+v", replay.Status, replay.RuntimeContext)
	}
	if runtimeJobCreator.last.ExecutionSpec.CodexSessionExecutionSpec == nil {
		t.Fatal("replay runtime job missing codex session execution spec")
	}
	if runtimeJobCreator.last.ExecutionSpec.WorkspacePVCRef != "pvc://runtime-jobs/runtime-workspace-agent-run" {
		t.Fatalf("replay runtime job workspace pvc ref = %q", runtimeJobCreator.last.ExecutionSpec.WorkspacePVCRef)
	}
}

func TestStartAgentRunRuntimeJobDispatchRejectsMissingGuidanceRefs(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	fixture.guidanceResolver.refs[0].ManifestDigest = ""
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                    uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String(),
		WorkspaceRef:               uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String(),
		ContextDigest:              "sha256:agent-run-context",
		WorkspacePVCRef:            "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint: "sha256:workspace",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{JobRef: "runtime-job-123", Status: "pending"}}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
		RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
		CodexSessionExecution:     testCodexSessionExecutionConfig(),
	})

	run, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.Status != enum.AgentRunStatusFailed || runtimePreparer.calls != 0 || runtimeJobCreator.calls != 0 {
		t.Fatalf("run/preparer/job state = %s/%d/%d", run.Status, runtimePreparer.calls, runtimeJobCreator.calls)
	}
}

func TestStartAgentRunRuntimeRequestDoesNotCarryTextPayloads(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	fixture.repository.promptVersionByID[fixture.promptVersionID] = entity.PromptTemplateVersion{
		ID:             fixture.promptVersionID,
		RoleProfileID:  fixture.roleID,
		PromptKind:     enum.PromptKindWork,
		TemplateObject: value.ObjectRef{ObjectURI: "object://instructions/safe-work", ObjectDigest: testInstructionDigest},
		TemplateDigest: "sha256:prompt",
		Status:         enum.PromptVersionStatusActive,
	}
	fixture.guidanceResolver.refs[0].PolicySummaryJSON = `{"payload_json":"SKILL.md prompt template flow file"}`
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{SlotRef: "slot-123", WorkspaceRef: "workspace-456", MaterializationFingerprint: "sha256:workspace"}}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimePreparationEnabled: true,
	})

	if _, err := service.StartAgentRun(context.Background(), fixture.input); err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	requestPayload, err := json.Marshal(runtimePreparer.last)
	if err != nil {
		t.Fatalf("marshal runtime request: %v", err)
	}
	for _, forbidden := range []string{"SKILL.md", "prompt-template-text", "flow file", "payload_json"} {
		if strings.Contains(string(requestPayload), forbidden) {
			t.Fatalf("runtime request contains forbidden payload marker %q: %s", forbidden, requestPayload)
		}
	}
}

func TestStartAgentRunRuntimeJobSpecDoesNotCarryTextPayloads(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	fixture.repository.promptVersionByID[fixture.promptVersionID] = entity.PromptTemplateVersion{
		ID:             fixture.promptVersionID,
		RoleProfileID:  fixture.roleID,
		PromptKind:     enum.PromptKindWork,
		TemplateObject: value.ObjectRef{ObjectURI: "s3://prompt-template-text/payload"},
		TemplateDigest: "sha256:prompt",
		Status:         enum.PromptVersionStatusActive,
	}
	fixture.guidanceResolver.refs[0].PolicySummaryJSON = `{"payload_json":"SKILL.md prompt template flow file"}`
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                        uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String(),
		SlotStatus:                     RuntimeSlotStatusReady,
		WorkspaceRef:                   uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String(),
		WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{JobRef: "runtime-job-123", Status: "pending"}}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
		RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
		CodexSessionExecution:     testCodexSessionExecutionConfig(),
	})

	if _, err := service.StartAgentRun(context.Background(), fixture.input); err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	specPayload, err := json.Marshal(runtimeJobCreator.last.ExecutionSpec)
	if err != nil {
		t.Fatalf("marshal runtime job spec: %v", err)
	}
	for _, forbidden := range []string{"SKILL.md", "prompt-template-text", "flow file", "payload_json", "raw_provider_payload", "secret_value"} {
		if strings.Contains(string(specPayload), forbidden) {
			t.Fatalf("runtime job spec contains forbidden payload marker %q: %s", forbidden, specPayload)
		}
	}
}

func TestStartAgentRunRuntimeJobDispatchReplayDoesNotCreateDuplicateJob(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("22222222-aaaa-bbbb-cccc-dddddddddddd")
	run := entity.AgentRun{
		VersionedBase:           entity.VersionedBase{ID: uuid.MustParse("22222222-bbbb-cccc-dddd-eeeeeeeeeeee"), Version: 2},
		SessionID:               uuid.MustParse("22222222-cccc-dddd-eeee-ffffffffffff"),
		RoleProfileID:           uuid.MustParse("22222222-dddd-eeee-ffff-111111111111"),
		RoleProfileVersion:      1,
		RoleProfileDigest:       "sha256:role",
		PromptTemplateVersionID: uuid.MustParse("22222222-eeee-ffff-1111-222222222222"),
		PromptTemplateDigest:    "sha256:prompt",
		RuntimeContext:          value.RuntimeContextRef{SlotRef: "slot-frozen", WorkspaceRef: "workspace-frozen", JobRef: "job-frozen"},
		Status:                  enum.AgentRunStatusStarting,
	}
	payload, err := marshalCommandPayload(agentRunCommandPayload{Run: run})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			CommandID:     &commandID,
			Actor:         testActor(),
			Operation:     operationStartAgentRun,
			AggregateType: enum.CommandAggregateTypeRun,
			AggregateID:   run.ID,
			ResultPayload: payload,
		},
		runByID: map[uuid.UUID]entity.AgentRun{run.ID: run},
	}
	runtimePreparer := &fakeRuntimePreparer{err: errs.ErrDependencyUnavailable}
	runtimeJobCreator := &fakeRuntimeJobCreator{err: errs.ErrDependencyUnavailable}
	service := New(Config{
		Repository:                repository,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
	})

	replay, err := service.StartAgentRun(context.Background(), StartAgentRunInput{
		Meta:                    value.CommandMeta{CommandID: commandID, Actor: testActor()},
		SessionID:               run.SessionID,
		RoleProfileID:           run.RoleProfileID,
		PromptTemplateVersionID: run.PromptTemplateVersionID,
	})
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if runtimePreparer.calls != 0 || runtimeJobCreator.calls != 0 {
		t.Fatalf("preparer/job calls = %d/%d", runtimePreparer.calls, runtimeJobCreator.calls)
	}
	if replay.RuntimeContext.JobRef != "job-frozen" {
		t.Fatalf("replayed runtime context = %+v", replay.RuntimeContext)
	}
}

func TestStartAgentRunReplayDispatchesRuntimeJobAfterMaterializationReady(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	slotRef := uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String()
	materializationRef := uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String()
	run := entity.AgentRun{
		VersionedBase:           entity.VersionedBase{ID: fixture.runID, Version: 2, CreatedAt: fixture.now, UpdatedAt: fixture.now},
		SessionID:               fixture.sessionID,
		RoleProfileID:           fixture.roleID,
		RoleProfileVersion:      1,
		RoleProfileDigest:       "sha256:role",
		PromptTemplateVersionID: fixture.promptVersionID,
		PromptTemplateDigest:    "sha256:prompt",
		RuntimeContext:          value.RuntimeContextRef{SlotRef: slotRef, WorkspaceRef: materializationRef},
		GuidanceRefs:            fixture.guidanceResolver.refs,
		Status:                  enum.AgentRunStatusWaiting,
		ResultSummary:           "runtime materialization pending",
	}
	startedRun := run
	startedRun.Version = 1
	startedRun.RuntimeContext = value.RuntimeContextRef{}
	startedRun.Status = enum.AgentRunStatusRequested
	startedRun.ResultSummary = ""
	payload, err := marshalCommandPayload(agentRunCommandPayload{Run: startedRun})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	commandID := fixture.input.Meta.CommandID
	fixture.repository.replay = &entity.CommandResult{
		CommandID:     &commandID,
		Actor:         testActor(),
		Operation:     operationStartAgentRun,
		AggregateType: enum.CommandAggregateTypeRun,
		AggregateID:   run.ID,
		ResultPayload: payload,
	}
	fixture.repository.runByID = make(map[uuid.UUID]entity.AgentRun)
	fixture.repository.runByID[run.ID] = run
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                        slotRef,
		SlotStatus:                     RuntimeSlotStatusReady,
		WorkspaceRef:                   materializationRef,
		WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusCompleted,
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{JobRef: "runtime-job-123", Status: "pending"}}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
		RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
		CodexSessionExecution:     testCodexSessionExecutionConfig(),
	})

	replay, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if fixture.repository.createRunCalled || fixture.guidanceResolver.calls != 0 {
		t.Fatalf("create/guidance calls = %t/%d", fixture.repository.createRunCalled, fixture.guidanceResolver.calls)
	}
	if runtimePreparer.calls != 1 || runtimeJobCreator.calls != 1 {
		t.Fatalf("preparer/job calls = %d/%d", runtimePreparer.calls, runtimeJobCreator.calls)
	}
	if replay.RuntimeContext.JobRef != "runtime-job-123" || replay.Status != enum.AgentRunStatusStarting {
		t.Fatalf("replayed run = %s/%+v", replay.Status, replay.RuntimeContext)
	}
}

func TestStartAgentRunReplayRecordsFailedRuntimeMaterialization(t *testing.T) {
	t.Parallel()

	fixture := newRuntimePreparationFixture()
	slotRef := uuid.MustParse("10101010-bbbb-cccc-dddd-eeeeeeeeeeee").String()
	materializationRef := uuid.MustParse("10101010-cccc-dddd-eeee-ffffffffffff").String()
	run := entity.AgentRun{
		VersionedBase:           entity.VersionedBase{ID: fixture.runID, Version: 2, CreatedAt: fixture.now, UpdatedAt: fixture.now},
		SessionID:               fixture.sessionID,
		RoleProfileID:           fixture.roleID,
		RoleProfileVersion:      1,
		RoleProfileDigest:       "sha256:role",
		PromptTemplateVersionID: fixture.promptVersionID,
		PromptTemplateDigest:    "sha256:prompt",
		RuntimeContext:          value.RuntimeContextRef{SlotRef: slotRef, WorkspaceRef: materializationRef},
		GuidanceRefs:            fixture.guidanceResolver.refs,
		Status:                  enum.AgentRunStatusWaiting,
		ResultSummary:           "runtime materialization pending",
	}
	startedRun := run
	startedRun.Version = 1
	startedRun.RuntimeContext = value.RuntimeContextRef{}
	startedRun.Status = enum.AgentRunStatusRequested
	startedRun.ResultSummary = ""
	payload, err := marshalCommandPayload(agentRunCommandPayload{Run: startedRun})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	commandID := fixture.input.Meta.CommandID
	fixture.repository.replay = &entity.CommandResult{
		CommandID:     &commandID,
		Actor:         testActor(),
		Operation:     operationStartAgentRun,
		AggregateType: enum.CommandAggregateTypeRun,
		AggregateID:   run.ID,
		ResultPayload: payload,
	}
	fixture.repository.runByID = map[uuid.UUID]entity.AgentRun{run.ID: run}
	runtimePreparer := &fakeRuntimePreparer{result: RuntimePreparationResult{
		SlotRef:                        slotRef,
		SlotStatus:                     RuntimeSlotStatusReady,
		WorkspaceRef:                   materializationRef,
		WorkspaceMaterializationStatus: RuntimeWorkspaceMaterializationStatusFailed,
		ContextDigest:                  "sha256:agent-run-context",
		WorkspacePVCRef:                "pvc://runtime-jobs/runtime-workspace-agent-run",
		MaterializationFingerprint:     "sha256:workspace",
	}}
	runtimeJobCreator := &fakeRuntimeJobCreator{result: RuntimeJobResult{JobRef: "runtime-job-123", Status: "pending"}}
	service := New(Config{
		Repository:                fixture.repository,
		Clock:                     fixedClock{now: fixture.now},
		IDGenerator:               &sequenceIDGenerator{ids: fixture.ids},
		GuidanceResolver:          fixture.guidanceResolver,
		WorkspacePolicyResolver:   fixture.policyResolver,
		RuntimePreparer:           runtimePreparer,
		RuntimeJobCreator:         runtimeJobCreator,
		RuntimePreparationEnabled: true,
		RuntimeJobDispatchEnabled: true,
		RuntimeJobRunnerImageRef:  "image://codex-agent@sha256:runner",
	})

	replay, err := service.StartAgentRun(context.Background(), fixture.input)
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if runtimePreparer.calls != 1 || runtimeJobCreator.calls != 0 {
		t.Fatalf("preparer/job calls = %d/%d", runtimePreparer.calls, runtimeJobCreator.calls)
	}
	if replay.Status != enum.AgentRunStatusFailed || replay.FailureCode != runtimePrepareFailureCode || replay.RuntimeContext.SlotRef != slotRef {
		t.Fatalf("replayed run = %s/%q/%+v", replay.Status, replay.FailureCode, replay.RuntimeContext)
	}
	if !strings.Contains(replay.ResultSummary, "runtime_materialization_failed") {
		t.Fatalf("result summary = %q", replay.ResultSummary)
	}
	if fixture.repository.updateRunEvent == nil || fixture.repository.updateRunEvent.EventType != agentevents.EventRunFailed {
		t.Fatalf("event = %+v", fixture.repository.updateRunEvent)
	}
}

func TestGetAgentRunRuntimeStatusWithoutRuntimeJob(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("11111111-aaaa-bbbb-cccc-dddddddddddd")
	updatedAt := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	run := entity.AgentRun{
		VersionedBase: entity.VersionedBase{ID: runID, Version: 3, UpdatedAt: updatedAt},
		SessionID:     uuid.MustParse("11111111-bbbb-cccc-dddd-eeeeeeeeeeee"),
		Status:        enum.AgentRunStatusStarting,
		ResultSummary: "runtime prepare started",
	}
	reader := &fakeRuntimeJobReader{}
	service := New(Config{
		Repository:       &fakeRepository{runByID: map[uuid.UUID]entity.AgentRun{runID: run}},
		RuntimeJobReader: reader,
	})

	result, err := service.GetAgentRunRuntimeStatus(context.Background(), GetAgentRunRuntimeStatusInput{Meta: value.QueryMeta{Actor: testActor()}, RunID: runID})
	if err != nil {
		t.Fatalf("GetAgentRunRuntimeStatus() err = %v", err)
	}
	status := result.RuntimeStatus
	if status.ObservationState != RuntimeObservationStateNotCreated || status.RuntimeJobRef != "" || reader.calls != 0 {
		t.Fatalf("runtime status = %+v, reader calls = %d", status, reader.calls)
	}
	if status.RunVersion != 3 || !status.RunUpdatedAt.Equal(updatedAt) || status.SafeSummary != "runtime prepare started" {
		t.Fatalf("run status fields = %+v", status)
	}
}

func TestGetAgentRunRuntimeStatusWithRuntimeJobRefReadsLiveStatus(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("22222222-aaaa-bbbb-cccc-dddddddddddd")
	sessionID := uuid.MustParse("22222222-bbbb-cccc-dddd-eeeeeeeeeeee")
	stageID := uuid.MustParse("22222222-cccc-dddd-eeee-ffffffffffff")
	createdAt := time.Date(2026, 5, 28, 11, 0, 0, 0, time.UTC)
	startedAt := createdAt.Add(2 * time.Minute)
	run := entity.AgentRun{
		VersionedBase:  entity.VersionedBase{ID: runID, Version: 5, UpdatedAt: startedAt},
		SessionID:      sessionID,
		StageID:        &stageID,
		RuntimeContext: value.RuntimeContextRef{SlotRef: "slot-1", WorkspaceRef: "workspace-1", JobRef: "job-1"},
		Status:         enum.AgentRunStatusStarting,
		ResultSummary:  "runtime job created",
	}
	reader := &fakeRuntimeJobReader{result: RuntimeJobReadResult{
		JobRef:      "job-1",
		AgentRunID:  runID,
		CommandRef:  "command-1",
		Status:      RuntimeJobStatusRunning,
		Version:     7,
		CreatedAt:   &createdAt,
		StartedAt:   &startedAt,
		SafeSummary: "job_status=running",
	}}
	gateID := uuid.MustParse("22222222-dddd-eeee-ffff-111111111111")
	service := New(Config{
		Repository: &fakeRepository{
			runByID: map[uuid.UUID]entity.AgentRun{runID: run},
			humanGateList: []entity.HumanGateRequest{{
				VersionedBase: entity.VersionedBase{ID: gateID},
				SessionID:     sessionID,
				RunID:         &runID,
				StageID:       &stageID,
				Status:        enum.HumanGateStatusWaiting,
				ReasonCode:    "owner_approval",
			}},
		},
		RuntimeJobReader: reader,
	})

	result, err := service.GetAgentRunRuntimeStatus(context.Background(), GetAgentRunRuntimeStatusInput{Meta: value.QueryMeta{Actor: testActor()}, RunID: runID})
	if err != nil {
		t.Fatalf("GetAgentRunRuntimeStatus() err = %v", err)
	}
	status := result.RuntimeStatus
	if status.ObservationState != RuntimeObservationStateLive || status.RuntimeJobStatus != RuntimeJobStatusRunning || status.RuntimeJobCommandRef != "command-1" {
		t.Fatalf("runtime status = %+v", status)
	}
	if status.RuntimeJobVersion != 7 || status.RuntimeJobCreatedAt == nil || !status.RuntimeJobCreatedAt.Equal(createdAt) {
		t.Fatalf("job timestamps/version = %+v", status)
	}
	if !status.HumanGateWaiting || status.HumanGateRequestRef != gateID.String() || status.HumanGateReasonCode != "owner_approval" {
		t.Fatalf("human gate signal = %+v", status)
	}
	if reader.calls != 1 || reader.last.Meta.Actor != testActor() || reader.last.AgentRunID != runID || reader.last.JobRef != "job-1" {
		t.Fatalf("runtime reader input = %+v calls=%d", reader.last, reader.calls)
	}
}

func TestGetAgentRunRuntimeStatusKeepsRuntimeCreateFailureSummary(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("33333333-aaaa-bbbb-cccc-dddddddddddd")
	run := entity.AgentRun{
		VersionedBase: entity.VersionedBase{ID: runID, Version: 4, UpdatedAt: time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)},
		SessionID:     uuid.MustParse("33333333-bbbb-cccc-dddd-eeeeeeeeeeee"),
		Status:        enum.AgentRunStatusFailed,
		ResultSummary: "runtime job permanent: code=failed_precondition; message=agent run job rejected",
		FailureCode:   runtimeJobFailureCode,
	}
	service := New(Config{Repository: &fakeRepository{runByID: map[uuid.UUID]entity.AgentRun{runID: run}}})

	result, err := service.GetAgentRunRuntimeStatus(context.Background(), GetAgentRunRuntimeStatusInput{Meta: value.QueryMeta{Actor: testActor()}, RunID: runID})
	if err != nil {
		t.Fatalf("GetAgentRunRuntimeStatus() err = %v", err)
	}
	status := result.RuntimeStatus
	if status.ObservationState != RuntimeObservationStateNotCreated || status.SafeErrorCode != runtimeJobFailureCode {
		t.Fatalf("runtime status = %+v", status)
	}
	if status.SafeSummary != run.ResultSummary {
		t.Fatalf("safe summary = %q", status.SafeSummary)
	}
}

func TestGetAgentRunRuntimeStatusMapsRuntimeReadFailureSafely(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("44444444-aaaa-bbbb-cccc-dddddddddddd")
	run := entity.AgentRun{
		VersionedBase:  entity.VersionedBase{ID: runID, Version: 2, UpdatedAt: time.Date(2026, 5, 28, 13, 0, 0, 0, time.UTC)},
		SessionID:      uuid.MustParse("44444444-bbbb-cccc-dddd-eeeeeeeeeeee"),
		RuntimeContext: value.RuntimeContextRef{JobRef: "job-1"},
		Status:         enum.AgentRunStatusStarting,
	}
	service := New(Config{
		Repository:       &fakeRepository{runByID: map[uuid.UUID]entity.AgentRun{runID: run}},
		RuntimeJobReader: &fakeRuntimeJobReader{err: errs.ErrDependencyUnavailable},
	})

	result, err := service.GetAgentRunRuntimeStatus(context.Background(), GetAgentRunRuntimeStatusInput{Meta: value.QueryMeta{Actor: testActor()}, RunID: runID})
	if err != nil {
		t.Fatalf("GetAgentRunRuntimeStatus() err = %v", err)
	}
	status := result.RuntimeStatus
	if status.ObservationState != RuntimeObservationStateUnavailable || status.SafeErrorCode != "dependency_unavailable" {
		t.Fatalf("runtime status = %+v", status)
	}
	for _, forbidden := range []string{"kubeconfig", "secret", "workspace/path"} {
		if strings.Contains(status.SafeSummary, forbidden) {
			t.Fatalf("safe summary contains forbidden marker %q: %s", forbidden, status.SafeSummary)
		}
	}
}

func TestGetAgentRunRuntimeStatusMapsMissingRuntimeJobRefToConflict(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("45454545-aaaa-bbbb-cccc-dddddddddddd")
	run := entity.AgentRun{
		VersionedBase:  entity.VersionedBase{ID: runID, Version: 2, UpdatedAt: time.Date(2026, 5, 28, 13, 30, 0, 0, time.UTC)},
		SessionID:      uuid.MustParse("45454545-bbbb-cccc-dddd-eeeeeeeeeeee"),
		RuntimeContext: value.RuntimeContextRef{JobRef: "stale-job-ref"},
		Status:         enum.AgentRunStatusStarting,
	}
	service := New(Config{
		Repository:       &fakeRepository{runByID: map[uuid.UUID]entity.AgentRun{runID: run}},
		RuntimeJobReader: &fakeRuntimeJobReader{err: errs.ErrNotFound},
	})

	result, err := service.GetAgentRunRuntimeStatus(context.Background(), GetAgentRunRuntimeStatusInput{Meta: value.QueryMeta{Actor: testActor()}, RunID: runID})
	if err != nil {
		t.Fatalf("GetAgentRunRuntimeStatus() err = %v", err)
	}
	status := result.RuntimeStatus
	if status.ObservationState != RuntimeObservationStateConflict || status.SafeErrorCode != "not_found" {
		t.Fatalf("runtime status = %+v", status)
	}
	if !strings.Contains(status.SafeSummary, "code=not_found") {
		t.Fatalf("safe summary = %q, want not_found code", status.SafeSummary)
	}
}

func TestGetAgentRunRuntimeStatusReadIsIdempotent(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("55555555-aaaa-bbbb-cccc-dddddddddddd")
	run := entity.AgentRun{
		VersionedBase:  entity.VersionedBase{ID: runID, Version: 6, UpdatedAt: time.Date(2026, 5, 28, 14, 0, 0, 0, time.UTC)},
		SessionID:      uuid.MustParse("55555555-bbbb-cccc-dddd-eeeeeeeeeeee"),
		RuntimeContext: value.RuntimeContextRef{JobRef: "job-1"},
		Status:         enum.AgentRunStatusStarting,
	}
	reader := &fakeRuntimeJobReader{result: RuntimeJobReadResult{JobRef: "job-1", AgentRunID: runID, Status: RuntimeJobStatusPending, Version: 1, SafeSummary: "job_status=pending"}}
	repository := &fakeRepository{runByID: map[uuid.UUID]entity.AgentRun{runID: run}}
	service := New(Config{Repository: repository, RuntimeJobReader: reader})

	first, err := service.GetAgentRunRuntimeStatus(context.Background(), GetAgentRunRuntimeStatusInput{Meta: value.QueryMeta{Actor: testActor()}, RunID: runID})
	if err != nil {
		t.Fatalf("first GetAgentRunRuntimeStatus() err = %v", err)
	}
	second, err := service.GetAgentRunRuntimeStatus(context.Background(), GetAgentRunRuntimeStatusInput{Meta: value.QueryMeta{Actor: testActor()}, RunID: runID})
	if err != nil {
		t.Fatalf("second GetAgentRunRuntimeStatus() err = %v", err)
	}
	if reader.calls != 2 || repository.updatedRun.ID != uuid.Nil {
		t.Fatalf("reader calls=%d updated run=%+v", reader.calls, repository.updatedRun)
	}
	if first.RuntimeStatus != second.RuntimeStatus {
		t.Fatalf("runtime reads differ: %+v != %+v", first.RuntimeStatus, second.RuntimeStatus)
	}
}

func TestStartAgentRunValidatesStageRoleBinding(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("55555555-aaaa-bbbb-cccc-dddddddddddd")
	flowVersionID := uuid.MustParse("55555555-bbbb-cccc-dddd-eeeeeeeeeeee")
	stageID := uuid.MustParse("55555555-cccc-dddd-eeee-ffffffffffff")
	roleID := uuid.MustParse("55555555-dddd-eeee-ffff-111111111111")
	promptVersionID := uuid.MustParse("55555555-eeee-ffff-1111-222222222222")
	runID := uuid.MustParse("55555555-ffff-1111-2222-333333333333")
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {
				VersionedBase:       entity.VersionedBase{ID: sessionID, Version: 1},
				Scope:               value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
				ProviderWorkItemRef: "github:issue:42",
				FlowVersionID:       &flowVersionID,
				CurrentStageID:      &stageID,
				Status:              enum.AgentSessionStatusOpen,
			},
		},
		flowVersionByID: map[uuid.UUID]entity.FlowVersion{
			flowVersionID: {
				ID: flowVersionID,
				Stages: []entity.Stage{{
					ID:            stageID,
					FlowVersionID: flowVersionID,
					Slug:          "dev",
				}},
				RoleBindings: []entity.StageRoleBinding{{
					ID:            uuid.MustParse("66666666-1111-2222-3333-444444444444"),
					StageID:       stageID,
					RoleProfileID: roleID,
					BindingKind:   enum.StageRoleBindingKindExecutor,
				}},
			},
		},
		roleByID: map[uuid.UUID]entity.RoleProfile{
			roleID: {
				VersionedBase:  entity.VersionedBase{ID: roleID, Version: 2},
				RoleKind:       enum.RoleKindWorker,
				RuntimeProfile: "full",
				Status:         enum.RoleStatusActive,
			},
		},
		promptVersionByID: map[uuid.UUID]entity.PromptTemplateVersion{
			promptVersionID: {
				ID:             promptVersionID,
				RoleProfileID:  roleID,
				PromptKind:     enum.PromptKindWork,
				TemplateObject: value.ObjectRef{ObjectURI: "object://instructions/work-v1", ObjectDigest: testInstructionDigest},
				TemplateDigest: "sha256:prompt",
				Status:         enum.PromptVersionStatusActive,
			},
		},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{runID, uuid.MustParse("66666666-2222-3333-4444-555555555555")}},
	})

	run, err := service.StartAgentRun(context.Background(), StartAgentRunInput{
		Meta:                    value.CommandMeta{CommandID: uuid.MustParse("66666666-3333-4444-5555-666666666666"), Actor: testActor()},
		SessionID:               sessionID,
		RoleProfileID:           roleID,
		PromptTemplateVersionID: promptVersionID,
	})
	if err != nil {
		t.Fatalf("StartAgentRun() err = %v", err)
	}
	if run.FlowVersionID == nil || *run.FlowVersionID != flowVersionID || run.StageID == nil || *run.StageID != stageID {
		t.Fatalf("run flow/stage = %v/%v", run.FlowVersionID, run.StageID)
	}
}

func TestStartAgentRunRejectsRoleWithoutStageBinding(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("77777777-aaaa-bbbb-cccc-dddddddddddd")
	flowVersionID := uuid.MustParse("77777777-bbbb-cccc-dddd-eeeeeeeeeeee")
	stageID := uuid.MustParse("77777777-cccc-dddd-eeee-ffffffffffff")
	roleID := uuid.MustParse("77777777-dddd-eeee-ffff-111111111111")
	promptVersionID := uuid.MustParse("77777777-eeee-ffff-1111-222222222222")
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {
				VersionedBase:  entity.VersionedBase{ID: sessionID, Version: 1},
				FlowVersionID:  &flowVersionID,
				CurrentStageID: &stageID,
				Status:         enum.AgentSessionStatusOpen,
			},
		},
		flowVersionByID: map[uuid.UUID]entity.FlowVersion{
			flowVersionID: {
				ID:     flowVersionID,
				Stages: []entity.Stage{{ID: stageID, FlowVersionID: flowVersionID, Slug: "dev"}},
			},
		},
		roleByID: map[uuid.UUID]entity.RoleProfile{
			roleID: {
				VersionedBase:  entity.VersionedBase{ID: roleID, Version: 1},
				RoleKind:       enum.RoleKindWorker,
				RuntimeProfile: "full",
				Status:         enum.RoleStatusActive,
			},
		},
		promptVersionByID: map[uuid.UUID]entity.PromptTemplateVersion{
			promptVersionID: {
				ID:             promptVersionID,
				RoleProfileID:  roleID,
				PromptKind:     enum.PromptKindWork,
				TemplateDigest: "sha256:prompt",
				Status:         enum.PromptVersionStatusActive,
			},
		},
	}
	service := New(Config{Repository: repository})

	_, err := service.StartAgentRun(context.Background(), StartAgentRunInput{
		Meta:                    value.CommandMeta{CommandID: uuid.MustParse("77777777-ffff-1111-2222-333333333333"), Actor: testActor()},
		SessionID:               sessionID,
		RoleProfileID:           roleID,
		PromptTemplateVersionID: promptVersionID,
	})
	if !errors.Is(err, errs.ErrPreconditionFailed) {
		t.Fatalf("StartAgentRun() err = %v, want %v", err, errs.ErrPreconditionFailed)
	}
}

func TestRecordRunStateRequiresExpectedVersionAndPublishesLifecycleEvent(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		runByID: map[uuid.UUID]entity.AgentRun{
			runID: {
				VersionedBase:           entity.VersionedBase{ID: runID, Version: expectedVersion},
				SessionID:               uuid.MustParse("bbbbbbbb-cccc-dddd-eeee-ffffffffffff"),
				RoleProfileID:           uuid.MustParse("cccccccc-dddd-eeee-ffff-111111111111"),
				RoleProfileVersion:      1,
				RoleProfileDigest:       "sha256:role",
				PromptTemplateVersionID: uuid.MustParse("dddddddd-eeee-ffff-1111-222222222222"),
				PromptTemplateDigest:    "sha256:prompt",
				Status:                  enum.AgentRunStatusRequested,
			},
		},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{uuid.MustParse("eeeeeeee-ffff-1111-2222-333333333333")}},
	})

	run, err := service.RecordRunState(context.Background(), RecordRunStateInput{
		Meta:           value.CommandMeta{CommandID: uuid.MustParse("ffffffff-1111-2222-3333-444444444444"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		RunID:          runID,
		Status:         enum.AgentRunStatusStarting,
		RuntimeContext: &value.RuntimeContextRef{SlotRef: "slot-1"},
	})
	if err != nil {
		t.Fatalf("RecordRunState() err = %v", err)
	}
	if run.Version != 2 || repository.updatedRun.Version != 2 {
		t.Fatalf("run version = %d, stored = %d", run.Version, repository.updatedRun.Version)
	}
	if repository.updateRunEvent == nil || repository.updateRunEvent.EventType != agentevents.EventRunStarted {
		t.Fatalf("event = %+v", repository.updateRunEvent)
	}
	payload := decodeAgentPayload(t, *repository.updateRunEvent)
	if payload.RuntimeSlotRef != "slot-1" {
		t.Fatalf("runtime slot ref = %q, want slot-1", payload.RuntimeSlotRef)
	}
}

func TestRecordRunStatePublishesWaitingReason(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("88888888-aaaa-bbbb-cccc-dddddddddddd")
	expectedVersion := int64(2)
	repository := &fakeRepository{
		runByID: map[uuid.UUID]entity.AgentRun{
			runID: {
				VersionedBase:           entity.VersionedBase{ID: runID, Version: expectedVersion},
				SessionID:               uuid.MustParse("88888888-bbbb-cccc-dddd-eeeeeeeeeeee"),
				RoleProfileID:           uuid.MustParse("88888888-cccc-dddd-eeee-ffffffffffff"),
				RoleProfileVersion:      1,
				RoleProfileDigest:       "sha256:role",
				PromptTemplateVersionID: uuid.MustParse("88888888-dddd-eeee-ffff-111111111111"),
				PromptTemplateDigest:    "sha256:prompt",
				RuntimeContext:          value.RuntimeContextRef{SlotRef: "slot-1"},
				Status:                  enum.AgentRunStatusRunning,
			},
		},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{uuid.MustParse("88888888-eeee-ffff-1111-222222222222")}},
	})
	reasonCode := "owner_feedback"

	_, err := service.RecordRunState(context.Background(), RecordRunStateInput{
		Meta:       value.CommandMeta{CommandID: uuid.MustParse("88888888-ffff-1111-2222-333333333333"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		RunID:      runID,
		Status:     enum.AgentRunStatusWaiting,
		ReasonCode: &reasonCode,
	})
	if err != nil {
		t.Fatalf("RecordRunState() err = %v", err)
	}
	if repository.updateRunEvent == nil || repository.updateRunEvent.EventType != agentevents.EventRunWaiting {
		t.Fatalf("event = %+v", repository.updateRunEvent)
	}
	payload := decodeAgentPayload(t, *repository.updateRunEvent)
	if payload.ReasonCode != reasonCode {
		t.Fatalf("reason_code = %q, want %q", payload.ReasonCode, reasonCode)
	}
}

func TestRecordRunStateRejectsStaleExpectedVersion(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("99999999-aaaa-bbbb-cccc-dddddddddddd")
	currentVersion := int64(3)
	expectedVersion := int64(2)
	repository := &fakeRepository{
		runByID: map[uuid.UUID]entity.AgentRun{
			runID: {
				VersionedBase:           entity.VersionedBase{ID: runID, Version: currentVersion},
				SessionID:               uuid.MustParse("99999999-bbbb-cccc-dddd-eeeeeeeeeeee"),
				RoleProfileID:           uuid.MustParse("99999999-cccc-dddd-eeee-ffffffffffff"),
				RoleProfileVersion:      1,
				RoleProfileDigest:       "sha256:role",
				PromptTemplateVersionID: uuid.MustParse("99999999-dddd-eeee-ffff-111111111111"),
				PromptTemplateDigest:    "sha256:prompt",
				Status:                  enum.AgentRunStatusStarting,
			},
		},
	}
	service := New(Config{Repository: repository})

	_, err := service.RecordRunState(context.Background(), RecordRunStateInput{
		Meta:           value.CommandMeta{CommandID: uuid.MustParse("99999999-eeee-ffff-1111-222222222222"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		RunID:          runID,
		Status:         enum.AgentRunStatusRunning,
		RuntimeContext: &value.RuntimeContextRef{SlotRef: "slot-1"},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordRunState() err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestRecordRunStateAllowsRepeatNonTerminalWithoutLifecycleEvent(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("99999999-ffff-1111-2222-333333333333")
	expectedVersion := int64(5)
	repository := &fakeRepository{
		runByID: map[uuid.UUID]entity.AgentRun{
			runID: {
				VersionedBase:           entity.VersionedBase{ID: runID, Version: expectedVersion},
				SessionID:               uuid.New(),
				RoleProfileID:           uuid.New(),
				RoleProfileVersion:      1,
				RoleProfileDigest:       "sha256:role",
				PromptTemplateVersionID: uuid.New(),
				PromptTemplateDigest:    "sha256:prompt",
				RuntimeContext:          value.RuntimeContextRef{SlotRef: "slot-1"},
				Status:                  enum.AgentRunStatusRunning,
			},
		},
	}
	service := New(Config{
		Repository: repository,
		Clock:      fixedClock{now: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)},
	})

	run, err := service.RecordRunState(context.Background(), RecordRunStateInput{
		Meta:           value.CommandMeta{CommandID: uuid.MustParse("99999999-1111-2222-3333-444444444444"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		RunID:          runID,
		Status:         enum.AgentRunStatusRunning,
		RuntimeContext: &value.RuntimeContextRef{SlotRef: "slot-1"},
	})
	if err != nil {
		t.Fatalf("RecordRunState() err = %v", err)
	}
	if run.Version != expectedVersion+1 || repository.updateRunEvent != nil {
		t.Fatalf("run version/event = %d/%+v", run.Version, repository.updateRunEvent)
	}
}

func TestRecordRunStateRejectsBackwardAndTerminalTransitions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		current     enum.AgentRunStatus
		next        enum.AgentRunStatus
		runtimeSlot string
	}{
		{name: "backward", current: enum.AgentRunStatusRunning, next: enum.AgentRunStatusStarting, runtimeSlot: "slot-1"},
		{name: "terminal", current: enum.AgentRunStatusCompleted, next: enum.AgentRunStatusRunning, runtimeSlot: "slot-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			runID := uuid.New()
			expectedVersion := int64(4)
			repository := &fakeRepository{
				runByID: map[uuid.UUID]entity.AgentRun{
					runID: {
						VersionedBase:           entity.VersionedBase{ID: runID, Version: expectedVersion},
						SessionID:               uuid.New(),
						RoleProfileID:           uuid.New(),
						RoleProfileVersion:      1,
						RoleProfileDigest:       "sha256:role",
						PromptTemplateVersionID: uuid.New(),
						PromptTemplateDigest:    "sha256:prompt",
						RuntimeContext:          value.RuntimeContextRef{SlotRef: tc.runtimeSlot},
						Status:                  tc.current,
					},
				},
			}
			service := New(Config{Repository: repository})

			_, err := service.RecordRunState(context.Background(), RecordRunStateInput{
				Meta:           value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
				RunID:          runID,
				Status:         tc.next,
				RuntimeContext: &value.RuntimeContextRef{SlotRef: tc.runtimeSlot},
			})
			if !errors.Is(err, errs.ErrPreconditionFailed) {
				t.Fatalf("RecordRunState() err = %v, want %v", err, errs.ErrPreconditionFailed)
			}
		})
	}
}

func TestRecordRunStateRejectsStartedEventWithoutRuntimeSlot(t *testing.T) {
	t.Parallel()

	runID := uuid.MustParse("aaaaaaaa-1111-2222-3333-444444444444")
	expectedVersion := int64(1)
	repository := &fakeRepository{
		runByID: map[uuid.UUID]entity.AgentRun{
			runID: {
				VersionedBase:           entity.VersionedBase{ID: runID, Version: expectedVersion},
				SessionID:               uuid.New(),
				RoleProfileID:           uuid.New(),
				RoleProfileVersion:      1,
				RoleProfileDigest:       "sha256:role",
				PromptTemplateVersionID: uuid.New(),
				PromptTemplateDigest:    "sha256:prompt",
				Status:                  enum.AgentRunStatusRequested,
			},
		},
	}
	service := New(Config{Repository: repository})

	_, err := service.RecordRunState(context.Background(), RecordRunStateInput{
		Meta:   value.CommandMeta{CommandID: uuid.MustParse("aaaaaaaa-2222-3333-4444-555555555555"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		RunID:  runID,
		Status: enum.AgentRunStatusStarting,
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RecordRunState() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestReportAgentRunStateAcceptsRunnerLifecycleReports(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("abababab-1111-2222-3333-444444444444")
	runID := uuid.MustParse("abababab-2222-3333-4444-555555555555")
	roleID := uuid.MustParse("abababab-3333-4444-5555-666666666666")
	promptVersionID := uuid.MustParse("abababab-4444-5555-6666-777777777777")
	expectedVersion := int64(4)

	cases := []struct {
		name          string
		currentStatus enum.AgentRunStatus
		reportState   RunnerRunState
		wantStatus    enum.AgentRunStatus
		failureCode   *string
		wantFailure   string
		wantEvent     string
	}{
		{name: "queued", currentStatus: enum.AgentRunStatusStarting, reportState: RunnerRunStateQueued, wantStatus: enum.AgentRunStatusStarting},
		{name: "running", currentStatus: enum.AgentRunStatusStarting, reportState: RunnerRunStateRunning, wantStatus: enum.AgentRunStatusRunning, wantEvent: agentevents.EventRunStarted},
		{name: "started", currentStatus: enum.AgentRunStatusStarting, reportState: RunnerRunStateStarted, wantStatus: enum.AgentRunStatusRunning, wantEvent: agentevents.EventRunStarted},
		{name: "completed", currentStatus: enum.AgentRunStatusRunning, reportState: RunnerRunStateCompleted, wantStatus: enum.AgentRunStatusCompleted, wantEvent: agentevents.EventRunCompleted},
		{name: "failed", currentStatus: enum.AgentRunStatusRunning, reportState: RunnerRunStateFailed, wantStatus: enum.AgentRunStatusFailed, failureCode: ptr("runner_failed"), wantFailure: "runner_failed", wantEvent: agentevents.EventRunFailed},
		{name: "cancelled", currentStatus: enum.AgentRunStatusRunning, reportState: RunnerRunStateCancelled, wantStatus: enum.AgentRunStatusCancelled, wantEvent: agentevents.EventRunCancelled},
		{name: "timed out", currentStatus: enum.AgentRunStatusRunning, reportState: RunnerRunStateTimedOut, wantStatus: enum.AgentRunStatusFailed, wantFailure: "runner_timeout", wantEvent: agentevents.EventRunFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repository := &fakeRepository{
				runByID: map[uuid.UUID]entity.AgentRun{
					runID: {
						VersionedBase:           entity.VersionedBase{ID: runID, Version: expectedVersion},
						SessionID:               sessionID,
						RoleProfileID:           roleID,
						RoleProfileVersion:      1,
						RoleProfileDigest:       "sha256:role",
						PromptTemplateVersionID: promptVersionID,
						PromptTemplateDigest:    "sha256:prompt",
						RuntimeContext:          value.RuntimeContextRef{SlotRef: "slot-runner", JobRef: "job-runner"},
						Status:                  tc.currentStatus,
					},
				},
			}
			service := New(Config{
				Repository:  repository,
				Clock:       fixedClock{now: time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)},
				IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{uuid.MustParse("abababab-5555-6666-7777-888888888888")}},
			})
			summary := "runner status accepted"
			digest := "sha256:runner-diagnostic"

			run, err := service.ReportAgentRunState(context.Background(), ReportAgentRunStateInput{
				Meta:             value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
				RunID:            runID,
				SessionID:        sessionID,
				RuntimeSlotRef:   "slot-runner",
				RuntimeJobRef:    "job-runner",
				State:            tc.reportState,
				SafeSummary:      &summary,
				FailureCode:      tc.failureCode,
				DiagnosticDigest: &digest,
			})
			if err != nil {
				t.Fatalf("ReportAgentRunState() err = %v", err)
			}
			if run.Status != tc.wantStatus || repository.updatedRun.Status != tc.wantStatus {
				t.Fatalf("status = %s/%s, want %s", run.Status, repository.updatedRun.Status, tc.wantStatus)
			}
			if repository.updateRunResult.Operation != operationReportAgentRunState || repository.updateRunResult.AggregateType != enum.CommandAggregateTypeRun {
				t.Fatalf("command result = %+v", repository.updateRunResult)
			}
			if !strings.Contains(run.ResultSummary, "diagnostic_digest=sha256:runner-diagnostic") || !strings.Contains(run.ResultSummary, summary) {
				t.Fatalf("result summary = %q", run.ResultSummary)
			}
			if tc.wantFailure != "" && run.FailureCode != tc.wantFailure {
				t.Fatalf("failure code = %q, want %q", run.FailureCode, tc.wantFailure)
			}
			if tc.wantEvent == "" {
				if repository.updateRunEvent != nil {
					t.Fatalf("event = %+v, want nil", repository.updateRunEvent)
				}
				return
			}
			if repository.updateRunEvent == nil || repository.updateRunEvent.EventType != tc.wantEvent {
				t.Fatalf("event = %+v, want %s", repository.updateRunEvent, tc.wantEvent)
			}
			payload := decodeAgentPayload(t, *repository.updateRunEvent)
			if payload.RunID != runID.String() || payload.RuntimeJobRef != "job-runner" || payload.Status != string(tc.wantStatus) {
				t.Fatalf("event payload = %+v", payload)
			}
			if strings.Contains(payload.FailureCode, "stdout") || strings.Contains(payload.ReasonCode, "workspace_path") {
				t.Fatalf("unsafe event payload = %+v", payload)
			}
		})
	}
}

func TestReportAgentRunStateReplaysSameReportAndRejectsConflict(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("bcbcbcbc-1111-2222-3333-444444444444")
	sessionID := uuid.MustParse("bcbcbcbc-2222-3333-4444-555555555555")
	runID := uuid.MustParse("bcbcbcbc-3333-4444-5555-666666666666")
	expectedVersion := int64(6)
	summary := "runner running"
	report := runnerRunStateCommandPayload{
		RunID:          runID.String(),
		SessionID:      sessionID.String(),
		RuntimeSlotRef: "slot-replay",
		RuntimeJobRef:  "job-replay",
		State:          string(RunnerRunStateRunning),
		SafeSummary:    summary,
	}
	run := entity.AgentRun{
		VersionedBase:           entity.VersionedBase{ID: runID, Version: expectedVersion + 1},
		SessionID:               sessionID,
		RoleProfileID:           uuid.New(),
		RoleProfileVersion:      1,
		RoleProfileDigest:       "sha256:role",
		PromptTemplateVersionID: uuid.New(),
		PromptTemplateDigest:    "sha256:prompt",
		RuntimeContext:          value.RuntimeContextRef{SlotRef: report.RuntimeSlotRef, JobRef: report.RuntimeJobRef},
		Status:                  enum.AgentRunStatusRunning,
		ResultSummary:           summary,
	}
	payload, err := marshalCommandPayload(agentRunCommandPayload{Run: run, RunnerReport: &report})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			CommandID:     &commandID,
			Actor:         testActor(),
			Operation:     operationReportAgentRunState,
			AggregateType: enum.CommandAggregateTypeRun,
			AggregateID:   runID,
			ResultPayload: payload,
		},
		runByID: map[uuid.UUID]entity.AgentRun{runID: run},
	}
	service := New(Config{Repository: repository})

	replay, err := service.ReportAgentRunState(context.Background(), ReportAgentRunStateInput{
		Meta:           value.CommandMeta{CommandID: commandID, ExpectedVersion: &expectedVersion, Actor: testActor()},
		RunID:          runID,
		SessionID:      sessionID,
		RuntimeSlotRef: "slot-replay",
		RuntimeJobRef:  "job-replay",
		State:          RunnerRunStateRunning,
		SafeSummary:    &summary,
	})
	if err != nil {
		t.Fatalf("ReportAgentRunState() replay err = %v", err)
	}
	if replay.ID != runID || repository.updatedRun.ID != uuid.Nil {
		t.Fatalf("replay/update = %+v/%+v", replay, repository.updatedRun)
	}

	changedSummary := "different summary"
	_, err = service.ReportAgentRunState(context.Background(), ReportAgentRunStateInput{
		Meta:           value.CommandMeta{CommandID: commandID, ExpectedVersion: &expectedVersion, Actor: testActor()},
		RunID:          runID,
		SessionID:      sessionID,
		RuntimeSlotRef: "slot-replay",
		RuntimeJobRef:  "job-replay",
		State:          RunnerRunStateRunning,
		SafeSummary:    &changedSummary,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("conflicting replay err = %v, want %v", err, errs.ErrConflict)
	}

	otherRunID := uuid.MustParse("bcbcbcbc-4444-5555-6666-777777777777")
	_, err = service.ReportAgentRunState(context.Background(), ReportAgentRunStateInput{
		Meta:           value.CommandMeta{CommandID: commandID, ExpectedVersion: &expectedVersion, Actor: testActor()},
		RunID:          otherRunID,
		SessionID:      sessionID,
		RuntimeSlotRef: "slot-replay",
		RuntimeJobRef:  "job-replay",
		State:          RunnerRunStateRunning,
		SafeSummary:    &summary,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("mismatched run replay err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestReportAgentRunStateRejectsStaleVersionBindingAndTransition(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("cdcdcdcd-1111-2222-3333-444444444444")
	runID := uuid.MustParse("cdcdcdcd-2222-3333-4444-555555555555")
	currentVersion := int64(3)
	baseRun := entity.AgentRun{
		VersionedBase:           entity.VersionedBase{ID: runID, Version: currentVersion},
		SessionID:               sessionID,
		RoleProfileID:           uuid.New(),
		RoleProfileVersion:      1,
		RoleProfileDigest:       "sha256:role",
		PromptTemplateVersionID: uuid.New(),
		PromptTemplateDigest:    "sha256:prompt",
		RuntimeContext:          value.RuntimeContextRef{SlotRef: "slot-bind", JobRef: "job-bind"},
		Status:                  enum.AgentRunStatusStarting,
	}
	report := ReportAgentRunStateInput{
		Meta:           value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &currentVersion, Actor: testActor()},
		RunID:          runID,
		SessionID:      sessionID,
		RuntimeSlotRef: "slot-bind",
		RuntimeJobRef:  "job-bind",
		State:          RunnerRunStateRunning,
	}

	t.Run("stale version", func(t *testing.T) {
		t.Parallel()

		staleVersion := currentVersion - 1
		input := report
		input.Meta.ExpectedVersion = &staleVersion
		service := New(Config{Repository: &fakeRepository{runByID: map[uuid.UUID]entity.AgentRun{runID: baseRun}}})
		_, err := service.ReportAgentRunState(context.Background(), input)
		if !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("ReportAgentRunState() err = %v, want %v", err, errs.ErrConflict)
		}
	})

	cases := []struct {
		name string
		run  entity.AgentRun
		edit func(*ReportAgentRunStateInput)
		want error
	}{
		{name: "session mismatch", run: baseRun, edit: func(input *ReportAgentRunStateInput) { input.SessionID = uuid.New() }, want: errs.ErrConflict},
		{name: "slot mismatch", run: baseRun, edit: func(input *ReportAgentRunStateInput) { input.RuntimeSlotRef = "slot-other" }, want: errs.ErrConflict},
		{name: "job mismatch", run: baseRun, edit: func(input *ReportAgentRunStateInput) { input.RuntimeJobRef = "job-other" }, want: errs.ErrConflict},
		{name: "missing job ref", run: agentRunWithRuntimeContext(baseRun, value.RuntimeContextRef{SlotRef: "slot-bind"}), want: errs.ErrPreconditionFailed},
		{name: "terminal transition", run: agentRunWithStatus(baseRun, enum.AgentRunStatusCompleted), want: errs.ErrPreconditionFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := report
			if tc.edit != nil {
				tc.edit(&input)
			}
			service := New(Config{Repository: &fakeRepository{runByID: map[uuid.UUID]entity.AgentRun{runID: tc.run}}})
			_, err := service.ReportAgentRunState(context.Background(), input)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ReportAgentRunState() err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestReportAgentRunStateRejectsUnsafeRunnerPayload(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("dededede-1111-2222-3333-444444444444")
	runID := uuid.MustParse("dededede-2222-3333-4444-555555555555")
	expectedVersion := int64(2)
	run := entity.AgentRun{
		VersionedBase:           entity.VersionedBase{ID: runID, Version: expectedVersion},
		SessionID:               sessionID,
		RoleProfileID:           uuid.New(),
		RoleProfileVersion:      1,
		RoleProfileDigest:       "sha256:role",
		PromptTemplateVersionID: uuid.New(),
		PromptTemplateDigest:    "sha256:prompt",
		RuntimeContext:          value.RuntimeContextRef{SlotRef: "slot-safe", JobRef: "job-safe"},
		Status:                  enum.AgentRunStatusRunning,
	}
	cases := []struct {
		name  string
		input ReportAgentRunStateInput
	}{
		{name: "unknown state", input: ReportAgentRunStateInput{State: RunnerRunState("paused")}},
		{name: "raw summary", input: ReportAgentRunStateInput{State: RunnerRunStateCompleted, SafeSummary: ptr("prompt_text: do not store")}},
		{name: "dsn summary", input: ReportAgentRunStateInput{State: RunnerRunStateCompleted, SafeSummary: ptr("postgres://user:pass@db/kodex")}},
		{name: "private url summary", input: ReportAgentRunStateInput{State: RunnerRunStateCompleted, SafeSummary: ptr("https://internal.example/path")}},
		{name: "raw digest", input: ReportAgentRunStateInput{State: RunnerRunStateCompleted, DiagnosticDigest: ptr("workspace_path:/tmp/run")}},
		{name: "private url digest", input: ReportAgentRunStateInput{State: RunnerRunStateCompleted, DiagnosticDigest: ptr("https://internal.example/path")}},
		{name: "failure code on non failed", input: ReportAgentRunStateInput{State: RunnerRunStateRunning, FailureCode: ptr("runner_failed")}},
		{name: "failure code on cancelled", input: ReportAgentRunStateInput{State: RunnerRunStateCancelled, FailureCode: ptr("runner_failed")}},
		{name: "missing failure code", input: ReportAgentRunStateInput{State: RunnerRunStateFailed}},
		{name: "unsafe failure code", input: ReportAgentRunStateInput{State: RunnerRunStateFailed, FailureCode: ptr("secret_value")}},
		{name: "unsafe timeout failure code", input: ReportAgentRunStateInput{State: RunnerRunStateTimedOut, FailureCode: ptr("secret_value")}},
		{name: "dsn failure code", input: ReportAgentRunStateInput{State: RunnerRunStateFailed, FailureCode: ptr("postgres://user:pass@db/kodex")}},
		{name: "private url failure code", input: ReportAgentRunStateInput{State: RunnerRunStateFailed, FailureCode: ptr("https://internal.example/path")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			input := tc.input
			input.Meta = value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()}
			input.RunID = runID
			input.SessionID = sessionID
			input.RuntimeSlotRef = "slot-safe"
			input.RuntimeJobRef = "job-safe"
			repository := &fakeRepository{runByID: map[uuid.UUID]entity.AgentRun{runID: run}}
			service := New(Config{Repository: repository})
			_, err := service.ReportAgentRunState(context.Background(), input)
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("ReportAgentRunState() err = %v, want %v", err, errs.ErrInvalidArgument)
			}
			if repository.updatedRun.ID != uuid.Nil {
				t.Fatalf("updated run = %+v", repository.updatedRun)
			}
		})
	}
}

func TestRecordSessionStateSnapshotUpdatesLatestSnapshot(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("11111111-aaaa-bbbb-cccc-222222222222")
	expectedVersion := int64(2)
	snapshotID := uuid.MustParse("22222222-bbbb-cccc-dddd-333333333333")
	eventID := uuid.MustParse("33333333-cccc-dddd-eeee-444444444444")
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {
				VersionedBase:     entity.VersionedBase{ID: sessionID, Version: expectedVersion},
				Scope:             value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
				Status:            enum.AgentSessionStatusOpen,
				CreatedByActorRef: "user:owner",
			},
		},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 15, 10, 0, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{snapshotID, eventID}},
	})

	output, err := service.RecordSessionStateSnapshot(context.Background(), RecordSessionStateSnapshotInput{
		Meta:         value.CommandMeta{CommandID: uuid.MustParse("44444444-dddd-eeee-ffff-555555555555"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		SessionID:    sessionID,
		SnapshotKind: enum.AgentSessionSnapshotKindTurnCheckpoint,
		Object:       value.ObjectRef{ObjectURI: "s3://bucket/session.jsonl", ObjectDigest: "sha256:state"},
		CapturedAt:   time.Date(2026, 5, 15, 9, 59, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordSessionStateSnapshot() err = %v", err)
	}
	if output.Snapshot.ID != snapshotID || output.Session.LatestStateSnapshotID == nil || *output.Session.LatestStateSnapshotID != snapshotID {
		t.Fatalf("output = %+v", output)
	}
	if repository.snapshotSession.Version != expectedVersion+1 || repository.snapshotEvent.EventType != agentevents.EventSessionSnapshotRecorded {
		t.Fatalf("session/event = %+v/%s", repository.snapshotSession, repository.snapshotEvent.EventType)
	}
}

func TestRequestAcceptanceCreatesPendingResultAndEvent(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("20202020-1111-2222-3333-444444444444")
	runID := uuid.MustParse("20202020-2222-3333-4444-555555555555")
	flowVersionID := uuid.MustParse("20202020-3333-4444-5555-666666666666")
	stageID := uuid.MustParse("20202020-4444-5555-6666-777777777777")
	acceptanceID := uuid.MustParse("20202020-5555-6666-7777-888888888888")
	eventID := uuid.MustParse("20202020-6666-7777-8888-999999999999")
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {
				VersionedBase: entity.VersionedBase{ID: sessionID, Version: 2},
				Scope:         value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project-1"},
				FlowVersionID: &flowVersionID,
				Status:        enum.AgentSessionStatusOpen,
			},
		},
		runByID: map[uuid.UUID]entity.AgentRun{
			runID: {
				VersionedBase: entity.VersionedBase{ID: runID, Version: 1},
				SessionID:     sessionID,
				FlowVersionID: &flowVersionID,
				StageID:       &stageID,
				Status:        enum.AgentRunStatusCompleted,
			},
		},
		flowVersionByID: map[uuid.UUID]entity.FlowVersion{
			flowVersionID: {
				ID: flowVersionID,
				Stages: []entity.Stage{{
					ID:            stageID,
					FlowVersionID: flowVersionID,
					Slug:          "review",
					StageType:     enum.StageTypeReview,
				}},
			},
		},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 26, 13, 0, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{acceptanceID, eventID}},
	})

	acceptance, err := service.RequestAcceptance(context.Background(), RequestAcceptanceInput{
		Meta:       value.CommandMeta{CommandID: uuid.MustParse("20202020-7777-8888-9999-aaaaaaaaaaaa"), Actor: testActor()},
		SessionID:  sessionID,
		RunID:      &runID,
		CheckKinds: []enum.AcceptanceCheckKind{enum.AcceptanceCheckKindRoleResult},
		TargetRef:  " artifact:run-summary ",
	})
	if err != nil {
		t.Fatalf("RequestAcceptance() err = %v", err)
	}
	if acceptance.ID != acceptanceID || acceptance.Status != enum.AcceptanceStatusPending || acceptance.RunID == nil || *acceptance.RunID != runID {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	if acceptance.TargetRef != "artifact:run-summary" {
		t.Fatalf("target ref = %q", acceptance.TargetRef)
	}
	if repository.acceptanceResult.AggregateType != enum.CommandAggregateTypeAcceptance || repository.acceptanceEvent.EventType != agentevents.EventAcceptanceRequested {
		t.Fatalf("result/event = %s/%s", repository.acceptanceResult.AggregateType, repository.acceptanceEvent.EventType)
	}
	payload := decodeAgentPayload(t, repository.acceptanceEvent)
	if payload.SessionID != sessionID.String() || payload.AcceptanceResultID != acceptanceID.String() || payload.Status != string(enum.AcceptanceStatusPending) || payload.Version != 1 {
		t.Fatalf("event payload = %+v", payload)
	}
}

func TestRequestAcceptanceReplaysCommandResult(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("30303030-1111-2222-3333-444444444444")
	sessionID := uuid.MustParse("30303030-2222-3333-4444-555555555555")
	acceptance := entity.AcceptanceResult{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse("30303030-3333-4444-5555-666666666666"), Version: 1},
		SessionID:     sessionID,
		CheckKind:     enum.AcceptanceCheckKindArtifact,
		Status:        enum.AcceptanceStatusPending,
		DetailsJSON:   []byte("{}"),
	}
	payload, err := marshalCommandPayload(acceptanceCommandPayload{AcceptanceResult: acceptance})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			CommandID:     &commandID,
			Actor:         testActor(),
			Operation:     operationRequestAcceptance,
			AggregateType: enum.CommandAggregateTypeAcceptance,
			AggregateID:   acceptance.ID,
			ResultPayload: payload,
		},
		sessionByID:    map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
		acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{acceptance.ID: acceptance},
	}
	service := New(Config{Repository: repository})

	replay, err := service.RequestAcceptance(context.Background(), RequestAcceptanceInput{
		Meta:       value.CommandMeta{CommandID: commandID, Actor: testActor()},
		SessionID:  sessionID,
		CheckKinds: []enum.AcceptanceCheckKind{enum.AcceptanceCheckKindArtifact},
	})
	if err != nil {
		t.Fatalf("RequestAcceptance() err = %v", err)
	}
	if replay.ID != acceptance.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, acceptance.ID)
	}
	if repository.createAcceptanceCalled {
		t.Fatal("CreateAcceptanceResultWithResult called during replay")
	}
}

func TestRequestAcceptanceRejectsConflictingGovernanceReplay(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("30303030-5555-6666-7777-888888888888")
	sessionID := uuid.MustParse("30303030-6666-7777-8888-999999999999")
	acceptance := entity.AcceptanceResult{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse("30303030-7777-8888-9999-aaaaaaaaaaaa"), Version: 1},
		SessionID:     sessionID,
		CheckKind:     enum.AcceptanceCheckKindPolicy,
		Status:        enum.AcceptanceStatusPending,
		TargetRef:     "artifact:policy-summary",
		DetailsJSON:   []byte("{}"),
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef: "governance:gate/request-1",
		},
	}
	payload, err := marshalCommandPayload(acceptanceCommandPayload{AcceptanceResult: acceptance})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			CommandID:     &commandID,
			Actor:         testActor(),
			Operation:     operationRequestAcceptance,
			AggregateType: enum.CommandAggregateTypeAcceptance,
			AggregateID:   acceptance.ID,
			ResultPayload: payload,
		},
		sessionByID:    map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
		acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{acceptance.ID: acceptance},
	}
	service := New(Config{Repository: repository})

	_, err = service.RequestAcceptance(context.Background(), RequestAcceptanceInput{
		Meta:       value.CommandMeta{CommandID: commandID, Actor: testActor()},
		SessionID:  sessionID,
		CheckKinds: []enum.AcceptanceCheckKind{enum.AcceptanceCheckKindPolicy},
		TargetRef:  "artifact:policy-summary",
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef: "governance:gate/request-2",
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RequestAcceptance() err = %v, want %v", err, errs.ErrConflict)
	}
	if repository.createAcceptanceCalled {
		t.Fatal("CreateAcceptanceResultWithResult called during conflicting replay")
	}
}

func TestRequestAcceptanceRejectsUnsafeTargetRef(t *testing.T) {
	t.Parallel()

	service := New(Config{Repository: &fakeRepository{}})

	cases := map[string]string{
		"raw marker":        "raw_provider_payload:body",
		"json-like value":   "artifact:{\"body\":\"not safe\"}",
		"too long":          strings.Repeat("a", acceptanceTargetRefLimit+1) + ":ref",
		"missing namespace": "artifact without namespace",
		"empty namespace":   ":artifact",
		"empty value":       "artifact:",
	}
	for name, targetRef := range cases {
		targetRef := targetRef
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := service.RequestAcceptance(context.Background(), RequestAcceptanceInput{
				Meta:       value.CommandMeta{CommandID: uuid.New(), Actor: testActor()},
				SessionID:  uuid.New(),
				CheckKinds: []enum.AcceptanceCheckKind{enum.AcceptanceCheckKindArtifact},
				TargetRef:  targetRef,
			})
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("RequestAcceptance() err = %v, want %v", err, errs.ErrInvalidArgument)
			}
		})
	}
}

func TestRecordAcceptanceResultStoresSafeDetailsAndCompletedEvent(t *testing.T) {
	t.Parallel()

	acceptanceID := uuid.MustParse("40404040-1111-2222-3333-444444444444")
	sessionID := uuid.MustParse("40404040-2222-3333-4444-555555555555")
	runID := uuid.MustParse("40404040-3333-4444-5555-666666666666")
	expectedVersion := int64(2)
	eventID := uuid.MustParse("40404040-4444-5555-6666-777777777777")
	repository := &fakeRepository{
		acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{
			acceptanceID: {
				VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: expectedVersion},
				SessionID:     sessionID,
				RunID:         &runID,
				CheckKind:     enum.AcceptanceCheckKindArtifact,
				Status:        enum.AcceptanceStatusPending,
				DetailsJSON:   []byte("{}"),
			},
		},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 26, 13, 5, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{eventID}},
	})

	acceptance, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
		Meta:               value.CommandMeta{CommandID: uuid.MustParse("40404040-5555-6666-7777-888888888888"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		AcceptanceResultID: acceptanceID,
		Status:             enum.AcceptanceStatusPassed,
		TargetRef:          "artifact:acceptance-summary",
		DetailsJSON:        []byte(`{"summary":"ok","digest":"sha256:result","artifact_refs":["artifact:1"],"risk_ref":"risk:1","gate_ref":"gate:1"}`),
		GovernanceContext: value.GovernanceContextRef{
			RiskAssessmentRef:         "governance:risk/1",
			GateRequestRef:            "governance:gate/1",
			ReleaseDecisionPackageRef: "governance:release-package/1",
			RiskProfileRef:            "governance:risk-profile/default",
			GatePolicyRef:             "governance:gate-policy/default",
			ReleasePolicyRef:          "project-policy:release/default",
		},
	})
	if err != nil {
		t.Fatalf("RecordAcceptanceResult() err = %v", err)
	}
	if acceptance.Version != expectedVersion+1 || acceptance.Status != enum.AcceptanceStatusPassed || acceptance.TargetRef != "artifact:acceptance-summary" {
		t.Fatalf("acceptance = %+v", acceptance)
	}
	if strings.Contains(string(acceptance.DetailsJSON), "\n") || !strings.Contains(string(acceptance.DetailsJSON), `"artifact_refs"`) {
		t.Fatalf("details_json = %s", acceptance.DetailsJSON)
	}
	if acceptance.GovernanceContext.RiskAssessmentRef != "governance:risk/1" || acceptance.GovernanceContext.GateRequestRef != "governance:gate/1" {
		t.Fatalf("governance context = %+v", acceptance.GovernanceContext)
	}
	if repository.updateAcceptanceResult.AggregateType != enum.CommandAggregateTypeAcceptance || repository.updateAcceptanceEvent == nil || repository.updateAcceptanceEvent.EventType != agentevents.EventAcceptanceCompleted {
		t.Fatalf("result/event = %s/%+v", repository.updateAcceptanceResult.AggregateType, repository.updateAcceptanceEvent)
	}
	payload := decodeAgentPayload(t, *repository.updateAcceptanceEvent)
	if payload.AcceptanceResultID != acceptanceID.String() || payload.Status != string(enum.AcceptanceStatusPassed) || payload.Version != expectedVersion+1 {
		t.Fatalf("event payload = %+v", payload)
	}
	if payload.GovernanceRiskAssessmentRef != "governance:risk/1" || payload.GovernanceGateRequestRef != "governance:gate/1" || payload.GovernanceReleaseDecisionPackageRef != "governance:release-package/1" {
		t.Fatalf("event governance refs = %+v", payload)
	}
}

func TestRecordAcceptanceResultReplaysCommandResult(t *testing.T) {
	t.Parallel()

	commandID := uuid.MustParse("50505050-1111-2222-3333-444444444444")
	expectedVersion := int64(3)
	acceptance := entity.AcceptanceResult{
		VersionedBase: entity.VersionedBase{ID: uuid.MustParse("50505050-2222-3333-4444-555555555555"), Version: expectedVersion + 1},
		SessionID:     uuid.MustParse("50505050-3333-4444-5555-666666666666"),
		CheckKind:     enum.AcceptanceCheckKindPolicy,
		Status:        enum.AcceptanceStatusPassed,
		DetailsJSON:   []byte(`{"summary":"ok"}`),
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef: "governance:gate/replay",
		},
	}
	payload, err := marshalCommandPayload(acceptanceCommandPayload{AcceptanceResult: acceptance})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			CommandID:     &commandID,
			Actor:         testActor(),
			Operation:     operationRecordAcceptanceResult,
			AggregateType: enum.CommandAggregateTypeAcceptance,
			AggregateID:   acceptance.ID,
			ResultPayload: payload,
		},
		acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{acceptance.ID: acceptance},
	}
	service := New(Config{Repository: repository})

	replay, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
		Meta:               value.CommandMeta{CommandID: commandID, ExpectedVersion: &expectedVersion, Actor: testActor()},
		AcceptanceResultID: acceptance.ID,
		Status:             enum.AcceptanceStatusPassed,
		DetailsJSON:        []byte(`{"summary":"ok"}`),
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef: "governance:gate/replay",
		},
	})
	if err != nil {
		t.Fatalf("RecordAcceptanceResult() err = %v", err)
	}
	if replay.ID != acceptance.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, acceptance.ID)
	}
	if repository.updatedAcceptance.ID != uuid.Nil {
		t.Fatal("UpdateAcceptanceResultWithResult called during replay")
	}
}

func TestRecordAcceptanceResultRejectsConflictNotFoundAndUnsafePayload(t *testing.T) {
	t.Parallel()

	t.Run("conflict", func(t *testing.T) {
		t.Parallel()

		acceptanceID := uuid.MustParse("60606060-1111-2222-3333-444444444444")
		expectedVersion := int64(5)
		repository := &fakeRepository{
			acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{
				acceptanceID: {
					VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: expectedVersion + 1},
					SessionID:     uuid.New(),
					CheckKind:     enum.AcceptanceCheckKindArtifact,
					Status:        enum.AcceptanceStatusPending,
					DetailsJSON:   []byte("{}"),
				},
			},
		}
		service := New(Config{Repository: repository})

		_, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
			Meta:               value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
			AcceptanceResultID: acceptanceID,
			Status:             enum.AcceptanceStatusPassed,
			DetailsJSON:        []byte(`{"summary":"ok"}`),
		})
		if !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("RecordAcceptanceResult() err = %v, want %v", err, errs.ErrConflict)
		}
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		expectedVersion := int64(1)
		service := New(Config{Repository: &fakeRepository{acceptanceGetErr: errs.ErrNotFound}})
		_, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
			Meta:               value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
			AcceptanceResultID: uuid.New(),
			Status:             enum.AcceptanceStatusPassed,
			DetailsJSON:        []byte(`{"summary":"ok"}`),
		})
		if !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("RecordAcceptanceResult() err = %v, want %v", err, errs.ErrNotFound)
		}
	})

	t.Run("unsafe payload", func(t *testing.T) {
		t.Parallel()

		acceptanceID := uuid.MustParse("60606060-2222-3333-4444-555555555555")
		expectedVersion := int64(1)
		repository := &fakeRepository{
			acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{
				acceptanceID: {
					VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: expectedVersion},
					SessionID:     uuid.New(),
					CheckKind:     enum.AcceptanceCheckKindArtifact,
					Status:        enum.AcceptanceStatusPending,
					DetailsJSON:   []byte("{}"),
				},
			},
		}
		service := New(Config{Repository: repository})

		_, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
			Meta:               value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
			AcceptanceResultID: acceptanceID,
			Status:             enum.AcceptanceStatusFailed,
			DetailsJSON:        []byte(`{"raw_provider_payload":{"body":"not safe"}}`),
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("RecordAcceptanceResult() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
		if repository.updatedAcceptance.ID != uuid.Nil {
			t.Fatal("unsafe payload was persisted")
		}
	})

	t.Run("unsafe target ref", func(t *testing.T) {
		t.Parallel()

		expectedVersion := int64(1)
		service := New(Config{Repository: &fakeRepository{}})

		_, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
			Meta:               value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
			AcceptanceResultID: uuid.New(),
			Status:             enum.AcceptanceStatusFailed,
			TargetRef:          "logs:raw-provider-stdout",
			DetailsJSON:        []byte(`{"summary":"not persisted"}`),
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("RecordAcceptanceResult() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
	})

	t.Run("conflicting governance replay", func(t *testing.T) {
		t.Parallel()

		commandID := uuid.MustParse("60606060-3333-4444-5555-666666666666")
		expectedVersion := int64(1)
		acceptanceID := uuid.MustParse("60606060-4444-5555-6666-777777777777")
		acceptance := entity.AcceptanceResult{
			VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: expectedVersion + 1},
			SessionID:     uuid.New(),
			CheckKind:     enum.AcceptanceCheckKindPolicy,
			Status:        enum.AcceptanceStatusPassed,
			DetailsJSON:   []byte(`{"summary":"ok"}`),
			GovernanceContext: value.GovernanceContextRef{
				GateRequestRef: "governance:gate/replay",
			},
		}
		payload, err := marshalCommandPayload(acceptanceCommandPayload{AcceptanceResult: acceptance})
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		service := New(Config{Repository: &fakeRepository{
			replay: &entity.CommandResult{
				CommandID:     &commandID,
				Actor:         testActor(),
				Operation:     operationRecordAcceptanceResult,
				AggregateType: enum.CommandAggregateTypeAcceptance,
				AggregateID:   acceptanceID,
				ResultPayload: payload,
			},
			acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{acceptanceID: acceptance},
		}})

		_, err = service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
			Meta:               value.CommandMeta{CommandID: commandID, ExpectedVersion: &expectedVersion, Actor: testActor()},
			AcceptanceResultID: acceptanceID,
			Status:             enum.AcceptanceStatusPassed,
			DetailsJSON:        []byte(`{"summary":"ok"}`),
			GovernanceContext: value.GovernanceContextRef{
				GateRequestRef: "governance:gate/other",
			},
		})
		if !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("RecordAcceptanceResult() err = %v, want %v", err, errs.ErrConflict)
		}
	})

	t.Run("missing governance binding ref", func(t *testing.T) {
		t.Parallel()

		expectedVersion := int64(1)
		service := New(Config{Repository: &fakeRepository{}})

		_, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
			Meta:               value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
			AcceptanceResultID: uuid.New(),
			Status:             enum.AcceptanceStatusPassed,
			DetailsJSON:        []byte(`{"summary":"ok"}`),
			GovernanceContext: value.GovernanceContextRef{
				GateDecisionRef: "governance:decision/1",
			},
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("RecordAcceptanceResult() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
	})

	t.Run("unsafe governance ref", func(t *testing.T) {
		t.Parallel()

		expectedVersion := int64(1)
		service := New(Config{Repository: &fakeRepository{}})

		_, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
			Meta:               value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
			AcceptanceResultID: uuid.New(),
			Status:             enum.AcceptanceStatusPassed,
			DetailsJSON:        []byte(`{"summary":"ok"}`),
			GovernanceContext: value.GovernanceContextRef{
				RiskAssessmentRef: "governance:raw_provider_payload/1",
			},
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("RecordAcceptanceResult() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
	})
}

func TestRecordAcceptanceResultHumanGateOnlyWaitsForOwnerDecision(t *testing.T) {
	t.Parallel()

	for _, status := range []enum.AcceptanceStatus{
		enum.AcceptanceStatusPassed,
		enum.AcceptanceStatusFailed,
		enum.AcceptanceStatusSkipped,
	} {
		status := status
		t.Run("reject "+string(status), func(t *testing.T) {
			t.Parallel()

			acceptanceID := uuid.New()
			expectedVersion := int64(1)
			repository := &fakeRepository{
				acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{
					acceptanceID: {
						VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: expectedVersion},
						SessionID:     uuid.New(),
						CheckKind:     enum.AcceptanceCheckKindHumanGate,
						Status:        enum.AcceptanceStatusPending,
						TargetRef:     "gate:request-1",
						DetailsJSON:   []byte("{}"),
					},
				},
			}
			service := New(Config{Repository: repository})

			_, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
				Meta:               value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
				AcceptanceResultID: acceptanceID,
				Status:             status,
				TargetRef:          "gate:decision-1",
				DetailsJSON:        []byte(`{"gate_ref":"gate:request-1"}`),
			})
			if !errors.Is(err, errs.ErrPreconditionFailed) {
				t.Fatalf("RecordAcceptanceResult() err = %v, want %v", err, errs.ErrPreconditionFailed)
			}
			if repository.updatedAcceptance.ID != uuid.Nil {
				t.Fatal("human gate final status was persisted")
			}
		})
	}

	t.Run("waiting with owner ref", func(t *testing.T) {
		t.Parallel()

		acceptanceID := uuid.MustParse("70707070-1111-2222-3333-444444444444")
		expectedVersion := int64(1)
		repository := &fakeRepository{
			acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{
				acceptanceID: {
					VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: expectedVersion},
					SessionID:     uuid.New(),
					CheckKind:     enum.AcceptanceCheckKindHumanGate,
					Status:        enum.AcceptanceStatusPending,
					DetailsJSON:   []byte("{}"),
				},
			},
		}
		service := New(Config{Repository: repository})

		acceptance, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
			Meta:               value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
			AcceptanceResultID: acceptanceID,
			Status:             enum.AcceptanceStatusWaiting,
			TargetRef:          " gate:request-1 ",
			DetailsJSON:        []byte(`{"gate_ref":"gate:request-1","risk_ref":"risk:low"}`),
		})
		if err != nil {
			t.Fatalf("RecordAcceptanceResult() err = %v", err)
		}
		if acceptance.Status != enum.AcceptanceStatusWaiting || acceptance.TargetRef != "gate:request-1" || acceptance.Version != expectedVersion+1 {
			t.Fatalf("acceptance = %+v", acceptance)
		}
		if repository.updateAcceptanceEvent != nil {
			t.Fatalf("waiting status emitted event: %+v", repository.updateAcceptanceEvent)
		}
	})

	t.Run("waiting requires gate or risk ref", func(t *testing.T) {
		t.Parallel()

		acceptanceID := uuid.New()
		expectedVersion := int64(1)
		repository := &fakeRepository{
			acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{
				acceptanceID: {
					VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: expectedVersion},
					SessionID:     uuid.New(),
					CheckKind:     enum.AcceptanceCheckKindHumanGate,
					Status:        enum.AcceptanceStatusPending,
					DetailsJSON:   []byte("{}"),
				},
			},
		}
		service := New(Config{Repository: repository})

		_, err := service.RecordAcceptanceResult(context.Background(), RecordAcceptanceResultInput{
			Meta:               value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()},
			AcceptanceResultID: acceptanceID,
			Status:             enum.AcceptanceStatusWaiting,
			TargetRef:          "artifact:not-a-gate",
			DetailsJSON:        []byte(`{"summary":"waiting"}`),
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("RecordAcceptanceResult() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
		if repository.updatedAcceptance.ID != uuid.Nil {
			t.Fatal("human gate waiting without owner ref was persisted")
		}
	})
}

func TestCreateFollowUpIntentStoresSafeIntentAndOutbox(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("81818181-1111-2222-3333-444444444444")
	runID := uuid.MustParse("81818181-2222-3333-4444-555555555555")
	fromStageID := uuid.MustParse("81818181-3333-4444-5555-666666666666")
	toStageID := uuid.MustParse("81818181-4444-5555-6666-777777777777")
	flowVersionID := uuid.MustParse("81818181-5555-6666-7777-888888888888")
	acceptanceID := uuid.MustParse("81818181-6666-7777-8888-999999999999")
	intentID := uuid.MustParse("81818181-7777-8888-9999-aaaaaaaaaaaa")
	eventID := uuid.MustParse("81818181-8888-9999-aaaa-bbbbbbbbbbbb")
	digest := "sha256:" + strings.Repeat("a", 64)
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {
				VersionedBase:       entity.VersionedBase{ID: sessionID, Version: 2},
				ProviderWorkItemRef: "issue:123",
				FlowVersionID:       &flowVersionID,
				Status:              enum.AgentSessionStatusOpen,
			},
		},
		runByID: map[uuid.UUID]entity.AgentRun{
			runID: {
				VersionedBase: entity.VersionedBase{ID: runID, Version: 4},
				SessionID:     sessionID,
				FlowVersionID: &flowVersionID,
				StageID:       &fromStageID,
				ProviderTarget: value.ProviderTargetRef{
					PullRequestRef: "pr:456",
				},
				Status: enum.AgentRunStatusCompleted,
			},
		},
		acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{
			acceptanceID: {
				VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: 3},
				SessionID:     sessionID,
				RunID:         &runID,
				StageID:       &fromStageID,
				CheckKind:     enum.AcceptanceCheckKindFollowUp,
				Status:        enum.AcceptanceStatusPassed,
				DetailsJSON:   []byte(`{"summary":"ok"}`),
			},
		},
		flowVersionByID: map[uuid.UUID]entity.FlowVersion{
			flowVersionID: {
				ID: flowVersionID,
				Stages: []entity.Stage{
					{ID: fromStageID, FlowVersionID: flowVersionID, Slug: "review", StageType: enum.StageTypeReview},
					{ID: toStageID, FlowVersionID: flowVersionID, Slug: "follow-up", StageType: enum.StageTypeWork},
				},
				Transitions: []entity.StageTransition{{
					ID:            uuid.MustParse("81818181-9999-aaaa-bbbb-cccccccccccc"),
					FlowVersionID: flowVersionID,
					FromStageID:   &fromStageID,
					ToStageID:     toStageID,
					FollowUpType:  "task",
				}},
			},
		},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 26, 18, 0, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{intentID, eventID}},
	})

	intent, err := service.CreateFollowUpIntent(context.Background(), CreateFollowUpIntentInput{
		Meta:                  value.CommandMeta{IdempotencyKey: "follow-up-1", Actor: testActor()},
		SessionID:             sessionID,
		RunID:                 &runID,
		ToStageID:             &toStageID,
		AcceptanceResultID:    &acceptanceID,
		ProviderTarget:        value.ProviderTargetRef{CommentRef: "comment:789"},
		ProviderWorkItemType:  "task",
		ProviderOperationRef:  "operation:planned",
		InstructionBodyDigest: digest,
		SafeTitle:             "Prepare follow-up task",
		SafeSummary:           "Create the next bounded provider-native task.",
		RoleHint:              "worker",
		StageHint:             "follow-up",
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef:            "governance:gate/follow-up",
			ReleaseDecisionPackageRef: "governance:release-package/follow-up",
		},
	})
	if err != nil {
		t.Fatalf("CreateFollowUpIntent() err = %v", err)
	}
	if intent.ID != intentID || intent.Status != enum.FollowUpIntentStatusRequested || intent.Version != 1 {
		t.Fatalf("intent = %+v", intent)
	}
	if intent.FromStageID == nil || *intent.FromStageID != fromStageID || intent.ToStageID == nil || *intent.ToStageID != toStageID {
		t.Fatalf("stage refs = from:%v to:%v", intent.FromStageID, intent.ToStageID)
	}
	if intent.ProviderTarget.WorkItemRef != "issue:123" || intent.ProviderTarget.PullRequestRef != "pr:456" || intent.ProviderTarget.CommentRef != "comment:789" {
		t.Fatalf("provider target = %+v", intent.ProviderTarget)
	}
	if intent.IdempotencyKey != operationCreateFollowUpIntent+":user:owner:follow-up-1" {
		t.Fatalf("idempotency key = %q", intent.IdempotencyKey)
	}
	if intent.GovernanceContext.GateRequestRef != "governance:gate/follow-up" || intent.GovernanceContext.ReleaseDecisionPackageRef != "governance:release-package/follow-up" {
		t.Fatalf("governance context = %+v", intent.GovernanceContext)
	}
	if repository.followUpResult.AggregateType != enum.CommandAggregateTypeFollowUp || repository.followUpEvent.EventType != agentevents.EventFollowUpRequested {
		t.Fatalf("result/event = %s/%s", repository.followUpResult.AggregateType, repository.followUpEvent.EventType)
	}
	payload := decodeAgentPayload(t, repository.followUpEvent)
	if payload.FollowUpIntentID != intentID.String() || payload.SessionID != sessionID.String() || payload.RunID != runID.String() || payload.AcceptanceResultID != acceptanceID.String() {
		t.Fatalf("event payload = %+v", payload)
	}
	if payload.ProviderWorkItemRef != "issue:123" || payload.ProviderPullRequestRef != "pr:456" || payload.ProviderCommentRef != "comment:789" || payload.Summary != intent.SafeSummary {
		t.Fatalf("event payload target = %+v", payload)
	}
	if payload.GovernanceGateRequestRef != "governance:gate/follow-up" || payload.GovernanceReleaseDecisionPackageRef != "governance:release-package/follow-up" {
		t.Fatalf("event governance refs = %+v", payload)
	}
}

func TestCreateFollowUpIntentReplaysAndRejectsConflictingPayload(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("82828282-1111-2222-3333-444444444444")
	runID := uuid.MustParse("82828282-2222-3333-4444-555555555555")
	intent := entity.FollowUpIntent{
		VersionedBase:        entity.VersionedBase{ID: uuid.MustParse("82828282-3333-4444-5555-666666666666"), Version: 1},
		SessionID:            sessionID,
		RunID:                &runID,
		ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "issue:123"},
		ProviderWorkItemType: "task",
		SafeTitle:            "Same title",
		IdempotencyKey:       operationCreateFollowUpIntent + ":user:owner:follow-up-replay",
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef: "governance:gate/follow-up-replay",
		},
		Status: enum.FollowUpIntentStatusRequested,
	}
	payload, err := marshalCommandPayload(followUpIntentCommandPayload{FollowUpIntent: intent})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			IdempotencyKey: "follow-up-replay",
			Actor:          testActor(),
			Operation:      operationCreateFollowUpIntent,
			AggregateType:  enum.CommandAggregateTypeFollowUp,
			AggregateID:    intent.ID,
			ResultPayload:  payload,
		},
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen},
		},
		runByID: map[uuid.UUID]entity.AgentRun{
			runID: {VersionedBase: entity.VersionedBase{ID: runID, Version: 1}, SessionID: sessionID, Status: enum.AgentRunStatusCompleted},
		},
		followUpByID: map[uuid.UUID]entity.FollowUpIntent{intent.ID: intent},
	}
	service := New(Config{Repository: repository})

	replay, err := service.CreateFollowUpIntent(context.Background(), CreateFollowUpIntentInput{
		Meta:                 value.CommandMeta{IdempotencyKey: "follow-up-replay", Actor: testActor()},
		SessionID:            sessionID,
		RunID:                &runID,
		ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "issue:123"},
		ProviderWorkItemType: "task",
		SafeTitle:            "Same title",
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef: "governance:gate/follow-up-replay",
		},
	})
	if err != nil {
		t.Fatalf("CreateFollowUpIntent() err = %v", err)
	}
	if replay.ID != intent.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, intent.ID)
	}
	if repository.createFollowUpCalled {
		t.Fatal("CreateFollowUpIntentWithResult called during replay")
	}

	_, err = service.CreateFollowUpIntent(context.Background(), CreateFollowUpIntentInput{
		Meta:                 value.CommandMeta{IdempotencyKey: "follow-up-replay", Actor: testActor()},
		SessionID:            sessionID,
		RunID:                &runID,
		ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "issue:123"},
		ProviderWorkItemType: "task",
		SafeTitle:            "Same title",
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef: "governance:gate/follow-up-other",
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateFollowUpIntent() err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestCreateFollowUpIntentRejectsInvalidStateAndUnsafePayload(t *testing.T) {
	t.Parallel()

	t.Run("failed acceptance cannot create follow-up", func(t *testing.T) {
		t.Parallel()

		sessionID := uuid.MustParse("83838383-1111-2222-3333-444444444444")
		acceptanceID := uuid.MustParse("83838383-2222-3333-4444-555555555555")
		repository := &fakeRepository{
			sessionByID: map[uuid.UUID]entity.AgentSession{
				sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, ProviderWorkItemRef: "issue:123", Status: enum.AgentSessionStatusOpen},
			},
			acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{
				acceptanceID: {VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: 1}, SessionID: sessionID, Status: enum.AcceptanceStatusFailed},
			},
		}
		service := New(Config{Repository: repository})

		_, err := service.CreateFollowUpIntent(context.Background(), CreateFollowUpIntentInput{
			Meta:                 value.CommandMeta{CommandID: uuid.New(), Actor: testActor()},
			SessionID:            sessionID,
			AcceptanceResultID:   &acceptanceID,
			ProviderWorkItemType: "task",
			SafeTitle:            "Follow-up",
		})
		if !errors.Is(err, errs.ErrPreconditionFailed) {
			t.Fatalf("CreateFollowUpIntent() err = %v, want %v", err, errs.ErrPreconditionFailed)
		}
		if repository.createFollowUpCalled {
			t.Fatal("invalid acceptance produced follow-up")
		}
	})

	t.Run("unsafe text and refs are rejected", func(t *testing.T) {
		t.Parallel()

		sessionID := uuid.MustParse("83838383-3333-4444-5555-666666666666")
		repository := &fakeRepository{
			sessionByID: map[uuid.UUID]entity.AgentSession{
				sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen},
			},
		}
		service := New(Config{Repository: repository})

		_, err := service.CreateFollowUpIntent(context.Background(), CreateFollowUpIntentInput{
			Meta:                 value.CommandMeta{CommandID: uuid.New(), Actor: testActor()},
			SessionID:            sessionID,
			ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "logs:stdout"},
			ProviderWorkItemType: "task",
			SafeTitle:            "raw_provider_payload dump",
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("CreateFollowUpIntent() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
		if repository.createFollowUpCalled {
			t.Fatal("unsafe payload was persisted")
		}
	})
}

func TestDispatchFollowUpIntentCreatesProviderIssueAndOutbox(t *testing.T) {
	t.Parallel()

	intentID := uuid.MustParse("87878787-1111-2222-3333-444444444444")
	sessionID := uuid.MustParse("87878787-2222-3333-4444-555555555555")
	projectID := uuid.MustParse("87878787-3333-4444-5555-666666666666")
	repositoryID := uuid.MustParse("87878787-4444-5555-6666-777777777777")
	accountID := uuid.MustParse("87878787-5555-6666-7777-888888888888")
	eventID := uuid.MustParse("87878787-6666-7777-8888-999999999999")
	expectedVersion := int64(2)
	intent := entity.FollowUpIntent{
		VersionedBase:        entity.VersionedBase{ID: intentID, Version: expectedVersion},
		SessionID:            sessionID,
		ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "github:issue:42"},
		ProviderWorkItemType: "task",
		SafeTitle:            "Prepare QA follow-up",
		SafeSummary:          "Create the bounded next-stage task.",
		Status:               enum.FollowUpIntentStatusRequested,
	}
	repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: intent}}
	creator := &fakeProviderFollowUpDispatcher{
		result: ProviderCommandResult{
			ProviderOperationRef: "provider_operation:op-123",
			ResultRef:            "github:issue:456",
			Target: ProviderCommandTarget{
				ProviderSlug:       "github",
				RepositoryFullName: "codex-k8s/kodex",
				WorkItemKind:       "issue",
				Number:             456,
			},
			Status: ProviderOperationStatusSucceeded,
		},
	}
	service := New(Config{
		Repository:                 repository,
		Clock:                      fixedClock{now: time.Date(2026, 5, 26, 20, 0, 0, 0, time.UTC)},
		IDGenerator:                &sequenceIDGenerator{ids: []uuid.UUID{eventID}},
		ProviderFollowUpDispatcher: creator,
	})
	callerCommandID := uuid.MustParse("87878787-7777-8888-9999-aaaaaaaaaaaa")

	updated, err := service.DispatchFollowUpIntent(context.Background(), DispatchFollowUpIntentInput{
		Meta:             value.CommandMeta{CommandID: callerCommandID, ExpectedVersion: &expectedVersion, Actor: testActor()},
		FollowUpIntentID: intentID,
		DispatchKind:     FollowUpDispatchKindCreateIssue,
		CreateIssue: &FollowUpCreateIssueCommand{
			ProjectID:         projectID,
			RepositoryID:      repositoryID,
			ProviderSlug:      "github",
			ExternalAccountID: accountID,
			RepositoryTarget:  ProviderCommandTarget{ProviderSlug: "github", RepositoryFullName: "codex-k8s/kodex"},
			SafeBodyHint:      "Use the safe summary only.",
		},
		OperationPolicyContext: ProviderOperationPolicyContext{
			RiskLevel: ProviderRiskLevelLow,
		},
	})
	if err != nil {
		t.Fatalf("DispatchFollowUpIntent() err = %v", err)
	}
	if updated.Status != enum.FollowUpIntentStatusCreated || updated.Version != expectedVersion+2 {
		t.Fatalf("updated intent = %+v", updated)
	}
	if updated.ProviderOperationRef != "provider_operation:op-123" || updated.ProviderTarget.WorkItemRef != "github:issue:456" {
		t.Fatalf("provider refs = %+v", updated.ProviderTarget)
	}
	if !repository.reserveFollowUpCalled || repository.reservedFollowUp.Version != expectedVersion+1 ||
		repository.reservedFollowUp.ProviderOperationRef != followUpProviderCommandRef(followUpProviderCommandID(intentID, FollowUpDispatchKindCreateIssue).String()) {
		t.Fatalf("reservation = %+v called=%v", repository.reservedFollowUp, repository.reserveFollowUpCalled)
	}
	if creator.calls != 1 || creator.input.Body != "Use the safe summary only." || creator.input.Title != intent.SafeTitle {
		t.Fatalf("provider input = %+v calls=%d", creator.input, creator.calls)
	}
	if creator.input.Meta.CommandID == callerCommandID ||
		creator.input.Meta.CommandID != followUpProviderCommandID(intentID, FollowUpDispatchKindCreateIssue) ||
		creator.input.Meta.IdempotencyKey != followUpProviderIdempotencyKey(intentID, FollowUpDispatchKindCreateIssue) ||
		creator.input.Meta.ExpectedVersion != nil {
		t.Fatalf("provider command meta = %+v", creator.input.Meta)
	}
	if creator.input.OperationPolicyContext.OperationType != ProviderOperationTypeCreateIssue ||
		creator.input.OperationPolicyContext.TargetRef != "github:repository:"+repositoryID.String() {
		t.Fatalf("policy = %+v", creator.input.OperationPolicyContext)
	}
	if repository.updateFollowUpResult.Operation != operationDispatchFollowUpIntent ||
		repository.updateFollowUpEvent == nil ||
		repository.updateFollowUpEvent.EventType != agentevents.EventFollowUpCreated {
		t.Fatalf("result/event = %s/%v", repository.updateFollowUpResult.Operation, repository.updateFollowUpEvent)
	}
	payload := decodeAgentPayload(t, *repository.updateFollowUpEvent)
	if payload.FollowUpIntentID != intentID.String() || payload.ProviderOperationRef != "provider_operation:op-123" || payload.ProviderWorkItemRef != "github:issue:456" {
		t.Fatalf("event payload = %+v", payload)
	}
	resultPayload := string(repository.updateFollowUpResult.ResultPayload)
	if strings.Contains(resultPayload, "raw_provider_payload") ||
		strings.Contains(resultPayload, "transcript") ||
		strings.Contains(resultPayload, "Use the safe summary only.") {
		t.Fatalf("unsafe marker in result payload: %s", repository.updateFollowUpResult.ResultPayload)
	}
	if !strings.Contains(resultPayload, `"body_digest":"sha256:`) {
		t.Fatalf("result payload does not contain body digest: %s", repository.updateFollowUpResult.ResultPayload)
	}

	_, err = service.DispatchFollowUpIntent(context.Background(), DispatchFollowUpIntentInput{
		Meta:             value.CommandMeta{CommandID: uuid.MustParse("87878787-8888-9999-aaaa-bbbbbbbbbbbb"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		FollowUpIntentID: intentID,
		DispatchKind:     FollowUpDispatchKindCreateIssue,
		CreateIssue: &FollowUpCreateIssueCommand{
			ProjectID:         projectID,
			RepositoryID:      repositoryID,
			ProviderSlug:      "github",
			ExternalAccountID: accountID,
			RepositoryTarget:  ProviderCommandTarget{ProviderSlug: "github", RepositoryFullName: "codex-k8s/kodex"},
			SafeBodyHint:      "Use the safe summary only.",
		},
		OperationPolicyContext: ProviderOperationPolicyContext{
			RiskLevel: ProviderRiskLevelLow,
		},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale DispatchFollowUpIntent() err = %v, want %v", err, errs.ErrConflict)
	}
	if creator.calls != 1 {
		t.Fatalf("provider called for stale dispatch: %d", creator.calls)
	}
}

func TestDispatchFollowUpIntentReplaysAndRejectsConflictingPayload(t *testing.T) {
	t.Parallel()

	intentID := uuid.MustParse("88888888-1111-2222-3333-444444444444")
	sessionID := uuid.MustParse("88888888-2222-3333-4444-555555555555")
	projectID := uuid.MustParse("88888888-3333-4444-5555-666666666666")
	repositoryID := uuid.MustParse("88888888-4444-5555-6666-777777777777")
	accountID := uuid.MustParse("88888888-5555-6666-7777-888888888888")
	commandID := uuid.MustParse("88888888-6666-7777-8888-999999999999")
	expectedVersion := int64(2)
	intent := entity.FollowUpIntent{
		VersionedBase:        entity.VersionedBase{ID: intentID, Version: expectedVersion},
		SessionID:            sessionID,
		ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "github:issue:42"},
		ProviderWorkItemType: "task",
		ProviderOperationRef: "provider_operation:op-123",
		SafeTitle:            "Prepare QA follow-up",
		SafeSummary:          "Create the bounded next-stage task.",
		Status:               enum.FollowUpIntentStatusCreated,
	}
	input := DispatchFollowUpIntentInput{
		Meta:             value.CommandMeta{CommandID: commandID, ExpectedVersion: &expectedVersion, Actor: testActor()},
		FollowUpIntentID: intentID,
		DispatchKind:     FollowUpDispatchKindCreateIssue,
		CreateIssue: &FollowUpCreateIssueCommand{
			ProjectID:         projectID,
			RepositoryID:      repositoryID,
			ProviderSlug:      "github",
			ExternalAccountID: accountID,
			RepositoryTarget:  ProviderCommandTarget{ProviderSlug: "github", RepositoryFullName: "codex-k8s/kodex"},
		},
		OperationPolicyContext: ProviderOperationPolicyContext{
			RiskLevel: ProviderRiskLevelLow,
		},
	}
	_, snapshot, err := normalizeFollowUpDispatchCommand(intent, input)
	if err != nil {
		t.Fatalf("normalize command: %v", err)
	}
	payload, err := marshalCommandPayload(followUpProviderCommandPayload{FollowUpIntent: intent, Dispatch: snapshot})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			CommandID:     &commandID,
			Actor:         testActor(),
			Operation:     operationDispatchFollowUpIntent,
			AggregateType: enum.CommandAggregateTypeFollowUp,
			AggregateID:   intentID,
			ResultPayload: payload,
		},
		followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: intent},
	}
	creator := &fakeProviderFollowUpDispatcher{}
	service := New(Config{Repository: repository, ProviderFollowUpDispatcher: creator})

	replay, err := service.DispatchFollowUpIntent(context.Background(), input)
	if err != nil {
		t.Fatalf("DispatchFollowUpIntent() err = %v", err)
	}
	if replay.ID != intentID || creator.calls != 0 || repository.updateFollowUpCalled {
		t.Fatalf("replay/update/calls = %s/%v/%d", replay.ID, repository.updateFollowUpCalled, creator.calls)
	}

	conflicting := input
	conflicting.CreateIssue.SafeBodyHint = "Different safe body."
	_, err = service.DispatchFollowUpIntent(context.Background(), conflicting)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("DispatchFollowUpIntent() err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestDispatchFollowUpIntentRecordsProviderFailure(t *testing.T) {
	t.Parallel()

	intentID := uuid.MustParse("89898989-1111-2222-3333-444444444444")
	sessionID := uuid.MustParse("89898989-2222-3333-4444-555555555555")
	projectID := uuid.MustParse("89898989-3333-4444-5555-666666666666")
	repositoryID := uuid.MustParse("89898989-4444-5555-6666-777777777777")
	accountID := uuid.MustParse("89898989-5555-6666-7777-888888888888")
	expectedVersion := int64(5)
	intent := entity.FollowUpIntent{
		VersionedBase:        entity.VersionedBase{ID: intentID, Version: expectedVersion},
		SessionID:            sessionID,
		ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "github:issue:42"},
		ProviderWorkItemType: "task",
		SafeTitle:            "Prepare QA follow-up",
		Status:               enum.FollowUpIntentStatusRequested,
	}
	repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: intent}}
	creator := &fakeProviderFollowUpDispatcher{result: ProviderCommandResult{
		ProviderOperationRef: "provider_operation:op-failed",
		Status:               ProviderOperationStatusRetryableFailed,
		ErrorCode:            "rate_limited",
		ErrorMessage:         "provider-hub command failed",
	}}
	service := New(Config{Repository: repository, IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{uuid.MustParse("89898989-6666-7777-8888-999999999999")}}, ProviderFollowUpDispatcher: creator})

	updated, err := service.DispatchFollowUpIntent(context.Background(), DispatchFollowUpIntentInput{
		Meta:             value.CommandMeta{CommandID: uuid.MustParse("89898989-7777-8888-9999-aaaaaaaaaaaa"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		FollowUpIntentID: intentID,
		DispatchKind:     FollowUpDispatchKindCreateIssue,
		CreateIssue: &FollowUpCreateIssueCommand{
			ProjectID:         projectID,
			RepositoryID:      repositoryID,
			ProviderSlug:      "github",
			ExternalAccountID: accountID,
			RepositoryTarget:  ProviderCommandTarget{ProviderSlug: "github", RepositoryFullName: "codex-k8s/kodex"},
		},
		OperationPolicyContext: ProviderOperationPolicyContext{
			RiskLevel: ProviderRiskLevelMedium,
		},
	})
	if err != nil {
		t.Fatalf("DispatchFollowUpIntent() err = %v", err)
	}
	if updated.Status != enum.FollowUpIntentStatusFailed || updated.ProviderOperationRef != "provider_operation:op-failed" {
		t.Fatalf("updated = %+v", updated)
	}
	if repository.updateFollowUpEvent == nil || repository.updateFollowUpEvent.EventType != agentevents.EventFollowUpFailed {
		t.Fatalf("event = %+v", repository.updateFollowUpEvent)
	}
	payload := decodeAgentPayload(t, *repository.updateFollowUpEvent)
	if payload.FailureCode != "provider_command_failed" || payload.ProviderOperationRef != "provider_operation:op-failed" {
		t.Fatalf("failure payload = %+v", payload)
	}
}

func TestDispatchFollowUpIntentUpdateAndCommentPaths(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("89898989-aaaa-bbbb-cccc-111111111111")
	repositoryID := uuid.MustParse("89898989-aaaa-bbbb-cccc-222222222222")
	accountID := uuid.MustParse("89898989-aaaa-bbbb-cccc-333333333333")
	target := ProviderCommandTarget{
		ProviderSlug:       "github",
		RepositoryFullName: "codex-k8s/kodex",
		WorkItemKind:       "issue",
		Number:             42,
	}
	pullRequestTarget := ProviderCommandTarget{
		ProviderSlug:       "github",
		RepositoryFullName: "codex-k8s/kodex",
		WorkItemKind:       "pull_request",
		Number:             77,
	}
	policy := ProviderOperationPolicyContext{
		ProjectID:    projectID.String(),
		RepositoryID: repositoryID.String(),
		RiskLevel:    ProviderRiskLevelLow,
	}

	t.Run("update issue", func(t *testing.T) {
		t.Parallel()
		intentID := uuid.MustParse("89898989-aaaa-bbbb-cccc-444444444444")
		sessionID := uuid.MustParse("89898989-aaaa-bbbb-cccc-555555555555")
		expectedVersion := int64(3)
		body := "Bounded status update."
		intent := entity.FollowUpIntent{
			VersionedBase:        entity.VersionedBase{ID: intentID, Version: expectedVersion},
			SessionID:            sessionID,
			ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "github:issue:42"},
			ProviderWorkItemType: "task",
			SafeTitle:            "Prepare QA follow-up",
			Status:               enum.FollowUpIntentStatusRequested,
		}
		dispatcher := &fakeProviderFollowUpDispatcher{result: ProviderCommandResult{
			ProviderOperationRef: "provider_operation:update-op",
			ResultRef:            "github:issue:42",
			Target:               target,
			Status:               ProviderOperationStatusSucceeded,
		}}
		repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: intent}}
		service := New(Config{
			Repository:                 repository,
			IDGenerator:                &sequenceIDGenerator{ids: []uuid.UUID{uuid.MustParse("89898989-aaaa-bbbb-cccc-666666666666")}},
			ProviderFollowUpDispatcher: dispatcher,
		})

		updated, err := service.DispatchFollowUpIntent(context.Background(), DispatchFollowUpIntentInput{
			Meta:             value.CommandMeta{CommandID: uuid.MustParse("89898989-aaaa-bbbb-cccc-777777777777"), ExpectedVersion: &expectedVersion, Actor: testActor()},
			FollowUpIntentID: intentID,
			DispatchKind:     FollowUpDispatchKindUpdateIssue,
			UpdateIssue: &FollowUpUpdateIssueCommand{
				ExternalAccountID: accountID,
				Target:            target,
				SafeBodyHint:      &body,
				Labels:            &ProviderStringListPatch{Values: []string{"follow-up", "qa"}},
			},
			OperationPolicyContext: policy,
		})
		if err != nil {
			t.Fatalf("DispatchFollowUpIntent() err = %v", err)
		}
		if updated.Status != enum.FollowUpIntentStatusUpdated || updated.ProviderTarget.WorkItemRef != "github:issue:42" {
			t.Fatalf("updated = %+v", updated)
		}
		if dispatcher.updateIssueCalls != 1 || dispatcher.updateIssueInput.Body == nil || *dispatcher.updateIssueInput.Body != body {
			t.Fatalf("update issue input = %+v calls=%d", dispatcher.updateIssueInput, dispatcher.updateIssueCalls)
		}
		if dispatcher.updateIssueInput.OperationPolicyContext.OperationType != ProviderOperationTypeUpdateIssue {
			t.Fatalf("policy = %+v", dispatcher.updateIssueInput.OperationPolicyContext)
		}
		if repository.updateFollowUpEvent == nil || repository.updateFollowUpEvent.EventType != agentevents.EventFollowUpUpdated {
			t.Fatalf("event = %+v", repository.updateFollowUpEvent)
		}
		if strings.Contains(string(repository.updateFollowUpResult.ResultPayload), body) {
			t.Fatalf("raw body stored in result payload: %s", repository.updateFollowUpResult.ResultPayload)
		}
	})

	t.Run("create comment", func(t *testing.T) {
		t.Parallel()
		intentID := uuid.MustParse("89898989-aaaa-bbbb-cccc-888888888888")
		sessionID := uuid.MustParse("89898989-aaaa-bbbb-cccc-999999999999")
		expectedVersion := int64(4)
		intent := entity.FollowUpIntent{
			VersionedBase:        entity.VersionedBase{ID: intentID, Version: expectedVersion},
			SessionID:            sessionID,
			ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "github:issue:42"},
			ProviderWorkItemType: "task",
			SafeTitle:            "Prepare QA follow-up",
			Status:               enum.FollowUpIntentStatusRequested,
		}
		dispatcher := &fakeProviderFollowUpDispatcher{result: ProviderCommandResult{
			ProviderOperationRef: "provider_operation:comment-op",
			ResultRef:            "github:comment:55",
			Status:               ProviderOperationStatusSucceeded,
		}}
		repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: intent}}
		service := New(Config{
			Repository:                 repository,
			IDGenerator:                &sequenceIDGenerator{ids: []uuid.UUID{uuid.MustParse("89898989-bbbb-cccc-dddd-111111111111")}},
			ProviderFollowUpDispatcher: dispatcher,
		})

		updated, err := service.DispatchFollowUpIntent(context.Background(), DispatchFollowUpIntentInput{
			Meta:             value.CommandMeta{CommandID: uuid.MustParse("89898989-bbbb-cccc-dddd-222222222222"), ExpectedVersion: &expectedVersion, Actor: testActor()},
			FollowUpIntentID: intentID,
			DispatchKind:     FollowUpDispatchKindCreateComment,
			CreateComment: &FollowUpCreateCommentCommand{
				ExternalAccountID: accountID,
				Target:            target,
				SafeBodyHint:      "Bounded public comment.",
			},
			OperationPolicyContext: policy,
		})
		if err != nil {
			t.Fatalf("DispatchFollowUpIntent() err = %v", err)
		}
		if updated.Status != enum.FollowUpIntentStatusCommented || updated.ProviderTarget.CommentRef != "github:comment:55" {
			t.Fatalf("updated = %+v", updated)
		}
		if dispatcher.createCommentCalls != 1 || dispatcher.createCommentInput.OperationPolicyContext.OperationType != ProviderOperationTypeCreateComment {
			t.Fatalf("create comment input = %+v calls=%d", dispatcher.createCommentInput, dispatcher.createCommentCalls)
		}
		if repository.updateFollowUpEvent == nil || repository.updateFollowUpEvent.EventType != agentevents.EventFollowUpCommented {
			t.Fatalf("event = %+v", repository.updateFollowUpEvent)
		}
	})

	t.Run("update pull request", func(t *testing.T) {
		t.Parallel()
		intentID := uuid.MustParse("89898989-bbbb-cccc-dddd-333333333333")
		sessionID := uuid.MustParse("89898989-bbbb-cccc-dddd-444444444444")
		expectedVersion := int64(5)
		body := "Bounded PR update."
		baseBranch := "release"
		intent := entity.FollowUpIntent{
			VersionedBase:        entity.VersionedBase{ID: intentID, Version: expectedVersion},
			SessionID:            sessionID,
			ProviderTarget:       value.ProviderTargetRef{PullRequestRef: "github:pull_request:77"},
			ProviderWorkItemType: "review",
			SafeTitle:            "Prepare review follow-up",
			Status:               enum.FollowUpIntentStatusRequested,
		}
		dispatcher := &fakeProviderFollowUpDispatcher{result: ProviderCommandResult{
			ProviderOperationRef: "provider_operation:pr-op",
			ResultRef:            "github:pull_request:77",
			Target:               pullRequestTarget,
			Status:               ProviderOperationStatusSucceeded,
		}}
		repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: intent}}
		service := New(Config{
			Repository:                 repository,
			IDGenerator:                &sequenceIDGenerator{ids: []uuid.UUID{uuid.MustParse("89898989-bbbb-cccc-dddd-555555555555")}},
			ProviderFollowUpDispatcher: dispatcher,
		})

		updated, err := service.DispatchFollowUpIntent(context.Background(), DispatchFollowUpIntentInput{
			Meta:             value.CommandMeta{CommandID: uuid.MustParse("89898989-bbbb-cccc-dddd-666666666666"), ExpectedVersion: &expectedVersion, Actor: testActor()},
			FollowUpIntentID: intentID,
			DispatchKind:     FollowUpDispatchKindUpdatePullRequest,
			UpdatePullRequest: &FollowUpUpdatePullRequestCommand{
				ExternalAccountID:       accountID,
				Target:                  pullRequestTarget,
				SafeBodyHint:            &body,
				BaseBranch:              &baseBranch,
				ExpectedProviderVersion: "etag:77",
			},
			OperationPolicyContext: policy,
		})
		if err != nil {
			t.Fatalf("DispatchFollowUpIntent() err = %v", err)
		}
		if updated.Status != enum.FollowUpIntentStatusUpdated || updated.ProviderTarget.PullRequestRef != "github:pull_request:77" {
			t.Fatalf("updated = %+v", updated)
		}
		if dispatcher.updatePullRequestCalls != 1 || dispatcher.updatePullRequestInput.Body == nil || *dispatcher.updatePullRequestInput.Body != body ||
			dispatcher.updatePullRequestInput.ExpectedProviderVersion != "etag:77" {
			t.Fatalf("update pull request input = %+v calls=%d", dispatcher.updatePullRequestInput, dispatcher.updatePullRequestCalls)
		}
		if dispatcher.updatePullRequestInput.OperationPolicyContext.OperationType != ProviderOperationTypeUpdatePullRequest ||
			!sameStringSet(dispatcher.updatePullRequestInput.OperationPolicyContext.ChangedFields, []string{"base_branch", "body"}) {
			t.Fatalf("policy = %+v", dispatcher.updatePullRequestInput.OperationPolicyContext)
		}
		if repository.updateFollowUpEvent == nil || repository.updateFollowUpEvent.EventType != agentevents.EventFollowUpUpdated {
			t.Fatalf("event = %+v", repository.updateFollowUpEvent)
		}
		if strings.Contains(string(repository.updateFollowUpResult.ResultPayload), body) {
			t.Fatalf("raw body stored in result payload: %s", repository.updateFollowUpResult.ResultPayload)
		}
	})

	t.Run("create review signal", func(t *testing.T) {
		t.Parallel()
		intentID := uuid.MustParse("89898989-bbbb-cccc-dddd-777777777777")
		sessionID := uuid.MustParse("89898989-bbbb-cccc-dddd-888888888888")
		expectedVersion := int64(6)
		body := "Bounded review signal."
		inlineBody := "Bounded inline note."
		line := int64(12)
		intent := entity.FollowUpIntent{
			VersionedBase:        entity.VersionedBase{ID: intentID, Version: expectedVersion},
			SessionID:            sessionID,
			ProviderTarget:       value.ProviderTargetRef{PullRequestRef: "github:pull_request:77"},
			ProviderWorkItemType: "review",
			SafeTitle:            "Prepare review follow-up",
			Status:               enum.FollowUpIntentStatusRequested,
		}
		dispatcher := &fakeProviderFollowUpDispatcher{result: ProviderCommandResult{
			ProviderOperationRef: "provider_operation:review-op",
			ResultRef:            "github:review_signal:990",
			Target:               pullRequestTarget,
			Status:               ProviderOperationStatusSucceeded,
		}}
		repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: intent}}
		service := New(Config{
			Repository:                 repository,
			IDGenerator:                &sequenceIDGenerator{ids: []uuid.UUID{uuid.MustParse("89898989-bbbb-cccc-dddd-999999999999")}},
			ProviderFollowUpDispatcher: dispatcher,
		})

		updated, err := service.DispatchFollowUpIntent(context.Background(), DispatchFollowUpIntentInput{
			Meta:             value.CommandMeta{CommandID: uuid.MustParse("89898989-cccc-dddd-eeee-111111111111"), ExpectedVersion: &expectedVersion, Actor: testActor()},
			FollowUpIntentID: intentID,
			DispatchKind:     FollowUpDispatchKindCreateReviewSignal,
			CreateReviewSignal: &FollowUpCreateReviewSignalCommand{
				ExternalAccountID: accountID,
				Target:            pullRequestTarget,
				Kind:              ProviderReviewSignalKindChangesRequested,
				SafeBodyHint:      &body,
				InlineComments: []ProviderReviewInlineComment{{
					Path: "services/internal/agent-manager/internal/domain/service/service_test.go",
					Body: inlineBody,
					Line: &line,
					Side: "RIGHT",
				}},
			},
			OperationPolicyContext: policy,
		})
		if err != nil {
			t.Fatalf("DispatchFollowUpIntent() err = %v", err)
		}
		if updated.Status != enum.FollowUpIntentStatusReviewSignaled || updated.ProviderTarget.ReviewSignalRef != "github:review_signal:990" {
			t.Fatalf("updated = %+v", updated)
		}
		if dispatcher.createReviewSignalCalls != 1 || dispatcher.createReviewSignalInput.Body != body ||
			dispatcher.createReviewSignalInput.Kind != ProviderReviewSignalKindChangesRequested ||
			len(dispatcher.createReviewSignalInput.InlineComments) != 1 {
			t.Fatalf("review signal input = %+v calls=%d", dispatcher.createReviewSignalInput, dispatcher.createReviewSignalCalls)
		}
		if dispatcher.createReviewSignalInput.OperationPolicyContext.OperationType != ProviderOperationTypeCreateReviewSignal {
			t.Fatalf("policy = %+v", dispatcher.createReviewSignalInput.OperationPolicyContext)
		}
		if repository.updateFollowUpEvent == nil || repository.updateFollowUpEvent.EventType != agentevents.EventFollowUpReviewSignaled {
			t.Fatalf("event = %+v", repository.updateFollowUpEvent)
		}
		resultPayload := string(repository.updateFollowUpResult.ResultPayload)
		if strings.Contains(resultPayload, body) || strings.Contains(resultPayload, inlineBody) {
			t.Fatalf("raw review body stored in result payload: %s", repository.updateFollowUpResult.ResultPayload)
		}
	})
}

func TestDispatchFollowUpIntentRejectsMismatchedProviderTargets(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("8b8b8b8b-1111-2222-3333-111111111111")
	repositoryID := uuid.MustParse("8b8b8b8b-1111-2222-3333-222222222222")
	accountID := uuid.MustParse("8b8b8b8b-1111-2222-3333-333333333333")
	expectedVersion := int64(5)
	body := "Bounded update body."
	policy := ProviderOperationPolicyContext{
		ProjectID:    projectID.String(),
		RepositoryID: repositoryID.String(),
		RiskLevel:    ProviderRiskLevelLow,
	}
	matchingIssueTarget := ProviderCommandTarget{
		ProviderSlug:       "github",
		RepositoryFullName: "codex-k8s/kodex",
		WorkItemKind:       "issue",
		Number:             42,
	}
	mismatchedIssueTarget := matchingIssueTarget
	mismatchedIssueTarget.Number = 99
	matchingPullRequestTarget := ProviderCommandTarget{
		ProviderSlug:       "github",
		RepositoryFullName: "codex-k8s/kodex",
		WorkItemKind:       "pull_request",
		Number:             77,
	}
	mismatchedPullRequestTarget := matchingPullRequestTarget
	mismatchedPullRequestTarget.Number = 88

	cases := []struct {
		name   string
		intent entity.FollowUpIntent
		input  DispatchFollowUpIntentInput
	}{
		{
			name: "update issue target",
			intent: entity.FollowUpIntent{
				VersionedBase:        entity.VersionedBase{ID: uuid.MustParse("8b8b8b8b-1111-2222-3333-444444444444"), Version: expectedVersion},
				SessionID:            uuid.MustParse("8b8b8b8b-1111-2222-3333-555555555555"),
				ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "github:issue:42"},
				ProviderWorkItemType: "task",
				SafeTitle:            "Prepare QA follow-up",
				Status:               enum.FollowUpIntentStatusRequested,
			},
			input: DispatchFollowUpIntentInput{
				DispatchKind: FollowUpDispatchKindUpdateIssue,
				UpdateIssue: &FollowUpUpdateIssueCommand{
					ExternalAccountID: accountID,
					Target:            mismatchedIssueTarget,
					SafeBodyHint:      &body,
				},
			},
		},
		{
			name: "create comment parent target",
			intent: entity.FollowUpIntent{
				VersionedBase:        entity.VersionedBase{ID: uuid.MustParse("8b8b8b8b-1111-2222-3333-666666666666"), Version: expectedVersion},
				SessionID:            uuid.MustParse("8b8b8b8b-1111-2222-3333-777777777777"),
				ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "github:issue:42"},
				ProviderWorkItemType: "task",
				SafeTitle:            "Prepare QA follow-up",
				Status:               enum.FollowUpIntentStatusRequested,
			},
			input: DispatchFollowUpIntentInput{
				DispatchKind: FollowUpDispatchKindCreateComment,
				CreateComment: &FollowUpCreateCommentCommand{
					ExternalAccountID: accountID,
					Target:            mismatchedIssueTarget,
					SafeBodyHint:      "Bounded public comment.",
				},
			},
		},
		{
			name: "update comment ref",
			intent: entity.FollowUpIntent{
				VersionedBase:        entity.VersionedBase{ID: uuid.MustParse("8b8b8b8b-1111-2222-3333-888888888888"), Version: expectedVersion},
				SessionID:            uuid.MustParse("8b8b8b8b-1111-2222-3333-999999999999"),
				ProviderTarget:       value.ProviderTargetRef{WorkItemRef: "github:issue:42", CommentRef: "github:comment:55"},
				ProviderWorkItemType: "task",
				SafeTitle:            "Prepare QA follow-up",
				Status:               enum.FollowUpIntentStatusRequested,
			},
			input: DispatchFollowUpIntentInput{
				DispatchKind: FollowUpDispatchKindUpdateComment,
				UpdateComment: &FollowUpUpdateCommentCommand{
					ExternalAccountID: accountID,
					Target:            matchingIssueTarget,
					ProviderCommentID: "99",
					SafeBodyHint:      "Bounded comment update.",
				},
			},
		},
		{
			name: "update pull request target",
			intent: entity.FollowUpIntent{
				VersionedBase:        entity.VersionedBase{ID: uuid.MustParse("8b8b8b8b-1111-2222-3333-aaaaaaaaaaaa"), Version: expectedVersion},
				SessionID:            uuid.MustParse("8b8b8b8b-1111-2222-3333-bbbbbbbbbbbb"),
				ProviderTarget:       value.ProviderTargetRef{PullRequestRef: "github:pull_request:77"},
				ProviderWorkItemType: "review",
				SafeTitle:            "Prepare review follow-up",
				Status:               enum.FollowUpIntentStatusRequested,
			},
			input: DispatchFollowUpIntentInput{
				DispatchKind: FollowUpDispatchKindUpdatePullRequest,
				UpdatePullRequest: &FollowUpUpdatePullRequestCommand{
					ExternalAccountID:       accountID,
					Target:                  mismatchedPullRequestTarget,
					SafeBodyHint:            &body,
					ExpectedProviderVersion: "etag:77",
				},
			},
		},
		{
			name: "create review signal target",
			intent: entity.FollowUpIntent{
				VersionedBase:        entity.VersionedBase{ID: uuid.MustParse("8b8b8b8b-1111-2222-3333-cccccccccccc"), Version: expectedVersion},
				SessionID:            uuid.MustParse("8b8b8b8b-1111-2222-3333-dddddddddddd"),
				ProviderTarget:       value.ProviderTargetRef{PullRequestRef: "github:pull_request:77"},
				ProviderWorkItemType: "review",
				SafeTitle:            "Prepare review follow-up",
				Status:               enum.FollowUpIntentStatusRequested,
			},
			input: DispatchFollowUpIntentInput{
				DispatchKind: FollowUpDispatchKindCreateReviewSignal,
				CreateReviewSignal: &FollowUpCreateReviewSignalCommand{
					ExternalAccountID: accountID,
					Target:            mismatchedPullRequestTarget,
					Kind:              ProviderReviewSignalKindComment,
					SafeBodyHint:      &body,
				},
			},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.input.Meta = value.CommandMeta{CommandID: uuid.New(), ExpectedVersion: &expectedVersion, Actor: testActor()}
			tc.input.FollowUpIntentID = tc.intent.ID
			tc.input.OperationPolicyContext = policy
			repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{tc.intent.ID: tc.intent}}
			dispatcher := &fakeProviderFollowUpDispatcher{}
			service := New(Config{Repository: repository, ProviderFollowUpDispatcher: dispatcher})

			_, err := service.DispatchFollowUpIntent(context.Background(), tc.input)
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("DispatchFollowUpIntent() err = %v, want %v", err, errs.ErrInvalidArgument)
			}
			if repository.updateFollowUpCalled {
				t.Fatalf("reserved mismatched follow-up target")
			}
			if dispatcher.calls+dispatcher.updateIssueCalls+dispatcher.createCommentCalls+dispatcher.updateCommentCalls+dispatcher.updatePullRequestCalls+dispatcher.createReviewSignalCalls != 0 {
				t.Fatalf("provider dispatcher called on mismatched target: %+v", dispatcher)
			}
		})
	}
}

func TestDispatchFollowUpIntentRejectsInvalidStateAndUnsafePayload(t *testing.T) {
	t.Parallel()

	intentID := uuid.MustParse("8a8a8a8a-1111-2222-3333-444444444444")
	projectID := uuid.MustParse("8a8a8a8a-2222-3333-4444-555555555555")
	repositoryID := uuid.MustParse("8a8a8a8a-3333-4444-5555-666666666666")
	accountID := uuid.MustParse("8a8a8a8a-4444-5555-6666-777777777777")
	expectedVersion := int64(2)
	base := entity.FollowUpIntent{
		VersionedBase:        entity.VersionedBase{ID: intentID, Version: expectedVersion},
		SessionID:            uuid.MustParse("8a8a8a8a-5555-6666-7777-888888888888"),
		ProviderWorkItemType: "task",
		SafeTitle:            "Prepare QA follow-up",
		Status:               enum.FollowUpIntentStatusCreated,
	}
	input := DispatchFollowUpIntentInput{
		Meta:             value.CommandMeta{CommandID: uuid.MustParse("8a8a8a8a-6666-7777-8888-999999999999"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		FollowUpIntentID: intentID,
		DispatchKind:     FollowUpDispatchKindCreateIssue,
		CreateIssue: &FollowUpCreateIssueCommand{
			ProjectID:         projectID,
			RepositoryID:      repositoryID,
			ProviderSlug:      "github",
			ExternalAccountID: accountID,
			RepositoryTarget:  ProviderCommandTarget{ProviderSlug: "github", RepositoryFullName: "codex-k8s/kodex"},
		},
		OperationPolicyContext: ProviderOperationPolicyContext{
			RiskLevel: ProviderRiskLevelLow,
		},
	}

	t.Run("terminal intent status", func(t *testing.T) {
		t.Parallel()
		repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: base}}
		creator := &fakeProviderFollowUpDispatcher{}
		service := New(Config{Repository: repository, ProviderFollowUpDispatcher: creator})
		_, err := service.DispatchFollowUpIntent(context.Background(), input)
		if !errors.Is(err, errs.ErrPreconditionFailed) {
			t.Fatalf("DispatchFollowUpIntent() err = %v, want %v", err, errs.ErrPreconditionFailed)
		}
		if creator.calls != 0 || repository.updateFollowUpCalled {
			t.Fatalf("creator/update called = %d/%v", creator.calls, repository.updateFollowUpCalled)
		}
	})

	t.Run("missing repository target", func(t *testing.T) {
		t.Parallel()
		intent := base
		intent.Status = enum.FollowUpIntentStatusRequested
		request := input
		createIssue := *input.CreateIssue
		request.CreateIssue = &createIssue
		request.CreateIssue.RepositoryTarget = ProviderCommandTarget{ProviderSlug: "github"}
		repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: intent}}
		creator := &fakeProviderFollowUpDispatcher{}
		service := New(Config{Repository: repository, ProviderFollowUpDispatcher: creator})
		_, err := service.DispatchFollowUpIntent(context.Background(), request)
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("DispatchFollowUpIntent() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
	})

	t.Run("unsafe command payload", func(t *testing.T) {
		t.Parallel()
		intent := base
		intent.Status = enum.FollowUpIntentStatusRequested
		request := input
		createIssue := *input.CreateIssue
		request.CreateIssue = &createIssue
		request.CreateIssue.SafeBodyHint = "raw_provider_payload must not be stored"
		repository := &fakeRepository{followUpByID: map[uuid.UUID]entity.FollowUpIntent{intentID: intent}}
		creator := &fakeProviderFollowUpDispatcher{}
		service := New(Config{Repository: repository, ProviderFollowUpDispatcher: creator})
		_, err := service.DispatchFollowUpIntent(context.Background(), request)
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("DispatchFollowUpIntent() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
		if creator.calls != 0 {
			t.Fatalf("provider called for unsafe payload: %d", creator.calls)
		}
	})
}

func TestRecordAgentActivityStoresSafeTimelineEntry(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("84848484-1111-2222-3333-444444444444")
	runID := uuid.MustParse("84848484-2222-3333-4444-555555555555")
	activityID := uuid.MustParse("84848484-3333-4444-5555-666666666666")
	startedAt := time.Date(2026, 5, 26, 19, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(2 * time.Second)
	digest := "sha256:" + strings.Repeat("b", 64)
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 2}, Status: enum.AgentSessionStatusOpen},
		},
		runByID: map[uuid.UUID]entity.AgentRun{
			runID: {VersionedBase: entity.VersionedBase{ID: runID, Version: 3}, SessionID: sessionID, Status: enum.AgentRunStatusRunning},
		},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: time.Date(2026, 5, 26, 19, 1, 0, 0, time.UTC)},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{activityID}},
	})

	activity, err := service.RecordAgentActivity(context.Background(), RecordAgentActivityInput{
		Meta:            value.CommandMeta{IdempotencyKey: "activity-1", Actor: testActor()},
		SessionID:       sessionID,
		RunID:           &runID,
		TurnID:          "turn:1",
		ToolUseID:       "tool:call-1",
		ActivityKind:    enum.AgentActivityKindToolResult,
		ToolName:        "functions.exec_command",
		ToolCategory:    "shell",
		Status:          enum.AgentActivityStatusSucceeded,
		StartedAt:       &startedAt,
		FinishedAt:      &finishedAt,
		SafeSummary:     "Listed repository files.",
		PayloadDigest:   digest,
		SafeRefsJSON:    []byte(`{"artifact_ref":"artifact:activity-summary"}`),
		SafeDetailsJSON: []byte(`{"summary":"bounded metadata","exit_code":0,"artifact_refs":["artifact:activity-summary"]}`),
		CorrelationID:   "trace:activity-1",
	})
	if err != nil {
		t.Fatalf("RecordAgentActivity() err = %v", err)
	}
	if activity.ID != activityID || activity.RunID == nil || *activity.RunID != runID || activity.DurationMs == nil || *activity.DurationMs != 2000 {
		t.Fatalf("activity = %+v", activity)
	}
	if repository.createdActivity.ID != activityID || repository.activityResult.AggregateType != enum.CommandAggregateTypeActivity {
		t.Fatalf("stored/result = %+v/%s", repository.createdActivity, repository.activityResult.AggregateType)
	}
	if strings.Contains(string(repository.createdActivity.SafeDetailsJSON), "\n") || strings.Contains(string(repository.activityResult.ResultPayload), "tool_input") {
		t.Fatalf("unsafe or uncompact payload persisted: %s", repository.activityResult.ResultPayload)
	}
}

func TestRecordAgentActivityReplaysAndRejectsConflictingPayload(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("85858585-1111-2222-3333-444444444444")
	startedAt := time.Date(2026, 5, 26, 19, 5, 0, 0, time.UTC)
	activity := entity.AgentActivity{
		VersionedBase:   entity.VersionedBase{ID: uuid.MustParse("85858585-2222-3333-4444-555555555555"), Version: 1},
		SessionID:       sessionID,
		ActivityKind:    enum.AgentActivityKindLifecycle,
		Status:          enum.AgentActivityStatusStarted,
		StartedAt:       startedAt,
		SafeSummary:     "Run started.",
		SafeRefsJSON:    []byte("{}"),
		SafeDetailsJSON: []byte("{}"),
		IdempotencyKey:  operationRecordAgentActivity + ":user:owner:activity-replay",
	}
	payload, err := marshalCommandPayload(activityCommandPayload{Activity: activity})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			IdempotencyKey: "activity-replay",
			Actor:          testActor(),
			Operation:      operationRecordAgentActivity,
			AggregateType:  enum.CommandAggregateTypeActivity,
			AggregateID:    activity.ID,
			ResultPayload:  payload,
		},
		sessionByID:  map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
		activityByID: map[uuid.UUID]entity.AgentActivity{activity.ID: activity},
	}
	service := New(Config{Repository: repository})

	replay, err := service.RecordAgentActivity(context.Background(), RecordAgentActivityInput{
		Meta:            value.CommandMeta{IdempotencyKey: "activity-replay", Actor: testActor()},
		SessionID:       sessionID,
		ActivityKind:    enum.AgentActivityKindLifecycle,
		Status:          enum.AgentActivityStatusStarted,
		StartedAt:       &startedAt,
		SafeSummary:     "Run started.",
		SafeRefsJSON:    []byte("{}"),
		SafeDetailsJSON: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("RecordAgentActivity() err = %v", err)
	}
	if replay.ID != activity.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, activity.ID)
	}
	if repository.createActivityCalled {
		t.Fatal("RecordAgentActivityWithResult called during replay")
	}

	_, err = service.RecordAgentActivity(context.Background(), RecordAgentActivityInput{
		Meta:            value.CommandMeta{IdempotencyKey: "activity-replay", Actor: testActor()},
		SessionID:       sessionID,
		ActivityKind:    enum.AgentActivityKindLifecycle,
		Status:          enum.AgentActivityStatusStarted,
		StartedAt:       &startedAt,
		SafeSummary:     "Different safe summary.",
		SafeRefsJSON:    []byte("{}"),
		SafeDetailsJSON: []byte("{}"),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordAgentActivity() err = %v, want %v", err, errs.ErrConflict)
	}
}

func TestRecordAgentActivityRejectsUnsafePayloadAndRunMismatch(t *testing.T) {
	t.Parallel()

	t.Run("unsafe details", func(t *testing.T) {
		t.Parallel()

		sessionID := uuid.MustParse("86868686-1111-2222-3333-444444444444")
		startedAt := time.Date(2026, 5, 26, 19, 10, 0, 0, time.UTC)
		repository := &fakeRepository{
			sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
		}
		service := New(Config{Repository: repository})

		_, err := service.RecordAgentActivity(context.Background(), RecordAgentActivityInput{
			Meta:            value.CommandMeta{IdempotencyKey: "unsafe-activity", Actor: testActor()},
			SessionID:       sessionID,
			ActivityKind:    enum.AgentActivityKindToolUse,
			ToolName:        "functions.exec_command",
			Status:          enum.AgentActivityStatusFailed,
			StartedAt:       &startedAt,
			SafeSummary:     "raw_tool_input dump",
			SafeDetailsJSON: []byte(`{"stdout":"not safe"}`),
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("RecordAgentActivity() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
		if repository.createActivityCalled {
			t.Fatal("unsafe activity was persisted")
		}
	})

	t.Run("run session mismatch", func(t *testing.T) {
		t.Parallel()

		sessionID := uuid.MustParse("86868686-2222-3333-4444-555555555555")
		otherSessionID := uuid.MustParse("86868686-3333-4444-5555-666666666666")
		runID := uuid.MustParse("86868686-4444-5555-6666-777777777777")
		startedAt := time.Date(2026, 5, 26, 19, 11, 0, 0, time.UTC)
		repository := &fakeRepository{
			sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
			runByID:     map[uuid.UUID]entity.AgentRun{runID: {VersionedBase: entity.VersionedBase{ID: runID, Version: 1}, SessionID: otherSessionID}},
		}
		service := New(Config{Repository: repository})

		_, err := service.RecordAgentActivity(context.Background(), RecordAgentActivityInput{
			Meta:         value.CommandMeta{IdempotencyKey: "mismatch-activity", Actor: testActor()},
			SessionID:    sessionID,
			RunID:        &runID,
			ActivityKind: enum.AgentActivityKindLifecycle,
			Status:       enum.AgentActivityStatusStarted,
			StartedAt:    &startedAt,
		})
		if !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("RecordAgentActivity() err = %v, want %v", err, errs.ErrConflict)
		}
	})

	t.Run("unsafe idempotency trace", func(t *testing.T) {
		t.Parallel()

		sessionID := uuid.MustParse("86868686-5555-6666-7777-888888888888")
		startedAt := time.Date(2026, 5, 26, 19, 12, 0, 0, time.UTC)
		repository := &fakeRepository{
			sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
		}
		service := New(Config{Repository: repository})

		_, err := service.RecordAgentActivity(context.Background(), RecordAgentActivityInput{
			Meta:         value.CommandMeta{IdempotencyKey: "activity-token-dump", Actor: testActor()},
			SessionID:    sessionID,
			ActivityKind: enum.AgentActivityKindLifecycle,
			Status:       enum.AgentActivityStatusStarted,
			StartedAt:    &startedAt,
		})
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Fatalf("RecordAgentActivity() err = %v, want %v", err, errs.ErrInvalidArgument)
		}
		if repository.createActivityCalled {
			t.Fatal("unsafe idempotency trace was persisted")
		}
	})
}

func TestListAgentActivitiesValidatesFilterAndDelegates(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("87878787-1111-2222-3333-444444444444")
	runID := uuid.MustParse("87878787-2222-3333-4444-555555555555")
	kind := enum.AgentActivityKindToolResult
	status := enum.AgentActivityStatusSucceeded
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
		runByID:     map[uuid.UUID]entity.AgentRun{runID: {VersionedBase: entity.VersionedBase{ID: runID, Version: 1}, SessionID: sessionID}},
		activityList: []entity.AgentActivity{{
			VersionedBase: entity.VersionedBase{ID: uuid.MustParse("87878787-3333-4444-5555-666666666666"), Version: 1},
			SessionID:     sessionID,
			RunID:         &runID,
			ActivityKind:  kind,
			Status:        status,
		}},
		activityPage: value.PageResult{NextPageToken: "next"},
	}
	service := New(Config{Repository: repository})

	activities, page, err := service.ListAgentActivities(context.Background(), query.AgentActivityFilter{
		SessionID:    sessionID,
		RunID:        runID,
		ActivityKind: &kind,
		Status:       &status,
		Page:         value.PageRequest{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("ListAgentActivities() err = %v", err)
	}
	if len(activities) != 1 || page.NextPageToken != "next" {
		t.Fatalf("activities/page = %+v/%+v", activities, page)
	}
	if repository.activityFilter.SessionID != sessionID || repository.activityFilter.RunID != runID ||
		repository.activityFilter.ActivityKind == nil || *repository.activityFilter.ActivityKind != kind {
		t.Fatalf("filter = %+v", repository.activityFilter)
	}

	_, _, err = service.ListAgentActivities(context.Background(), query.AgentActivityFilter{})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListAgentActivities() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestListAgentSessionSummariesDelegatesScopedEmptyRead(t *testing.T) {
	t.Parallel()

	scope := value.ScopeRef{Type: "project", Ref: "project:operator"}
	page := value.PageResult{NextPageToken: "next"}
	repository := &fakeRepository{sessionSummaryPage: page}
	service := New(Config{Repository: repository})

	items, result, err := service.ListAgentSessionSummaries(context.Background(), query.AgentSessionFilter{
		Scope: scope,
		Page:  value.PageRequest{PageSize: 20},
	})
	if err != nil {
		t.Fatalf("ListAgentSessionSummaries() err = %v", err)
	}
	if len(items) != 0 || result.NextPageToken != page.NextPageToken {
		t.Fatalf("items/page = %v/%+v", items, result)
	}
	if repository.sessionSummaryFilter.Scope != scope || repository.sessionSummaryFilter.Page.PageSize != 20 {
		t.Fatalf("filter = %+v", repository.sessionSummaryFilter)
	}
}

func TestListAgentRunSummariesDelegatesSafeRead(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("90909090-aaaa-bbbb-cccc-dddddddddddd")
	runID := uuid.MustParse("90909090-bbbb-cccc-dddd-eeeeeeeeeeee")
	activityID := uuid.MustParse("90909090-cccc-dddd-eeee-ffffffffffff")
	repository := &fakeRepository{
		runSummaryList: []entity.AgentRunListItem{{
			Run: entity.AgentRun{
				VersionedBase:  entity.VersionedBase{ID: runID, Version: 3},
				SessionID:      sessionID,
				RuntimeContext: value.RuntimeContextRef{JobRef: "runtime-job:123"},
				Status:         enum.AgentRunStatusRunning,
				ResultSummary:  "safe runtime summary",
			},
			HumanGateWaiting:    true,
			HumanGateRequestRef: "human-gate:123",
			LatestActivity: &entity.AgentActivitySummary{
				ID:           activityID,
				ActivityKind: enum.AgentActivityKindLifecycle,
				Status:       enum.AgentActivityStatusStarted,
				SafeSummary:  "safe activity",
			},
		}},
		runSummaryPage: value.PageResult{NextPageToken: "next"},
	}
	service := New(Config{Repository: repository})

	items, page, err := service.ListAgentRunSummaries(context.Background(), query.AgentRunSummaryFilter{
		SessionID: sessionID,
		Page:      value.PageRequest{PageSize: 10},
	})
	if err != nil {
		t.Fatalf("ListAgentRunSummaries() err = %v", err)
	}
	if len(items) != 1 || items[0].Run.ID != runID || !items[0].HumanGateWaiting || page.NextPageToken != "next" {
		t.Fatalf("items/page = %+v/%+v", items, page)
	}
	if repository.runSummaryFilter.SessionID != sessionID || repository.runSummaryFilter.Page.PageSize != 10 {
		t.Fatalf("filter = %+v", repository.runSummaryFilter)
	}
}

func TestListSummariesRejectBroadOrInvalidRange(t *testing.T) {
	t.Parallel()

	service := New(Config{Repository: &fakeRepository{}})
	after := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	before := after.Add(-time.Hour)

	if _, _, err := service.ListAgentSessionSummaries(context.Background(), query.AgentSessionFilter{}); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListAgentSessionSummaries() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if _, _, err := service.ListAgentRunSummaries(context.Background(), query.AgentRunSummaryFilter{}); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListAgentRunSummaries() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if _, _, err := service.ListAgentRunSummaries(context.Background(), query.AgentRunSummaryFilter{
		Scope:         value.ScopeRef{Type: "project", Ref: "project:operator"},
		CreatedAfter:  &after,
		CreatedBefore: &before,
	}); !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListAgentRunSummaries() invalid range err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestRequestHumanGateStoresWaitAndOutbox(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 27, 11, 0, 0, 0, time.UTC)
	sessionID := uuid.MustParse("89898989-1111-2222-3333-444444444444")
	runID := uuid.MustParse("89898989-2222-3333-4444-555555555555")
	acceptanceID := uuid.MustParse("89898989-3333-4444-5555-666666666666")
	gateID := uuid.MustParse("89898989-4444-5555-6666-777777777777")
	eventID := uuid.MustParse("89898989-5555-6666-7777-888888888888")
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
		runByID:     map[uuid.UUID]entity.AgentRun{runID: {VersionedBase: entity.VersionedBase{ID: runID, Version: 1}, SessionID: sessionID}},
		acceptanceByID: map[uuid.UUID]entity.AcceptanceResult{acceptanceID: {
			VersionedBase: entity.VersionedBase{ID: acceptanceID, Version: 1},
			SessionID:     sessionID,
			RunID:         &runID,
			CheckKind:     enum.AcceptanceCheckKindHumanGate,
			Status:        enum.AcceptanceStatusWaiting,
		}},
		humanGateByID: map[uuid.UUID]entity.HumanGateRequest{},
	}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: fixedIDGenerator{ids: []uuid.UUID{gateID, eventID}},
	})

	gate, err := service.RequestHumanGate(context.Background(), RequestHumanGateInput{
		Meta:                     value.CommandMeta{IdempotencyKey: "human-gate-1", Actor: testActor()},
		SessionID:                sessionID,
		RunID:                    &runID,
		AcceptanceResultID:       &acceptanceID,
		ProviderTarget:           value.ProviderTargetRef{PullRequestRef: "provider-pr:42"},
		TargetRef:                "artifact:run-summary",
		RequestKind:              "owner_decision",
		ReasonCode:               "needs_owner_approval",
		SafeSummary:              "Review stage needs owner decision",
		InteractionRequestRef:    "interaction:request/42",
		GovernanceGateRequestRef: "governance:gate/42",
		GovernanceContext: value.GovernanceContextRef{
			RiskAssessmentRef:         "governance:risk/42",
			ReleaseDecisionPackageRef: "governance:release-package/42",
			RiskProfileRef:            "governance:risk-profile/default",
			GatePolicyRef:             "governance:gate-policy/default",
			ReleasePolicyRef:          "project-policy:release/default",
		},
	})
	if err != nil {
		t.Fatalf("RequestHumanGate() err = %v", err)
	}
	if gate.ID != gateID || gate.Status != enum.HumanGateStatusWaiting || gate.Outcome != enum.HumanGateOutcomeNone {
		t.Fatalf("gate = %+v", gate)
	}
	if gate.IdempotencyKey != operationRequestHumanGate+":user:owner:human-gate-1" {
		t.Fatalf("idempotency key = %q", gate.IdempotencyKey)
	}
	if gate.GovernanceContext.GateRequestRef != "governance:gate/42" || gate.GovernanceContext.RiskAssessmentRef != "governance:risk/42" {
		t.Fatalf("governance context = %+v", gate.GovernanceContext)
	}
	if repository.humanGateResult.AggregateType != enum.CommandAggregateTypeHumanGate || repository.humanGateResult.AggregateID != gateID {
		t.Fatalf("command result = %+v", repository.humanGateResult)
	}
	if repository.humanGateEvent.EventType != agentevents.EventHumanGateRequested {
		t.Fatalf("event type = %s", repository.humanGateEvent.EventType)
	}
	payload := decodeAgentPayload(t, repository.humanGateEvent)
	if payload.HumanGateRequestID != gateID.String() || payload.InteractionRequestRef != "interaction:request/42" || payload.GovernanceGateRequestRef != "governance:gate/42" {
		t.Fatalf("payload = %+v", payload)
	}
	if payload.GovernanceRiskAssessmentRef != "governance:risk/42" || payload.GovernanceReleaseDecisionPackageRef != "governance:release-package/42" {
		t.Fatalf("payload governance refs = %+v", payload)
	}
}

func TestRequestHumanGateCreatesInteractionRequestWhenEnabled(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 27, 11, 30, 0, 0, time.UTC)
	sessionID := uuid.MustParse("89898989-6666-7777-8888-999999999999")
	runID := uuid.MustParse("89898989-7777-8888-9999-aaaaaaaaaaaa")
	eventID := uuid.MustParse("89898989-8888-9999-aaaa-bbbbbbbbbbbb")
	requester := &fakeHumanGateRequester{
		result: HumanGateInteractionRequestResult{InteractionRequestRef: "interaction:request/ih-1", Status: "waiting", Version: 1, SafeSummary: "Review stage needs owner decision"},
	}
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {
			VersionedBase:     entity.VersionedBase{ID: sessionID, Version: 1},
			Scope:             value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project:alpha"},
			Status:            enum.AgentSessionStatusOpen,
			CreatedByActorRef: "user:owner",
		}},
		runByID:       map[uuid.UUID]entity.AgentRun{runID: {VersionedBase: entity.VersionedBase{ID: runID, Version: 1}, SessionID: sessionID}},
		humanGateByID: map[uuid.UUID]entity.HumanGateRequest{},
	}
	service := New(Config{
		Repository:              repository,
		Clock:                   fixedClock{now: now},
		IDGenerator:             fixedIDGenerator{ids: []uuid.UUID{eventID}},
		HumanGateRequester:      requester,
		HumanGateRequestEnabled: true,
	})

	gate, err := service.RequestHumanGate(context.Background(), RequestHumanGateInput{
		Meta:           value.CommandMeta{IdempotencyKey: "human-gate-interaction", Actor: testActor()},
		SessionID:      sessionID,
		RunID:          &runID,
		ProviderTarget: value.ProviderTargetRef{WorkItemRef: "provider:issue/42"},
		TargetRef:      "artifact:run-summary",
		RequestKind:    "owner_decision",
		ReasonCode:     "needs_owner_approval",
		SafeSummary:    "Review stage needs owner decision",
		GovernanceContext: value.GovernanceContextRef{
			RiskAssessmentRef:         "governance:risk/ih-1",
			GateRequestRef:            "governance:gate/ih-1",
			ReleaseDecisionPackageRef: "governance:release-package/ih-1",
		},
	})
	if err != nil {
		t.Fatalf("RequestHumanGate() err = %v", err)
	}
	if requester.calls != 1 {
		t.Fatalf("requester calls = %d, want 1", requester.calls)
	}
	if gate.InteractionRequestRef != "interaction:request/ih-1" || repository.createdHumanGate.InteractionRequestRef != "interaction:request/ih-1" {
		t.Fatalf("interaction ref gate=%q stored=%q", gate.InteractionRequestRef, repository.createdHumanGate.InteractionRequestRef)
	}
	if requester.last.HumanGateRequestID != gate.ID || requester.last.SourceOwnerRef != "agent:human_gate/"+gate.ID.String() {
		t.Fatalf("requester owner refs = %+v gate=%s", requester.last, gate.ID)
	}
	if len(requester.last.TargetRefs) != 1 || requester.last.TargetRefs[0] != (HumanGateInteractionActorRef{Kind: "user", Ref: "owner"}) {
		t.Fatalf("target refs = %+v", requester.last.TargetRefs)
	}
	if !hasHumanGateContextRef(requester.last.ContextRefs, "agent_session", sessionID.String()) ||
		!hasHumanGateContextRef(requester.last.ContextRefs, "agent_run", runID.String()) ||
		!hasHumanGateContextRef(requester.last.ContextRefs, "provider_work_item", "provider:issue/42") ||
		!hasHumanGateContextRef(requester.last.ContextRefs, "governance_risk_assessment", "governance:risk/ih-1") ||
		!hasHumanGateContextRef(requester.last.ContextRefs, "governance_gate_request", "governance:gate/ih-1") ||
		!hasHumanGateContextRef(requester.last.ContextRefs, "governance_release_decision_package", "governance:release-package/ih-1") {
		t.Fatalf("context refs = %+v", requester.last.ContextRefs)
	}
	if requester.last.GovernanceGateRequestRef != "governance:gate/ih-1" {
		t.Fatalf("request governance ref = %q", requester.last.GovernanceGateRequestRef)
	}
	for _, action := range []enum.HumanGateOutcome{
		enum.HumanGateOutcomeApprove,
		enum.HumanGateOutcomeReject,
		enum.HumanGateOutcomeRequestChanges,
		enum.HumanGateOutcomeAnswer,
	} {
		if !hasHumanGateAction(requester.last.AllowedActions, string(action)) {
			t.Fatalf("allowed actions = %+v, missing %s", requester.last.AllowedActions, action)
		}
	}
	payload := decodeAgentPayload(t, repository.humanGateEvent)
	if payload.InteractionRequestRef != "interaction:request/ih-1" || payload.GovernanceGateRequestRef != "governance:gate/ih-1" {
		t.Fatalf("event payload = %+v", payload)
	}
	resultPayload := string(repository.humanGateResult.ResultPayload)
	if strings.Contains(resultPayload, "raw_provider_payload") || strings.Contains(resultPayload, "transcript") || strings.Contains(resultPayload, "secret") {
		t.Fatalf("command payload contains unsafe marker: %s", resultPayload)
	}
}

func TestRequestHumanGateInteractionReplayDoesNotDuplicateRequest(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("89898989-9999-aaaa-bbbb-cccccccccccc")
	gateID := uuid.MustParse("89898989-aaaa-bbbb-cccc-dddddddddddd")
	gate := entity.HumanGateRequest{
		VersionedBase:         entity.VersionedBase{ID: gateID, Version: 1},
		SessionID:             sessionID,
		RequestKind:           "owner_decision",
		ReasonCode:            "needs_owner_approval",
		SafeSummary:           "Review stage needs owner decision",
		InteractionRequestRef: "interaction:request/replay",
		IdempotencyKey:        operationRequestHumanGate + ":user:owner:human-gate-replay",
		Status:                enum.HumanGateStatusWaiting,
		Outcome:               enum.HumanGateOutcomeNone,
	}
	payload, err := marshalCommandPayload(humanGateCommandPayload{HumanGateRequest: gate})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	requester := &fakeHumanGateRequester{result: HumanGateInteractionRequestResult{InteractionRequestRef: "interaction:request/new"}}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			IdempotencyKey: "human-gate-replay",
			Actor:          testActor(),
			Operation:      operationRequestHumanGate,
			AggregateType:  enum.CommandAggregateTypeHumanGate,
			AggregateID:    gateID,
			ResultPayload:  payload,
		},
		sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {
			VersionedBase:     entity.VersionedBase{ID: sessionID, Version: 1},
			Scope:             value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project:alpha"},
			Status:            enum.AgentSessionStatusOpen,
			CreatedByActorRef: "user:owner",
		}},
		humanGateByID: map[uuid.UUID]entity.HumanGateRequest{gateID: gate},
	}
	service := New(Config{Repository: repository, HumanGateRequester: requester, HumanGateRequestEnabled: true})

	replay, err := service.RequestHumanGate(context.Background(), RequestHumanGateInput{
		Meta:        value.CommandMeta{IdempotencyKey: "human-gate-replay", Actor: testActor()},
		SessionID:   sessionID,
		RequestKind: "owner_decision",
		ReasonCode:  "needs_owner_approval",
		SafeSummary: "Review stage needs owner decision",
	})
	if err != nil {
		t.Fatalf("RequestHumanGate() err = %v", err)
	}
	if replay.ID != gateID || requester.calls != 0 || repository.createHumanGateCalled {
		t.Fatalf("replay=%s requester=%d create=%v", replay.ID, requester.calls, repository.createHumanGateCalled)
	}
}

func TestRequestHumanGateInteractionFailureDoesNotPersistWait(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("89898989-bbbb-cccc-dddd-eeeeeeeeeeee")
	requester := &fakeHumanGateRequester{err: errs.ErrDependencyUnavailable}
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {
			VersionedBase:     entity.VersionedBase{ID: sessionID, Version: 1},
			Scope:             value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project:alpha"},
			Status:            enum.AgentSessionStatusOpen,
			CreatedByActorRef: "user:owner",
		}},
	}
	service := New(Config{Repository: repository, HumanGateRequester: requester, HumanGateRequestEnabled: true})

	_, err := service.RequestHumanGate(context.Background(), RequestHumanGateInput{
		Meta:        value.CommandMeta{IdempotencyKey: "human-gate-failure", Actor: testActor()},
		SessionID:   sessionID,
		RequestKind: "owner_decision",
		ReasonCode:  "needs_owner_approval",
		SafeSummary: "Review stage needs owner decision",
	})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("RequestHumanGate() err = %v, want %v", err, errs.ErrDependencyUnavailable)
	}
	if !requester.called() || repository.createHumanGateCalled {
		t.Fatalf("requester calls=%d create=%v", requester.calls, repository.createHumanGateCalled)
	}
}

func TestRequestHumanGateInteractionDuplicateDoesNotPersistWait(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("89898989-cdcd-dede-efef-010101010101")
	requester := &fakeHumanGateRequester{err: errs.ErrAlreadyExists}
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {
			VersionedBase:     entity.VersionedBase{ID: sessionID, Version: 1},
			Scope:             value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project:alpha"},
			Status:            enum.AgentSessionStatusOpen,
			CreatedByActorRef: "user:owner",
		}},
	}
	service := New(Config{Repository: repository, HumanGateRequester: requester, HumanGateRequestEnabled: true})

	_, err := service.RequestHumanGate(context.Background(), RequestHumanGateInput{
		Meta:        value.CommandMeta{IdempotencyKey: "human-gate-duplicate", Actor: testActor()},
		SessionID:   sessionID,
		RequestKind: "owner_decision",
		ReasonCode:  "needs_owner_approval",
		SafeSummary: "Review stage needs owner decision",
	})
	if !errors.Is(err, errs.ErrAlreadyExists) {
		t.Fatalf("RequestHumanGate() err = %v, want %v", err, errs.ErrAlreadyExists)
	}
	if !requester.called() || repository.createHumanGateCalled {
		t.Fatalf("requester calls=%d create=%v", requester.calls, repository.createHumanGateCalled)
	}
}

func TestRequestHumanGateInteractionRejectsInvalidOwnerRef(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("89898989-cccc-dddd-eeee-ffffffffffff")
	requester := &fakeHumanGateRequester{result: HumanGateInteractionRequestResult{InteractionRequestRef: "interaction:request/invalid"}}
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {
			VersionedBase:     entity.VersionedBase{ID: sessionID, Version: 1},
			Scope:             value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: "project:alpha"},
			Status:            enum.AgentSessionStatusOpen,
			CreatedByActorRef: "owner-without-kind",
		}},
	}
	service := New(Config{Repository: repository, HumanGateRequester: requester, HumanGateRequestEnabled: true})

	_, err := service.RequestHumanGate(context.Background(), RequestHumanGateInput{
		Meta:        value.CommandMeta{IdempotencyKey: "human-gate-invalid-owner", Actor: testActor()},
		SessionID:   sessionID,
		RequestKind: "owner_decision",
		ReasonCode:  "needs_owner_approval",
		SafeSummary: "Review stage needs owner decision",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RequestHumanGate() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if requester.calls != 0 || repository.createHumanGateCalled {
		t.Fatalf("requester calls=%d create=%v", requester.calls, repository.createHumanGateCalled)
	}
}

func TestRequestHumanGateInteractionRejectsInvalidScope(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("89898989-dede-efef-0101-020202020202")
	requester := &fakeHumanGateRequester{result: HumanGateInteractionRequestResult{InteractionRequestRef: "interaction:request/invalid-scope"}}
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {
			VersionedBase:     entity.VersionedBase{ID: sessionID, Version: 1},
			Scope:             value.ScopeRef{Type: "invalid_scope", Ref: "project:alpha"},
			Status:            enum.AgentSessionStatusOpen,
			CreatedByActorRef: "user:owner",
		}},
	}
	service := New(Config{Repository: repository, HumanGateRequester: requester, HumanGateRequestEnabled: true})

	_, err := service.RequestHumanGate(context.Background(), RequestHumanGateInput{
		Meta:        value.CommandMeta{IdempotencyKey: "human-gate-invalid-scope", Actor: testActor()},
		SessionID:   sessionID,
		RequestKind: "owner_decision",
		ReasonCode:  "needs_owner_approval",
		SafeSummary: "Review stage needs owner decision",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RequestHumanGate() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if requester.calls != 0 || repository.createHumanGateCalled {
		t.Fatalf("requester calls=%d create=%v", requester.calls, repository.createHumanGateCalled)
	}
}

func TestRequestHumanGateReplaysSameCommand(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("90909090-1111-2222-3333-444444444444")
	gateID := uuid.MustParse("90909090-2222-3333-4444-555555555555")
	gate := entity.HumanGateRequest{
		VersionedBase:  entity.VersionedBase{ID: gateID, Version: 1},
		SessionID:      sessionID,
		RequestKind:    "owner_decision",
		ReasonCode:     "needs_owner_approval",
		IdempotencyKey: operationRequestHumanGate + ":user:owner:human-gate-replay",
		Status:         enum.HumanGateStatusWaiting,
		Outcome:        enum.HumanGateOutcomeNone,
	}
	payload, err := marshalCommandPayload(humanGateCommandPayload{HumanGateRequest: gate})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			IdempotencyKey: "human-gate-replay",
			Actor:          testActor(),
			Operation:      operationRequestHumanGate,
			AggregateType:  enum.CommandAggregateTypeHumanGate,
			AggregateID:    gateID,
			ResultPayload:  payload,
		},
		sessionByID:   map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
		humanGateByID: map[uuid.UUID]entity.HumanGateRequest{gateID: gate},
	}
	service := New(Config{Repository: repository})

	replay, err := service.RequestHumanGate(context.Background(), RequestHumanGateInput{
		Meta:        value.CommandMeta{IdempotencyKey: "human-gate-replay", Actor: testActor()},
		SessionID:   sessionID,
		RequestKind: "owner_decision",
		ReasonCode:  "needs_owner_approval",
	})
	if err != nil {
		t.Fatalf("RequestHumanGate() err = %v", err)
	}
	if replay.ID != gateID || repository.createHumanGateCalled {
		t.Fatalf("replay/create = %s/%v", replay.ID, repository.createHumanGateCalled)
	}
}

func TestRequestHumanGateRejectsUnsafePayload(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("91919191-1111-2222-3333-444444444444")
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{sessionID: {VersionedBase: entity.VersionedBase{ID: sessionID, Version: 1}, Status: enum.AgentSessionStatusOpen}},
	}
	service := New(Config{Repository: repository})

	_, err := service.RequestHumanGate(context.Background(), RequestHumanGateInput{
		Meta:        value.CommandMeta{IdempotencyKey: "human-gate-unsafe", Actor: testActor()},
		SessionID:   sessionID,
		RequestKind: "owner_decision",
		ReasonCode:  "needs_owner_approval",
		SafeSummary: "raw_provider_payload should not be stored",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RequestHumanGate() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if repository.createHumanGateCalled {
		t.Fatal("CreateHumanGateRequestWithResult called for unsafe payload")
	}
}

func TestRecordHumanGateDecisionStoresOutcomeAndOutbox(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	sessionID := uuid.MustParse("92929292-1111-2222-3333-444444444444")
	gateID := uuid.MustParse("92929292-2222-3333-4444-555555555555")
	eventID := uuid.MustParse("92929292-3333-4444-5555-666666666666")
	gate := entity.HumanGateRequest{
		VersionedBase:            entity.VersionedBase{ID: gateID, Version: 1},
		SessionID:                sessionID,
		RequestKind:              "owner_decision",
		ReasonCode:               "needs_owner_approval",
		InteractionRequestRef:    "interaction:request/42",
		GovernanceGateRequestRef: "governance:gate/42",
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef:    "governance:gate/42",
			RiskAssessmentRef: "governance:risk/42",
		},
		IdempotencyKey: operationRequestHumanGate + ":user:owner:human-gate-decision",
		Status:         enum.HumanGateStatusWaiting,
		Outcome:        enum.HumanGateOutcomeNone,
	}
	repository := &fakeRepository{humanGateByID: map[uuid.UUID]entity.HumanGateRequest{gateID: gate}}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: fixedIDGenerator{ids: []uuid.UUID{eventID}},
	})
	expectedVersion := int64(1)

	resolved, err := service.RecordHumanGateDecision(context.Background(), RecordHumanGateDecisionInput{
		Meta:                     value.CommandMeta{IdempotencyKey: "human-gate-decision", ExpectedVersion: &expectedVersion, Actor: testActor()},
		HumanGateRequestID:       gateID,
		Status:                   enum.HumanGateStatusResolved,
		Outcome:                  enum.HumanGateOutcomeApprove,
		SafeSummary:              "Owner approved the next step",
		InteractionRequestRef:    "interaction:request/42",
		InteractionResponseRef:   "interaction:response/42",
		GovernanceGateRequestRef: "governance:gate/42",
		GovernanceDecisionRef:    "governance:decision/42",
		GovernanceContext: value.GovernanceContextRef{
			RiskAssessmentRef:         "governance:risk/42",
			ReleaseDecisionPackageRef: "governance:release-package/42",
		},
	})
	if err != nil {
		t.Fatalf("RecordHumanGateDecision() err = %v", err)
	}
	if resolved.Status != enum.HumanGateStatusResolved || resolved.Outcome != enum.HumanGateOutcomeApprove || resolved.ResolvedAt == nil {
		t.Fatalf("resolved = %+v", resolved)
	}
	if resolved.Version != 2 || repository.updateHumanGateResult.Operation != operationRecordHumanGateDecision {
		t.Fatalf("update result = %+v", repository.updateHumanGateResult)
	}
	if resolved.GovernanceDecisionRef != "governance:decision/42" || resolved.GovernanceContext.ReleaseDecisionPackageRef != "governance:release-package/42" {
		t.Fatalf("resolved governance context = %+v", resolved.GovernanceContext)
	}
	if repository.updateHumanGateEvent == nil || repository.updateHumanGateEvent.EventType != agentevents.EventHumanGateResolved {
		t.Fatalf("event = %+v", repository.updateHumanGateEvent)
	}
	payload := decodeAgentPayload(t, *repository.updateHumanGateEvent)
	if payload.HumanGateOutcome != string(enum.HumanGateOutcomeApprove) || payload.InteractionResponseRef != "interaction:response/42" || payload.GovernanceDecisionRef != "governance:decision/42" {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestRecordHumanGateDecisionReplaysSameRequestChangesOutcome(t *testing.T) {
	t.Parallel()

	gateID := uuid.MustParse("92929292-3333-4444-5555-666666666666")
	expectedVersion := int64(1)
	resolved := entity.HumanGateRequest{
		VersionedBase:          entity.VersionedBase{ID: gateID, Version: 2},
		RequestKind:            "owner_decision",
		ReasonCode:             "needs_owner_changes",
		SafeSummary:            "Owner requested bounded changes",
		InteractionRequestRef:  "interaction:request/42",
		InteractionResponseRef: "interaction:response/42",
		Status:                 enum.HumanGateStatusResolved,
		Outcome:                enum.HumanGateOutcomeRequestChanges,
	}
	decision := humanGateDecision{
		HumanGateRequestID: gateID.String(),
		Status:             string(enum.HumanGateStatusResolved),
		Outcome:            string(enum.HumanGateOutcomeRequestChanges),
		SafeSummary:        "Owner requested bounded changes",
		humanGateDecisionInteraction: humanGateDecisionInteraction{
			InteractionRequestRef:          "interaction:request/42",
			InteractionResponseRef:         "interaction:response/42",
			InteractionResponseFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			InteractionRequestVersion:      2,
		},
	}
	payload, err := marshalCommandPayload(humanGateCommandPayload{HumanGateRequest: resolved, Decision: &decision})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			IdempotencyKey: "human-gate-request-changes-replay",
			Actor:          testActor(),
			Operation:      operationRecordHumanGateDecision,
			AggregateType:  enum.CommandAggregateTypeHumanGate,
			AggregateID:    gateID,
			ResultPayload:  payload,
		},
		humanGateByID: map[uuid.UUID]entity.HumanGateRequest{gateID: resolved},
	}
	service := New(Config{Repository: repository})

	replay, err := service.RecordHumanGateDecision(context.Background(), RecordHumanGateDecisionInput{
		Meta:                           value.CommandMeta{IdempotencyKey: "human-gate-request-changes-replay", ExpectedVersion: &expectedVersion, Actor: testActor()},
		HumanGateRequestID:             gateID,
		Status:                         enum.HumanGateStatusResolved,
		Outcome:                        enum.HumanGateOutcomeRequestChanges,
		SafeSummary:                    "Owner requested bounded changes",
		InteractionRequestRef:          "interaction:request/42",
		InteractionResponseRef:         "interaction:response/42",
		InteractionResponseFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		InteractionRequestVersion:      2,
	})
	if err != nil {
		t.Fatalf("RecordHumanGateDecision() err = %v", err)
	}
	if replay.ID != gateID || replay.Outcome != enum.HumanGateOutcomeRequestChanges || repository.updateHumanGateCalled {
		t.Fatalf("replay = %+v update=%v", replay, repository.updateHumanGateCalled)
	}
}

func TestRecordHumanGateDecisionRejectsMismatchedRequestRefs(t *testing.T) {
	t.Parallel()

	gateID := uuid.MustParse("92929292-4444-5555-6666-777777777777")
	expectedVersion := int64(1)
	tests := []struct {
		name  string
		input RecordHumanGateDecisionInput
	}{
		{
			name: "interaction request mismatch",
			input: RecordHumanGateDecisionInput{
				Meta:                   value.CommandMeta{IdempotencyKey: "human-gate-interaction-mismatch", ExpectedVersion: &expectedVersion, Actor: testActor()},
				HumanGateRequestID:     gateID,
				Status:                 enum.HumanGateStatusResolved,
				Outcome:                enum.HumanGateOutcomeApprove,
				InteractionRequestRef:  "interaction:request/other",
				InteractionResponseRef: "interaction:response/42",
			},
		},
		{
			name: "governance gate mismatch",
			input: RecordHumanGateDecisionInput{
				Meta:                     value.CommandMeta{IdempotencyKey: "human-gate-governance-mismatch", ExpectedVersion: &expectedVersion, Actor: testActor()},
				HumanGateRequestID:       gateID,
				Status:                   enum.HumanGateStatusResolved,
				Outcome:                  enum.HumanGateOutcomeApprove,
				GovernanceGateRequestRef: "governance:gate/other",
				GovernanceDecisionRef:    "governance:decision/42",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repository := &fakeRepository{humanGateByID: map[uuid.UUID]entity.HumanGateRequest{
				gateID: {
					VersionedBase:            entity.VersionedBase{ID: gateID, Version: 1},
					RequestKind:              "owner_decision",
					ReasonCode:               "needs_owner_approval",
					InteractionRequestRef:    "interaction:request/42",
					GovernanceGateRequestRef: "governance:gate/42",
					Status:                   enum.HumanGateStatusWaiting,
					Outcome:                  enum.HumanGateOutcomeNone,
				},
			}}
			service := New(Config{
				Repository:  repository,
				Clock:       fixedClock{now: time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)},
				IDGenerator: fixedIDGenerator{ids: []uuid.UUID{uuid.MustParse("92929292-5555-6666-7777-888888888888")}},
			})

			_, err := service.RecordHumanGateDecision(context.Background(), test.input)
			if !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("RecordHumanGateDecision() err = %v, want %v", err, errs.ErrConflict)
			}
			if repository.updateHumanGateCalled || repository.updateHumanGateEvent != nil {
				t.Fatalf("update/outbox happened: %v/%+v", repository.updateHumanGateCalled, repository.updateHumanGateEvent)
			}
		})
	}
}

func TestRecordHumanGateDecisionRejectsMissingGovernanceBinding(t *testing.T) {
	t.Parallel()

	gateID := uuid.MustParse("92929292-6666-7777-8888-999999999999")
	expectedVersion := int64(1)
	service := New(Config{Repository: &fakeRepository{}})

	_, err := service.RecordHumanGateDecision(context.Background(), RecordHumanGateDecisionInput{
		Meta:                  value.CommandMeta{IdempotencyKey: "human-gate-governance-missing", ExpectedVersion: &expectedVersion, Actor: testActor()},
		HumanGateRequestID:    gateID,
		Status:                enum.HumanGateStatusResolved,
		Outcome:               enum.HumanGateOutcomeApprove,
		GovernanceDecisionRef: "governance:decision/42",
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RecordHumanGateDecision() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestRecordHumanGateDecisionRejectsVersionConflict(t *testing.T) {
	t.Parallel()

	sessionID := uuid.MustParse("93939393-1111-2222-3333-444444444444")
	gateID := uuid.MustParse("93939393-2222-3333-4444-555555555555")
	gate := entity.HumanGateRequest{
		VersionedBase: entity.VersionedBase{ID: gateID, Version: 1},
		SessionID:     sessionID,
		RequestKind:   "owner_decision",
		ReasonCode:    "needs_owner_approval",
		Status:        enum.HumanGateStatusWaiting,
		Outcome:       enum.HumanGateOutcomeNone,
	}
	repository := &fakeRepository{humanGateByID: map[uuid.UUID]entity.HumanGateRequest{gateID: gate}}
	service := New(Config{Repository: repository})
	expectedVersion := int64(2)

	_, err := service.RecordHumanGateDecision(context.Background(), RecordHumanGateDecisionInput{
		Meta:                   value.CommandMeta{IdempotencyKey: "human-gate-conflict", ExpectedVersion: &expectedVersion, Actor: testActor()},
		HumanGateRequestID:     gateID,
		Status:                 enum.HumanGateStatusResolved,
		Outcome:                enum.HumanGateOutcomeReject,
		InteractionResponseRef: "interaction:response/42",
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordHumanGateDecision() err = %v, want %v", err, errs.ErrConflict)
	}
	if repository.updateHumanGateCalled {
		t.Fatal("UpdateHumanGateRequestWithResult called after version conflict")
	}
}

func TestRecordHumanGateDecisionReplayRejectsConflictingPayload(t *testing.T) {
	t.Parallel()

	gateID := uuid.MustParse("92929292-7777-8888-9999-aaaaaaaaaaaa")
	expectedVersion := int64(1)
	resolved := entity.HumanGateRequest{
		VersionedBase:          entity.VersionedBase{ID: gateID, Version: 2},
		RequestKind:            "owner_decision",
		ReasonCode:             "needs_owner_approval",
		SafeSummary:            "Owner approved the next step",
		InteractionRequestRef:  "interaction:request/42",
		InteractionResponseRef: "interaction:response/42",
		Status:                 enum.HumanGateStatusResolved,
		Outcome:                enum.HumanGateOutcomeApprove,
	}
	decision := humanGateDecision{
		HumanGateRequestID: gateID.String(),
		Status:             string(enum.HumanGateStatusResolved),
		Outcome:            string(enum.HumanGateOutcomeApprove),
		SafeSummary:        "Owner approved the next step",
		humanGateDecisionInteraction: humanGateDecisionInteraction{
			InteractionRequestRef:  "interaction:request/42",
			InteractionResponseRef: "interaction:response/42",
		},
	}
	payload, err := marshalCommandPayload(humanGateCommandPayload{HumanGateRequest: resolved, Decision: &decision})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			IdempotencyKey: "human-gate-decision-replay",
			Actor:          testActor(),
			Operation:      operationRecordHumanGateDecision,
			AggregateType:  enum.CommandAggregateTypeHumanGate,
			AggregateID:    gateID,
			ResultPayload:  payload,
		},
		humanGateByID: map[uuid.UUID]entity.HumanGateRequest{gateID: resolved},
	}
	service := New(Config{
		Repository: repository,
		Clock:      fixedClock{now: time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)},
	})

	_, err = service.RecordHumanGateDecision(context.Background(), RecordHumanGateDecisionInput{
		Meta:                   value.CommandMeta{IdempotencyKey: "human-gate-decision-replay", ExpectedVersion: &expectedVersion, Actor: testActor()},
		HumanGateRequestID:     gateID,
		Status:                 enum.HumanGateStatusResolved,
		Outcome:                enum.HumanGateOutcomeApprove,
		SafeSummary:            "Different approved summary",
		InteractionRequestRef:  "interaction:request/42",
		InteractionResponseRef: "interaction:response/42",
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordHumanGateDecision() err = %v, want %v", err, errs.ErrConflict)
	}
	if repository.updateHumanGateCalled {
		t.Fatal("UpdateHumanGateRequestWithResult called for conflicting replay")
	}
}

func TestRecordHumanGateDecisionReplayRejectsConflictingInteractionFingerprint(t *testing.T) {
	t.Parallel()

	gateID := uuid.MustParse("92929292-aaaa-bbbb-cccc-dddddddddddd")
	expectedVersion := int64(1)
	resolved := entity.HumanGateRequest{
		VersionedBase:          entity.VersionedBase{ID: gateID, Version: 2},
		RequestKind:            "owner_decision",
		ReasonCode:             "needs_owner_approval",
		SafeSummary:            "Owner approved the Human gate response",
		InteractionRequestRef:  "interaction:request/42",
		InteractionResponseRef: "interaction:response/42",
		Status:                 enum.HumanGateStatusResolved,
		Outcome:                enum.HumanGateOutcomeApprove,
	}
	decision := humanGateDecision{
		HumanGateRequestID: gateID.String(),
		Status:             string(enum.HumanGateStatusResolved),
		Outcome:            string(enum.HumanGateOutcomeApprove),
		SafeSummary:        "Owner approved the Human gate response",
		humanGateDecisionInteraction: humanGateDecisionInteraction{
			InteractionRequestRef:          "interaction:request/42",
			InteractionResponseRef:         "interaction:response/42",
			InteractionResponseFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			InteractionRequestVersion:      2,
		},
	}
	payload, err := marshalCommandPayload(humanGateCommandPayload{HumanGateRequest: resolved, Decision: &decision})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			IdempotencyKey: "human-gate-interaction-fingerprint-replay",
			Actor:          testActor(),
			Operation:      operationRecordHumanGateDecision,
			AggregateType:  enum.CommandAggregateTypeHumanGate,
			AggregateID:    gateID,
			ResultPayload:  payload,
		},
		humanGateByID: map[uuid.UUID]entity.HumanGateRequest{gateID: resolved},
	}
	service := New(Config{Repository: repository})

	_, err = service.RecordHumanGateDecision(context.Background(), RecordHumanGateDecisionInput{
		Meta:                           value.CommandMeta{IdempotencyKey: "human-gate-interaction-fingerprint-replay", ExpectedVersion: &expectedVersion, Actor: testActor()},
		HumanGateRequestID:             gateID,
		Status:                         enum.HumanGateStatusResolved,
		Outcome:                        enum.HumanGateOutcomeApprove,
		SafeSummary:                    "Owner approved the Human gate response",
		InteractionRequestRef:          "interaction:request/42",
		InteractionResponseRef:         "interaction:response/42",
		InteractionResponseFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		InteractionRequestVersion:      3,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordHumanGateDecision() err = %v, want %v", err, errs.ErrConflict)
	}
	if repository.updateHumanGateCalled {
		t.Fatal("UpdateHumanGateRequestWithResult called for conflicting replay")
	}
}

func TestCreateSelfDeployPlanStoresPendingSafePlan(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	planID := uuid.MustParse("5f7f3a10-0001-4000-8000-000000000001")
	eventID := uuid.MustParse("5f7f3a10-0002-4000-8000-000000000002")
	repository := &fakeRepository{}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{planID, eventID}},
	})

	plan, err := service.CreateSelfDeployPlan(context.Background(), validSelfDeployPlanInput())
	if err != nil {
		t.Fatalf("CreateSelfDeployPlan() err = %v", err)
	}
	if !repository.createSelfDeployCalled {
		t.Fatal("CreateSelfDeployPlanWithResult was not called")
	}
	if plan.ID != planID || repository.createdSelfDeploy.ID != planID {
		t.Fatalf("plan id = %s, stored = %s, want %s", plan.ID, repository.createdSelfDeploy.ID, planID)
	}
	if plan.Status != enum.SelfDeployPlanStatusPendingApproval {
		t.Fatalf("status = %s, want %s", plan.Status, enum.SelfDeployPlanStatusPendingApproval)
	}
	if repository.selfDeployResult.AggregateType != enum.CommandAggregateTypeSelfDeployPlan {
		t.Fatalf("aggregate type = %s, want %s", repository.selfDeployResult.AggregateType, enum.CommandAggregateTypeSelfDeployPlan)
	}
	if repository.selfDeployEvent.EventType != agentevents.EventSelfDeployPlanRequested {
		t.Fatalf("event type = %s, want %s", repository.selfDeployEvent.EventType, agentevents.EventSelfDeployPlanRequested)
	}
	payload := decodeAgentPayload(t, repository.selfDeployEvent)
	if payload.SelfDeployPlanID != planID.String() || payload.Status != string(enum.SelfDeployPlanStatusPendingApproval) {
		t.Fatalf("event payload = %+v", payload)
	}
	if payload.ExpectedRuntimeJobTypes == nil || strings.Contains(strings.Join(payload.ExpectedRuntimeJobTypes, ","), "agent_run") {
		t.Fatalf("event runtime job types = %v", payload.ExpectedRuntimeJobTypes)
	}
	commandPayload := string(repository.selfDeployResult.ResultPayload)
	for _, forbidden := range []string{"webhook_body", "full_diff", "full_yaml", "raw_provider_payload", "secret_value", "token"} {
		if strings.Contains(commandPayload, forbidden) || strings.Contains(string(repository.selfDeployEvent.Payload), forbidden) {
			t.Fatalf("stored self-deploy data contains forbidden marker %q", forbidden)
		}
	}
}

func TestCreateSelfDeployPlanReplaysSameCommand(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanGateInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0003-4000-8000-000000000003"), "command:"+input.Meta.CommandID.String())
	payload, err := marshalCommandPayload(selfDeployPlanCommandPayload{SelfDeployPlan: plan})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	storedPlan := plan
	storedPlan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	storedPlan.GovernanceContext.GateRequestRef = "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	storedPlan.Version = 2
	commandID := input.Meta.CommandID
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			CommandID:     &commandID,
			Actor:         testActor(),
			Operation:     operationCreateSelfDeployPlan,
			AggregateType: enum.CommandAggregateTypeSelfDeployPlan,
			AggregateID:   plan.ID,
			ResultPayload: payload,
		},
		selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: storedPlan},
	}
	preparer := &fakeSelfDeployGatePreparer{}
	service := New(Config{
		Repository:             repository,
		SelfDeployGatePreparer: preparer,
		SelfDeployGateEnabled:  true,
	})

	replay, err := service.CreateSelfDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSelfDeployPlan() err = %v", err)
	}
	if replay.ID != plan.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, plan.ID)
	}
	if replay.Version != storedPlan.Version || replay.GovernanceContext.GateRequestRef != storedPlan.GovernanceContext.GateRequestRef {
		t.Fatalf("replay = %+v, want current stored plan %+v", replay, storedPlan)
	}
	if repository.createSelfDeployCalled {
		t.Fatal("CreateSelfDeployPlanWithResult called during replay")
	}
	if preparer.calls != 0 {
		t.Fatalf("PrepareSelfDeployPlanGate calls = %d, want 0", preparer.calls)
	}
}

func TestCreateSelfDeployPlanReplayRejectsChangedPayload(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanInput()
	idempotencyKey := "self-deploy-plan"
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0004-4000-8000-000000000004"), idempotencyKey)
	payload, err := marshalCommandPayload(selfDeployPlanCommandPayload{SelfDeployPlan: plan})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	repository := &fakeRepository{
		replay: &entity.CommandResult{
			IdempotencyKey: idempotencyKey,
			Actor:          testActor(),
			Operation:      operationCreateSelfDeployPlan,
			AggregateType:  enum.CommandAggregateTypeSelfDeployPlan,
			AggregateID:    plan.ID,
			ResultPayload:  payload,
		},
		selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
	}
	service := New(Config{Repository: repository})
	changed := input
	changed.Meta.CommandID = uuid.Nil
	changed.Meta.IdempotencyKey = idempotencyKey
	changed.AffectedServiceKeys = []string{"agent-manager", "runtime-manager"}

	_, err = service.CreateSelfDeployPlan(context.Background(), changed)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateSelfDeployPlan() err = %v, want %v", err, errs.ErrConflict)
	}
	if repository.createSelfDeployCalled {
		t.Fatal("CreateSelfDeployPlanWithResult called after rejected replay")
	}
}

func TestCreateSelfDeployPlanFromSignalReplaysExistingSignal(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0005-4000-8000-000000000005"), "command:"+input.Meta.CommandID.String())
	repository := &fakeRepository{selfDeployList: []entity.SelfDeployPlan{plan}}
	service := New(Config{Repository: repository})
	replayInput := input
	replayInput.Meta.CommandID = uuid.MustParse("5f7f3a10-0006-4000-8000-000000000006")

	replay, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: replayInput})
	if err != nil {
		t.Fatalf("CreateSelfDeployPlanFromSignal() err = %v", err)
	}
	if replay.ID != plan.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, plan.ID)
	}
	if repository.createSelfDeployCalled {
		t.Fatal("CreateSelfDeployPlanWithResult called during signal replay")
	}
	if repository.selfDeployFilter.ProviderSignalRef != input.ProviderSignalRef {
		t.Fatalf("provider signal filter = %q, want %q", repository.selfDeployFilter.ProviderSignalRef, input.ProviderSignalRef)
	}
}

func TestCreateSelfDeployPlanFromSignalPreparesGovernanceGate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	planID := uuid.MustParse("5f7f3a10-0010-4000-8000-000000000010")
	createEventID := uuid.MustParse("5f7f3a10-0011-4000-8000-000000000011")
	updateEventID := uuid.MustParse("5f7f3a10-0012-4000-8000-000000000012")
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{}}
	preparer := &fakeSelfDeployGatePreparer{
		result: SelfDeployPlanGatePreparationResult{
			Status: SelfDeployPlanGateStatusPending,
			GovernanceContext: value.GovernanceContextRef{
				RiskAssessmentRef: "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
				GateRequestRef:    "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb",
			},
			SafeSummary: "owner approval required for self-deploy",
		},
	}
	service := New(Config{
		Repository:             repository,
		Clock:                  fixedClock{now: now},
		IDGenerator:            &sequenceIDGenerator{ids: []uuid.UUID{planID, createEventID, updateEventID}},
		SelfDeployGatePreparer: preparer,
		SelfDeployGateEnabled:  true,
	})
	input := validSelfDeployPlanGateInput()

	plan, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: input})
	if err != nil {
		t.Fatalf("CreateSelfDeployPlanFromSignal() err = %v", err)
	}
	if preparer.calls != 1 {
		t.Fatalf("PrepareSelfDeployPlanGate calls = %d, want 1", preparer.calls)
	}
	if preparer.last.Plan.ID != planID || preparer.last.Meta.IdempotencyKey != "self_deploy_plan_gate:"+planID.String() {
		t.Fatalf("gate input = %+v", preparer.last)
	}
	if !repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult was not called")
	}
	if plan.Version != 2 || plan.Status != enum.SelfDeployPlanStatusPendingApproval {
		t.Fatalf("plan version/status = %d/%s", plan.Version, plan.Status)
	}
	if plan.GovernanceContext.GateRequestRef != "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb" {
		t.Fatalf("gate request ref = %q", plan.GovernanceContext.GateRequestRef)
	}
	if repository.updateSelfDeployResult.Operation != operationPrepareSelfDeployPlanGate {
		t.Fatalf("operation = %q, want %q", repository.updateSelfDeployResult.Operation, operationPrepareSelfDeployPlanGate)
	}
	if repository.updateSelfDeployEvent == nil || repository.updateSelfDeployEvent.EventType != agentevents.EventSelfDeployPlanRequested {
		t.Fatalf("update event = %+v", repository.updateSelfDeployEvent)
	}
	payload := decodeAgentPayload(t, *repository.updateSelfDeployEvent)
	if payload.GovernanceGateRequestRef != plan.GovernanceContext.GateRequestRef ||
		payload.GovernanceRiskAssessmentRef != plan.GovernanceContext.RiskAssessmentRef ||
		payload.Status != string(enum.SelfDeployPlanStatusPendingApproval) {
		t.Fatalf("event payload = %+v", payload)
	}
	storedPayload := string(repository.updateSelfDeployResult.ResultPayload) + string(repository.updateSelfDeployEvent.Payload)
	for _, forbidden := range []string{"webhook_body", "raw_provider_payload", "full_yaml", "secret_value", "token", "validated_payload"} {
		if strings.Contains(storedPayload, forbidden) {
			t.Fatalf("stored gate data contains forbidden marker %q", forbidden)
		}
	}
}

func TestCreateSelfDeployPlanFromSignalReusesPreparedGovernanceGate(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0013-4000-8000-000000000013"), "command:"+input.Meta.CommandID.String())
	plan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	repository := &fakeRepository{
		selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
		selfDeployList: []entity.SelfDeployPlan{plan},
	}
	preparer := &fakeSelfDeployGatePreparer{}
	service := New(Config{
		Repository:             repository,
		SelfDeployGatePreparer: preparer,
		SelfDeployGateEnabled:  true,
	})
	replayInput := input
	replayInput.Meta.CommandID = uuid.MustParse("5f7f3a10-0014-4000-8000-000000000014")

	replay, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: replayInput})
	if err != nil {
		t.Fatalf("CreateSelfDeployPlanFromSignal() err = %v", err)
	}
	if replay.ID != plan.ID || replay.GovernanceContext.GateRequestRef != plan.GovernanceContext.GateRequestRef {
		t.Fatalf("replay = %+v, want prepared plan %+v", replay, plan)
	}
	if preparer.calls != 0 {
		t.Fatalf("PrepareSelfDeployPlanGate calls = %d, want 0", preparer.calls)
	}
	if repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult called during prepared replay")
	}
}

func TestEnsureSelfDeployPlanGovernanceGatePreparesExistingPendingPlan(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanGateInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0041-4000-8000-000000000041"), "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusPendingApproval
	plan.GovernanceContext.RiskAssessmentRef = ""
	plan.GovernanceContext.GateRequestRef = ""
	repository := &fakeRepository{
		selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
	}
	preparer := &fakeSelfDeployGatePreparer{
		result: SelfDeployPlanGatePreparationResult{
			Status: SelfDeployPlanGateStatusPending,
			GovernanceContext: value.GovernanceContextRef{
				RiskAssessmentRef: "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
				GateRequestRef:    "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb",
			},
			SafeSummary: "owner approval required for existing self-deploy plan",
		},
	}
	service := New(Config{
		Repository:             repository,
		SelfDeployGatePreparer: preparer,
		SelfDeployGateEnabled:  true,
	})

	ensured, err := service.EnsureSelfDeployPlanGovernanceGate(context.Background(), EnsureSelfDeployPlanGovernanceGateInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-existing-self-deploy-gate", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanGovernanceGate() err = %v", err)
	}
	if preparer.calls != 1 {
		t.Fatalf("PrepareSelfDeployPlanGate calls = %d, want 1", preparer.calls)
	}
	if !repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult was not called")
	}
	if ensured.Version != plan.Version+1 || ensured.GovernanceContext.GateRequestRef == "" || ensured.GovernanceContext.RiskAssessmentRef == "" {
		t.Fatalf("ensured plan = %+v", ensured)
	}
	if repository.updateSelfDeployResult.Operation != operationPrepareSelfDeployPlanGate {
		t.Fatalf("operation = %q, want %q", repository.updateSelfDeployResult.Operation, operationPrepareSelfDeployPlanGate)
	}

	repository.updateSelfDeployCalled = false
	replayed, err := service.EnsureSelfDeployPlanGovernanceGate(context.Background(), EnsureSelfDeployPlanGovernanceGateInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-existing-self-deploy-gate-again", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanGovernanceGate() replay err = %v", err)
	}
	if replayed.GovernanceContext.GateRequestRef != ensured.GovernanceContext.GateRequestRef {
		t.Fatalf("replayed gate ref = %q, want %q", replayed.GovernanceContext.GateRequestRef, ensured.GovernanceContext.GateRequestRef)
	}
	if preparer.calls != 1 || repository.updateSelfDeployCalled {
		t.Fatalf("replay calls/update = %d/%v, want 1/false", preparer.calls, repository.updateSelfDeployCalled)
	}
}

func TestEnsureSelfDeployPlanGovernanceGateRecoversResolvedGateAndDispatchesBuild(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0049-4000-8000-000000000049"), "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusPendingApproval
	plan.GovernanceContext.RiskAssessmentRef = ""
	plan.GovernanceContext.GateRequestRef = ""
	plan.GovernanceContext.GateDecisionRef = ""
	repository := &fakeRepository{
		selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
	}
	preparer := &fakeSelfDeployGatePreparer{
		result: SelfDeployPlanGatePreparationResult{
			Status: SelfDeployPlanGateStatusApproved,
			GovernanceContext: value.GovernanceContextRef{
				RiskAssessmentRef: "governance:risk_assessment/59c67c5d-847b-4296-9afa-25aa3028a313",
				GateRequestRef:    "governance:gate_request/82ec64f2-ad76-4188-9058-0df058f5a5f5",
				GateDecisionRef:   "governance:gate_decision/cccccccc-3333-4333-8333-cccccccccccc",
			},
			SafeSummary: "owner approved self-deploy build",
		},
	}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployGatePreparer:         preparer,
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	ensured, err := service.EnsureSelfDeployPlanGovernanceGate(context.Background(), EnsureSelfDeployPlanGovernanceGateInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-resolved-self-deploy-gate", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanGovernanceGate() err = %v", err)
	}
	if ensured.Status != enum.SelfDeployPlanStatusApproved || ensured.GovernanceContext.GateDecisionRef == "" {
		t.Fatalf("ensured status/context = %s/%+v, want approved with decision", ensured.Status, ensured.GovernanceContext)
	}
	if ensured.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusRequested ||
		len(ensured.RuntimeBuildJobs) != 1 ||
		ensured.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/build-agent-manager" {
		t.Fatalf("runtime build state = %s/%+v, want requested job", ensured.RuntimeBuildStatus, ensured.RuntimeBuildJobs)
	}
	if preparer.calls != 1 || buildReader.calls != 1 || buildCreator.calls != 1 {
		t.Fatalf("calls preparer/build reader/build creator = %d/%d/%d, want 1/1/1", preparer.calls, buildReader.calls, buildCreator.calls)
	}
}

func TestEnsureSelfDeployPlanGovernanceGateRetriesResolvedGateAfterPendingRefRace(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0054-4000-8000-000000000054"), "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusPendingApproval
	plan.GovernanceContext.RiskAssessmentRef = ""
	plan.GovernanceContext.GateRequestRef = ""
	plan.GovernanceContext.GateDecisionRef = ""
	fresh := plan
	fresh.Version = plan.Version + 1
	fresh.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/59c67c5d-847b-4296-9afa-25aa3028a313"
	fresh.GovernanceContext.GateRequestRef = "governance:gate_request/82ec64f2-ad76-4188-9058-0df058f5a5f5"
	var repository *fakeRepository
	repository = &fakeRepository{
		selfDeployByID:       map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
		updateSelfDeployErrs: []error{errs.ErrConflict},
		updateSelfDeployOnErr: func() {
			repository.selfDeployByID[plan.ID] = fresh
		},
	}
	preparer := &fakeSelfDeployGatePreparer{
		result: SelfDeployPlanGatePreparationResult{
			Status: SelfDeployPlanGateStatusApproved,
			GovernanceContext: value.GovernanceContextRef{
				RiskAssessmentRef: fresh.GovernanceContext.RiskAssessmentRef,
				GateRequestRef:    fresh.GovernanceContext.GateRequestRef,
				GateDecisionRef:   "governance:gate_decision/cccccccc-3333-4333-8333-cccccccccccc",
			},
			SafeSummary: "owner approved self-deploy build",
		},
	}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployGatePreparer:         preparer,
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	ensured, err := service.EnsureSelfDeployPlanGovernanceGate(context.Background(), EnsureSelfDeployPlanGovernanceGateInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-resolved-self-deploy-gate-race", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanGovernanceGate() err = %v", err)
	}
	if ensured.Status != enum.SelfDeployPlanStatusApproved ||
		ensured.GovernanceContext.GateRequestRef != fresh.GovernanceContext.GateRequestRef ||
		ensured.GovernanceContext.GateDecisionRef != "governance:gate_decision/cccccccc-3333-4333-8333-cccccccccccc" {
		t.Fatalf("ensured plan = %+v, want approved with recovered decision refs", ensured)
	}
	if ensured.Version <= fresh.Version {
		t.Fatalf("ensured version = %d, want retry write above fresh version %d", ensured.Version, fresh.Version)
	}
	if ensured.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusRequested ||
		len(ensured.RuntimeBuildJobs) != 1 ||
		ensured.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/build-agent-manager" {
		t.Fatalf("runtime build state = %s/%+v, want requested job", ensured.RuntimeBuildStatus, ensured.RuntimeBuildJobs)
	}
	if len(repository.updateSelfDeployErrs) != 0 || buildReader.calls != 1 || buildCreator.calls != 1 {
		t.Fatalf("remaining update errors/build calls = %d/%d/%d, want 0/1/1", len(repository.updateSelfDeployErrs), buildReader.calls, buildCreator.calls)
	}
}

func TestEnsureSelfDeployPlanGovernanceGateReportsGatePrepareFailure(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanGateInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0042-4000-8000-000000000042"), "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusPendingApproval
	plan.GovernanceContext.RiskAssessmentRef = ""
	plan.GovernanceContext.GateRequestRef = ""
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	preparer := &fakeSelfDeployGatePreparer{err: errs.ErrDependencyUnavailable}
	service := New(Config{
		Repository:             repository,
		SelfDeployGatePreparer: preparer,
		SelfDeployGateEnabled:  true,
	})

	_, err := service.EnsureSelfDeployPlanGovernanceGate(context.Background(), EnsureSelfDeployPlanGovernanceGateInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-existing-self-deploy-gate-risk-failure", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("EnsureSelfDeployPlanGovernanceGate() err = %v, want %v", err, errs.ErrDependencyUnavailable)
	}
	if code := SelfDeployGateRecoveryErrorCode(err); code != SelfDeployGateRecoveryCodeGatePrepareFailed {
		t.Fatalf("recovery code = %q, want %q", code, SelfDeployGateRecoveryCodeGatePrepareFailed)
	}
	if repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult called after risk failure")
	}
}

func TestEnsureSelfDeployPlanGovernanceGateReportsPlanGovernanceRefsUpdateFailure(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanGateInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0043-4000-8000-000000000043"), "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusPendingApproval
	plan.GovernanceContext.RiskAssessmentRef = ""
	plan.GovernanceContext.GateRequestRef = ""
	repository := &fakeRepository{
		selfDeployByID:      map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
		updateSelfDeployErr: errs.ErrConflict,
	}
	preparer := &fakeSelfDeployGatePreparer{
		result: SelfDeployPlanGatePreparationResult{
			Status: SelfDeployPlanGateStatusPending,
			GovernanceContext: value.GovernanceContextRef{
				RiskAssessmentRef: "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
				GateRequestRef:    "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb",
			},
			SafeSummary: "owner approval required for existing self-deploy plan",
		},
	}
	service := New(Config{
		Repository:             repository,
		SelfDeployGatePreparer: preparer,
		SelfDeployGateEnabled:  true,
	})

	_, err := service.EnsureSelfDeployPlanGovernanceGate(context.Background(), EnsureSelfDeployPlanGovernanceGateInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-existing-self-deploy-gate-update-failure", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("EnsureSelfDeployPlanGovernanceGate() err = %v, want %v", err, errs.ErrConflict)
	}
	if code := SelfDeployGateRecoveryErrorCode(err); code != SelfDeployGateRecoveryCodePlanGovernanceRefsUpdateFailed {
		t.Fatalf("recovery code = %q, want %q", code, SelfDeployGateRecoveryCodePlanGovernanceRefsUpdateFailed)
	}
}

func TestRecordSelfDeployPlanGateDecisionApprovesAndDispatchesBuild(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0044-4000-8000-000000000044"), "command:"+input.Meta.CommandID.String())
	plan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	runtimeReader := &fakeSelfDeployRuntimeJobReader{result: SelfDeployRuntimeJobReadResult{JobRef: "runtime:job/existing-build", JobType: enum.SelfDeployRuntimeJobTypeBuild, Status: RuntimeJobStatusPending}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployRuntimeJobReader:     runtimeReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})
	expectedVersion := plan.Version

	approved, err := service.RecordSelfDeployPlanGateDecision(context.Background(), RecordSelfDeployPlanGateDecisionInput{
		Meta: value.CommandMeta{
			IdempotencyKey:  "gate-decision-approved",
			ExpectedVersion: &expectedVersion,
			Actor:           testActor(),
		},
		SelfDeployPlanID: plan.ID,
		GateRequestRef:   plan.GovernanceContext.GateRequestRef,
		GateDecisionRef:  "governance:gate_decision/cccccccc-cccc-4ccc-cccc-cccccccccccc",
		Outcome:          SelfDeployPlanGateDecisionOutcomeApprove,
		SafeSummary:      "owner approved self-deploy build",
	})
	if err != nil {
		t.Fatalf("RecordSelfDeployPlanGateDecision() err = %v", err)
	}
	if approved.Status != enum.SelfDeployPlanStatusApproved || approved.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusRequested {
		t.Fatalf("statuses = %s/%s, want approved/requested", approved.Status, approved.RuntimeBuildStatus)
	}
	if approved.GovernanceContext.GateDecisionRef != "governance:gate_decision/cccccccc-cccc-4ccc-cccc-cccccccccccc" {
		t.Fatalf("gate decision ref = %q", approved.GovernanceContext.GateDecisionRef)
	}
	if len(approved.RuntimeBuildJobs) != 1 || approved.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/build-agent-manager" {
		t.Fatalf("runtime build jobs = %+v", approved.RuntimeBuildJobs)
	}
	if buildReader.calls != 1 || buildCreator.calls != 1 {
		t.Fatalf("build reader/creator calls = %d/%d, want 1/1", buildReader.calls, buildCreator.calls)
	}
	if strings.Contains(approved.SafeSummary+approved.RuntimeBuildSummary, "raw_provider_payload") {
		t.Fatalf("stored summaries leaked unsafe marker: %q / %q", approved.SafeSummary, approved.RuntimeBuildSummary)
	}
}

func TestRecordSelfDeployPlanGateDecisionBackfillsMissingGateRefAndDispatchesBuild(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0052-4000-8000-000000000052"), "command:"+input.Meta.CommandID.String())
	plan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateRequestRef = ""
	plan.GovernanceContext.GateDecisionRef = ""
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})
	expectedVersion := plan.Version

	approved, err := service.RecordSelfDeployPlanGateDecision(context.Background(), RecordSelfDeployPlanGateDecisionInput{
		Meta: value.CommandMeta{
			IdempotencyKey:  "gate-decision-approved-missing-gate-ref",
			ExpectedVersion: &expectedVersion,
			Actor:           testActor(),
		},
		SelfDeployPlanID: plan.ID,
		GateRequestRef:   "governance:gate_request/82ec64f2-ad76-4188-9058-0df058f5a5f5",
		GateDecisionRef:  "governance:gate_decision/cccccccc-3333-4333-8333-cccccccccccc",
		Outcome:          SelfDeployPlanGateDecisionOutcomeApprove,
		SafeSummary:      "owner approved self-deploy build",
	})
	if err != nil {
		t.Fatalf("RecordSelfDeployPlanGateDecision() err = %v", err)
	}
	if approved.Status != enum.SelfDeployPlanStatusApproved ||
		approved.GovernanceContext.GateRequestRef != "governance:gate_request/82ec64f2-ad76-4188-9058-0df058f5a5f5" ||
		approved.GovernanceContext.GateDecisionRef != "governance:gate_decision/cccccccc-3333-4333-8333-cccccccccccc" {
		t.Fatalf("approved plan = %+v", approved)
	}
	if approved.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusRequested ||
		len(approved.RuntimeBuildJobs) != 1 ||
		approved.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/build-agent-manager" {
		t.Fatalf("runtime build state = %s/%+v, want requested job", approved.RuntimeBuildStatus, approved.RuntimeBuildJobs)
	}
	if buildReader.calls != 1 || buildCreator.calls != 1 {
		t.Fatalf("build reader/creator calls = %d/%d, want 1/1", buildReader.calls, buildCreator.calls)
	}
}

func TestRecordSelfDeployPlanGateDecisionRetriesAfterPendingGateRefRace(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0053-4000-8000-000000000053"), "command:"+input.Meta.CommandID.String())
	plan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateRequestRef = ""
	plan.GovernanceContext.GateDecisionRef = ""
	fresh := plan
	fresh.Version = plan.Version + 1
	fresh.GovernanceContext.GateRequestRef = "governance:gate_request/82ec64f2-ad76-4188-9058-0df058f5a5f5"
	var repository *fakeRepository
	repository = &fakeRepository{
		selfDeployByID:       map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
		updateSelfDeployErrs: []error{errs.ErrConflict},
		updateSelfDeployOnErr: func() {
			repository.selfDeployByID[plan.ID] = fresh
		},
	}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})
	expectedVersion := plan.Version

	approved, err := service.RecordSelfDeployPlanGateDecision(context.Background(), RecordSelfDeployPlanGateDecisionInput{
		Meta: value.CommandMeta{
			IdempotencyKey:  "gate-decision-approved-pending-ref-race",
			ExpectedVersion: &expectedVersion,
			Actor:           testActor(),
		},
		SelfDeployPlanID: plan.ID,
		GateRequestRef:   fresh.GovernanceContext.GateRequestRef,
		GateDecisionRef:  "governance:gate_decision/cccccccc-3333-4333-8333-cccccccccccc",
		Outcome:          SelfDeployPlanGateDecisionOutcomeApprove,
		SafeSummary:      "owner approved self-deploy build",
	})
	if err != nil {
		t.Fatalf("RecordSelfDeployPlanGateDecision() err = %v", err)
	}
	if approved.Status != enum.SelfDeployPlanStatusApproved ||
		approved.GovernanceContext.GateRequestRef != fresh.GovernanceContext.GateRequestRef ||
		approved.GovernanceContext.GateDecisionRef != "governance:gate_decision/cccccccc-3333-4333-8333-cccccccccccc" {
		t.Fatalf("approved plan = %+v", approved)
	}
	if approved.Version <= fresh.Version {
		t.Fatalf("approved version = %d, want retry write above fresh version %d", approved.Version, fresh.Version)
	}
	if approved.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusRequested ||
		len(approved.RuntimeBuildJobs) != 1 {
		t.Fatalf("runtime build state = %s/%+v, want requested job", approved.RuntimeBuildStatus, approved.RuntimeBuildJobs)
	}
	if len(repository.updateSelfDeployErrs) != 0 || buildReader.calls != 1 || buildCreator.calls != 1 {
		t.Fatalf("remaining update errors/build calls = %d/%d/%d, want 0/1/1", len(repository.updateSelfDeployErrs), buildReader.calls, buildCreator.calls)
	}
}

func TestRecordSelfDeployPlanGateDecisionRejectsWithoutBuild(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0045-4000-8000-000000000045"), "command:"+input.Meta.CommandID.String())
	plan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	rejected, err := service.RecordSelfDeployPlanGateDecision(context.Background(), RecordSelfDeployPlanGateDecisionInput{
		Meta:             value.CommandMeta{IdempotencyKey: "gate-decision-rejected", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
		GateRequestRef:   plan.GovernanceContext.GateRequestRef,
		GateDecisionRef:  "governance:gate_decision/dddddddd-dddd-4ddd-dddd-dddddddddddd",
		Outcome:          SelfDeployPlanGateDecisionOutcomeReject,
		SafeSummary:      "owner rejected self-deploy plan",
	})
	if err != nil {
		t.Fatalf("RecordSelfDeployPlanGateDecision() err = %v", err)
	}
	if rejected.Status != enum.SelfDeployPlanStatusRejected || rejected.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusNotRequested {
		t.Fatalf("statuses = %s/%s, want rejected/not_requested", rejected.Status, rejected.RuntimeBuildStatus)
	}
	if buildReader.calls != 0 || buildCreator.calls != 0 {
		t.Fatalf("build reader/creator calls = %d/%d, want 0/0", buildReader.calls, buildCreator.calls)
	}
	if rejected.SafeSummary != "owner rejected self-deploy plan" {
		t.Fatalf("safe summary = %q", rejected.SafeSummary)
	}
}

func TestRecordSelfDeployPlanGateDecisionRequestChangesFailsClosed(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0046-4000-8000-000000000046"), "command:"+input.Meta.CommandID.String())
	plan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()},
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	changes, err := service.RecordSelfDeployPlanGateDecision(context.Background(), RecordSelfDeployPlanGateDecisionInput{
		Meta:             value.CommandMeta{IdempotencyKey: "gate-decision-request-changes", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
		GateRequestRef:   plan.GovernanceContext.GateRequestRef,
		GateDecisionRef:  "governance:gate_decision/eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee",
		Outcome:          SelfDeployPlanGateDecisionOutcomeRequestChanges,
		SafeSummary:      "owner requested bounded changes",
	})
	if err != nil {
		t.Fatalf("RecordSelfDeployPlanGateDecision() err = %v", err)
	}
	if changes.Status != enum.SelfDeployPlanStatusRejected || changes.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusNotRequested {
		t.Fatalf("statuses = %s/%s, want terminal non-approved without build", changes.Status, changes.RuntimeBuildStatus)
	}
	if buildCreator.calls != 0 {
		t.Fatalf("build creator calls = %d, want 0", buildCreator.calls)
	}
	if changes.SafeSummary != "owner requested bounded changes" {
		t.Fatalf("safe summary = %q", changes.SafeSummary)
	}
}

func TestRecordSelfDeployPlanGateDecisionReplaysExistingDecision(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0047-4000-8000-000000000047"), "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/cccccccc-cccc-4ccc-cccc-cccccccccccc"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusRequested
	plan.RuntimeBuildJobs = []entity.SelfDeployRuntimeBuildJob{{ServiceKey: "agent-manager", RuntimeJobRef: "runtime:job/existing-build", RuntimeJobStatus: "pending"}}
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	runtimeReader := &fakeSelfDeployRuntimeJobReader{result: SelfDeployRuntimeJobReadResult{JobRef: "runtime:job/existing-build", JobType: enum.SelfDeployRuntimeJobTypeBuild, Status: RuntimeJobStatusPending}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()},
		SelfDeployRuntimeJobReader:     runtimeReader,
		SelfDeployBuildJobCreator:      &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/new-build", Status: "pending"}},
		SelfDeployBuildDispatchEnabled: true,
	})

	replayed, err := service.RecordSelfDeployPlanGateDecision(context.Background(), RecordSelfDeployPlanGateDecisionInput{
		Meta:             value.CommandMeta{IdempotencyKey: "gate-decision-approved-replay", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
		GateRequestRef:   plan.GovernanceContext.GateRequestRef,
		GateDecisionRef:  plan.GovernanceContext.GateDecisionRef,
		Outcome:          SelfDeployPlanGateDecisionOutcomeApprove,
		SafeSummary:      "owner approved self-deploy build",
	})
	if err != nil {
		t.Fatalf("RecordSelfDeployPlanGateDecision() err = %v", err)
	}
	if replayed.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/existing-build" {
		t.Fatalf("runtime build jobs = %+v, want existing", replayed.RuntimeBuildJobs)
	}
	if repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult called during replay")
	}
}

func TestRecordSelfDeployPlanGateDecisionReplayRejectsChangedPayload(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0050-4000-8000-000000000050"), "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/cccccccc-cccc-4ccc-cccc-cccccccccccc"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusRequested
	plan.RuntimeBuildJobs = []entity.SelfDeployRuntimeBuildJob{{ServiceKey: "agent-manager", RuntimeJobRef: "runtime:job/existing-build"}}
	replayInput := RecordSelfDeployPlanGateDecisionInput{
		Meta:             value.CommandMeta{IdempotencyKey: "gate-decision-conflicting-replay", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
		GateRequestRef:   plan.GovernanceContext.GateRequestRef,
		GateDecisionRef:  plan.GovernanceContext.GateDecisionRef,
		Outcome:          SelfDeployPlanGateDecisionOutcomeApprove,
		SafeSummary:      "owner approved self-deploy build",
	}
	replayResult, err := selfDeployGateDecisionResult(replayInput)
	if err != nil {
		t.Fatalf("selfDeployGateDecisionResult() err = %v", err)
	}
	replayDecision, err := selfDeployGateDecisionCommand(replayInput, replayResult)
	if err != nil {
		t.Fatalf("selfDeployGateDecisionCommand() err = %v", err)
	}
	payload, err := marshalCommandPayload(selfDeployPlanCommandPayload{SelfDeployPlan: plan, GateDecision: &replayDecision})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	for _, tc := range []struct {
		name   string
		change func(*RecordSelfDeployPlanGateDecisionInput)
	}{
		{
			name: "changed outcome",
			change: func(input *RecordSelfDeployPlanGateDecisionInput) {
				input.Outcome = SelfDeployPlanGateDecisionOutcomeReject
				input.SafeSummary = "owner rejected self-deploy plan"
			},
		},
		{
			name: "changed target",
			change: func(input *RecordSelfDeployPlanGateDecisionInput) {
				input.SelfDeployPlanID = uuid.MustParse("5f7f3a10-0051-4000-8000-000000000051")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &fakeRepository{
				replay: &entity.CommandResult{
					IdempotencyKey: replayInput.Meta.IdempotencyKey,
					Actor:          testActor(),
					Operation:      operationRecordSelfDeployGateDecision,
					AggregateType:  enum.CommandAggregateTypeSelfDeployPlan,
					AggregateID:    plan.ID,
					ResultPayload:  payload,
				},
				selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
			}
			buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/new-build", Status: "pending"}}
			service := New(Config{
				Repository:                     repository,
				SelfDeployBuildPlanReader:      &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()},
				SelfDeployBuildJobCreator:      buildCreator,
				SelfDeployBuildDispatchEnabled: true,
			})
			conflicting := replayInput
			tc.change(&conflicting)

			_, err := service.RecordSelfDeployPlanGateDecision(context.Background(), conflicting)
			if !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("RecordSelfDeployPlanGateDecision() err = %v, want %v", err, errs.ErrConflict)
			}
			if repository.updateSelfDeployCalled {
				t.Fatal("UpdateSelfDeployPlanWithResult called for conflicting replay")
			}
			if buildCreator.calls != 0 {
				t.Fatalf("build creator calls = %d, want 0", buildCreator.calls)
			}
		})
	}
}

func TestRecordSelfDeployPlanGateDecisionRejectsMismatchedGateRequest(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0048-4000-8000-000000000048"), "command:"+input.Meta.CommandID.String())
	plan.GovernanceContext.RiskAssessmentRef = "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	service := New(Config{Repository: repository})

	_, err := service.RecordSelfDeployPlanGateDecision(context.Background(), RecordSelfDeployPlanGateDecisionInput{
		Meta:             value.CommandMeta{IdempotencyKey: "gate-decision-mismatch", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
		GateRequestRef:   "governance:gate_request/ffffffff-ffff-4fff-ffff-ffffffffffff",
		GateDecisionRef:  "governance:gate_decision/cccccccc-cccc-4ccc-cccc-cccccccccccc",
		Outcome:          SelfDeployPlanGateDecisionOutcomeApprove,
		SafeSummary:      "owner approved self-deploy build",
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("RecordSelfDeployPlanGateDecision() err = %v, want %v", err, errs.ErrConflict)
	}
	if repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult called for mismatched gate request")
	}
}

func TestCreateSelfDeployPlanFromSignalReportsGovernanceGateFailure(t *testing.T) {
	t.Parallel()

	planID := uuid.MustParse("5f7f3a10-0015-4000-8000-000000000015")
	eventID := uuid.MustParse("5f7f3a10-0016-4000-8000-000000000016")
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{}}
	preparer := &fakeSelfDeployGatePreparer{err: errs.ErrDependencyUnavailable}
	service := New(Config{
		Repository:             repository,
		IDGenerator:            &sequenceIDGenerator{ids: []uuid.UUID{planID, eventID}},
		SelfDeployGatePreparer: preparer,
		SelfDeployGateEnabled:  true,
	})
	input := validSelfDeployPlanGateInput()

	_, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: input})
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("CreateSelfDeployPlanFromSignal() err = %v, want %v", err, errs.ErrDependencyUnavailable)
	}
	if !repository.createSelfDeployCalled {
		t.Fatal("CreateSelfDeployPlanWithResult was not called before gate failure")
	}
	if repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult called after gate failure")
	}
}

func TestCreateSelfDeployPlanMapsApprovedGovernanceGate(t *testing.T) {
	t.Parallel()

	planID := uuid.MustParse("5f7f3a10-0017-4000-8000-000000000017")
	createEventID := uuid.MustParse("5f7f3a10-0018-4000-8000-000000000018")
	updateEventID := uuid.MustParse("5f7f3a10-0019-4000-8000-000000000019")
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{}}
	preparer := &fakeSelfDeployGatePreparer{
		result: SelfDeployPlanGatePreparationResult{
			Status: SelfDeployPlanGateStatusApproved,
			GovernanceContext: value.GovernanceContextRef{
				RiskAssessmentRef: "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
			},
		},
	}
	service := New(Config{
		Repository:             repository,
		IDGenerator:            &sequenceIDGenerator{ids: []uuid.UUID{planID, createEventID, updateEventID}},
		SelfDeployGatePreparer: preparer,
		SelfDeployGateEnabled:  true,
	})
	input := validSelfDeployPlanGateInput()

	plan, err := service.CreateSelfDeployPlan(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateSelfDeployPlan() err = %v", err)
	}
	if plan.Status != enum.SelfDeployPlanStatusApproved {
		t.Fatalf("status = %s, want %s", plan.Status, enum.SelfDeployPlanStatusApproved)
	}
	if plan.GovernanceContext.RiskAssessmentRef == "" {
		t.Fatal("risk assessment ref is empty")
	}
}

func TestCreateSelfDeployPlanDispatchesBuildAfterApprovedGate(t *testing.T) {
	t.Parallel()

	planID := uuid.MustParse("5f7f3a10-0030-4000-8000-000000000030")
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		IDGenerator:                    &sequenceIDGenerator{ids: []uuid.UUID{planID, uuid.New(), uuid.New(), uuid.New()}},
		SelfDeployGatePreparer:         approvedSelfDeployGatePreparer(),
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildContextPreparer: &fakeSelfDeployBuildContextPreparer{result: SelfDeployBuildContextResult{RuntimeBuildContextRef: "runtime:build-context/ready", RuntimeBuildContextStatus: "ready", BuildContextRef: "runtime://build-contexts/agent-manager", BuildContextDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", ManifestBundleDigests: map[string]string{"agent-manager": "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}, SourceSnapshotDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", MaterializationFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}},
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	plan, err := service.CreateSelfDeployPlan(context.Background(), validSelfDeployBuildPlanInput())
	if err != nil {
		t.Fatalf("CreateSelfDeployPlan() err = %v", err)
	}
	if plan.Status != enum.SelfDeployPlanStatusApproved || plan.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusRequested {
		t.Fatalf("statuses = %s/%s, want approved/requested", plan.Status, plan.RuntimeBuildStatus)
	}
	if len(plan.RuntimeBuildJobs) != 1 || plan.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/build-agent-manager" {
		t.Fatalf("runtime build jobs = %+v", plan.RuntimeBuildJobs)
	}
	if buildReader.calls != 1 || buildCreator.calls != 1 {
		t.Fatalf("build reader/creator calls = %d/%d, want 1/1", buildReader.calls, buildCreator.calls)
	}
	if buildCreator.last.BuildExecutionSpec.ImageRef != "registry.example/kodex/agent-manager" ||
		buildCreator.last.GovernanceApprovalRef == "" ||
		buildCreator.last.Meta.CommandID == uuid.Nil {
		t.Fatalf("runtime build input = %+v", buildCreator.last)
	}
	if repository.updateSelfDeployResult.Operation != operationDispatchSelfDeployBuild {
		t.Fatalf("operation = %q, want %q", repository.updateSelfDeployResult.Operation, operationDispatchSelfDeployBuild)
	}
}

func TestCreateSelfDeployPlanBlocksBuildWhenProjectBuildPlanNotReady(t *testing.T) {
	t.Parallel()

	planID := uuid.MustParse("5f7f3a10-0031-4000-8000-000000000031")
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: SelfDeployBuildPlanReadResult{
		Status:     SelfDeployBuildPlanStatusPolicyStale,
		SafeReason: "services_policy_digest_mismatch",
	}}
	buildCreator := &fakeSelfDeployBuildJobCreator{}
	service := New(Config{
		Repository:                     repository,
		IDGenerator:                    &sequenceIDGenerator{ids: []uuid.UUID{planID, uuid.New(), uuid.New(), uuid.New()}},
		SelfDeployGatePreparer:         approvedSelfDeployGatePreparer(),
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildContextPreparer: &fakeSelfDeployBuildContextPreparer{result: SelfDeployBuildContextResult{RuntimeBuildContextRef: "runtime:build-context/pending", RuntimeBuildContextStatus: "pending", MaterializationFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}},
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	plan, err := service.CreateSelfDeployPlan(context.Background(), validSelfDeployBuildPlanInput())
	if err != nil {
		t.Fatalf("CreateSelfDeployPlan() err = %v", err)
	}
	if plan.Status != enum.SelfDeployPlanStatusFailed ||
		plan.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusBlocked ||
		plan.RuntimeBuildErrorCode != string(SelfDeployBuildPlanStatusPolicyStale) ||
		!strings.Contains(plan.RuntimeBuildSummary, "services_policy_digest_mismatch") {
		t.Fatalf("plan/runtime build diagnostic = %s/%s/%s/%s", plan.Status, plan.RuntimeBuildStatus, plan.RuntimeBuildErrorCode, plan.RuntimeBuildSummary)
	}
	if !strings.Contains(plan.SafeSummary, "services_policy_digest_mismatch") {
		t.Fatalf("safe summary = %q, want policy stale reason", plan.SafeSummary)
	}
	if buildCreator.calls != 0 {
		t.Fatalf("build creator calls = %d, want 0", buildCreator.calls)
	}
}

func TestCreateSelfDeployPlanFromSignalMarksExistingPolicyStalePlanFailed(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	planID := uuid.MustParse("5f7f3a10-003d-4000-8000-00000000003d")
	plan := selfDeployPlanFromInputForTest(input, planID, "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.SafeSummary = "self-deploy plan approved"
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusBlocked
	plan.RuntimeBuildErrorCode = string(SelfDeployBuildPlanStatusPolicyStale)
	plan.RuntimeBuildSummary = "services_policy_source_ref_mismatch"
	repository := &fakeRepository{
		selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
		selfDeployList: []entity.SelfDeployPlan{plan},
	}
	buildReader := &fakeSelfDeployBuildPlanReader{result: SelfDeployBuildPlanReadResult{
		Status:     SelfDeployBuildPlanStatusPolicyStale,
		SafeReason: "services_policy_source_ref_mismatch",
	}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployGatePreparer:         approvedSelfDeployGatePreparer(),
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildContextPreparer: &fakeSelfDeployBuildContextPreparer{},
		SelfDeployBuildJobCreator:      &fakeSelfDeployBuildJobCreator{},
		SelfDeployBuildDispatchEnabled: true,
	})

	replayed, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: input})
	if err != nil {
		t.Fatalf("CreateSelfDeployPlanFromSignal() err = %v", err)
	}
	if replayed.Status != enum.SelfDeployPlanStatusFailed ||
		replayed.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusBlocked ||
		replayed.RuntimeBuildErrorCode != string(SelfDeployBuildPlanStatusPolicyStale) {
		t.Fatalf("replayed plan = %+v, want failed policy_stale terminal state", replayed)
	}
	if !repository.updateSelfDeployCalled {
		t.Fatalf("expected policy stale replay to update terminal plan status")
	}
}

func TestCreateSelfDeployPlanBlocksReadyBuildPlanWithoutContextRefs(t *testing.T) {
	t.Parallel()

	planID := uuid.MustParse("5f7f3a10-003c-4000-8000-00000000003c")
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{}}
	readResult := readySelfDeployBuildPlanResult()
	readResult.Plan.BuildItems[0].BuildExecutionSpec.BuildContextRef = ""
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		IDGenerator:                    &sequenceIDGenerator{ids: []uuid.UUID{planID, uuid.New(), uuid.New(), uuid.New()}},
		SelfDeployGatePreparer:         approvedSelfDeployGatePreparer(),
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      &fakeSelfDeployBuildPlanReader{result: readResult},
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	plan, err := service.CreateSelfDeployPlan(context.Background(), validSelfDeployBuildPlanInput())
	if err != nil {
		t.Fatalf("CreateSelfDeployPlan() err = %v", err)
	}
	if plan.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusBlocked ||
		plan.RuntimeBuildErrorCode != "invalid_build_execution_spec" ||
		!strings.Contains(plan.RuntimeBuildSummary, "without safe runtime build context refs or digests") {
		t.Fatalf("runtime build diagnostic = %s/%s/%s", plan.RuntimeBuildStatus, plan.RuntimeBuildErrorCode, plan.RuntimeBuildSummary)
	}
	if buildCreator.calls != 0 {
		t.Fatalf("build creator calls = %d, want 0", buildCreator.calls)
	}
	for _, forbidden := range []string{"raw_provider_payload", "token", "kubeconfig"} {
		if strings.Contains(plan.RuntimeBuildSummary, forbidden) {
			t.Fatalf("runtime build summary contains forbidden marker %q: %s", forbidden, plan.RuntimeBuildSummary)
		}
	}
}

func TestCreateSelfDeployPlanFromSignalDispatchesBuildAfterBuildContextBecomesReady(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	planID := uuid.MustParse("5f7f3a10-0034-4000-8000-000000000034")
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: SelfDeployBuildPlanReadResult{
		Status:     SelfDeployBuildPlanStatusBuildContextRequired,
		SafeReason: "runtime build context is not materialized yet",
		Plan:       SelfDeployBuildPlan{PlanFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
	}}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository: repository,
		IDGenerator: &sequenceIDGenerator{ids: []uuid.UUID{
			planID,
			uuid.MustParse("5f7f3a10-0035-4000-8000-000000000035"),
			uuid.MustParse("5f7f3a10-0036-4000-8000-000000000036"),
			uuid.MustParse("5f7f3a10-0037-4000-8000-000000000037"),
			uuid.MustParse("5f7f3a10-0038-4000-8000-000000000038"),
		}},
		SelfDeployGatePreparer:         approvedSelfDeployGatePreparer(),
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildContextPreparer: &fakeSelfDeployBuildContextPreparer{result: SelfDeployBuildContextResult{RuntimeBuildContextRef: "runtime:build-context/pending", RuntimeBuildContextStatus: "pending", MaterializationFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}},
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	waiting, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: input})
	if err != nil {
		t.Fatalf("CreateSelfDeployPlanFromSignal() waiting err = %v", err)
	}
	if waiting.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusPreparingContext ||
		waiting.RuntimeBuildErrorCode != string(SelfDeployBuildPlanStatusBuildContextRequired) ||
		!strings.Contains(waiting.RuntimeBuildSummary, "runtime build context is not materialized yet") {
		t.Fatalf("runtime build diagnostic = %s/%s/%s", waiting.RuntimeBuildStatus, waiting.RuntimeBuildErrorCode, waiting.RuntimeBuildSummary)
	}
	waitingCommandKey := repository.updateSelfDeployResult.IdempotencyKey
	repository.selfDeployList = []entity.SelfDeployPlan{waiting}
	repository.updateSelfDeployCalled = false

	sameDiagnosticInput := input
	sameDiagnosticInput.Meta.CommandID = uuid.MustParse("5f7f3a10-0039-4000-8000-000000000039")
	sameDiagnostic, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: sameDiagnosticInput})
	if err != nil {
		t.Fatalf("CreateSelfDeployPlanFromSignal() same diagnostic err = %v", err)
	}
	if sameDiagnostic.Version != waiting.Version || repository.updateSelfDeployCalled {
		t.Fatalf("same diagnostic replay version/update = %d/%v, want %d/false", sameDiagnostic.Version, repository.updateSelfDeployCalled, waiting.Version)
	}

	buildReader.result = readySelfDeployBuildPlanResult()
	repository.selfDeployList = []entity.SelfDeployPlan{sameDiagnostic}
	repository.updateSelfDeployCalled = false
	readyInput := input
	readyInput.Meta.CommandID = uuid.MustParse("5f7f3a10-0040-4000-8000-000000000040")

	requested, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: readyInput})
	if err != nil {
		t.Fatalf("CreateSelfDeployPlanFromSignal() ready err = %v", err)
	}
	if requested.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusRequested ||
		len(requested.RuntimeBuildJobs) != 1 ||
		requested.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/build-agent-manager" {
		t.Fatalf("runtime build state = %s/%+v, want requested job", requested.RuntimeBuildStatus, requested.RuntimeBuildJobs)
	}
	if buildCreator.calls != 1 {
		t.Fatalf("build creator calls = %d, want 1", buildCreator.calls)
	}
	if repository.updateSelfDeployResult.IdempotencyKey == waitingCommandKey {
		t.Fatalf("dispatch state idempotency key was reused: %q", waitingCommandKey)
	}
}

func TestCreateSelfDeployPlanFromSignalReplaysExistingBuildJob(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	planID := uuid.MustParse("5f7f3a10-0032-4000-8000-000000000032")
	plan := selfDeployPlanFromInputForTest(input, planID, "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusRequested
	plan.RuntimeBuildFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plan.RuntimeBuildJobs = []entity.SelfDeployRuntimeBuildJob{{
		ServiceKey:               "agent-manager",
		ServiceRef:               "project-catalog:service-descriptor:agent-manager",
		RuntimeJobRef:            "runtime:job/existing-build",
		RuntimeJobStatus:         "pending",
		BuildPlanItemFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}}
	repository := &fakeRepository{
		selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
		selfDeployList: []entity.SelfDeployPlan{plan},
	}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{}
	service := New(Config{
		Repository:                     repository,
		SelfDeployGatePreparer:         approvedSelfDeployGatePreparer(),
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployRuntimeJobReader:     &fakeSelfDeployRuntimeJobReader{result: SelfDeployRuntimeJobReadResult{JobRef: "runtime:job/existing-build", JobType: enum.SelfDeployRuntimeJobTypeBuild, Status: RuntimeJobStatusPending}},
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	replayed, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: input})
	if err != nil {
		t.Fatalf("CreateSelfDeployPlanFromSignal() err = %v", err)
	}
	if replayed.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/existing-build" {
		t.Fatalf("runtime build jobs = %+v", replayed.RuntimeBuildJobs)
	}
	if buildReader.calls != 0 || buildCreator.calls != 0 || repository.updateSelfDeployCalled {
		t.Fatalf("calls reader/creator/update = %d/%d/%v, want no calls", buildReader.calls, buildCreator.calls, repository.updateSelfDeployCalled)
	}
}

func TestEnsureSelfDeployPlanRuntimeRetriesApprovedBuildPermissionDenied(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	planID := uuid.MustParse("5f7f3a10-0056-4000-8000-000000000056")
	plan := selfDeployPlanFromInputForTest(input, planID, "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusFailed
	plan.RuntimeBuildErrorCode = "permission_denied"
	plan.RuntimeBuildSummary = "runtime job permanent: code=permission_denied; message=runtime-manager rejected the runtime job caller"
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	runtimeReader := &fakeSelfDeployRuntimeJobReader{result: SelfDeployRuntimeJobReadResult{JobRef: "runtime:job/build-agent-manager", JobType: enum.SelfDeployRuntimeJobTypeBuild, Status: RuntimeJobStatusPending}}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	preparer := &fakeSelfDeployGatePreparer{}
	service := New(Config{
		Repository:                     repository,
		SelfDeployGatePreparer:         preparer,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployRuntimeJobReader:     runtimeReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	recovered, err := service.EnsureSelfDeployPlanRuntime(context.Background(), EnsureSelfDeployPlanRuntimeInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-approved-self-deploy-runtime", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanRuntime() err = %v", err)
	}
	if recovered.Status != enum.SelfDeployPlanStatusApproved ||
		recovered.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusRequested ||
		recovered.RuntimeBuildErrorCode != "" ||
		len(recovered.RuntimeBuildJobs) != 1 ||
		recovered.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/build-agent-manager" {
		t.Fatalf("recovered runtime state = status:%s build:%s code:%s jobs:%+v", recovered.Status, recovered.RuntimeBuildStatus, recovered.RuntimeBuildErrorCode, recovered.RuntimeBuildJobs)
	}
	if recovered.GovernanceContext.GateDecisionRef != plan.GovernanceContext.GateDecisionRef {
		t.Fatalf("gate decision ref = %q, want saved ref", recovered.GovernanceContext.GateDecisionRef)
	}
	if preparer.calls != 0 || buildReader.calls != 1 || buildCreator.calls != 1 {
		t.Fatalf("calls gate/build reader/build creator = %d/%d/%d, want 0/1/1", preparer.calls, buildReader.calls, buildCreator.calls)
	}

	repository.updateSelfDeployCalled = false
	replayed, err := service.EnsureSelfDeployPlanRuntime(context.Background(), EnsureSelfDeployPlanRuntimeInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-approved-self-deploy-runtime-replay", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanRuntime() replay err = %v", err)
	}
	if replayed.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/build-agent-manager" {
		t.Fatalf("replay runtime jobs = %+v, want existing", replayed.RuntimeBuildJobs)
	}
	if buildCreator.calls != 1 {
		t.Fatalf("build creator calls = %d, want no duplicate", buildCreator.calls)
	}
	if repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult called during idempotent runtime replay")
	}
}

func TestEnsureSelfDeployPlanRuntimeRetriesTerminalBuildJobWithBoundedAttempt(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	planID := uuid.MustParse("5f7f3a10-0059-4000-8000-000000000059")
	plan := selfDeployPlanFromInputForTest(input, planID, "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusFailed
	plan.RuntimeBuildFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plan.RuntimeBuildErrorCode = "permission_denied"
	plan.RuntimeBuildSummary = "runtime job permanent: code=permission_denied; message=runtime-manager rejected the runtime job caller"
	plan.RuntimeBuildJobs = []entity.SelfDeployRuntimeBuildJob{{
		ServiceKey:               "agent-manager",
		ServiceRef:               "project-catalog:service-descriptor:agent-manager",
		RuntimeJobRef:            "runtime:job/terminal-build",
		RuntimeJobStatus:         string(RuntimeJobStatusFailed),
		BuildPlanItemFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}}
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	buildRead := readySelfDeployBuildPlanResult()
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/retry-build-agent-manager", Status: "pending"}}
	runtimeReader := &fakeSelfDeployRuntimeJobReader{result: SelfDeployRuntimeJobReadResult{JobRef: "runtime:job/retry-build-agent-manager", JobType: enum.SelfDeployRuntimeJobTypeBuild, Status: RuntimeJobStatusPending}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      &fakeSelfDeployBuildPlanReader{result: buildRead},
		SelfDeployRuntimeJobReader:     runtimeReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	recovered, err := service.EnsureSelfDeployPlanRuntime(context.Background(), EnsureSelfDeployPlanRuntimeInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-approved-self-deploy-terminal-build-runtime", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanRuntime() err = %v", err)
	}
	if recovered.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusRequested ||
		len(recovered.RuntimeBuildJobs) != 1 ||
		recovered.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/retry-build-agent-manager" ||
		recovered.RuntimeBuildJobs[0].RuntimeJobAttemptRef == "" {
		t.Fatalf("recovered runtime jobs = status:%s jobs:%+v, want retry job with attempt ref", recovered.RuntimeBuildStatus, recovered.RuntimeBuildJobs)
	}
	baseMeta := selfDeployRuntimeBuildCommandMeta(plan.ID, buildRead.Plan.BuildItems[0], "")
	if buildCreator.last.Meta.CommandID == baseMeta.CommandID {
		t.Fatal("retry build command id reused base runtime job command id")
	}
	if buildCreator.last.Meta.IdempotencyKey == baseMeta.IdempotencyKey {
		t.Fatal("retry build idempotency key reused base runtime job key")
	}

	repository.updateSelfDeployCalled = false
	_, err = service.EnsureSelfDeployPlanRuntime(context.Background(), EnsureSelfDeployPlanRuntimeInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-approved-self-deploy-terminal-build-runtime-replay", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanRuntime() replay err = %v", err)
	}
	if buildCreator.calls != 1 {
		t.Fatalf("build creator calls = %d, want no duplicate retry job", buildCreator.calls)
	}
	if repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult called during retry replay")
	}
}

func TestEnsureSelfDeployPlanRuntimeDoesNotLoopRetriedTerminalBuildJob(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	planID := uuid.MustParse("5f7f3a10-0060-4000-8000-000000000060")
	plan := selfDeployPlanFromInputForTest(input, planID, "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusFailed
	plan.RuntimeBuildErrorCode = "permission_denied"
	plan.RuntimeBuildSummary = "runtime retry job failed"
	plan.RuntimeBuildJobs = []entity.SelfDeployRuntimeBuildJob{{
		ServiceKey:               "agent-manager",
		ServiceRef:               "project-catalog:service-descriptor:agent-manager",
		RuntimeJobRef:            "runtime:job/retry-build-agent-manager",
		RuntimeJobStatus:         string(RuntimeJobStatusFailed),
		RuntimeJobAttemptRef:     "agent:self-deploy-runtime-retry:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		BuildPlanItemFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}}
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/another-retry", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	recovered, err := service.EnsureSelfDeployPlanRuntime(context.Background(), EnsureSelfDeployPlanRuntimeInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-approved-self-deploy-runtime-retry-loop", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanRuntime() err = %v", err)
	}
	if recovered.RuntimeBuildJobs[0].RuntimeJobRef != "runtime:job/retry-build-agent-manager" {
		t.Fatalf("runtime jobs = %+v, want existing retry job", recovered.RuntimeBuildJobs)
	}
	if buildReader.calls != 0 || buildCreator.calls != 0 || repository.updateSelfDeployCalled {
		t.Fatalf("calls reader/creator/update = %d/%d/%v, want none", buildReader.calls, buildCreator.calls, repository.updateSelfDeployCalled)
	}
}

func TestEnsureSelfDeployPlanRuntimeDoesNotRetryNonTransientBuildFailure(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	planID := uuid.MustParse("5f7f3a10-0057-4000-8000-000000000057")
	plan := selfDeployPlanFromInputForTest(input, planID, "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusFailed
	plan.RuntimeBuildErrorCode = "failed_precondition"
	plan.RuntimeBuildSummary = "runtime job permanent: code=failed_precondition; message=runtime job precondition failed"
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	buildCreator := &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/build-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	recovered, err := service.EnsureSelfDeployPlanRuntime(context.Background(), EnsureSelfDeployPlanRuntimeInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-approved-self-deploy-non-retryable-runtime", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanRuntime() err = %v", err)
	}
	if recovered.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusFailed ||
		recovered.RuntimeBuildErrorCode != "failed_precondition" {
		t.Fatalf("runtime build state = %s/%s, want unchanged non-retryable failure", recovered.RuntimeBuildStatus, recovered.RuntimeBuildErrorCode)
	}
	if buildReader.calls != 0 || buildCreator.calls != 0 || repository.updateSelfDeployCalled {
		t.Fatalf("calls reader/creator/update = %d/%d/%v, want none", buildReader.calls, buildCreator.calls, repository.updateSelfDeployCalled)
	}
}

func TestEnsureSelfDeployPlanRuntimeRetriesApprovedDeployPermissionDenied(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	input.ExpectedRuntimeJobTypes = []enum.SelfDeployRuntimeJobType{enum.SelfDeployRuntimeJobTypeBuild, enum.SelfDeployRuntimeJobTypeDeploy}
	planID := uuid.MustParse("5f7f3a10-0058-4000-8000-000000000058")
	plan := selfDeployPlanFromInputForTest(input, planID, "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusSucceeded
	plan.RuntimeBuildFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plan.RuntimeBuildContexts = []entity.SelfDeployRuntimeBuildContext{{
		ServiceKey:                 "agent-manager",
		RuntimeBuildContextRef:     "runtime:build-context/ready",
		RuntimeBuildContextStatus:  "ready",
		BuildContextRef:            "runtime://build-contexts/agent-manager",
		BuildContextDigest:         "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		ManifestBundleDigest:       "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		MaterializationFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		BuildPlanItemFingerprint:   "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}}
	plan.RuntimeBuildJobs = []entity.SelfDeployRuntimeBuildJob{{
		ServiceKey:               "agent-manager",
		ServiceRef:               "project-catalog:service-descriptor:agent-manager",
		RuntimeJobRef:            "runtime:job/existing-build",
		RuntimeJobStatus:         string(RuntimeJobStatusSucceeded),
		BuildPlanItemFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}}
	plan.RuntimeDeployStatus = enum.SelfDeployRuntimeDeployStatusFailed
	plan.RuntimeDeployErrorCode = "permission_denied"
	plan.RuntimeDeploySummary = "runtime job permanent: code=permission_denied; message=runtime-manager rejected the runtime job caller"
	plan.RuntimeDeployJobs = []entity.SelfDeployRuntimeDeployJob{{
		ServiceKey:                "agent-manager",
		ServiceRef:                "project-catalog:service-descriptor:agent-manager",
		RuntimeJobRef:             "runtime:job/terminal-deploy",
		RuntimeJobStatus:          string(RuntimeJobStatusFailed),
		DeployPlanItemFingerprint: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}}
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan}}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	deployRead := readySelfDeployDeployPlanResult()
	deployReader := &fakeSelfDeployDeployPlanReader{result: deployRead}
	deployCreator := &fakeSelfDeployDeployJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/deploy-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployDeployPlanReader:     deployReader,
		SelfDeployDeployJobCreator:     deployCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	recovered, err := service.EnsureSelfDeployPlanRuntime(context.Background(), EnsureSelfDeployPlanRuntimeInput{
		Meta:             value.CommandMeta{IdempotencyKey: "recover-approved-self-deploy-deploy-runtime", Actor: testActor()},
		SelfDeployPlanID: plan.ID,
	})
	if err != nil {
		t.Fatalf("EnsureSelfDeployPlanRuntime() err = %v", err)
	}
	if recovered.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusSucceeded ||
		recovered.RuntimeDeployStatus != enum.SelfDeployRuntimeDeployStatusRequested ||
		recovered.RuntimeDeployErrorCode != "" ||
		len(recovered.RuntimeDeployJobs) != 1 ||
		recovered.RuntimeDeployJobs[0].RuntimeJobRef != "runtime:job/deploy-agent-manager" ||
		recovered.RuntimeDeployJobs[0].RuntimeJobAttemptRef == "" {
		t.Fatalf("recovered runtime state = build:%s deploy:%s code:%s deploy jobs:%+v", recovered.RuntimeBuildStatus, recovered.RuntimeDeployStatus, recovered.RuntimeDeployErrorCode, recovered.RuntimeDeployJobs)
	}
	baseMeta := selfDeployRuntimeDeployCommandMeta(plan.ID, deployRead.Plan.DeployItems[0], "")
	if deployCreator.last.Meta.CommandID == baseMeta.CommandID {
		t.Fatal("retry deploy command id reused base runtime job command id")
	}
	if buildReader.calls != 1 || deployReader.calls != 1 || deployCreator.calls != 1 {
		t.Fatalf("calls build reader/deploy reader/deploy creator = %d/%d/%d, want 1/1/1", buildReader.calls, deployReader.calls, deployCreator.calls)
	}
}

func TestCreateSelfDeployPlanFromSignalDispatchesDeployAfterBuildSucceeded(t *testing.T) {
	t.Parallel()

	input := validSelfDeployBuildPlanInput()
	input.ExpectedRuntimeJobTypes = []enum.SelfDeployRuntimeJobType{enum.SelfDeployRuntimeJobTypeBuild, enum.SelfDeployRuntimeJobTypeDeploy}
	planID := uuid.MustParse("5f7f3a10-0055-4000-8000-000000000055")
	plan := selfDeployPlanFromInputForTest(input, planID, "command:"+input.Meta.CommandID.String())
	plan.Status = enum.SelfDeployPlanStatusApproved
	plan.GovernanceContext.GateRequestRef = "governance:gate_request/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa"
	plan.GovernanceContext.GateDecisionRef = "governance:gate_decision/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb"
	plan.RuntimeBuildStatus = enum.SelfDeployRuntimeBuildStatusRequested
	plan.RuntimeBuildFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	plan.RuntimeBuildContexts = []entity.SelfDeployRuntimeBuildContext{{
		ServiceKey:                 "agent-manager",
		RuntimeBuildContextRef:     "runtime:build-context/ready",
		RuntimeBuildContextStatus:  "ready",
		BuildContextRef:            "runtime://build-contexts/agent-manager",
		BuildContextDigest:         "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		ManifestBundleDigest:       "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		MaterializationFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		BuildPlanItemFingerprint:   "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}}
	plan.RuntimeBuildJobs = []entity.SelfDeployRuntimeBuildJob{{
		ServiceKey:               "agent-manager",
		ServiceRef:               "project-catalog:service-descriptor:agent-manager",
		RuntimeJobRef:            "runtime:job/existing-build",
		RuntimeJobStatus:         string(RuntimeJobStatusPending),
		BuildPlanItemFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}}
	repository := &fakeRepository{
		selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{plan.ID: plan},
		selfDeployList: []entity.SelfDeployPlan{plan},
	}
	buildReader := &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()}
	deployReader := &fakeSelfDeployDeployPlanReader{result: readySelfDeployDeployPlanResult()}
	deployCreator := &fakeSelfDeployDeployJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/deploy-agent-manager", Status: "pending"}}
	service := New(Config{
		Repository:                     repository,
		SelfDeployGatePreparer:         approvedSelfDeployGatePreparer(),
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      buildReader,
		SelfDeployDeployPlanReader:     deployReader,
		SelfDeployRuntimeJobReader:     &fakeSelfDeployRuntimeJobReader{result: SelfDeployRuntimeJobReadResult{JobRef: "runtime:job/existing-build", JobType: enum.SelfDeployRuntimeJobTypeBuild, Status: RuntimeJobStatusSucceeded}},
		SelfDeployBuildJobCreator:      &fakeSelfDeployBuildJobCreator{result: RuntimeJobResult{JobRef: "runtime:job/new-build", Status: "pending"}},
		SelfDeployDeployJobCreator:     deployCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	updated, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: input})
	if err != nil {
		t.Fatalf("CreateSelfDeployPlanFromSignal() err = %v", err)
	}
	if updated.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusSucceeded ||
		updated.RuntimeDeployStatus != enum.SelfDeployRuntimeDeployStatusRequested {
		t.Fatalf("runtime statuses = %s/%s, want succeeded/requested", updated.RuntimeBuildStatus, updated.RuntimeDeployStatus)
	}
	if len(updated.RuntimeDeployJobs) != 1 || updated.RuntimeDeployJobs[0].RuntimeJobRef != "runtime:job/deploy-agent-manager" {
		t.Fatalf("runtime deploy jobs = %+v, want deploy job", updated.RuntimeDeployJobs)
	}
	if buildReader.calls != 1 || deployReader.calls != 1 || deployCreator.calls != 1 {
		t.Fatalf("reader/creator calls = build:%d deploy:%d creator:%d, want 1/1/1", buildReader.calls, deployReader.calls, deployCreator.calls)
	}
	if deployReader.last.ExpectedBuildPlanFingerprint != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		len(deployReader.last.BuildOutputs) != 1 ||
		deployReader.last.BuildOutputs[0].RuntimeJobRef != "runtime:job/existing-build" {
		t.Fatalf("deploy plan lookup = %+v, want build output refs", deployReader.last)
	}
	if deployCreator.last.DeployExecutionSpec.ManifestBundleDigest != "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" ||
		deployCreator.last.GovernanceApprovalRef == "" {
		t.Fatalf("deploy job input = %+v, want manifest digest and approval refs", deployCreator.last)
	}
}

func TestCreateSelfDeployPlanRecordsSafeBuildFailure(t *testing.T) {
	t.Parallel()

	planID := uuid.MustParse("5f7f3a10-0033-4000-8000-000000000033")
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{}}
	buildCreator := &fakeSelfDeployBuildJobCreator{
		err: NewRuntimeJobError(false, "failed_precondition", "raw_provider_payload token kubeconfig"),
	}
	service := New(Config{
		Repository:                     repository,
		IDGenerator:                    &sequenceIDGenerator{ids: []uuid.UUID{planID, uuid.New(), uuid.New(), uuid.New()}},
		SelfDeployGatePreparer:         approvedSelfDeployGatePreparer(),
		SelfDeployGateEnabled:          true,
		SelfDeployBuildPlanReader:      &fakeSelfDeployBuildPlanReader{result: readySelfDeployBuildPlanResult()},
		SelfDeployBuildJobCreator:      buildCreator,
		SelfDeployBuildDispatchEnabled: true,
	})

	plan, err := service.CreateSelfDeployPlan(context.Background(), validSelfDeployBuildPlanInput())
	if err != nil {
		t.Fatalf("CreateSelfDeployPlan() err = %v", err)
	}
	if plan.RuntimeBuildStatus != enum.SelfDeployRuntimeBuildStatusFailed || plan.RuntimeBuildErrorCode != "failed_precondition" {
		t.Fatalf("runtime build failure = %s/%s", plan.RuntimeBuildStatus, plan.RuntimeBuildErrorCode)
	}
	for _, forbidden := range []string{"raw_provider_payload", "token", "kubeconfig"} {
		if strings.Contains(plan.RuntimeBuildSummary, forbidden) {
			t.Fatalf("runtime build summary contains forbidden marker %q: %s", forbidden, plan.RuntimeBuildSummary)
		}
	}
}

func TestCreateSelfDeployPlanRejectsPendingGateWithoutGateRef(t *testing.T) {
	t.Parallel()

	planID := uuid.MustParse("5f7f3a10-0020-4000-8000-000000000020")
	eventID := uuid.MustParse("5f7f3a10-0021-4000-8000-000000000021")
	repository := &fakeRepository{selfDeployByID: map[uuid.UUID]entity.SelfDeployPlan{}}
	preparer := &fakeSelfDeployGatePreparer{
		result: SelfDeployPlanGatePreparationResult{
			Status: SelfDeployPlanGateStatusPending,
			GovernanceContext: value.GovernanceContextRef{
				RiskAssessmentRef: "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
			},
		},
	}
	service := New(Config{
		Repository:             repository,
		IDGenerator:            &sequenceIDGenerator{ids: []uuid.UUID{planID, eventID}},
		SelfDeployGatePreparer: preparer,
		SelfDeployGateEnabled:  true,
	})
	input := validSelfDeployPlanGateInput()

	_, err := service.CreateSelfDeployPlan(context.Background(), input)
	if !errors.Is(err, errs.ErrDependencyUnavailable) {
		t.Fatalf("CreateSelfDeployPlan() err = %v, want %v", err, errs.ErrDependencyUnavailable)
	}
	if repository.updateSelfDeployCalled {
		t.Fatal("UpdateSelfDeployPlanWithResult called for incomplete pending gate")
	}
}

func TestCreateSelfDeployPlanFromSignalRejectsChangedFingerprint(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanInput()
	plan := selfDeployPlanFromInputForTest(input, uuid.MustParse("5f7f3a10-0007-4000-8000-000000000007"), "command:"+input.Meta.CommandID.String())
	repository := &fakeRepository{selfDeployList: []entity.SelfDeployPlan{plan}}
	service := New(Config{Repository: repository})
	changed := input
	changed.Meta.CommandID = uuid.MustParse("5f7f3a10-0008-4000-8000-000000000008")
	changed.AffectedServiceKeys = []string{"agent-manager"}

	_, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: changed})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateSelfDeployPlanFromSignal() err = %v, want %v", err, errs.ErrConflict)
	}
	if repository.createSelfDeployCalled {
		t.Fatal("CreateSelfDeployPlanWithResult called after signal conflict")
	}
}

func TestCreateSelfDeployPlanFromSignalRequiresProviderSignalRef(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanInput()
	input.ProviderSignalRef = ""
	repository := &fakeRepository{}
	service := New(Config{Repository: repository})

	_, err := service.CreateSelfDeployPlanFromSignal(context.Background(), CreateSelfDeployPlanFromSignalInput{CreateSelfDeployPlanInput: input})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("CreateSelfDeployPlanFromSignal() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if repository.createSelfDeployCalled {
		t.Fatal("CreateSelfDeployPlanWithResult called without provider signal ref")
	}
}

func TestCreateSelfDeployPlanRejectsUnsafePayload(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanInput()
	input.SafeSummary = "safe refs plus full_yaml content marker"
	repository := &fakeRepository{}
	service := New(Config{Repository: repository})

	_, err := service.CreateSelfDeployPlan(context.Background(), input)
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("CreateSelfDeployPlan() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
	if repository.createSelfDeployCalled {
		t.Fatal("CreateSelfDeployPlanWithResult called for unsafe input")
	}
}

func TestCreateSelfDeployPlanRejectsAgentRunJobType(t *testing.T) {
	t.Parallel()

	input := validSelfDeployPlanInput()
	input.ExpectedRuntimeJobTypes = []enum.SelfDeployRuntimeJobType{enum.SelfDeployRuntimeJobType("agent_run")}
	service := New(Config{Repository: &fakeRepository{}})

	_, err := service.CreateSelfDeployPlan(context.Background(), input)
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("CreateSelfDeployPlan() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func TestListSelfDeployPlansRequiresBoundedFilter(t *testing.T) {
	t.Parallel()

	service := New(Config{Repository: &fakeRepository{}})
	_, _, err := service.ListSelfDeployPlans(context.Background(), query.SelfDeployPlanFilter{})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListSelfDeployPlans() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

func validSelfDeployPlanInput() CreateSelfDeployPlanInput {
	commandID := uuid.MustParse("5f7f3a10-0100-4000-8000-000000000100")
	return CreateSelfDeployPlanInput{
		Meta:               value.CommandMeta{CommandID: commandID, Actor: testActor()},
		Scope:              value.ScopeRef{Type: string(enum.AgentScopeTypeRepository), Ref: "repository:codex-k8s/kodex"},
		ProjectRef:         "project:codex",
		RepositoryRef:      "repository:codex-k8s/kodex",
		ProviderSignalRef:  "provider-signal:github/push-main/5f7f3a1",
		ProviderSlug:       "github",
		RepositoryFullName: "codex-k8s/kodex",
		SourceRef:          "git:refs/heads/main@5f7f3a10",
		MergeCommitSHA:     "5f7f3a105f7f3a105f7f3a105f7f3a105f7f3a10",
		ServicesYAMLRef:    "object://project-catalog/services-yaml/5f7f3a1",
		ServicesYAMLDigest: testInstructionDigest,
		AffectedServiceKeys: []string{
			"runtime-manager",
			"agent-manager",
		},
		PathCategories: []enum.SelfDeployPathCategory{
			enum.SelfDeployPathCategoryRuntimeConfig,
			enum.SelfDeployPathCategoryServiceSource,
		},
		ExpectedRuntimeJobTypes: []enum.SelfDeployRuntimeJobType{
			enum.SelfDeployRuntimeJobTypeDeploy,
			enum.SelfDeployRuntimeJobTypeBuild,
		},
		GovernanceContext: value.GovernanceContextRef{
			GateRequestRef:   "governance:gate-request/self-deploy",
			GatePolicyRef:    "governance:policy/self-deploy",
			ReleasePolicyRef: "governance:release-policy/self-deploy",
		},
		SafeSummary: "После merge в main затронуты agent-manager и runtime-manager; требуется approval перед build/deploy jobs.",
	}
}

func validSelfDeployPlanGateInput() CreateSelfDeployPlanInput {
	input := validSelfDeployPlanInput()
	input.GovernanceContext.GateRequestRef = ""
	return input
}

func validSelfDeployBuildPlanInput() CreateSelfDeployPlanInput {
	projectID := uuid.MustParse("63135040-fe44-4ec4-83d5-b0126dc23b32")
	repositoryID := uuid.MustParse("63135040-fe44-4ec4-83d5-b0126dc23b33")
	input := validSelfDeployPlanInput()
	input.ProjectRef = projectID.String()
	input.RepositoryRef = repositoryID.String()
	input.SourceRef = "refs/heads/main"
	input.MergeCommitSHA = "abcdef0123456789abcdef0123456789abcdef01"
	input.ServicesYAMLRef = "project-catalog:services-policy:63135040/services.yaml"
	input.ServicesYAMLDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	input.AffectedServiceKeys = []string{"agent-manager"}
	input.PathCategories = []enum.SelfDeployPathCategory{enum.SelfDeployPathCategoryServiceSource}
	input.ExpectedRuntimeJobTypes = []enum.SelfDeployRuntimeJobType{enum.SelfDeployRuntimeJobTypeBuild}
	input.GovernanceContext = value.GovernanceContextRef{
		GatePolicyRef:    "governance:policy/self-deploy",
		ReleasePolicyRef: "governance:release-policy/self-deploy",
	}
	input.SafeSummary = "self-deploy build plan for agent-manager is waiting for approval"
	return input
}

func approvedSelfDeployGatePreparer() *fakeSelfDeployGatePreparer {
	return &fakeSelfDeployGatePreparer{result: SelfDeployPlanGatePreparationResult{
		Status: SelfDeployPlanGateStatusApproved,
		GovernanceContext: value.GovernanceContextRef{
			RiskAssessmentRef: "governance:risk_assessment/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
			GateRequestRef:    "governance:gate_request/aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa",
			GateDecisionRef:   "governance:gate_decision/bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb",
		},
	}}
}

func readySelfDeployBuildPlanResult() SelfDeployBuildPlanReadResult {
	return SelfDeployBuildPlanReadResult{
		Status: SelfDeployBuildPlanStatusReady,
		Plan: SelfDeployBuildPlan{
			ProjectRef:          "63135040-fe44-4ec4-83d5-b0126dc23b32",
			RepositoryRef:       "63135040-fe44-4ec4-83d5-b0126dc23b33",
			ProviderSignalRef:   "provider-signal:github/push-main/5f7f3a1",
			SourceRef:           "refs/heads/main",
			MergeCommitSHA:      "abcdef0123456789abcdef0123456789abcdef01",
			ServicesYAML:        SelfDeploySignalServicesYAML{Ref: "project-catalog:services-policy:63135040/services.yaml", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			AffectedServiceKeys: []string{"agent-manager"},
			BuildItems: []SelfDeployBuildPlanItem{{
				ServiceKey:          "agent-manager",
				ServiceRef:          "project-catalog:service-descriptor:agent-manager",
				PlanItemFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				BuildExecutionSpec:  readySelfDeployBuildExecutionSpec(),
			}},
			PlanFingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			SafeSummary:     "self-deploy build plan ready",
			Version:         1,
		},
	}
}

func readySelfDeployBuildExecutionSpec() SelfDeployBuildExecutionSpec {
	spec := SelfDeployBuildExecutionSpec{}
	spec.SourceRef = "refs/heads/main"
	spec.SourceCommitSHA = "abcdef0123456789abcdef0123456789abcdef01"
	spec.ServiceKey = "agent-manager"
	spec.ImageRef = "registry.example/kodex/agent-manager"
	spec.ImageTag = "abcdef0"
	spec.BuildContextRef = "runtime://build-contexts/agent-manager"
	spec.BuildContextDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	spec.DockerfileRef = "runtime://build-contexts/agent-manager/Dockerfile"
	spec.DockerfileTarget = "prod"
	spec.BuilderImageRef = "gcr.io/kaniko-project/executor:v1.23.2"
	spec.BuildPlanFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	spec.AllowedSecretRefs = []RuntimeJobAllowedSecretRef{
		{SecretRef: "secret://runtime/registry", Purpose: "registry_docker_config"},
	}
	spec.OutputRefs = []RuntimeJobOutputRef{
		{Kind: "image", Ref: "runtime:image:agent-manager"},
	}
	return spec
}

func readySelfDeployDeployPlanResult() SelfDeployDeployPlanReadResult {
	return SelfDeployDeployPlanReadResult{
		Status: SelfDeployDeployPlanStatusReady,
		Plan: SelfDeployDeployPlan{
			ProjectRef:          "63135040-fe44-4ec4-83d5-b0126dc23b32",
			RepositoryRef:       "63135040-fe44-4ec4-83d5-b0126dc23b33",
			ProviderSignalRef:   "provider-signal:github/push-main/5f7f3a1",
			SourceRef:           "refs/heads/main",
			MergeCommitSHA:      "abcdef0123456789abcdef0123456789abcdef01",
			ServicesYAML:        SelfDeploySignalServicesYAML{Ref: "project-catalog:services-policy:63135040/services.yaml", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			AffectedServiceKeys: []string{"agent-manager"},
			DeployItems: []SelfDeployDeployPlanItem{{
				ServiceKey:          "agent-manager",
				ServiceRef:          "project-catalog:service-descriptor:agent-manager",
				PlanItemFingerprint: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
				DeployExecutionSpec: readySelfDeployDeployExecutionSpec(),
			}},
			PlanFingerprint: "sha256:9999999999999999999999999999999999999999999999999999999999999999",
			SafeSummary:     "self-deploy deploy plan ready",
			Version:         1,
		},
	}
}

func readySelfDeployDeployExecutionSpec() SelfDeployDeployExecutionSpec {
	spec := SelfDeployDeployExecutionSpec{}
	spec.SourceRef = "refs/heads/main"
	spec.SourceCommitSHA = "abcdef0123456789abcdef0123456789abcdef01"
	spec.ServiceKey = "agent-manager"
	spec.ImageRef = "registry.example/kodex/agent-manager"
	spec.ImageTag = "abcdef0"
	spec.ManifestBundleRef = "runtime://deploy/agent-manager/bundle"
	spec.ManifestBundleDigest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	spec.ManifestRef = "runtime://deploy/agent-manager/manifests"
	spec.ManifestDigest = "sha256:7777777777777777777777777777777777777777777777777777777777777777"
	spec.KustomizationRef = "runtime://deploy/agent-manager/kustomization"
	spec.KustomizationDigest = "sha256:8888888888888888888888888888888888888888888888888888888888888888"
	spec.TargetNamespace = "kodex"
	spec.TargetClusterRef = "runtime:cluster:local"
	spec.DeployPlanFingerprint = "sha256:9999999999999999999999999999999999999999999999999999999999999999"
	spec.RolloutTargets = []SelfDeployDeployRolloutTarget{{
		Kind:      "deployment",
		Ref:       "k8s:deployment/kodex/agent-manager",
		Namespace: "kodex",
		Name:      "agent-manager",
	}}
	spec.ExpectedImageRefs = []SelfDeployDeployExpectedImageRef{{
		ContainerName: "agent-manager",
		ImageRef:      "registry.example/kodex/agent-manager:abcdef0",
	}}
	spec.AllowedSecretRefs = []RuntimeJobAllowedSecretRef{
		{SecretRef: "secret://runtime/kubernetes", Purpose: "kubernetes_apply"},
	}
	spec.OutputRefs = []RuntimeJobOutputRef{
		{Kind: "rollout", Ref: "runtime:deploy:agent-manager"},
	}
	return spec
}

func selfDeployPlanFromInputForTest(input CreateSelfDeployPlanInput, id uuid.UUID, idempotencyKey string) entity.SelfDeployPlan {
	plan, err := normalizeSelfDeployPlanInput(input, idempotencyKey)
	if err != nil {
		panic(err)
	}
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	plan.ID = id
	plan.Version = 1
	plan.CreatedAt = now
	plan.UpdatedAt = now
	return plan
}

func decodeAgentPayload(t *testing.T, event entity.OutboxEvent) agentevents.Payload {
	t.Helper()

	var payload agentevents.Payload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	return payload
}

type fakeRepository struct {
	replay                 *entity.CommandResult
	createdFlow            entity.Flow
	createdResult          entity.CommandResult
	flowByID               map[uuid.UUID]entity.Flow
	flowVersionByID        map[uuid.UUID]entity.FlowVersion
	sessionByID            map[uuid.UUID]entity.AgentSession
	runByID                map[uuid.UUID]entity.AgentRun
	acceptanceByID         map[uuid.UUID]entity.AcceptanceResult
	acceptanceGetErr       error
	followUpByID           map[uuid.UUID]entity.FollowUpIntent
	activityByID           map[uuid.UUID]entity.AgentActivity
	activityList           []entity.AgentActivity
	activityPage           value.PageResult
	activityFilter         query.AgentActivityFilter
	sessionSummaryList     []entity.AgentSessionListItem
	sessionSummaryPage     value.PageResult
	sessionSummaryFilter   query.AgentSessionFilter
	runSummaryList         []entity.AgentRunListItem
	runSummaryPage         value.PageResult
	runSummaryFilter       query.AgentRunSummaryFilter
	humanGateByID          map[uuid.UUID]entity.HumanGateRequest
	humanGateList          []entity.HumanGateRequest
	humanGatePage          value.PageResult
	humanGateFilter        query.HumanGateFilter
	selfDeployByID         map[uuid.UUID]entity.SelfDeployPlan
	selfDeployList         []entity.SelfDeployPlan
	selfDeployPage         value.PageResult
	selfDeployFilter       query.SelfDeployPlanFilter
	updateSelfDeployErr    error
	updateSelfDeployErrs   []error
	updateSelfDeployOnErr  func()
	roleByID               map[uuid.UUID]entity.RoleProfile
	promptVersionByID      map[uuid.UUID]entity.PromptTemplateVersion
	activeSession          entity.AgentSession
	activeSessionFound     bool
	recordedCommandResult  entity.CommandResult
	createdSession         entity.AgentSession
	sessionResult          entity.CommandResult
	sessionEvent           entity.OutboxEvent
	createdRun             entity.AgentRun
	runResult              entity.CommandResult
	runEvent               entity.OutboxEvent
	updatedRun             entity.AgentRun
	updateRunResult        entity.CommandResult
	updateRunEvent         *entity.OutboxEvent
	createdSnapshot        entity.AgentSessionStateSnapshot
	snapshotSession        entity.AgentSession
	snapshotResult         entity.CommandResult
	snapshotEvent          entity.OutboxEvent
	createdAcceptance      entity.AcceptanceResult
	acceptanceResult       entity.CommandResult
	acceptanceEvent        entity.OutboxEvent
	updatedAcceptance      entity.AcceptanceResult
	updateAcceptanceResult entity.CommandResult
	updateAcceptanceEvent  *entity.OutboxEvent
	createdFollowUp        entity.FollowUpIntent
	followUpResult         entity.CommandResult
	followUpEvent          entity.OutboxEvent
	updatedFollowUp        entity.FollowUpIntent
	updateFollowUpResult   entity.CommandResult
	updateFollowUpEvent    *entity.OutboxEvent
	reservedFollowUp       entity.FollowUpIntent
	createdActivity        entity.AgentActivity
	activityResult         entity.CommandResult
	createdHumanGate       entity.HumanGateRequest
	humanGateResult        entity.CommandResult
	humanGateEvent         entity.OutboxEvent
	updatedHumanGate       entity.HumanGateRequest
	updateHumanGateResult  entity.CommandResult
	updateHumanGateEvent   *entity.OutboxEvent
	createdSelfDeploy      entity.SelfDeployPlan
	selfDeployResult       entity.CommandResult
	selfDeployEvent        entity.OutboxEvent
	updatedSelfDeploy      entity.SelfDeployPlan
	updateSelfDeployResult entity.CommandResult
	updateSelfDeployEvent  *entity.OutboxEvent
	createFlowCalled       bool
	createSessionCalled    bool
	createRunCalled        bool
	createAcceptanceCalled bool
	createFollowUpCalled   bool
	reserveFollowUpCalled  bool
	updateFollowUpCalled   bool
	createActivityCalled   bool
	createHumanGateCalled  bool
	updateHumanGateCalled  bool
	createSelfDeployCalled bool
	updateSelfDeployCalled bool
}

type fakeHumanGateRequester struct {
	last   HumanGateInteractionRequestInput
	result HumanGateInteractionRequestResult
	err    error
	calls  int
}

func (f *fakeHumanGateRequester) RequestHumanGate(_ context.Context, input HumanGateInteractionRequestInput) (HumanGateInteractionRequestResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return HumanGateInteractionRequestResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeHumanGateRequester) called() bool {
	return f.calls > 0
}

type fakeSelfDeployGatePreparer struct {
	last   SelfDeployPlanGatePreparationInput
	result SelfDeployPlanGatePreparationResult
	err    error
	calls  int
}

func (f *fakeSelfDeployGatePreparer) PrepareSelfDeployPlanGate(_ context.Context, input SelfDeployPlanGatePreparationInput) (SelfDeployPlanGatePreparationResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return SelfDeployPlanGatePreparationResult{}, f.err
	}
	return f.result, nil
}

type fakeSelfDeployBuildPlanReader struct {
	last   SelfDeployBuildPlanLookupInput
	result SelfDeployBuildPlanReadResult
	err    error
	calls  int
}

func (f *fakeSelfDeployBuildPlanReader) GetSelfDeployBuildPlan(_ context.Context, input SelfDeployBuildPlanLookupInput) (SelfDeployBuildPlanReadResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return SelfDeployBuildPlanReadResult{}, f.err
	}
	return f.result, nil
}

type fakeSelfDeployDeployPlanReader struct {
	last   SelfDeployDeployPlanLookupInput
	result SelfDeployDeployPlanReadResult
	err    error
	calls  int
}

func (f *fakeSelfDeployDeployPlanReader) GetSelfDeployDeployPlan(_ context.Context, input SelfDeployDeployPlanLookupInput) (SelfDeployDeployPlanReadResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return SelfDeployDeployPlanReadResult{}, f.err
	}
	return f.result, nil
}

type fakeSelfDeployBuildContextPreparer struct {
	lastPrepare SelfDeployBuildContextInput
	lastGet     SelfDeployBuildContextReadInput
	result      SelfDeployBuildContextResult
	err         error
	calls       int
}

func (f *fakeSelfDeployBuildContextPreparer) PrepareSelfDeployBuildContext(_ context.Context, input SelfDeployBuildContextInput) (SelfDeployBuildContextResult, error) {
	f.calls++
	f.lastPrepare = input
	if f.err != nil {
		return SelfDeployBuildContextResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeSelfDeployBuildContextPreparer) GetSelfDeployBuildContext(_ context.Context, input SelfDeployBuildContextReadInput) (SelfDeployBuildContextResult, error) {
	f.calls++
	f.lastGet = input
	if f.err != nil {
		return SelfDeployBuildContextResult{}, f.err
	}
	return f.result, nil
}

type fakeSelfDeployRuntimeJobReader struct {
	last   SelfDeployRuntimeJobReadInput
	result SelfDeployRuntimeJobReadResult
	err    error
	calls  int
}

func (f *fakeSelfDeployRuntimeJobReader) GetSelfDeployRuntimeJob(_ context.Context, input SelfDeployRuntimeJobReadInput) (SelfDeployRuntimeJobReadResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return SelfDeployRuntimeJobReadResult{}, f.err
	}
	result := f.result
	if result.JobRef == "" {
		result.JobRef = input.JobRef
	}
	if result.JobType == "" {
		result.JobType = input.JobType
	}
	return result, nil
}

type fakeSelfDeployBuildJobCreator struct {
	last   SelfDeployBuildJobInput
	result RuntimeJobResult
	err    error
	calls  int
}

func (f *fakeSelfDeployBuildJobCreator) CreateSelfDeployBuildJob(_ context.Context, input SelfDeployBuildJobInput) (RuntimeJobResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return RuntimeJobResult{}, f.err
	}
	return f.result, nil
}

type fakeSelfDeployDeployJobCreator struct {
	last   SelfDeployDeployJobInput
	result RuntimeJobResult
	err    error
	calls  int
}

func (f *fakeSelfDeployDeployJobCreator) CreateSelfDeployDeployJob(_ context.Context, input SelfDeployDeployJobInput) (RuntimeJobResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return RuntimeJobResult{}, f.err
	}
	return f.result, nil
}

func hasHumanGateContextRef(refs []HumanGateInteractionExternalRef, kind string, ref string) bool {
	for _, candidate := range refs {
		if candidate.Kind == kind && candidate.Ref == ref {
			return true
		}
	}
	return false
}

func hasHumanGateAction(actions []HumanGateInteractionAction, actionKey string) bool {
	for _, candidate := range actions {
		if candidate.ActionKey == actionKey && candidate.Terminal {
			return true
		}
	}
	return false
}

func hasAgentRunExecutionRef(refs []AgentRunExecutionRef, kind string, ref string) bool {
	for _, candidate := range refs {
		if candidate.Kind == kind && candidate.Ref == ref {
			return true
		}
	}
	return false
}

func (f *fakeRepository) CreateFlowWithResult(_ context.Context, flow entity.Flow, result entity.CommandResult) error {
	f.createFlowCalled = true
	f.createdFlow = flow
	f.createdResult = result
	return nil
}

func (f *fakeRepository) GetCommandResult(_ context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	if f.replay == nil {
		return entity.CommandResult{}, errs.ErrNotFound
	}
	if identity.CommandID != nil {
		if f.replay.CommandID == nil || *f.replay.CommandID != *identity.CommandID {
			return entity.CommandResult{}, errs.ErrNotFound
		}
		return *f.replay, nil
	}
	if f.replay.Operation != identity.Operation || f.replay.Actor != identity.Actor {
		return entity.CommandResult{}, errs.ErrNotFound
	}
	if f.replay.IdempotencyKey != identity.IdempotencyKey {
		return entity.CommandResult{}, errs.ErrNotFound
	}
	return *f.replay, nil
}

func (f *fakeRepository) UpdateFlowWithResult(context.Context, entity.Flow, int64, entity.CommandResult) error {
	return errors.ErrUnsupported
}

func (f *fakeRepository) GetFlow(_ context.Context, id uuid.UUID) (entity.Flow, error) {
	flow, ok := f.flowByID[id]
	if !ok {
		return entity.Flow{}, errors.ErrUnsupported
	}
	return flow, nil
}

func (f *fakeRepository) ListFlows(context.Context, query.FlowFilter) ([]entity.Flow, value.PageResult, error) {
	return nil, value.PageResult{}, errors.ErrUnsupported
}

func (f *fakeRepository) CreateFlowVersionWithResult(context.Context, entity.FlowVersion, entity.CommandResult) (entity.FlowVersion, error) {
	return entity.FlowVersion{}, errors.ErrUnsupported
}

func (f *fakeRepository) ActivateFlowVersionWithResult(context.Context, entity.Flow, int64, entity.FlowVersion, entity.CommandResult, entity.OutboxEvent) error {
	return errors.ErrUnsupported
}

func (f *fakeRepository) GetFlowVersion(_ context.Context, id uuid.UUID) (entity.FlowVersion, error) {
	if f.flowVersionByID != nil {
		version, ok := f.flowVersionByID[id]
		if ok {
			return version, nil
		}
	}
	return entity.FlowVersion{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListFlowVersions(context.Context, query.FlowVersionFilter) ([]entity.FlowVersion, value.PageResult, error) {
	return nil, value.PageResult{}, errors.ErrUnsupported
}

func (f *fakeRepository) CreateRoleProfileWithResult(context.Context, entity.RoleProfile, entity.CommandResult) error {
	return errors.ErrUnsupported
}

func (f *fakeRepository) UpdateRoleProfileWithResult(context.Context, entity.RoleProfile, int64, entity.CommandResult, *entity.OutboxEvent) error {
	return errors.ErrUnsupported
}

func (f *fakeRepository) GetRoleProfile(_ context.Context, id uuid.UUID) (entity.RoleProfile, error) {
	if f.roleByID != nil {
		role, ok := f.roleByID[id]
		if ok {
			return role, nil
		}
	}
	return entity.RoleProfile{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListRoleProfiles(context.Context, query.RoleProfileFilter) ([]entity.RoleProfile, value.PageResult, error) {
	return nil, value.PageResult{}, errors.ErrUnsupported
}

func (f *fakeRepository) CreatePromptTemplateWithResult(context.Context, entity.PromptTemplate, entity.CommandResult) error {
	return errors.ErrUnsupported
}

func (f *fakeRepository) GetPromptTemplate(context.Context, uuid.UUID) (entity.PromptTemplate, error) {
	return entity.PromptTemplate{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListPromptTemplates(context.Context, query.PromptTemplateFilter) ([]entity.PromptTemplate, value.PageResult, error) {
	return nil, value.PageResult{}, errors.ErrUnsupported
}

func (f *fakeRepository) CreatePromptTemplateVersionWithResult(context.Context, *entity.PromptTemplate, entity.PromptTemplateVersion, entity.CommandResult) (entity.PromptTemplateVersion, error) {
	return entity.PromptTemplateVersion{}, errors.ErrUnsupported
}

func (f *fakeRepository) ActivatePromptTemplateVersionWithResult(context.Context, entity.PromptTemplate, int64, entity.PromptTemplateVersion, entity.CommandResult, entity.OutboxEvent) error {
	return errors.ErrUnsupported
}

func (f *fakeRepository) GetPromptTemplateVersion(_ context.Context, id uuid.UUID) (entity.PromptTemplateVersion, error) {
	if f.promptVersionByID != nil {
		version, ok := f.promptVersionByID[id]
		if ok {
			return version, nil
		}
	}
	return entity.PromptTemplateVersion{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListPromptTemplateVersions(context.Context, query.PromptTemplateVersionFilter) ([]entity.PromptTemplateVersion, value.PageResult, error) {
	return nil, value.PageResult{}, errors.ErrUnsupported
}

func (f *fakeRepository) CreateAgentSessionWithResult(_ context.Context, session entity.AgentSession, result entity.CommandResult, event entity.OutboxEvent) error {
	f.createSessionCalled = true
	f.createdSession = session
	f.sessionResult = result
	f.sessionEvent = event
	return nil
}

func (f *fakeRepository) UpdateAgentSessionWithResult(_ context.Context, session entity.AgentSession, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	f.snapshotSession = session
	f.snapshotResult = result
	f.snapshotEvent = event
	return nil
}

func (f *fakeRepository) GetAgentSession(_ context.Context, id uuid.UUID) (entity.AgentSession, error) {
	if f.sessionByID != nil {
		session, ok := f.sessionByID[id]
		if ok {
			return session, nil
		}
	}
	return entity.AgentSession{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListAgentSessionSummaries(_ context.Context, filter query.AgentSessionFilter) ([]entity.AgentSessionListItem, value.PageResult, error) {
	f.sessionSummaryFilter = filter
	return f.sessionSummaryList, f.sessionSummaryPage, nil
}

func (f *fakeRepository) FindActiveAgentSessionByProviderWorkItem(_ context.Context, scope value.ScopeRef, providerWorkItemRef string) (entity.AgentSession, error) {
	if f.activeSessionFound && f.activeSession.Scope == scope && f.activeSession.ProviderWorkItemRef == providerWorkItemRef {
		return f.activeSession, nil
	}
	return entity.AgentSession{}, errs.ErrNotFound
}

func (f *fakeRepository) RecordCommandResult(_ context.Context, result entity.CommandResult) error {
	f.recordedCommandResult = result
	return nil
}

func (f *fakeRepository) CreateAgentRunWithResult(_ context.Context, run entity.AgentRun, result entity.CommandResult, event entity.OutboxEvent) error {
	f.createRunCalled = true
	f.createdRun = run
	f.runResult = result
	f.runEvent = event
	return nil
}

func (f *fakeRepository) UpdateAgentRunWithResult(_ context.Context, run entity.AgentRun, _ int64, result entity.CommandResult, event *entity.OutboxEvent) error {
	f.updatedRun = run
	f.updateRunResult = result
	f.updateRunEvent = event
	return nil
}

func (f *fakeRepository) GetAgentRun(_ context.Context, id uuid.UUID) (entity.AgentRun, error) {
	if f.runByID != nil {
		run, ok := f.runByID[id]
		if ok {
			return run, nil
		}
	}
	return entity.AgentRun{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListAgentRuns(context.Context, query.AgentRunFilter) ([]entity.AgentRun, value.PageResult, error) {
	return nil, value.PageResult{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListAgentRunSummaries(_ context.Context, filter query.AgentRunSummaryFilter) ([]entity.AgentRunListItem, value.PageResult, error) {
	f.runSummaryFilter = filter
	return f.runSummaryList, f.runSummaryPage, nil
}

func (f *fakeRepository) CreateSessionStateSnapshotWithResult(_ context.Context, snapshot entity.AgentSessionStateSnapshot, session entity.AgentSession, _ int64, result entity.CommandResult, event entity.OutboxEvent) error {
	f.createdSnapshot = snapshot
	f.snapshotSession = session
	f.snapshotResult = result
	f.snapshotEvent = event
	return nil
}

func (f *fakeRepository) GetSessionStateSnapshot(context.Context, uuid.UUID) (entity.AgentSessionStateSnapshot, error) {
	return entity.AgentSessionStateSnapshot{}, errors.ErrUnsupported
}

func (f *fakeRepository) CreateAcceptanceResultWithResult(_ context.Context, acceptance entity.AcceptanceResult, result entity.CommandResult, event entity.OutboxEvent) error {
	f.createAcceptanceCalled = true
	f.createdAcceptance = acceptance
	f.acceptanceResult = result
	f.acceptanceEvent = event
	return nil
}

func (f *fakeRepository) UpdateAcceptanceResultWithResult(_ context.Context, acceptance entity.AcceptanceResult, _ int64, result entity.CommandResult, event *entity.OutboxEvent) error {
	f.updatedAcceptance = acceptance
	f.updateAcceptanceResult = result
	f.updateAcceptanceEvent = event
	return nil
}

func (f *fakeRepository) GetAcceptanceResult(_ context.Context, id uuid.UUID) (entity.AcceptanceResult, error) {
	if f.acceptanceByID != nil {
		acceptance, ok := f.acceptanceByID[id]
		if ok {
			return acceptance, nil
		}
	}
	if f.acceptanceGetErr != nil {
		return entity.AcceptanceResult{}, f.acceptanceGetErr
	}
	return entity.AcceptanceResult{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListAcceptanceResults(context.Context, query.AcceptanceResultFilter) ([]entity.AcceptanceResult, value.PageResult, error) {
	return nil, value.PageResult{}, errors.ErrUnsupported
}

func (f *fakeRepository) CreateFollowUpIntentWithResult(_ context.Context, intent entity.FollowUpIntent, result entity.CommandResult, event entity.OutboxEvent) error {
	f.createFollowUpCalled = true
	f.createdFollowUp = intent
	f.followUpResult = result
	f.followUpEvent = event
	return nil
}

func (f *fakeRepository) ReserveFollowUpIntentDispatch(_ context.Context, intent entity.FollowUpIntent, previousVersion int64) error {
	stored, ok := f.followUpByID[intent.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Version != previousVersion || intent.Version != previousVersion+1 || !dispatchableFollowUpStatus(stored.Status) {
		return errs.ErrConflict
	}
	f.reserveFollowUpCalled = true
	f.reservedFollowUp = intent
	if f.followUpByID != nil {
		f.followUpByID[intent.ID] = intent
	}
	return nil
}

func (f *fakeRepository) UpdateFollowUpIntentWithResult(_ context.Context, intent entity.FollowUpIntent, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error {
	stored, ok := f.followUpByID[intent.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Version != previousVersion || intent.Version != previousVersion+1 {
		return errs.ErrConflict
	}
	f.updateFollowUpCalled = true
	f.updatedFollowUp = intent
	f.updateFollowUpResult = result
	f.updateFollowUpEvent = event
	if f.followUpByID != nil {
		f.followUpByID[intent.ID] = intent
	}
	return nil
}

func (f *fakeRepository) GetFollowUpIntent(_ context.Context, id uuid.UUID) (entity.FollowUpIntent, error) {
	if f.followUpByID != nil {
		intent, ok := f.followUpByID[id]
		if ok {
			return intent, nil
		}
	}
	return entity.FollowUpIntent{}, errors.ErrUnsupported
}

func (f *fakeRepository) RecordAgentActivityWithResult(_ context.Context, activity entity.AgentActivity, result entity.CommandResult) error {
	f.createActivityCalled = true
	f.createdActivity = activity
	f.activityResult = result
	return nil
}

func (f *fakeRepository) GetAgentActivity(_ context.Context, id uuid.UUID) (entity.AgentActivity, error) {
	if f.activityByID != nil {
		activity, ok := f.activityByID[id]
		if ok {
			return activity, nil
		}
	}
	return entity.AgentActivity{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListAgentActivities(_ context.Context, filter query.AgentActivityFilter) ([]entity.AgentActivity, value.PageResult, error) {
	f.activityFilter = filter
	return f.activityList, f.activityPage, nil
}

func (f *fakeRepository) CreateHumanGateRequestWithResult(_ context.Context, gate entity.HumanGateRequest, result entity.CommandResult, event entity.OutboxEvent) error {
	f.createHumanGateCalled = true
	f.createdHumanGate = gate
	f.humanGateResult = result
	f.humanGateEvent = event
	if f.humanGateByID != nil {
		f.humanGateByID[gate.ID] = gate
	}
	return nil
}

func (f *fakeRepository) UpdateHumanGateRequestWithResult(_ context.Context, gate entity.HumanGateRequest, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error {
	stored, ok := f.humanGateByID[gate.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Version != previousVersion || gate.Version != previousVersion+1 {
		return errs.ErrConflict
	}
	f.updateHumanGateCalled = true
	f.updatedHumanGate = gate
	f.updateHumanGateResult = result
	f.updateHumanGateEvent = event
	if f.humanGateByID != nil {
		f.humanGateByID[gate.ID] = gate
	}
	return nil
}

func (f *fakeRepository) GetHumanGateRequest(_ context.Context, id uuid.UUID) (entity.HumanGateRequest, error) {
	if f.humanGateByID != nil {
		gate, ok := f.humanGateByID[id]
		if ok {
			return gate, nil
		}
	}
	return entity.HumanGateRequest{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListHumanGateRequests(_ context.Context, filter query.HumanGateFilter) ([]entity.HumanGateRequest, value.PageResult, error) {
	f.humanGateFilter = filter
	return f.humanGateList, f.humanGatePage, nil
}

func (f *fakeRepository) CreateSelfDeployPlanWithResult(_ context.Context, plan entity.SelfDeployPlan, result entity.CommandResult, event entity.OutboxEvent) error {
	f.createSelfDeployCalled = true
	f.createdSelfDeploy = plan
	f.selfDeployResult = result
	f.selfDeployEvent = event
	if f.selfDeployByID != nil {
		f.selfDeployByID[plan.ID] = plan
	}
	return nil
}

func (f *fakeRepository) UpdateSelfDeployPlanWithResult(_ context.Context, plan entity.SelfDeployPlan, previousVersion int64, result entity.CommandResult, event *entity.OutboxEvent) error {
	if len(f.updateSelfDeployErrs) > 0 {
		err := f.updateSelfDeployErrs[0]
		f.updateSelfDeployErrs = f.updateSelfDeployErrs[1:]
		if f.updateSelfDeployOnErr != nil {
			f.updateSelfDeployOnErr()
		}
		return err
	}
	if f.updateSelfDeployErr != nil {
		return f.updateSelfDeployErr
	}
	stored, ok := f.selfDeployByID[plan.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if stored.Version != previousVersion || plan.Version != previousVersion+1 {
		return errs.ErrConflict
	}
	f.updateSelfDeployCalled = true
	f.updatedSelfDeploy = plan
	f.updateSelfDeployResult = result
	f.updateSelfDeployEvent = event
	if f.selfDeployByID != nil {
		f.selfDeployByID[plan.ID] = plan
	}
	return nil
}

func (f *fakeRepository) GetSelfDeployPlan(_ context.Context, id uuid.UUID) (entity.SelfDeployPlan, error) {
	if f.selfDeployByID != nil {
		plan, ok := f.selfDeployByID[id]
		if ok {
			return plan, nil
		}
	}
	return entity.SelfDeployPlan{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListSelfDeployPlans(_ context.Context, filter query.SelfDeployPlanFilter) ([]entity.SelfDeployPlan, value.PageResult, error) {
	f.selfDeployFilter = filter
	return f.selfDeployList, f.selfDeployPage, nil
}

func (f *fakeRepository) ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error) {
	return nil, errors.ErrUnsupported
}

func (f *fakeRepository) MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error {
	return errors.ErrUnsupported
}

func (f *fakeRepository) MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return errors.ErrUnsupported
}

func (f *fakeRepository) MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return errors.ErrUnsupported
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type fixedIDGenerator struct {
	ids []uuid.UUID
}

func (g fixedIDGenerator) New() uuid.UUID {
	if len(g.ids) == 0 {
		return uuid.Nil
	}
	return g.ids[0]
}

type sequenceIDGenerator struct {
	ids   []uuid.UUID
	index int
}

func (g *sequenceIDGenerator) New() uuid.UUID {
	if g.index >= len(g.ids) {
		return uuid.Nil
	}
	id := g.ids[g.index]
	g.index++
	return id
}

func testActor() value.Actor {
	return value.Actor{Type: "user", ID: "owner"}
}

func ptr(value string) *string {
	return &value
}

func agentRunWithRuntimeContext(run entity.AgentRun, runtimeContext value.RuntimeContextRef) entity.AgentRun {
	run.RuntimeContext = runtimeContext
	return run
}

func agentRunWithStatus(run entity.AgentRun, status enum.AgentRunStatus) entity.AgentRun {
	run.Status = status
	return run
}

type fakeGuidanceResolver struct {
	refs  []value.GuidanceRef
	err   error
	calls int
	last  GuidanceResolutionInput
}

func (f *fakeGuidanceResolver) ResolveGuidanceRefs(_ context.Context, input GuidanceResolutionInput) ([]value.GuidanceRef, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return nil, f.err
	}
	return append([]value.GuidanceRef(nil), f.refs...), nil
}

type runtimePreparationFixture struct {
	now              time.Time
	projectID        uuid.UUID
	repositoryID     uuid.UUID
	documentationID  uuid.UUID
	sessionID        uuid.UUID
	roleID           uuid.UUID
	promptVersionID  uuid.UUID
	runID            uuid.UUID
	input            StartAgentRunInput
	ids              []uuid.UUID
	repository       *fakeRepository
	guidanceResolver *fakeGuidanceResolver
	policyResolver   *fakeWorkspacePolicyResolver
}

func newRuntimePreparationFixture() runtimePreparationFixture {
	projectID := uuid.MustParse("10101010-1111-2222-3333-444444444444")
	repositoryID := uuid.MustParse("10101010-2222-3333-4444-555555555555")
	documentationID := uuid.MustParse("10101010-3333-4444-5555-666666666666")
	sessionID := uuid.MustParse("10101010-4444-5555-6666-777777777777")
	roleID := uuid.MustParse("10101010-5555-6666-7777-888888888888")
	promptVersionID := uuid.MustParse("10101010-6666-7777-8888-999999999999")
	runID := uuid.MustParse("10101010-7777-8888-9999-aaaaaaaaaaaa")
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	repository := &fakeRepository{
		sessionByID: map[uuid.UUID]entity.AgentSession{
			sessionID: {
				VersionedBase:       entity.VersionedBase{ID: sessionID, Version: 1},
				Scope:               value.ScopeRef{Type: string(enum.AgentScopeTypeProject), Ref: projectID.String()},
				ProviderWorkItemRef: "github:issue:42",
				Status:              enum.AgentSessionStatusOpen,
			},
		},
		roleByID: map[uuid.UUID]entity.RoleProfile{
			roleID: {
				VersionedBase:   entity.VersionedBase{ID: roleID, Version: 2},
				RoleKind:        enum.RoleKindWorker,
				RuntimeProfile:  "go-full",
				AllowedMCPTools: []string{"github.create_pr", "runtime.get_workspace"},
				Status:          enum.RoleStatusActive,
			},
		},
		promptVersionByID: map[uuid.UUID]entity.PromptTemplateVersion{
			promptVersionID: {
				ID:             promptVersionID,
				RoleProfileID:  roleID,
				PromptKind:     enum.PromptKindWork,
				TemplateObject: value.ObjectRef{ObjectURI: "object://instructions/work-v1", ObjectDigest: testInstructionDigest},
				TemplateDigest: "sha256:prompt",
				Status:         enum.PromptVersionStatusActive,
			},
		},
	}
	guidanceResolver := &fakeGuidanceResolver{refs: []value.GuidanceRef{{
		PackageInstallationRef: "installation-go",
		PackageVersionRef:      "version-go",
		ManifestDigest:         "sha256:guidance",
		SourceRef:              "PACKAGE_VERSION_SOURCE_REF_KIND_GIT_TAG:v1.0.0:abc123",
		CapabilityRef:          "guidance:installation-go",
		CapabilityKind:         "guidance",
		PackageRef:             "package-go",
		PackageSlug:            "go-guidelines",
		PackageVersionLabel:    "v1.0.0",
		PolicySummaryJSON:      `{"status":"safe"}`,
	}}}
	policyResolver := &fakeWorkspacePolicyResolver{policy: WorkspacePolicySnapshot{
		ProjectID: projectID,
		CodeSources: []WorkspaceCodeSource{{
			RepositoryID:  repositoryID,
			Provider:      "github",
			ProviderOwner: "codex-k8s",
			ProviderName:  "example-api",
			DefaultBranch: "main",
			LocalPath:     "src/example-api",
			AccessMode:    WorkspaceSourceAccessWrite,
		}},
		DocumentationSources: []WorkspaceDocumentationSource{{
			DocumentationSourceID: documentationID,
			RepositoryID:          &repositoryID,
			ScopeType:             "DOCUMENTATION_SCOPE_TYPE_PROJECT",
			LocalPath:             "docs/project",
			AccessMode:            WorkspaceSourceAccessRead,
		}},
		GuidancePackageRefs: []string{"installation-go"},
		PolicyVersion:       7,
	}}
	return runtimePreparationFixture{
		now:             now,
		projectID:       projectID,
		repositoryID:    repositoryID,
		documentationID: documentationID,
		sessionID:       sessionID,
		roleID:          roleID,
		promptVersionID: promptVersionID,
		runID:           runID,
		input: StartAgentRunInput{
			Meta:                    value.CommandMeta{CommandID: uuid.MustParse("10101010-8888-9999-aaaa-bbbbbbbbbbbb"), Actor: testActor()},
			SessionID:               sessionID,
			RoleProfileID:           roleID,
			PromptTemplateVersionID: promptVersionID,
			GuidanceSelectionHints:  []value.GuidanceSelectionHint{{PackageSlug: "go-guidelines"}},
		},
		ids: []uuid.UUID{
			runID,
			uuid.MustParse("10101010-9999-aaaa-bbbb-cccccccccccc"),
			uuid.MustParse("10101010-aaaa-bbbb-cccc-dddddddddddd"),
		},
		repository:       repository,
		guidanceResolver: guidanceResolver,
		policyResolver:   policyResolver,
	}
}

type fakeWorkspacePolicyResolver struct {
	policy WorkspacePolicySnapshot
	err    error
	calls  int
	last   WorkspacePolicyResolutionInput
}

func (f *fakeWorkspacePolicyResolver) ResolveWorkspacePolicy(_ context.Context, input WorkspacePolicyResolutionInput) (WorkspacePolicySnapshot, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return WorkspacePolicySnapshot{}, f.err
	}
	return f.policy, nil
}

type fakeRuntimePreparer struct {
	result RuntimePreparationResult
	err    error
	calls  int
	last   RuntimePreparationInput
}

func (f *fakeRuntimePreparer) PrepareRuntime(_ context.Context, input RuntimePreparationInput) (RuntimePreparationResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return RuntimePreparationResult{}, f.err
	}
	return f.result, nil
}

type fakeRuntimeJobCreator struct {
	result RuntimeJobResult
	err    error
	calls  int
	last   RuntimeJobInput
}

func (f *fakeRuntimeJobCreator) CreateAgentRunJob(_ context.Context, input RuntimeJobInput) (RuntimeJobResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return RuntimeJobResult{}, f.err
	}
	return f.result, nil
}

type fakeRuntimeJobReader struct {
	result RuntimeJobReadResult
	err    error
	calls  int
	last   RuntimeJobReadInput
}

func (f *fakeRuntimeJobReader) GetAgentRunJob(_ context.Context, input RuntimeJobReadInput) (RuntimeJobReadResult, error) {
	f.calls++
	f.last = input
	if f.err != nil {
		return RuntimeJobReadResult{}, f.err
	}
	return f.result, nil
}

type fakeProviderFollowUpDispatcher struct {
	result                  ProviderCommandResult
	err                     error
	calls                   int
	input                   ProviderCreateIssueInput
	inputs                  []ProviderCreateIssueInput
	updateIssueCalls        int
	updateIssueInput        ProviderUpdateIssueInput
	createCommentCalls      int
	createCommentInput      ProviderCreateCommentInput
	updateCommentCalls      int
	updateCommentInput      ProviderUpdateCommentInput
	updatePullRequestCalls  int
	updatePullRequestInput  ProviderUpdatePullRequestInput
	createReviewSignalCalls int
	createReviewSignalInput ProviderCreateReviewSignalInput
}

func (f *fakeProviderFollowUpDispatcher) CreateIssue(_ context.Context, input ProviderCreateIssueInput) (ProviderCommandResult, error) {
	f.calls++
	f.input = input
	f.inputs = append(f.inputs, input)
	return f.result, f.err
}

func (f *fakeProviderFollowUpDispatcher) UpdateIssue(_ context.Context, input ProviderUpdateIssueInput) (ProviderCommandResult, error) {
	f.updateIssueCalls++
	f.updateIssueInput = input
	return f.result, f.err
}

func (f *fakeProviderFollowUpDispatcher) CreateComment(_ context.Context, input ProviderCreateCommentInput) (ProviderCommandResult, error) {
	f.createCommentCalls++
	f.createCommentInput = input
	return f.result, f.err
}

func (f *fakeProviderFollowUpDispatcher) UpdateComment(_ context.Context, input ProviderUpdateCommentInput) (ProviderCommandResult, error) {
	f.updateCommentCalls++
	f.updateCommentInput = input
	return f.result, f.err
}

func (f *fakeProviderFollowUpDispatcher) UpdatePullRequest(_ context.Context, input ProviderUpdatePullRequestInput) (ProviderCommandResult, error) {
	f.updatePullRequestCalls++
	f.updatePullRequestInput = input
	return f.result, f.err
}

func (f *fakeProviderFollowUpDispatcher) CreateReviewSignal(_ context.Context, input ProviderCreateReviewSignalInput) (ProviderCommandResult, error) {
	f.createReviewSignalCalls++
	f.createReviewSignalInput = input
	return f.result, f.err
}
