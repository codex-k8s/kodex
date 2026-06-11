package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/repository/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

func TestReserveSlotPersistsLeaseAndFleetPlacementRefs(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, repo := newTestServiceWithPlacementResolver(resolver)
	projectID := mustUUID("00000000-0000-0000-0000-000000000021")

	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-sha",
		ProjectID:             &projectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000101"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	if slot.Status != enum.SlotStatusReserved {
		t.Fatalf("Status = %s, want reserved", slot.Status)
	}
	if slot.FleetScopeID == nil || *slot.FleetScopeID != testFleetScopeID {
		t.Fatalf("FleetScopeID = %v, want %s", slot.FleetScopeID, testFleetScopeID)
	}
	if slot.ClusterID == nil || *slot.ClusterID != testClusterID {
		t.Fatalf("ClusterID = %v, want %s", slot.ClusterID, testClusterID)
	}
	if slot.LeaseOwner != "service:agent-manager" {
		t.Fatalf("LeaseOwner = %s, want service:agent-manager", slot.LeaseOwner)
	}
	if slot.LeaseUntil == nil || !slot.LeaseUntil.Equal(testNow.Add(30*time.Minute)) {
		t.Fatalf("LeaseUntil = %v, want default ttl", slot.LeaseUntil)
	}
	if len(repo.events) != 1 || repo.events[0].EventType != eventSlotReserved {
		t.Fatalf("events = %#v, want slot reserved", repo.events)
	}
	if len(resolver.requests) != 1 {
		t.Fatalf("placement resolver calls = %d, want 1", len(resolver.requests))
	}
	if resolver.requests[0].ProjectID == nil || *resolver.requests[0].ProjectID != projectID {
		t.Fatalf("placement project = %v, want %s", resolver.requests[0].ProjectID, projectID)
	}
}

func TestReserveSlotIdempotentReplayChecksScope(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	meta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000102"), 0)
	projectID := mustUUID("00000000-0000-0000-0000-000000000022")
	otherProjectID := mustUUID("00000000-0000-0000-0000-000000000023")

	first, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		ProjectID:             &projectID,
		Meta:                  meta,
	})
	if err != nil {
		t.Fatalf("first ReserveSlot(): %v", err)
	}
	replay, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		ProjectID:             &projectID,
		Meta:                  meta,
	})
	if err != nil {
		t.Fatalf("replay ReserveSlot(): %v", err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay slot id = %s, want %s", replay.ID, first.ID)
	}
	_, err = svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		ProjectID:             &otherProjectID,
		Meta:                  meta,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("cross-scope replay error = %v, want conflict", err)
	}
}

func TestReserveSlotReplayRejectsChangedPlacementInput(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, _ := newTestServiceWithPlacementResolver(resolver)
	meta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000115"), 0)

	_, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		PlacementConstraints: PlacementConstraintsInput{
			RequiredCapabilities: []string{"standard"},
			MetadataJSON:         []byte(`{"regions":["eu-1"]}`),
		},
		Meta: meta,
	})
	if err != nil {
		t.Fatalf("first ReserveSlot(): %v", err)
	}
	_, err = svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		PlacementConstraints: PlacementConstraintsInput{
			RequiredCapabilities: []string{"gpu"},
			MetadataJSON:         []byte(`{"regions":["eu-1"]}`),
		},
		Meta: meta,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("changed placement replay error = %v, want conflict", err)
	}
	if len(resolver.requests) != 1 {
		t.Fatalf("placement resolver calls = %d, want no fleet call on conflicting replay", len(resolver.requests))
	}
}

func TestReserveSlotRejectsUnsupportedWidePlacementRequest(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, _ := newTestServiceWithPlacementResolver(resolver)
	firstRepositoryID := mustUUID("00000000-0000-0000-0000-000000000116")
	secondRepositoryID := mustUUID("00000000-0000-0000-0000-000000000117")

	_, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		RepositoryIDs:         []uuid.UUID{firstRepositoryID, secondRepositoryID},
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000118"), 0),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ReserveSlot() err = %v, want invalid argument for multi-repository placement", err)
	}
	if len(resolver.requests) != 0 {
		t.Fatalf("placement resolver calls = %d, want none for unsupported request", len(resolver.requests))
	}
}

func TestReserveSlotIdempotencyKeyIsScopedByActor(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	metaA := idempotencyMeta("shared-client-key", "agent-manager-a")
	metaB := idempotencyMeta("shared-client-key", "agent-manager-b")

	first, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		Meta:                  metaA,
	})
	if err != nil {
		t.Fatalf("first ReserveSlot(): %v", err)
	}
	second, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		Meta:                  metaB,
	})
	if err != nil {
		t.Fatalf("second ReserveSlot(): %v", err)
	}
	if second.ID == first.ID {
		t.Fatalf("second slot id = %s, want separate actor-scoped command result", second.ID)
	}
}

