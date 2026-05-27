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
	for _, source := range runtimePreparer.last.WorkspacePolicy.Sources {
		kinds[source.Kind]++
	}
	if kinds[WorkspaceSourceKindCode] != 1 || kinds[WorkspaceSourceKindDocumentation] != 1 ||
		kinds[WorkspaceSourceKindGuidancePackage] != 1 || kinds[WorkspaceSourceKindGeneratedContext] != 1 {
		t.Fatalf("workspace source kinds = %+v", kinds)
	}
	if runtimePreparer.last.WorkspacePolicy.PolicyDigest == "" {
		t.Fatal("workspace policy digest is empty")
	}
	if fixture.repository.updatedRun.Status != enum.AgentRunStatusStarting || fixture.repository.updateRunEvent == nil ||
		fixture.repository.updateRunEvent.EventType != agentevents.EventRunStarted {
		t.Fatalf("updated run/event = %+v/%+v", fixture.repository.updatedRun, fixture.repository.updateRunEvent)
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

func TestStartAgentRunRuntimeRequestDoesNotCarryTextPayloads(t *testing.T) {
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
	if repository.updateAcceptanceResult.AggregateType != enum.CommandAggregateTypeAcceptance || repository.updateAcceptanceEvent == nil || repository.updateAcceptanceEvent.EventType != agentevents.EventAcceptanceCompleted {
		t.Fatalf("result/event = %s/%+v", repository.updateAcceptanceResult.AggregateType, repository.updateAcceptanceEvent)
	}
	payload := decodeAgentPayload(t, *repository.updateAcceptanceEvent)
	if payload.AcceptanceResultID != acceptanceID.String() || payload.Status != string(enum.AcceptanceStatusPassed) || payload.Version != expectedVersion+1 {
		t.Fatalf("event payload = %+v", payload)
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
		Status:               enum.FollowUpIntentStatusRequested,
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
		SafeTitle:            "Different title",
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
		VersionedBase:         entity.VersionedBase{ID: gateID, Version: 1},
		SessionID:             sessionID,
		RequestKind:           "owner_decision",
		ReasonCode:            "needs_owner_approval",
		InteractionRequestRef: "interaction:request/42",
		IdempotencyKey:        operationRequestHumanGate + ":user:owner:human-gate-decision",
		Status:                enum.HumanGateStatusWaiting,
		Outcome:               enum.HumanGateOutcomeNone,
	}
	repository := &fakeRepository{humanGateByID: map[uuid.UUID]entity.HumanGateRequest{gateID: gate}}
	service := New(Config{
		Repository:  repository,
		Clock:       fixedClock{now: now},
		IDGenerator: fixedIDGenerator{ids: []uuid.UUID{eventID}},
	})
	expectedVersion := int64(1)

	resolved, err := service.RecordHumanGateDecision(context.Background(), RecordHumanGateDecisionInput{
		Meta:                   value.CommandMeta{IdempotencyKey: "human-gate-decision", ExpectedVersion: &expectedVersion, Actor: testActor()},
		HumanGateRequestID:     gateID,
		Status:                 enum.HumanGateStatusResolved,
		Outcome:                enum.HumanGateOutcomeApprove,
		SafeSummary:            "Owner approved the next step",
		InteractionResponseRef: "interaction:response/42",
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
	if repository.updateHumanGateEvent == nil || repository.updateHumanGateEvent.EventType != agentevents.EventHumanGateResolved {
		t.Fatalf("event = %+v", repository.updateHumanGateEvent)
	}
	payload := decodeAgentPayload(t, *repository.updateHumanGateEvent)
	if payload.HumanGateOutcome != string(enum.HumanGateOutcomeApprove) || payload.InteractionResponseRef != "interaction:response/42" {
		t.Fatalf("payload = %+v", payload)
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
	humanGateByID          map[uuid.UUID]entity.HumanGateRequest
	humanGateList          []entity.HumanGateRequest
	humanGatePage          value.PageResult
	humanGateFilter        query.HumanGateFilter
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
