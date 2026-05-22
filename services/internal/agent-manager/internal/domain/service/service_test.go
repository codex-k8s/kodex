package service

import (
	"context"
	"encoding/json"
	"errors"
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
		Meta:   value.CommandMeta{CommandID: uuid.MustParse("ffffffff-1111-2222-3333-444444444444"), ExpectedVersion: &expectedVersion, Actor: testActor()},
		RunID:  runID,
		Status: enum.AgentRunStatusRunning,
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

func decodeAgentPayload(t *testing.T, event entity.OutboxEvent) agentevents.Payload {
	t.Helper()

	var payload agentevents.Payload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	return payload
}

type fakeRepository struct {
	replay              *entity.CommandResult
	createdFlow         entity.Flow
	createdResult       entity.CommandResult
	flowByID            map[uuid.UUID]entity.Flow
	sessionByID         map[uuid.UUID]entity.AgentSession
	runByID             map[uuid.UUID]entity.AgentRun
	roleByID            map[uuid.UUID]entity.RoleProfile
	promptVersionByID   map[uuid.UUID]entity.PromptTemplateVersion
	createdSession      entity.AgentSession
	sessionResult       entity.CommandResult
	sessionEvent        entity.OutboxEvent
	createdRun          entity.AgentRun
	runResult           entity.CommandResult
	runEvent            entity.OutboxEvent
	updatedRun          entity.AgentRun
	updateRunResult     entity.CommandResult
	updateRunEvent      *entity.OutboxEvent
	createdSnapshot     entity.AgentSessionStateSnapshot
	snapshotSession     entity.AgentSession
	snapshotResult      entity.CommandResult
	snapshotEvent       entity.OutboxEvent
	createFlowCalled    bool
	createSessionCalled bool
	createRunCalled     bool
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

func (f *fakeRepository) GetFlowVersion(context.Context, uuid.UUID) (entity.FlowVersion, error) {
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