func TestExtendReleaseAndFailSlotUseExpectedVersion(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-sha",
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000103"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	extendedUntil := testNow.Add(time.Hour)
	extended, err := svc.ExtendSlotLease(context.Background(), ExtendSlotLeaseInput{
		SlotID:     slot.ID,
		LeaseOwner: slot.LeaseOwner,
		LeaseUntil: extendedUntil,
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000104"), slot.Version),
	})
	if err != nil {
		t.Fatalf("ExtendSlotLease(): %v", err)
	}
	if extended.Version != 2 || extended.LeaseUntil == nil || !extended.LeaseUntil.Equal(extendedUntil) {
		t.Fatalf("extended slot = %#v, want version 2 and extended lease", extended)
	}
	_, err = svc.ReleaseSlot(context.Background(), ReleaseSlotInput{
		SlotID:     slot.ID,
		LeaseOwner: slot.LeaseOwner,
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000105"), 1),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale ReleaseSlot() error = %v, want conflict", err)
	}
	failed, err := svc.MarkSlotFailed(context.Background(), MarkSlotFailedInput{
		SlotID:       slot.ID,
		ErrorCode:    "KUBERNETES_ERROR",
		ErrorMessage: "pod failed",
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000106"), extended.Version),
	})
	if err != nil {
		t.Fatalf("MarkSlotFailed(): %v", err)
	}
	if failed.Status != enum.SlotStatusFailed || failed.LastErrorCode != "KUBERNETES_ERROR" {
		t.Fatalf("failed slot = %#v, want failed with error code", failed)
	}
}

