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

func decodeAgentPayload(t *testing.T, event entity.OutboxEvent) agentevents.Payload {
	t.Helper()

	var payload agentevents.Payload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatalf("decode event payload: %v", err)
	}
	return payload
}

type fakeRepository struct {
	replay           *entity.CommandResult
	createdFlow      entity.Flow
	createdResult    entity.CommandResult
	flowByID         map[uuid.UUID]entity.Flow
	createFlowCalled bool
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
	if f.replay.Operation != identity.Operation || f.replay.Actor != identity.Actor {
		return entity.CommandResult{}, errs.ErrNotFound
	}
	if identity.CommandID != nil {
		if f.replay.CommandID == nil || *f.replay.CommandID != *identity.CommandID {
			return entity.CommandResult{}, errs.ErrNotFound
		}
		return *f.replay, nil
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

func (f *fakeRepository) GetRoleProfile(context.Context, uuid.UUID) (entity.RoleProfile, error) {
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

func (f *fakeRepository) GetPromptTemplateVersion(context.Context, uuid.UUID) (entity.PromptTemplateVersion, error) {
	return entity.PromptTemplateVersion{}, errors.ErrUnsupported
}

func (f *fakeRepository) ListPromptTemplateVersions(context.Context, query.PromptTemplateVersionFilter) ([]entity.PromptTemplateVersion, value.PageResult, error) {
	return nil, value.PageResult{}, errors.ErrUnsupported
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

func testActor() value.Actor {
	return value.Actor{Type: "user", ID: "owner"}
}
