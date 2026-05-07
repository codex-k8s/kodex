package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

func TestReserveSlotPersistsLeaseAndDefaultFleetRefs(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
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
		slots:   make(map[uuid.UUID]entity.Slot),
		results: make(map[string]entity.CommandResult),
	}
	svc := NewWithConfig(repo, fixedClock{now: testNow}, &sequenceIDs{values: []uuid.UUID{mustUUID("00000000-0000-0000-0000-000000000209")}}, Config{
		DefaultFleetScopeID: testFleetScopeID,
		DefaultClusterID:    testClusterID,
		NamespacePrefix:     "kodex-rt",
		DefaultLeaseTTL:     30 * time.Minute,
		Authorizer:          denyAuthorizer{},
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
	repo := &fakeRepository{
		slots:   make(map[uuid.UUID]entity.Slot),
		results: make(map[string]entity.CommandResult),
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
	svc := NewWithConfig(repo, fixedClock{now: testNow}, ids, config)
	return svc, repo
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
	slots   map[uuid.UUID]entity.Slot
	results map[string]entity.CommandResult
	events  []entity.OutboxEvent
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