func TestExpiredLeaseCannotBeExtended(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-sha",
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000107"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	expired := testNow.Add(-time.Minute)
	slot.LeaseUntil = &expired
	repo.slots[slot.ID] = slot

	_, err = svc.ExtendSlotLease(context.Background(), ExtendSlotLeaseInput{
		SlotID:     slot.ID,
		LeaseOwner: slot.LeaseOwner,
		LeaseUntil: testNow.Add(time.Hour),
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000108"), slot.Version),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("ExtendSlotLease() err = %v, want conflict for expired lease", err)
	}
}

func TestExistingSlotCommandsAuthorizeWithCurrentProjectScopeBeforeReplay(t *testing.T) {
	t.Parallel()

	authorizer := &recordAuthorizer{}
	svc, _ := newTestServiceWithAuthorizer(authorizer)
	projectID := mustUUID("00000000-0000-0000-0000-000000000024")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-sha",
		ProjectID:             &projectID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000110"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	authorizer.requests = nil
	extendMeta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000111"), slot.Version)
	_, err = svc.ExtendSlotLease(context.Background(), ExtendSlotLeaseInput{
		SlotID:     slot.ID,
		LeaseOwner: slot.LeaseOwner,
		LeaseUntil: testNow.Add(time.Hour),
		Meta:       extendMeta,
	})
	if err != nil {
		t.Fatalf("ExtendSlotLease(): %v", err)
	}
	if len(authorizer.requests) != 1 {
		t.Fatalf("authorization requests = %d, want 1", len(authorizer.requests))
	}
	request := authorizer.requests[0]
	if request.ScopeType != accesscatalog.ScopeProject || request.ScopeID != projectID.String() {
		t.Fatalf("scope = %s/%s, want project/%s", request.ScopeType, request.ScopeID, projectID)
	}
	if request.ResourceID != slot.ID.String() || request.ActionKey != actionSlotExtendLease {
		t.Fatalf("resource/action = %s/%s, want %s/%s", request.ResourceID, request.ActionKey, slot.ID, actionSlotExtendLease)
	}

	authorizer.deny = true
	_, err = svc.ExtendSlotLease(context.Background(), ExtendSlotLeaseInput{
		SlotID:     slot.ID,
		LeaseOwner: slot.LeaseOwner,
		LeaseUntil: testNow.Add(time.Hour),
		Meta:       extendMeta,
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("replayed ExtendSlotLease() err = %v, want forbidden before replay", err)
	}
}

func TestReserveSlotAuthorizesBeforeReplay(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{
		slots:                     make(map[uuid.UUID]entity.Slot),
		workspaceMaterializations: make(map[uuid.UUID]entity.WorkspaceMaterialization),
		buildContexts:             make(map[uuid.UUID]entity.BuildContext),
		jobs:                      make(map[uuid.UUID]entity.Job),
		runtimeArtifactRefs:       make(map[uuid.UUID]entity.RuntimeArtifactRef),
		cleanupPolicies:           make(map[uuid.UUID]entity.CleanupPolicy),
		prewarmPools:              make(map[uuid.UUID]entity.PrewarmPool),
		results:                   make(map[string]entity.CommandResult),
	}
	svc := NewWithConfig(repo, fixedClock{now: testNow}, &sequenceIDs{values: []uuid.UUID{mustUUID("00000000-0000-0000-0000-000000000209")}}, Config{
		DefaultFleetScopeID: testFleetScopeID,
		DefaultClusterID:    testClusterID,
		NamespacePrefix:     "kodex-rt",
		DefaultLeaseTTL:     30 * time.Minute,
		Authorizer:          denyAuthorizer{},
		PlacementResolver:   defaultPlacementResolver(),
	})

	_, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeCodeOnly,
		WorkspacePolicyDigest: "policy-sha",
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000109"), 0),
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("ReserveSlot() err = %v, want forbidden", err)
	}
	if len(repo.events) != 0 {
		t.Fatalf("events = %d, want no mutation before authorization", len(repo.events))
	}
}

var (
	testNow          = time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	testFleetScopeID = mustUUID("00000000-0000-0000-0000-000000000011")
	testClusterID    = mustUUID("00000000-0000-0000-0000-000000000012")
)

func newTestService() (*Service, *fakeRepository) {
	return newTestServiceWithAuthorizer(nil)
}

func newTestServiceWithAuthorizer(authorizer Authorizer) (*Service, *fakeRepository) {
	return newTestServiceWithAuthorizerAndPlacementResolver(authorizer, defaultPlacementResolver())
}

func newTestServiceWithPlacementResolver(resolver PlacementResolver) (*Service, *fakeRepository) {
	return newTestServiceWithAuthorizerAndPlacementResolver(nil, resolver)
}

func newTestServiceWithAuthorizerAndPlacementResolver(authorizer Authorizer, resolver PlacementResolver) (*Service, *fakeRepository) {
	repo := &fakeRepository{
		slots:                     make(map[uuid.UUID]entity.Slot),
		workspaceMaterializations: make(map[uuid.UUID]entity.WorkspaceMaterialization),
		jobs:                      make(map[uuid.UUID]entity.Job),
		runtimeArtifactRefs:       make(map[uuid.UUID]entity.RuntimeArtifactRef),
		cleanupPolicies:           make(map[uuid.UUID]entity.CleanupPolicy),
		prewarmPools:              make(map[uuid.UUID]entity.PrewarmPool),
		results:                   make(map[string]entity.CommandResult),
	}
	ids := &sequenceIDs{values: []uuid.UUID{
		mustUUID("00000000-0000-0000-0000-000000000201"),
		mustUUID("00000000-0000-0000-0000-000000000202"),
		mustUUID("00000000-0000-0000-0000-000000000203"),
		mustUUID("00000000-0000-0000-0000-000000000204"),
		mustUUID("00000000-0000-0000-0000-000000000205"),
		mustUUID("00000000-0000-0000-0000-000000000206"),
		mustUUID("00000000-0000-0000-0000-000000000207"),
		mustUUID("00000000-0000-0000-0000-000000000208"),
		mustUUID("00000000-0000-0000-0000-000000000209"),
		mustUUID("00000000-0000-0000-0000-000000000210"),
		mustUUID("00000000-0000-0000-0000-000000000211"),
		mustUUID("00000000-0000-0000-0000-000000000212"),
		mustUUID("00000000-0000-0000-0000-000000000213"),
		mustUUID("00000000-0000-0000-0000-000000000214"),
	}}
	config := Config{
		DefaultFleetScopeID: testFleetScopeID,
		DefaultClusterID:    testClusterID,
		NamespacePrefix:     "kodex-rt",
		DefaultLeaseTTL:     30 * time.Minute,
	}
	if authorizer != nil {
		config.Authorizer = authorizer
	}
	config.PlacementResolver = resolver
	svc := NewWithConfig(repo, fixedClock{now: testNow}, ids, config)
	return svc, repo
}

type fakePlacementResolver struct {
	result   PlacementResolution
	err      error
	requests []PlacementResolutionRequest
}

func defaultPlacementResolver() *fakePlacementResolver {
	return &fakePlacementResolver{result: PlacementResolution{FleetScopeID: testFleetScopeID, ClusterID: testClusterID}}
}

func (r *fakePlacementResolver) ResolvePlacement(_ context.Context, request PlacementResolutionRequest) (PlacementResolution, error) {
	r.requests = append(r.requests, request)
	if r.err != nil {
		return PlacementResolution{}, r.err
	}
	return r.result, nil
}

func commandMeta(commandID uuid.UUID, expectedVersion int64) value.CommandMeta {
	var expected *int64
	if expectedVersion > 0 {
		expected = &expectedVersion
	}
	return value.CommandMeta{
		CommandID:       commandID,
		ExpectedVersion: expected,
		Actor:           value.Actor{Type: "service", ID: "agent-manager"},
	}
}

func idempotencyMeta(key string, actorID string) value.CommandMeta {
	return value.CommandMeta{
		IdempotencyKey: key,
		Actor:          value.Actor{Type: "service", ID: actorID},
	}
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type sequenceIDs struct {
	values []uuid.UUID
}

func (g *sequenceIDs) New() uuid.UUID {
	if len(g.values) == 0 {
		return uuid.New()
	}
	id := g.values[0]
	g.values = g.values[1:]
	return id
}

type fakeRepository struct {
	slots                     map[uuid.UUID]entity.Slot
	workspaceMaterializations map[uuid.UUID]entity.WorkspaceMaterialization
	buildContexts             map[uuid.UUID]entity.BuildContext
	jobs                      map[uuid.UUID]entity.Job
	runtimeArtifactRefs       map[uuid.UUID]entity.RuntimeArtifactRef
	cleanupPolicies           map[uuid.UUID]entity.CleanupPolicy
	prewarmPools              map[uuid.UUID]entity.PrewarmPool
	results                   map[string]entity.CommandResult
	events                    []entity.OutboxEvent
}

func (r *fakeRepository) Ping(context.Context) error { return nil }

func (r *fakeRepository) GetCommandResult(_ context.Context, identity query.CommandIdentity) (entity.CommandResult, error) {
	key := identity.CommandID.String()
	if identity.CommandID == uuid.Nil {
		key = identity.Operation + ":" + identity.Actor.Type + ":" + identity.Actor.ID + ":" + identity.IdempotencyKey
	}
	result, ok := r.results[key]
	if !ok {
		return entity.CommandResult{}, errs.ErrNotFound
	}
	return result, nil
}

func (r *fakeRepository) CreateSlot(_ context.Context, slot entity.Slot, event entity.OutboxEvent, result entity.CommandResult) error {
	r.slots[slot.ID] = slot
	r.events = append(r.events, event)
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) ClaimReusableSlot(_ context.Context, filter query.ReusableSlotFilter, recordFactory runtimerepo.SlotReuseRecordFactory) (entity.Slot, error) {
	for id, slot := range r.slots {
		if !reusableFakeSlot(slot, filter, r.jobs) {
			continue
		}
		slot.Status = enum.SlotStatusReserved
		slot.AgentRunID = filter.AgentRunID
		slot.ProjectID = filter.ProjectID
		slot.RepositoryIDs = append([]uuid.UUID(nil), filter.RepositoryIDs...)
		slot.ClusterID = filter.ClusterID
		slot.Fingerprint = filter.Fingerprint
		slot.LeaseOwner = filter.LeaseOwner
		slot.LeaseUntil = &filter.LeaseUntil
		slot.LastErrorCode = ""
		slot.LastErrorMessage = ""
		slot.UpdatedAt = filter.Now
		slot.Version++
		event, result, err := recordFactory(slot)
		if err != nil {
			return entity.Slot{}, err
		}
		r.slots[id] = slot
		r.events = append(r.events, event)
		r.results[result.Key] = result
		return slot, nil
	}
	return entity.Slot{}, errs.ErrNotFound
}

func (r *fakeRepository) PrepareRuntime(
	_ context.Context,
	slot entity.Slot,
	materialization entity.WorkspaceMaterialization,
	slotEvent entity.OutboxEvent,
	workspaceEvent entity.OutboxEvent,
	result entity.CommandResult,
) error {
	r.slots[slot.ID] = slot
	r.workspaceMaterializations[materialization.ID] = materialization
	r.events = append(r.events, slotEvent, workspaceEvent)
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) CreateWorkspaceMaterialization(
	_ context.Context,
	slot entity.Slot,
	materialization entity.WorkspaceMaterialization,
	previousSlotVersion int64,
	event entity.OutboxEvent,
	result entity.CommandResult,
) error {
	currentSlot, ok := r.slots[slot.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if currentSlot.Version != previousSlotVersion {
		return errs.ErrConflict
	}
	r.slots[slot.ID] = slot
	r.workspaceMaterializations[materialization.ID] = materialization
	r.events = append(r.events, event)
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) UpdateWorkspaceMaterialization(
	_ context.Context,
	slot entity.Slot,
	materialization entity.WorkspaceMaterialization,
	previousSlotVersion int64,
	previousMaterializationVersion int64,
	event *entity.OutboxEvent,
	result entity.CommandResult,
) error {
	currentSlot, ok := r.slots[slot.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if currentSlot.Version != previousSlotVersion {
		return errs.ErrConflict
	}
	currentMaterialization, ok := r.workspaceMaterializations[materialization.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if currentMaterialization.Version != previousMaterializationVersion {
		return errs.ErrConflict
	}
	r.slots[slot.ID] = slot
	r.workspaceMaterializations[materialization.ID] = materialization
	if event != nil {
		r.events = append(r.events, *event)
	}
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) GetWorkspaceMaterialization(_ context.Context, id uuid.UUID) (entity.WorkspaceMaterialization, error) {
	materialization, ok := r.workspaceMaterializations[id]
	if !ok {
		return entity.WorkspaceMaterialization{}, errs.ErrNotFound
	}
	return materialization, nil
}

func (r *fakeRepository) ListWorkspaceMaterializations(context.Context, query.WorkspaceMaterializationFilter) ([]entity.WorkspaceMaterialization, query.PageResult, error) {
	items := make([]entity.WorkspaceMaterialization, 0, len(r.workspaceMaterializations))
	for _, materialization := range r.workspaceMaterializations {
		items = append(items, materialization)
	}
	return items, query.PageResult{}, nil
}

func (r *fakeRepository) PrepareBuildContext(_ context.Context, buildContext entity.BuildContext, resultFactory runtimerepo.BuildContextCommandResultFactory) (entity.BuildContext, error) {
	if r.buildContexts == nil {
		r.buildContexts = make(map[uuid.UUID]entity.BuildContext)
	}
	if r.results == nil {
		r.results = make(map[string]entity.CommandResult)
	}
	for _, existing := range r.buildContexts {
		if existing.ContextFingerprint != buildContext.ContextFingerprint {
			continue
		}
		result, err := resultFactory(existing)
		if err != nil {
			return entity.BuildContext{}, err
		}
		r.results[result.Key] = result
		return existing, nil
	}
	result, err := resultFactory(buildContext)
	if err != nil {
		return entity.BuildContext{}, err
	}
	r.buildContexts[buildContext.ID] = buildContext
	r.results[result.Key] = result
	return buildContext, nil
}

func (r *fakeRepository) UpdateBuildContext(_ context.Context, buildContext entity.BuildContext, previousVersion int64, result entity.CommandResult) error {
	if r.buildContexts == nil {
		return errs.ErrNotFound
	}
	current, ok := r.buildContexts[buildContext.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if current.Version != previousVersion {
		return errs.ErrConflict
	}
	r.buildContexts[buildContext.ID] = buildContext
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) GetBuildContext(_ context.Context, id uuid.UUID) (entity.BuildContext, error) {
	if r.buildContexts == nil {
		return entity.BuildContext{}, errs.ErrNotFound
	}
	buildContext, ok := r.buildContexts[id]
	if !ok {
		return entity.BuildContext{}, errs.ErrNotFound
	}
	return buildContext, nil
}

func (r *fakeRepository) GetBuildContextByFingerprint(_ context.Context, fingerprint string) (entity.BuildContext, error) {
	if r.buildContexts == nil {
		return entity.BuildContext{}, errs.ErrNotFound
	}
	for _, buildContext := range r.buildContexts {
		if buildContext.ContextFingerprint == fingerprint {
			return buildContext, nil
		}
	}
	return entity.BuildContext{}, errs.ErrNotFound
}

func (r *fakeRepository) ListRunnableBuildContexts(_ context.Context, limit int) ([]entity.BuildContext, error) {
	if r.buildContexts == nil {
		return nil, nil
	}
	result := make([]entity.BuildContext, 0, limit)
	for _, buildContext := range r.buildContexts {
		if buildContext.Status != enum.BuildContextStatusPending && buildContext.Status != enum.BuildContextStatusRunning {
			continue
		}
		result = append(result, buildContext)
		if limit > 0 && len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (r *fakeRepository) UpdateSlot(_ context.Context, slot entity.Slot, previousVersion int64, event entity.OutboxEvent, result *entity.CommandResult) error {
	current, ok := r.slots[slot.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if current.Version != previousVersion {
		return errs.ErrConflict
	}
	r.slots[slot.ID] = slot
	r.events = append(r.events, event)
	if result != nil {
		r.results[result.Key] = *result
	}
	return nil
}

func (r *fakeRepository) GetSlot(_ context.Context, id uuid.UUID) (entity.Slot, error) {
	slot, ok := r.slots[id]
	if !ok {
		return entity.Slot{}, errs.ErrNotFound
	}
	return slot, nil
}

func (r *fakeRepository) ListSlots(context.Context, query.SlotFilter) ([]entity.Slot, query.PageResult, error) {
	slots := make([]entity.Slot, 0, len(r.slots))
	for _, slot := range r.slots {
		slots = append(slots, slot)
	}
	return slots, query.PageResult{}, nil
}

func (r *fakeRepository) CreateJob(_ context.Context, job entity.Job, event entity.OutboxEvent, result entity.CommandResult) error {
	r.jobs[job.ID] = job
	r.events = append(r.events, event)
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) ClaimRunnableJob(_ context.Context, filter query.JobClaimFilter, recordFactory runtimerepo.JobClaimRecordFactory) (entity.Job, error) {
	for id, job := range r.jobs {
		if !runnableFakeJob(job, filter) {
			continue
		}
		job.Status = enum.JobStatusClaimed
		job.LeaseOwner = filter.LeaseOwner
		job.LeaseTokenHash = filter.LeaseTokenHash
		job.LeaseUntil = &filter.LeaseUntil
		job.ClaimAttempt++
		if job.StartedAt == nil {
			job.StartedAt = &filter.Now
		}
		job.UpdatedAt = filter.Now
		job.Version++
		event, result, err := recordFactory(job)
		if err != nil {
			return entity.Job{}, err
		}
		r.jobs[id] = job
		r.events = append(r.events, event)
		r.results[result.Key] = result
		return job, nil
	}
	return entity.Job{}, errs.ErrNotFound
}

func (r *fakeRepository) UpdateJob(_ context.Context, job entity.Job, previousVersion int64, steps []entity.JobStep, refs []entity.RuntimeArtifactRef, event *entity.OutboxEvent, result entity.CommandResult) error {
	current, ok := r.jobs[job.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if current.Version != previousVersion {
		return errs.ErrConflict
	}
	job.Steps = replaceFakeSteps(current.Steps, steps)
	r.jobs[job.ID] = job
	for _, ref := range refs {
		r.runtimeArtifactRefs[ref.ID] = ref
	}
	if event != nil {
		r.events = append(r.events, *event)
	}
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) GetJob(_ context.Context, id uuid.UUID) (entity.Job, error) {
	job, ok := r.jobs[id]
	if !ok {
		return entity.Job{}, errs.ErrNotFound
	}
	return job, nil
}

func (r *fakeRepository) ListJobs(context.Context, query.JobFilter) ([]entity.Job, query.PageResult, error) {
	jobs := make([]entity.Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		jobs = append(jobs, job)
	}
	return jobs, query.PageResult{}, nil
}

func (r *fakeRepository) RecordRuntimeArtifactRef(_ context.Context, ref entity.RuntimeArtifactRef, result entity.CommandResult) error {
	r.runtimeArtifactRefs[ref.ID] = ref
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) GetRuntimeArtifactRef(_ context.Context, id uuid.UUID) (entity.RuntimeArtifactRef, error) {
	ref, ok := r.runtimeArtifactRefs[id]
	if !ok {
		return entity.RuntimeArtifactRef{}, errs.ErrNotFound
	}
	return ref, nil
}

func (r *fakeRepository) ListRuntimeArtifactRefs(context.Context, query.RuntimeArtifactRefFilter) ([]entity.RuntimeArtifactRef, query.PageResult, error) {
	refs := make([]entity.RuntimeArtifactRef, 0, len(r.runtimeArtifactRefs))
	for _, ref := range r.runtimeArtifactRefs {
		refs = append(refs, ref)
	}
	return refs, query.PageResult{}, nil
}

func (r *fakeRepository) CreateCleanupPolicy(_ context.Context, policy entity.CleanupPolicy, result entity.CommandResult) error {
	r.cleanupPolicies[policy.ID] = policy
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) UpdateCleanupPolicy(_ context.Context, policy entity.CleanupPolicy, previousVersion int64, result entity.CommandResult) error {
	current, ok := r.cleanupPolicies[policy.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if current.Version != previousVersion {
		return errs.ErrConflict
	}
	r.cleanupPolicies[policy.ID] = policy
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) GetCleanupPolicy(_ context.Context, id uuid.UUID) (entity.CleanupPolicy, error) {
	policy, ok := r.cleanupPolicies[id]
	if !ok {
		return entity.CleanupPolicy{}, errs.ErrNotFound
	}
	return policy, nil
}

func (r *fakeRepository) RunCleanupBatch(_ context.Context, filter query.CleanupBatchFilter, recordFactory runtimerepo.CleanupBatchRecordFactory) (runtimerepo.CleanupBatchResult, error) {
	result := runtimerepo.CleanupBatchResult{}
	for _, policy := range r.cleanupPolicies {
		if policy.Status != enum.CleanupPolicyStatusActive || (filter.CleanupPolicyID != nil && policy.ID != *filter.CleanupPolicyID) {
			continue
		}
		for id, slot := range r.slots {
			if result.ClaimedCount >= filter.Limit || !cleanupFakeSlotMatches(slot, policy, filter.Now) {
				continue
			}
			if fakeSlotHasActiveJob(slot.ID, r.jobs) {
				slot.LastErrorCode = "CLEANUP_BLOCKED_BY_ACTIVE_JOB"
				slot.LastErrorMessage = "cleanup is blocked by active runtime jobs"
				slot.UpdatedAt = filter.Now
				slot.Version++
				r.slots[id] = slot
				result.FailedSlots = append(result.FailedSlots, slot)
			} else {
				slot.Status = enum.SlotStatusCleaned
				slot.LeaseOwner = ""
				slot.LeaseUntil = nil
				slot.LastErrorCode = ""
				slot.LastErrorMessage = ""
				slot.UpdatedAt = filter.Now
				slot.Version++
				r.slots[id] = slot
				if !policy.KeepShortLogTail {
					r.scrubFakeJobTails(slot.ID, filter.Now)
				}
				result.CleanedSlots = append(result.CleanedSlots, slot)
			}
			result.ClaimedCount++
			result.AffectedSlotIDs = append(result.AffectedSlotIDs, slot.ID)
		}
	}
	result.CleanedCount = len(result.CleanedSlots)
	result.FailedCount = len(result.FailedSlots)
	events, command, err := recordFactory(result)
	if err != nil {
		return runtimerepo.CleanupBatchResult{}, err
	}
	r.events = append(r.events, events...)
	r.results[command.Key] = command
	return result, nil
}

func (r *fakeRepository) scrubFakeJobTails(slotID uuid.UUID, now time.Time) {
	for id, job := range r.jobs {
		if job.SlotID == nil || *job.SlotID != slotID {
			continue
		}
		job.ShortLogTail = ""
		for index := range job.Steps {
			job.Steps[index].ShortLogTail = ""
			job.Steps[index].UpdatedAt = now
			job.Steps[index].Version++
		}
		job.UpdatedAt = now
		job.Version++
		r.jobs[id] = job
	}
}

func (r *fakeRepository) CreatePrewarmPool(_ context.Context, pool entity.PrewarmPool, result entity.CommandResult) error {
	r.prewarmPools[pool.ID] = pool
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) UpdatePrewarmPool(_ context.Context, pool entity.PrewarmPool, previousVersion int64, result entity.CommandResult) error {
	current, ok := r.prewarmPools[pool.ID]
	if !ok {
		return errs.ErrNotFound
	}
	if current.Version != previousVersion {
		return errs.ErrConflict
	}
	r.prewarmPools[pool.ID] = pool
	r.results[result.Key] = result
	return nil
}

func (r *fakeRepository) GetPrewarmPool(_ context.Context, id uuid.UUID) (entity.PrewarmPool, error) {
	pool, ok := r.prewarmPools[id]
	if !ok {
		return entity.PrewarmPool{}, errs.ErrNotFound
	}
	return pool, nil
}

func (r *fakeRepository) ReconcilePrewarmPool(_ context.Context, filter query.PrewarmPoolReconcileFilter, recordFactory runtimerepo.PrewarmPoolReconcileRecordFactory) (entity.PrewarmPool, error) {
	pool, ok := r.prewarmPools[filter.PrewarmPoolID]
	if !ok {
		return entity.PrewarmPool{}, errs.ErrNotFound
	}
	currentSize := int64(0)
	excessSlots := []entity.Slot{}
	for _, slot := range r.slots {
		if prewarmFakeSlotMatchesPool(slot, pool) {
			currentSize++
			excessSlots = append(excessSlots, slot)
		}
	}
	record, events, command, err := recordFactory(runtimerepo.PrewarmPoolReconcileState{Pool: pool, CurrentSize: currentSize, ExcessSlots: excessSlots})
	if err != nil {
		return entity.PrewarmPool{}, err
	}
	r.prewarmPools[pool.ID] = record.Pool
	for _, slot := range record.CreatedSlots {
		r.slots[slot.ID] = slot
	}
	for _, slot := range record.CleanupSlots {
		r.slots[slot.ID] = slot
	}
	r.events = append(r.events, events...)
	r.results[command.Key] = command
	return record.Pool, nil
}

func runnableFakeJob(job entity.Job, filter query.JobClaimFilter) bool {
	if job.JobType == enum.JobTypeDeploy {
		return false
	}
	if len(filter.JobTypes) > 0 {
		found := false
		for _, jobType := range filter.JobTypes {
			if job.JobType == jobType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if filter.FleetScopeID != nil && !sameUUIDPtr(job.FleetScopeID, filter.FleetScopeID) {
		return false
	}
	if !buildDeployJobInputHasRequiredExecutionSpec(job.JobType, job.JobInputJSON) {
		return false
	}
	return job.Status == enum.JobStatusPending || ((job.Status == enum.JobStatusClaimed || job.Status == enum.JobStatusRunning) && job.LeaseUntil != nil && !job.LeaseUntil.After(filter.Now))
}

func reusableFakeSlot(slot entity.Slot, filter query.ReusableSlotFilter, jobs map[uuid.UUID]entity.Job) bool {
	if slot.RuntimeProfile != filter.RuntimeProfile || slot.RuntimeMode != filter.RuntimeMode {
		return false
	}
	if !sameUUIDPtr(slot.FleetScopeID, filter.FleetScopeID) || fakeSlotHasActiveJob(slot.ID, jobs) {
		return false
	}
	if slot.ProjectID != nil && !sameUUIDPtr(slot.ProjectID, filter.ProjectID) {
		return false
	}
	if !repositoryScopeContained(slot.RepositoryIDs, filter.RepositoryIDs) {
		return false
	}
	if slot.LeaseUntil != nil && slot.LeaseUntil.After(filter.Now) {
		return false
	}
	switch slot.Status {
	case enum.SlotStatusPrewarmed:
		return slot.Fingerprint == "" || slot.Fingerprint == filter.Fingerprint
	case enum.SlotStatusReady:
		return slot.Fingerprint == filter.Fingerprint
	default:
		return false
	}
}

func repositoryScopeContained(slotRepositoryIDs []uuid.UUID, requestedRepositoryIDs []uuid.UUID) bool {
	for _, slotRepositoryID := range slotRepositoryIDs {
		found := false
		for _, requestedRepositoryID := range requestedRepositoryIDs {
			if slotRepositoryID == requestedRepositoryID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func cleanupFakeSlotMatches(slot entity.Slot, policy entity.CleanupPolicy, now time.Time) bool {
	if !cleanupFakeScopeMatches(slot, policy) {
		return false
	}
	switch slot.Status {
	case enum.SlotStatusCleanupPending:
		return !slot.UpdatedAt.After(now.Add(-time.Duration(policy.TTLSeconds) * time.Second))
	case enum.SlotStatusFailed:
		return !slot.UpdatedAt.After(now.Add(-time.Duration(policy.FailedTTLSeconds) * time.Second))
	default:
		return false
	}
}

func cleanupFakeScopeMatches(slot entity.Slot, policy entity.CleanupPolicy) bool {
	switch policy.ScopeType {
	case enum.RuntimeScopePlatform:
		return true
	case enum.RuntimeScopeProject:
		return slot.ProjectID != nil && slot.ProjectID.String() == policy.ScopeID
	case enum.RuntimeScopeRepository:
		for _, id := range slot.RepositoryIDs {
			if id.String() == policy.ScopeID {
				return true
			}
		}
		return false
	case enum.RuntimeScopeRuntimeProfile:
		return slot.RuntimeProfile == policy.ScopeID
	default:
		return false
	}
}

func fakeSlotHasActiveJob(slotID uuid.UUID, jobs map[uuid.UUID]entity.Job) bool {
	for _, job := range jobs {
		if job.SlotID != nil && *job.SlotID == slotID && (job.Status == enum.JobStatusPending || job.Status == enum.JobStatusClaimed || job.Status == enum.JobStatusRunning) {
			return true
		}
	}
	return false
}

func prewarmFakeSlotMatchesPool(slot entity.Slot, pool entity.PrewarmPool) bool {
	if slot.Status != enum.SlotStatusPrewarmed || !slot.IsPrewarmed || slot.RuntimeProfile != pool.RuntimeProfile || !sameUUIDPtr(slot.FleetScopeID, pool.FleetScopeID) {
		return false
	}
	switch pool.ScopeType {
	case enum.PrewarmPoolScopePlatform:
		return true
	case enum.PrewarmPoolScopeProject:
		return slot.ProjectID != nil && slot.ProjectID.String() == pool.ScopeID
	case enum.PrewarmPoolScopeRepository:
		for _, id := range slot.RepositoryIDs {
			if id.String() == pool.ScopeID {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func replaceFakeSteps(current []entity.JobStep, updates []entity.JobStep) []entity.JobStep {
	result := append([]entity.JobStep(nil), current...)
	for _, update := range updates {
		replaced := false
		for index := range result {
			if result[index].StepKey == update.StepKey {
				result[index] = update
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, update)
		}
	}
	return result
}

type denyAuthorizer struct{}

func (denyAuthorizer) Authorize(context.Context, AuthorizationRequest) error {
	return errs.ErrForbidden
}

type recordAuthorizer struct {
	deny     bool
	requests []AuthorizationRequest
}

func (a *recordAuthorizer) Authorize(_ context.Context, request AuthorizationRequest) error {
	a.requests = append(a.requests, request)
	if a.deny {
		return errs.ErrForbidden
	}
	return nil
}

func (r *fakeRepository) ClaimOutboxEvents(context.Context, int, time.Time, time.Time) ([]entity.OutboxEvent, error) {
	return nil, nil
}

func (r *fakeRepository) MarkOutboxEventPublished(context.Context, uuid.UUID, int, time.Time) error {
	return nil
}

func (r *fakeRepository) MarkOutboxEventFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return nil
}

func (r *fakeRepository) MarkOutboxEventPermanentlyFailed(context.Context, uuid.UUID, int, time.Time, string) error {
	return nil
}

func mustUUID(text string) uuid.UUID {
	return uuid.MustParse(text)
}
