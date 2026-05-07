package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

func TestServicePingDelegatesToRepository(t *testing.T) {
	t.Parallel()

	expectedErr := errors.New("storage unavailable")
	repository := &fakeRepository{err: expectedErr}
	service := New(repository)

	if err := service.Ping(context.Background()); !errors.Is(err, expectedErr) {
		t.Fatalf("Ping() err = %v, want %v", err, expectedErr)
	}
	if repository.calls != 1 {
		t.Fatalf("repository calls = %d, want 1", repository.calls)
	}
}

func TestNewPanicsWithoutRepository(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Fatal("New() did not panic")
		}
	}()
	_ = New(nil)
}

func TestRecordProviderLimitSnapshotStoresLimitAndRuntimeState(t *testing.T) {
	t.Parallel()

	snapshotID := uuid.New()
	runtimeStateID := uuid.New()
	accountID := uuid.New()
	now := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
	capturedAt := now.Add(-time.Minute)
	remaining := int64(0)
	limitValue := int64(5000)
	repository := &fakeRepository{}
	service := NewWithRuntime(repository, fixedClock{now: now}, &sequenceIDs{ids: []uuid.UUID{snapshotID, runtimeStateID}})

	snapshot, err := service.RecordProviderLimitSnapshot(context.Background(), RecordProviderLimitSnapshotInput{
		ExternalAccountID: accountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		LimitClass:        " core ",
		Remaining:         &remaining,
		LimitValue:        &limitValue,
		CapturedAt:        capturedAt,
		Source:            enum.ProviderLimitSourceProviderHub,
		Meta:              value.CommandMeta{CommandID: uuid.New()},
	})
	if err != nil {
		t.Fatalf("RecordProviderLimitSnapshot(): %v", err)
	}
	if snapshot.ID != snapshotID || snapshot.LimitClass != "core" {
		t.Fatalf("snapshot = %+v, want id %s and trimmed limit class", snapshot, snapshotID)
	}
	if repository.recordedRuntimeState.ID != runtimeStateID {
		t.Fatalf("runtime state id = %s, want %s", repository.recordedRuntimeState.ID, runtimeStateID)
	}
	if repository.recordedRuntimeState.Status != enum.ProviderAccountRuntimeStatusLimited {
		t.Fatalf("runtime state status = %s, want limited", repository.recordedRuntimeState.Status)
	}
	if repository.recordedRuntimeState.LastCheckedAt == nil || !repository.recordedRuntimeState.LastCheckedAt.Equal(capturedAt) {
		t.Fatalf("last checked at = %v, want %s", repository.recordedRuntimeState.LastCheckedAt, capturedAt)
	}
}

func TestListProviderAccountRuntimeStatesRejectsScopeFiltersUntilResolverExists(t *testing.T) {
	t.Parallel()

	service := New(&fakeRepository{})
	projectID := uuid.New()

	_, err := service.ListProviderAccountRuntimeStates(context.Background(), ListProviderAccountRuntimeStatesInput{
		ProjectID: &projectID,
		Meta:      value.QueryMeta{Actor: value.Actor{Type: "user", ID: uuid.NewString()}},
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("ListProviderAccountRuntimeStates() err = %v, want %v", err, errs.ErrInvalidArgument)
	}
}

type fakeRepository struct {
	err                  error
	calls                int
	recordedSnapshot     entity.ProviderLimitSnapshot
	recordedRuntimeState entity.ProviderAccountRuntimeState
}

func (r *fakeRepository) Ping(context.Context) error {
	r.calls++
	return r.err
}

func (r *fakeRepository) UpsertAccountRuntimeState(context.Context, entity.ProviderAccountRuntimeState) (entity.ProviderAccountRuntimeState, error) {
	return entity.ProviderAccountRuntimeState{}, r.err
}

func (r *fakeRepository) GetAccountRuntimeState(context.Context, query.AccountRuntimeStateLookup) (entity.ProviderAccountRuntimeState, error) {
	return entity.ProviderAccountRuntimeState{}, r.err
}

func (r *fakeRepository) ListAccountRuntimeStates(context.Context, query.AccountRuntimeStateFilter) ([]entity.ProviderAccountRuntimeState, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) RecordLimitSnapshot(_ context.Context, snapshot entity.ProviderLimitSnapshot, state entity.ProviderAccountRuntimeState) (entity.ProviderLimitSnapshot, error) {
	r.recordedSnapshot = snapshot
	r.recordedRuntimeState = state
	return snapshot, r.err
}

func (r *fakeRepository) ListLimitSnapshots(context.Context, query.LimitSnapshotFilter) ([]entity.ProviderLimitSnapshot, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

func (r *fakeRepository) RecordProviderOperation(context.Context, entity.ProviderOperation) (entity.ProviderOperation, error) {
	return entity.ProviderOperation{}, r.err
}

func (r *fakeRepository) ListProviderOperations(context.Context, query.ProviderOperationFilter) ([]entity.ProviderOperation, query.PageResult, error) {
	return nil, query.PageResult{}, r.err
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type sequenceIDs struct {
	ids []uuid.UUID
}

func (g *sequenceIDs) New() uuid.UUID {
	if len(g.ids) == 0 {
		panic("test id sequence is empty")
	}
	id := g.ids[0]
	g.ids = g.ids[1:]
	return id
}
