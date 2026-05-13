package provider

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	migrationtest "github.com/codex-k8s/kodex/libs/go/migrationtest"
	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

var sqlHeaderPattern = regexp.MustCompile(`^-- name: ([a-z0-9_]+__[a-z0-9_]+) :(one|many|exec)$`)

func TestSQLFilesHaveNamedHeaders(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("expected embedded SQL files")
	}

	for _, file := range files {
		contentBytes, err := SQLFiles.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		firstLine, _, _ := strings.Cut(string(contentBytes), "\n")
		match := sqlHeaderPattern.FindStringSubmatch(firstLine)
		if match == nil {
			t.Fatalf("%s has invalid named query header: %q", file, firstLine)
		}
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		if match[1] != queryName {
			t.Fatalf("%s header query name = %s, want %s", file, match[1], queryName)
		}
	}
}

func TestRepositoryLoadsEverySQLFile(t *testing.T) {
	t.Parallel()

	files, err := fs.Glob(SQLFiles, "sql/*.sql")
	if err != nil {
		t.Fatalf("glob sql files: %v", err)
	}
	for _, file := range files {
		queryName := strings.TrimSuffix(filepath.Base(file), ".sql")
		query, err := loadQuery(queryName)
		if err != nil {
			t.Fatalf("load query %s: %v", queryName, err)
		}
		if strings.TrimSpace(query) == "" {
			t.Fatalf("query %s is empty", queryName)
		}
	}
}

func TestWrapErrorMapsPostgresErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "not found", err: pgx.ErrNoRows, want: errs.ErrNotFound},
		{name: "unique", err: &pgconn.PgError{Code: "23505"}, want: errs.ErrAlreadyExists},
		{name: "foreign key", err: &pgconn.PgError{Code: "23503"}, want: errs.ErrPreconditionFailed},
		{name: "check", err: &pgconn.PgError{Code: "23514"}, want: errs.ErrInvalidArgument},
		{name: "serialization", err: &pgconn.PgError{Code: "40001"}, want: errs.ErrConflict},
		{name: "deadlock", err: &pgconn.PgError{Code: "40P01"}, want: errs.ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wrapError("test operation", tc.err); !errors.Is(got, tc.want) {
				t.Fatalf("wrapError() = %v, want %v", got, tc.want)
			}
			var pgErr *pgconn.PgError
			if errors.As(tc.err, &pgErr) && !errors.As(wrapError("test operation", tc.err), &pgErr) {
				t.Fatalf("wrapError() lost postgres cause")
			}
		})
	}
}

func TestRepositoryIntegrationSyncCursors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 7, 13, 0, 0, 0, time.UTC)
	accountID := uuid.New()
	otherAccountID := uuid.New()
	request := entity.ReconciliationRequest{
		ID:                uuid.New(),
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: accountID,
		ScopeType:         enum.SyncCursorScopeRepository,
		ScopeRef:          "codex-k8s/kodex",
		IdempotencyKey:    "repo-sync-1",
		ArtifactKinds:     []enum.SyncArtifactKind{enum.SyncArtifactIssue, enum.SyncArtifactPullRequest},
		Priority:          enum.SyncCursorPriorityWarm,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	cursor := testSyncCursor(uuid.New(), request, enum.SyncArtifactIssue, now)
	prCursor := testSyncCursor(uuid.New(), request, enum.SyncArtifactPullRequest, now)
	stored, err := repository.EnqueueSyncCursors(ctx, request, []entity.SyncCursor{cursor, prCursor})
	if err != nil {
		t.Fatalf("enqueue sync cursors: %v", err)
	}
	if len(stored) != 2 || stored[0].ID != cursor.ID || stored[1].ID != prCursor.ID {
		t.Fatalf("stored cursors = %+v, want issue and pr cursors", stored)
	}

	replayed, err := repository.EnqueueSyncCursors(ctx, request, []entity.SyncCursor{
		testSyncCursor(uuid.New(), request, enum.SyncArtifactIssue, now.Add(time.Minute)),
		testSyncCursor(uuid.New(), request, enum.SyncArtifactPullRequest, now.Add(time.Minute)),
	})
	if err != nil {
		t.Fatalf("replay enqueue sync cursors: %v", err)
	}
	if len(replayed) != 2 || replayed[0].ID != cursor.ID || replayed[0].Version != 1 {
		t.Fatalf("replayed cursors = %+v, want original cursor without version bump", replayed)
	}

	loaded, err := repository.GetSyncCursor(ctx, cursor.ID)
	if err != nil {
		t.Fatalf("get sync cursor: %v", err)
	}
	if loaded.ID != cursor.ID || loaded.Priority != enum.SyncCursorPriorityWarm {
		t.Fatalf("loaded cursor = %+v, want original warm cursor", loaded)
	}
	if loaded.ExternalAccountID != accountID {
		t.Fatalf("loaded cursor account = %s, want %s", loaded.ExternalAccountID, accountID)
	}

	changedRequest := request
	changedRequest.Priority = enum.SyncCursorPriorityHot
	changedRequest.UpdatedAt = now.Add(time.Minute)
	if _, err := repository.EnqueueSyncCursors(ctx, changedRequest, []entity.SyncCursor{
		testSyncCursor(uuid.New(), changedRequest, enum.SyncArtifactIssue, now.Add(time.Minute)),
		testSyncCursor(uuid.New(), changedRequest, enum.SyncArtifactPullRequest, now.Add(time.Minute)),
	}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("changed idempotent request err = %v, want %v", err, errs.ErrConflict)
	}

	requeueRequest := request
	requeueRequest.ID = uuid.New()
	requeueRequest.IdempotencyKey = "repo-sync-2"
	requeueRequest.ArtifactKinds = []enum.SyncArtifactKind{enum.SyncArtifactIssue}
	requeueRequest.Priority = enum.SyncCursorPriorityHot
	requeueRequest.CreatedAt = now.Add(2 * time.Minute)
	requeueRequest.UpdatedAt = now.Add(2 * time.Minute)
	requeued, err := repository.EnqueueSyncCursors(ctx, requeueRequest, []entity.SyncCursor{
		testSyncCursor(uuid.New(), requeueRequest, enum.SyncArtifactIssue, now.Add(2*time.Minute)),
	})
	if err != nil {
		t.Fatalf("requeue sync cursor: %v", err)
	}
	if len(requeued) != 1 || requeued[0].ID != cursor.ID || requeued[0].Priority != enum.SyncCursorPriorityHot || requeued[0].Version != 2 {
		t.Fatalf("requeued cursor = %+v, want original id %s hot version 2", requeued, cursor.ID)
	}

	conflictingAccountRequest := request
	conflictingAccountRequest.ID = uuid.New()
	conflictingAccountRequest.ExternalAccountID = otherAccountID
	conflictingAccountRequest.IdempotencyKey = "repo-sync-other-account"
	conflictingAccountRequest.ArtifactKinds = []enum.SyncArtifactKind{enum.SyncArtifactIssue}
	conflictingAccountRequest.CreatedAt = now.Add(3 * time.Minute)
	conflictingAccountRequest.UpdatedAt = now.Add(3 * time.Minute)
	if _, err := repository.EnqueueSyncCursors(ctx, conflictingAccountRequest, []entity.SyncCursor{
		testSyncCursor(uuid.New(), conflictingAccountRequest, enum.SyncArtifactIssue, now.Add(3*time.Minute)),
	}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("conflicting account enqueue err = %v, want %v", err, errs.ErrConflict)
	}

	failedCursor := entity.SyncCursor{
		Base: entity.Base{
			ID:        uuid.New(),
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		ProviderSlug:        enum.ProviderSlugGitHub,
		ExternalAccountID:   accountID,
		ScopeType:           enum.SyncCursorScopeWorkItem,
		ScopeRef:            "github:issue:42",
		ArtifactKind:        enum.SyncArtifactComment,
		Priority:            enum.SyncCursorPriorityCold,
		LastError:           "rate limited",
		RateBudgetStateJSON: []byte(`{}`),
	}
	failedRequest := entity.ReconciliationRequest{
		ID:                uuid.New(),
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: accountID,
		ScopeType:         enum.SyncCursorScopeWorkItem,
		ScopeRef:          "github:issue:42",
		IdempotencyKey:    "work-item-comments-1",
		ArtifactKinds:     []enum.SyncArtifactKind{enum.SyncArtifactComment},
		Priority:          enum.SyncCursorPriorityCold,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if _, err := repository.EnqueueSyncCursors(ctx, failedRequest, []entity.SyncCursor{failedCursor}); err != nil {
		t.Fatalf("upsert failed sync cursor: %v", err)
	}
	cursors, _, err := repository.ListSyncCursors(ctx, query.SyncCursorFilter{
		ProviderSlug:   enum.ProviderSlugGitHub,
		ScopeRef:       "github:issue:42",
		IncludeHealthy: false,
	})
	if err != nil {
		t.Fatalf("list unhealthy sync cursors: %v", err)
	}
	if len(cursors) != 1 || cursors[0].ID != failedCursor.ID {
		t.Fatalf("unhealthy cursors = %+v, want failed cursor %s", cursors, failedCursor.ID)
	}

	claimed, err := repository.ClaimSyncCursor(ctx, providerrepo.SyncCursorClaim{
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: &accountID,
		LeaseOwner:        "worker-1",
		Now:               now.Add(2 * time.Minute),
		LeaseUntil:        now.Add(2*time.Minute + 30*time.Second),
	})
	if err != nil {
		t.Fatalf("claim sync cursor: %v", err)
	}
	if claimed.ID != cursor.ID || claimed.LeaseOwner != "worker-1" || claimed.LeaseUntil == nil {
		t.Fatalf("claimed cursor = %+v, want hot cursor leased by worker-1", claimed)
	}
	_, err = repository.ClaimSyncCursor(ctx, providerrepo.SyncCursorClaim{
		ID:         &cursor.ID,
		LeaseOwner: "worker-2",
		Now:        now.Add(2*time.Minute + time.Second),
		LeaseUntil: now.Add(3 * time.Minute),
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("claim leased sync cursor err = %v, want %v", err, errs.ErrNotFound)
	}

	completedAt := now.Add(3 * time.Minute)
	completedCursor := claimed
	completedCursor.CursorValue = completedAt.Format(time.RFC3339Nano)
	completedCursor.LastSuccessAt = &completedAt
	completedCursor.LastCheckedAt = &completedAt
	completedCursor.LastError = ""
	completedCursor.RateBudgetStateJSON = []byte(`{"core":{"remaining":4998}}`)
	completedCursor.LeaseOwner = ""
	completedCursor.LeaseUntil = nil
	workItemID := uuid.New()
	commentID := uuid.New()
	storedCursor, _, err := repository.ApplyReconciliationBatch(ctx, providerrepo.ReconciliationBatchCompletion{
		Cursor:             completedCursor,
		ExpectedLeaseOwner: "worker-1",
		ProjectionUpdate:   projectionUpdateForTest(workItemID, commentID, completedAt, "Сверенная задача", "body-v1", "Комментарий сверки", "comment-v1", "https://github.com/codex-k8s/kodex/issues/8"),
		Now:                completedAt,
	})
	if err != nil {
		t.Fatalf("apply reconciliation batch: %v", err)
	}
	if storedCursor.ID != claimed.ID || storedCursor.LeaseOwner != "" || storedCursor.LastError != "" || storedCursor.LastSuccessAt == nil {
		t.Fatalf("stored cursor = %+v, want completed cursor", storedCursor)
	}
	workItem, err := repository.GetWorkItemProjection(ctx, query.ProviderTargetLookup{ID: &workItemID})
	if err != nil {
		t.Fatalf("get reconciled work item: %v", err)
	}
	if workItem.Title != "Сверенная задача" || workItem.DriftStatus != enum.WorkItemDriftStatusFresh {
		t.Fatalf("work item = %+v, want reconciled projection", workItem)
	}
}

func TestRepositoryIntegrationProviderArtifactSignals(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 11, 14, 0, 0, 0, time.UTC)
	signal := entity.ProviderArtifactSignal{
		ID:                uuid.New(),
		IdentityKey:       "artifact-signal:id:signal-1",
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: uuid.New(),
		Source:            "slot_agent_after",
		ScopeType:         enum.SyncCursorScopeWorkItem,
		ScopeRef:          "codex-k8s/kodex#pull_request:703",
		ArtifactKinds:     []enum.SyncArtifactKind{enum.SyncArtifactComment, enum.SyncArtifactPullRequest, enum.SyncArtifactRelationship},
		TargetJSON:        []byte(`{"provider_slug":"github","repository_full_name":"codex-k8s/kodex","work_item_kind":"pull_request","number":703}`),
		PayloadJSON:       []byte(`{"run_id":"run-1"}`),
		ObservedAt:        now.Add(-time.Minute),
		CreatedAt:         now,
	}
	request := entity.ReconciliationRequest{
		ID:                uuid.New(),
		ProviderSlug:      signal.ProviderSlug,
		ExternalAccountID: signal.ExternalAccountID,
		ScopeType:         signal.ScopeType,
		ScopeRef:          signal.ScopeRef,
		IdempotencyKey:    signal.IdentityKey,
		ArtifactKinds:     signal.ArtifactKinds,
		Priority:          enum.SyncCursorPriorityHot,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	cursors := []entity.SyncCursor{
		testSyncCursor(uuid.New(), request, enum.SyncArtifactComment, now),
		testSyncCursor(uuid.New(), request, enum.SyncArtifactPullRequest, now),
		testSyncCursor(uuid.New(), request, enum.SyncArtifactRelationship, now),
	}
	stored, err := repository.RegisterProviderArtifactSignal(ctx, signal, request, cursors)
	if err != nil {
		t.Fatalf("register artifact signal: %v", err)
	}
	if len(stored) != len(cursors) || stored[1].ID != cursors[1].ID || stored[1].Priority != enum.SyncCursorPriorityHot {
		t.Fatalf("stored cursors = %+v, want hot cursors %+v", stored, cursors)
	}

	replay := signal
	replay.ID = uuid.New()
	replay.CreatedAt = now.Add(time.Minute)
	replayRequest := request
	replayRequest.ID = uuid.New()
	replayRequest.CreatedAt = now.Add(time.Minute)
	replayRequest.UpdatedAt = now.Add(time.Minute)
	replayCursors := []entity.SyncCursor{
		testSyncCursor(uuid.New(), replayRequest, enum.SyncArtifactComment, now.Add(time.Minute)),
		testSyncCursor(uuid.New(), replayRequest, enum.SyncArtifactPullRequest, now.Add(time.Minute)),
		testSyncCursor(uuid.New(), replayRequest, enum.SyncArtifactRelationship, now.Add(time.Minute)),
	}
	replayed, err := repository.RegisterProviderArtifactSignal(ctx, replay, replayRequest, replayCursors)
	if err != nil {
		t.Fatalf("replay artifact signal: %v", err)
	}
	if len(replayed) != len(cursors) || replayed[1].ID != cursors[1].ID {
		t.Fatalf("replayed cursors = %+v, want original %+v", replayed, cursors)
	}

	conflict := replay
	conflict.ScopeRef = "codex-k8s/kodex#pull_request:704"
	conflict.TargetJSON = []byte(`{"provider_slug":"github","repository_full_name":"codex-k8s/kodex","work_item_kind":"pull_request","number":704}`)
	conflictRequest := replayRequest
	conflictRequest.ScopeRef = conflict.ScopeRef
	conflictCursors := []entity.SyncCursor{
		testSyncCursor(uuid.New(), conflictRequest, enum.SyncArtifactComment, now.Add(time.Minute)),
		testSyncCursor(uuid.New(), conflictRequest, enum.SyncArtifactPullRequest, now.Add(time.Minute)),
		testSyncCursor(uuid.New(), conflictRequest, enum.SyncArtifactRelationship, now.Add(time.Minute)),
	}
	if _, err := repository.RegisterProviderArtifactSignal(ctx, conflict, conflictRequest, conflictCursors); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("conflicting artifact signal err = %v, want %v", err, errs.ErrConflict)
	}
}

func testSyncCursor(id uuid.UUID, request entity.ReconciliationRequest, artifactKind enum.SyncArtifactKind, now time.Time) entity.SyncCursor {
	return entity.SyncCursor{
		Base: entity.Base{
			ID:        id,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		ProviderSlug:        request.ProviderSlug,
		ExternalAccountID:   request.ExternalAccountID,
		ScopeType:           request.ScopeType,
		ScopeRef:            request.ScopeRef,
		ArtifactKind:        artifactKind,
		Priority:            request.Priority,
		RateBudgetStateJSON: []byte(`{"remaining":4999}`),
	}
}

func TestRepositoryIntegrationRuntimeStateLimitsAndOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	accountID := uuid.New()
	remaining := int64(4999)
	limitValue := int64(5000)
	resetAt := now.Add(time.Hour)
	state := entity.ProviderAccountRuntimeState{
		Base:              entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		ExternalAccountID: accountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		Status:            enum.ProviderAccountRuntimeStatusActive,
		LastCheckedAt:     &now,
		LastSuccessAt:     &now,
	}
	snapshot := entity.ProviderLimitSnapshot{
		ID:                uuid.New(),
		ExternalAccountID: accountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		LimitClass:        "core",
		Remaining:         &remaining,
		LimitValue:        &limitValue,
		ResetAt:           &resetAt,
		CapturedAt:        now,
		Source:            enum.ProviderLimitSourceProviderHub,
	}
	storedSnapshot, err := repository.RecordLimitSnapshot(ctx, snapshot, state)
	if err != nil {
		t.Fatalf("record limit snapshot: %v", err)
	}
	if storedSnapshot.ID != snapshot.ID || storedSnapshot.Remaining == nil || *storedSnapshot.Remaining != remaining {
		t.Fatalf("stored snapshot = %+v, want id %s remaining %d", storedSnapshot, snapshot.ID, remaining)
	}
	firstSnapshotID := snapshot.ID
	loadedState, err := repository.GetAccountRuntimeState(ctx, query.AccountRuntimeStateLookup{ExternalAccountID: &accountID, ProviderSlug: enum.ProviderSlugGitHub})
	if err != nil {
		t.Fatalf("get account runtime state: %v", err)
	}
	if loadedState.ID != state.ID || loadedState.Status != enum.ProviderAccountRuntimeStatusActive {
		t.Fatalf("loaded state = %+v, want id %s active", loadedState, state.ID)
	}
	limitedState := state
	limitedState.ID = uuid.New()
	limitedState.Status = enum.ProviderAccountRuntimeStatusLimited
	limitedState.UpdatedAt = now.Add(time.Minute)
	limitedRemaining := int64(0)
	snapshot.ID = uuid.New()
	snapshot.Remaining = &limitedRemaining
	snapshot.CapturedAt = now.Add(time.Minute)
	if _, err := repository.RecordLimitSnapshot(ctx, snapshot, limitedState); err != nil {
		t.Fatalf("record second limit snapshot: %v", err)
	}
	loadedState, err = repository.GetAccountRuntimeState(ctx, query.AccountRuntimeStateLookup{ExternalAccountID: &accountID, ProviderSlug: enum.ProviderSlugGitHub})
	if err != nil {
		t.Fatalf("get updated account runtime state: %v", err)
	}
	if loadedState.ID != state.ID || loadedState.Status != enum.ProviderAccountRuntimeStatusLimited || loadedState.Version != 2 {
		t.Fatalf("updated state = %+v, want same id %s limited version 2", loadedState, state.ID)
	}
	replayedSnapshot := entity.ProviderLimitSnapshot{
		ID:                uuid.New(),
		ExternalAccountID: accountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		LimitClass:        "core",
		Remaining:         &remaining,
		LimitValue:        &limitValue,
		ResetAt:           &resetAt,
		CapturedAt:        now,
		Source:            enum.ProviderLimitSourceProviderHub,
	}
	storedSnapshot, err = repository.RecordLimitSnapshot(ctx, replayedSnapshot, state)
	if err != nil {
		t.Fatalf("record duplicate limit snapshot: %v", err)
	}
	if storedSnapshot.ID != firstSnapshotID || storedSnapshot.Remaining == nil || *storedSnapshot.Remaining != remaining {
		t.Fatalf("duplicate snapshot = %+v, want original id %s remaining %d", storedSnapshot, firstSnapshotID, remaining)
	}
	loadedState, err = repository.GetAccountRuntimeState(ctx, query.AccountRuntimeStateLookup{ExternalAccountID: &accountID, ProviderSlug: enum.ProviderSlugGitHub})
	if err != nil {
		t.Fatalf("get runtime state after duplicate snapshot: %v", err)
	}
	if loadedState.Status != enum.ProviderAccountRuntimeStatusLimited || loadedState.Version != 2 {
		t.Fatalf("runtime state after duplicate snapshot = %+v, want unchanged limited version 2", loadedState)
	}
	changedRemaining := int64(4998)
	changedSnapshot := replayedSnapshot
	changedSnapshot.ID = uuid.New()
	changedSnapshot.Remaining = &changedRemaining
	_, err = repository.RecordLimitSnapshot(ctx, changedSnapshot, state)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("record changed duplicate snapshot err = %v, want %v", err, errs.ErrConflict)
	}
	loadedState, err = repository.GetAccountRuntimeState(ctx, query.AccountRuntimeStateLookup{ExternalAccountID: &accountID, ProviderSlug: enum.ProviderSlugGitHub})
	if err != nil {
		t.Fatalf("get runtime state after changed duplicate snapshot: %v", err)
	}
	if loadedState.Status != enum.ProviderAccountRuntimeStatusLimited || loadedState.Version != 2 {
		t.Fatalf("runtime state after changed duplicate snapshot = %+v, want unchanged limited version 2", loadedState)
	}
	activeState := state
	activeState.ID = uuid.New()
	activeState.Status = enum.ProviderAccountRuntimeStatusActive
	activeState.UpdatedAt = now.Add(2 * time.Minute)
	activeSnapshot := snapshot
	activeSnapshot.ID = uuid.New()
	activeSnapshot.LimitClass = "search"
	activeSnapshot.Remaining = &remaining
	activeSnapshot.CapturedAt = now.Add(2 * time.Minute)
	if _, err := repository.RecordLimitSnapshot(ctx, activeSnapshot, activeState); err != nil {
		t.Fatalf("record active class after limited snapshot: %v", err)
	}
	loadedState, err = repository.GetAccountRuntimeState(ctx, query.AccountRuntimeStateLookup{ExternalAccountID: &accountID, ProviderSlug: enum.ProviderSlugGitHub})
	if err != nil {
		t.Fatalf("get runtime state after active class: %v", err)
	}
	if loadedState.Status != enum.ProviderAccountRuntimeStatusLimited {
		t.Fatalf("runtime state after active class = %+v, want limited until full reconciliation clears it", loadedState)
	}
	authoritativeState := activeState
	authoritativeState.ID = uuid.New()
	authoritativeState.UpdatedAt = now.Add(3 * time.Minute)
	authoritativeState.LastCheckedAt = &authoritativeState.UpdatedAt
	authoritativeState.LastSuccessAt = &authoritativeState.UpdatedAt
	storedState, err := repository.UpsertAccountRuntimeState(ctx, authoritativeState)
	if err != nil {
		t.Fatalf("authoritative upsert active runtime state: %v", err)
	}
	if storedState.Status != enum.ProviderAccountRuntimeStatusActive {
		t.Fatalf("authoritative runtime state = %+v, want active", storedState)
	}
	delayedLimitedState := limitedState
	delayedLimitedState.ID = uuid.New()
	delayedLimitedState.UpdatedAt = now.Add(30 * time.Second)
	delayedCheckedAt := now.Add(30 * time.Second)
	delayedLimitedState.LastCheckedAt = &delayedCheckedAt
	delayedLimitedState.LastSuccessAt = &delayedCheckedAt
	delayedSnapshot := snapshot
	delayedSnapshot.ID = uuid.New()
	delayedSnapshot.LimitClass = "graphql"
	delayedSnapshot.CapturedAt = delayedCheckedAt
	if _, err := repository.RecordLimitSnapshot(ctx, delayedSnapshot, delayedLimitedState); err != nil {
		t.Fatalf("record delayed limited snapshot: %v", err)
	}
	loadedState, err = repository.GetAccountRuntimeState(ctx, query.AccountRuntimeStateLookup{ExternalAccountID: &accountID, ProviderSlug: enum.ProviderSlugGitHub})
	if err != nil {
		t.Fatalf("get runtime state after delayed limited snapshot: %v", err)
	}
	if loadedState.Status != enum.ProviderAccountRuntimeStatusActive || loadedState.Version != storedState.Version {
		t.Fatalf("runtime state after delayed limited snapshot = %+v, want unchanged active version %d", loadedState, storedState.Version)
	}

	snapshots, page, err := repository.ListLimitSnapshots(ctx, query.LimitSnapshotFilter{
		ExternalAccountID: &accountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		LimitClasses:      []string{"core"},
		Page:              value.PageRequest{PageSize: 1},
	})
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(snapshots) != 1 || page.NextPageToken == "" {
		t.Fatalf("snapshots = %d token %q, want one item and continuation", len(snapshots), page.NextPageToken)
	}

	operation := entity.ProviderOperation{
		Base:              entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
		CommandID:         uuid.NewString(),
		ExternalAccountID: accountID,
		ProviderSlug:      enum.ProviderSlugGitHub,
		OperationType:     enum.ProviderOperationCreateIssue,
		TargetRef:         "codex-k8s/kodex#1",
		Status:            enum.ProviderOperationStatusSucceeded,
		ResultRef:         "https://github.com/codex-k8s/kodex/issues/1",
		StartedAt:         now,
		FinishedAt:        &now,
	}
	if _, err := repository.RecordProviderOperation(ctx, operation); err != nil {
		t.Fatalf("record provider operation: %v", err)
	}
	replayedOperation := operation
	replayedOperation.ID = uuid.New()
	replayedOperation.StartedAt = now.Add(time.Minute)
	replayedOperation.FinishedAt = &replayedOperation.StartedAt
	replayedOperation.UpdatedAt = now.Add(time.Minute)
	storedOperation, err := repository.RecordProviderOperation(ctx, replayedOperation)
	if err != nil {
		t.Fatalf("record duplicate provider operation: %v", err)
	}
	if storedOperation.ID != operation.ID || !storedOperation.StartedAt.Equal(operation.StartedAt) {
		t.Fatalf("duplicate operation = %+v, want original id %s", storedOperation, operation.ID)
	}
	changedOperation := operation
	changedOperation.ID = uuid.New()
	changedOperation.ExternalAccountID = uuid.New()
	_, err = repository.RecordProviderOperation(ctx, changedOperation)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("record changed duplicate provider operation err = %v, want %v", err, errs.ErrConflict)
	}
	operations, _, err := repository.ListProviderOperations(ctx, query.ProviderOperationFilter{
		ProviderSlug:      enum.ProviderSlugGitHub,
		ExternalAccountID: &accountID,
		OperationTypes:    []enum.ProviderOperationType{enum.ProviderOperationCreateIssue},
		Statuses:          []enum.ProviderOperationStatus{enum.ProviderOperationStatusSucceeded},
	})
	if err != nil {
		t.Fatalf("list provider operations: %v", err)
	}
	if len(operations) != 1 || operations[0].ID != operation.ID {
		t.Fatalf("operations = %+v, want operation %s", operations, operation.ID)
	}

	webhook := entity.WebhookEvent{
		ID:                   uuid.New(),
		ProviderSlug:         enum.ProviderSlugGitHub,
		DeliveryID:           "delivery-1",
		EventName:            "issues",
		RepositoryProviderID: "100",
		ReceivedAt:           now,
		ProcessingStatus:     enum.WebhookProcessingStatusProcessed,
		PayloadJSON:          []byte(`{"issue":{"id":55,"number":7},"repository":{"id":100}}`),
		RetainUntil:          now.Add(30 * 24 * time.Hour),
	}
	sourceWebhookID := webhook.ID
	providerEvent := entity.ProviderEvent{
		ID:                   uuid.New(),
		SourceWebhookEventID: &sourceWebhookID,
		EventType:            providerevents.EventWorkItemSynced,
		AggregateType:        providerevents.AggregateWorkItem,
		AggregateID:          "55",
		PayloadJSON:          []byte(`{"provider_slug":"github","webhook_event_id":"` + webhook.ID.String() + `"}`),
		OccurredAt:           now,
	}
	outboxEvents := []entity.OutboxEvent{
		testOutboxEvent(providerevents.EventWebhookReceived, providerevents.AggregateWebhookEvent, webhook.ID, now),
		testOutboxEvent(providerevents.EventWebhookNormalized, providerevents.AggregateProviderEvent, providerEvent.ID, now),
	}
	providerUpdatedAt := now.Add(-time.Minute)
	projectionUpdate := providerrepo.ProjectionUpdate{
		WorkItem: &entity.ProviderWorkItemProjection{
			Base:               entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
			ProviderSlug:       enum.ProviderSlugGitHub,
			ProviderWorkItemID: "55",
			RepositoryFullName: "codex-k8s/kodex",
			Kind:               enum.WorkItemKindIssue,
			Number:             7,
			URL:                "https://github.com/codex-k8s/kodex/issues/7",
			Title:              "Синхронизировать задачу",
			State:              "open",
			WorkItemType:       "dev",
			LabelsJSON:         []byte(`["type:dev","area:provider-hub"]`),
			AssigneesJSON:      []byte(`["kodex-agent"]`),
			ProjectFieldsJSON:  []byte(`{"stage":"dev"}`),
			WatermarkStatus:    enum.WorkItemWatermarkStatusValid,
			WatermarkJSON:      []byte(`{"work_type":"dev","next_ref":"https://github.com/codex-k8s/kodex/issues/8"}`),
			BodyDigest:         "body-digest",
			ProviderUpdatedAt:  &providerUpdatedAt,
			SyncedAt:           now,
			DriftStatus:        enum.WorkItemDriftStatusFresh,
		},
		Comments: []entity.ProviderCommentProjection{{
			Base:                entity.Base{ID: uuid.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
			ProviderCommentID:   "900",
			Kind:                enum.CommentKindComment,
			AuthorProviderLogin: "kodex-agent",
			BodyDigest:          "comment-digest",
			Summary:             "Комментарий агента",
			ProviderCreatedAt:   &providerUpdatedAt,
			ProviderUpdatedAt:   &providerUpdatedAt,
		}},
		Relationships: []entity.ProviderRelationship{{
			ID:                uuid.New(),
			TargetProviderRef: "https://github.com/codex-k8s/kodex/issues/8",
			RelationshipType:  "next",
			Source:            enum.RelationshipSourceWatermark,
			Confidence:        enum.RelationshipConfidenceConfirmed,
			CreatedAt:         now,
		}},
	}
	storedWebhook, providerEvents, err := repository.StoreWebhookEvent(ctx, webhook, projectionUpdate, []entity.ProviderEvent{providerEvent}, outboxEvents)
	if err != nil {
		t.Fatalf("store webhook event: %v", err)
	}
	if storedWebhook.ID != webhook.ID || storedWebhook.ProcessingStatus != enum.WebhookProcessingStatusProcessed {
		t.Fatalf("stored webhook = %+v, want processed id %s", storedWebhook, webhook.ID)
	}
	if len(providerEvents) != 1 || providerEvents[0].AggregateID != "55" {
		t.Fatalf("provider events = %+v, want aggregate 55", providerEvents)
	}
	workItem, err := repository.GetWorkItemProjection(ctx, query.ProviderTargetLookup{
		ProviderSlug:     enum.ProviderSlugGitHub,
		ProviderObjectID: "55",
	})
	if err != nil {
		t.Fatalf("get work item projection: %v", err)
	}
	if workItem.ID != projectionUpdate.WorkItem.ID || workItem.WorkItemType != "dev" || workItem.WatermarkStatus != enum.WorkItemWatermarkStatusValid {
		t.Fatalf("work item projection = %+v, want stored dev projection", workItem)
	}
	workItems, _, err := repository.ListWorkItemProjections(ctx, query.WorkItemProjectionFilter{})
	if err != nil {
		t.Fatalf("list all work item projections: %v", err)
	}
	if len(workItems) != 1 || workItems[0].ID != workItem.ID {
		t.Fatalf("all work item projections = %+v, want stored projection %s", workItems, workItem.ID)
	}
	workItems, _, err = repository.ListWorkItemProjections(ctx, query.WorkItemProjectionFilter{
		ProviderSlug:       enum.ProviderSlugGitHub,
		RepositoryFullName: "codex-k8s/kodex",
		Kinds:              []enum.WorkItemKind{enum.WorkItemKindIssue},
		Labels:             []string{"type:dev"},
	})
	if err != nil {
		t.Fatalf("list work item projections: %v", err)
	}
	if len(workItems) != 1 || workItems[0].ID != workItem.ID {
		t.Fatalf("work item projections = %+v, want stored projection %s", workItems, workItem.ID)
	}
	comments, _, err := repository.ListComments(ctx, query.CommentProjectionFilter{
		WorkItemProjectionID: workItem.ID,
		Kinds:                []enum.CommentKind{enum.CommentKindComment},
	})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 1 || comments[0].ProviderCommentID != "900" || comments[0].WorkItemProjectionID != workItem.ID {
		t.Fatalf("comments = %+v, want provider comment 900 linked to %s", comments, workItem.ID)
	}
	relationships, _, err := repository.ListRelationships(ctx, query.RelationshipFilter{
		WorkItemProjectionID: &workItem.ID,
		RelationshipTypes:    []string{"next"},
		Sources:              []enum.RelationshipSource{enum.RelationshipSourceWatermark},
	})
	if err != nil {
		t.Fatalf("list relationships: %v", err)
	}
	if len(relationships) != 1 || relationships[0].TargetProviderRef != "https://github.com/codex-k8s/kodex/issues/8" {
		t.Fatalf("relationships = %+v, want next issue relationship", relationships)
	}
	relationship, err := repository.GetRelationshipByIdentity(ctx, query.RelationshipLookup{
		SourceWorkItemID:  workItem.ID,
		TargetProviderRef: "https://github.com/codex-k8s/kodex/issues/8",
		RelationshipType:  "next",
	})
	if err != nil {
		t.Fatalf("get relationship by identity: %v", err)
	}
	if relationship.ID != relationships[0].ID || relationship.Version != 1 {
		t.Fatalf("relationship = %+v, want stored relationship %s with version 1", relationship, relationships[0].ID)
	}
	replayedWebhook := webhook
	replayedWebhook.ID = uuid.New()
	storedWebhook, providerEvents, err = repository.StoreWebhookEvent(ctx, replayedWebhook, providerrepo.ProjectionUpdate{}, []entity.ProviderEvent{{ID: uuid.New()}}, []entity.OutboxEvent{testOutboxEvent(providerevents.EventWebhookReceived, providerevents.AggregateWebhookEvent, replayedWebhook.ID, now)})
	if err != nil {
		t.Fatalf("replay webhook event: %v", err)
	}
	if storedWebhook.ID != webhook.ID || len(providerEvents) != 1 || providerEvents[0].ID != providerEvent.ID {
		t.Fatalf("replayed webhook = %+v provider events = %+v, want original", storedWebhook, providerEvents)
	}
	changedWebhook := webhook
	changedWebhook.ID = uuid.New()
	changedWebhook.PayloadJSON = []byte(`{"issue":{"id":56},"repository":{"id":100}}`)
	_, _, err = repository.StoreWebhookEvent(ctx, changedWebhook, providerrepo.ProjectionUpdate{}, nil, nil)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("store changed duplicate webhook err = %v, want %v", err, errs.ErrConflict)
	}
	webhooks, _, err := repository.ListWebhookEvents(ctx, query.WebhookEventFilter{
		ProviderSlug:       enum.ProviderSlugGitHub,
		EventNames:         []string{"issues"},
		ProcessingStatuses: []enum.WebhookProcessingStatus{enum.WebhookProcessingStatusProcessed},
	})
	if err != nil {
		t.Fatalf("list webhook events: %v", err)
	}
	if len(webhooks) != 1 || webhooks[0].ID != webhook.ID {
		t.Fatalf("webhooks = %+v, want webhook %s", webhooks, webhook.ID)
	}
	claimed, err := repository.ClaimOutboxEvents(ctx, 10, now, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("claim outbox events: %v", err)
	}
	if len(claimed) < 2 {
		t.Fatalf("claimed outbox events = %d, want at least 2", len(claimed))
	}
	if err := repository.MarkOutboxEventPublished(ctx, claimed[0].ID, claimed[0].AttemptCount, now.Add(time.Second)); err != nil {
		t.Fatalf("mark outbox published: %v", err)
	}
}

func TestRepositoryIntegrationProjectionIgnoresStaleWorkItemAndComment(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	workItemID := uuid.New()
	commentID := uuid.New()
	initial := projectionUpdateForTest(workItemID, commentID, now, "Свежая задача", "fresh-body", "Комментарий свежий", "comment-fresh", "https://github.com/codex-k8s/kodex/issues/8")
	if _, _, err := repository.StoreWebhookEvent(ctx, webhookEventForTest(now, "delivery-fresh"), initial, nil, nil); err != nil {
		t.Fatalf("store fresh projection: %v", err)
	}
	staleAt := now.Add(-time.Hour)
	stale := projectionUpdateForTest(workItemID, commentID, staleAt, "Старая задача", "stale-body", "Комментарий старый", "comment-stale", "https://github.com/codex-k8s/kodex/issues/9")
	if _, _, err := repository.StoreWebhookEvent(ctx, webhookEventForTest(now.Add(time.Minute), "delivery-stale"), stale, nil, nil); err != nil {
		t.Fatalf("store stale projection: %v", err)
	}

	workItem, err := repository.GetWorkItemProjection(ctx, query.ProviderTargetLookup{
		ProviderSlug:     enum.ProviderSlugGitHub,
		ProviderObjectID: "github:codex-k8s/kodex:issue:7",
	})
	if err != nil {
		t.Fatalf("get work item projection: %v", err)
	}
	if workItem.Title != "Свежая задача" || workItem.BodyDigest != "fresh-body" || workItem.ProviderUpdatedAt == nil || !workItem.ProviderUpdatedAt.Equal(now) {
		t.Fatalf("work item = %+v, want fresh projection", workItem)
	}
	comments, _, err := repository.ListComments(ctx, query.CommentProjectionFilter{WorkItemProjectionID: workItem.ID})
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(comments) != 1 || comments[0].Summary != "Комментарий свежий" || comments[0].BodyDigest != "comment-fresh" {
		t.Fatalf("comments = %+v, want fresh comment", comments)
	}
	relationships, _, err := repository.ListRelationships(ctx, query.RelationshipFilter{WorkItemProjectionID: &workItem.ID})
	if err != nil {
		t.Fatalf("list relationships: %v", err)
	}
	if len(relationships) != 1 || relationships[0].TargetProviderRef != "https://github.com/codex-k8s/kodex/issues/8" {
		t.Fatalf("relationships = %+v, want fresh relationship", relationships)
	}
}

func TestRepositoryIntegrationProjectionRebuildsWatermarkRelationships(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	workItemID := uuid.New()
	commentID := uuid.New()
	initial := projectionUpdateForTest(workItemID, commentID, now, "Задача", "body-1", "Комментарий", "comment-1", "https://github.com/codex-k8s/kodex/issues/8")
	initial.Relationships = append(initial.Relationships, entity.ProviderRelationship{
		ID:                uuid.New(),
		TargetProviderRef: "https://github.com/codex-k8s/kodex/issues/1",
		RelationshipType:  "source",
		Source:            enum.RelationshipSourceWatermark,
		Confidence:        enum.RelationshipConfidenceConfirmed,
		CreatedAt:         now,
	})
	if _, _, err := repository.StoreWebhookEvent(ctx, webhookEventForTest(now, "delivery-rel-1"), initial, nil, nil); err != nil {
		t.Fatalf("store initial relationships: %v", err)
	}
	updated := projectionUpdateForTest(workItemID, commentID, now.Add(time.Minute), "Задача", "body-2", "Комментарий", "comment-2", "https://github.com/codex-k8s/kodex/issues/9")
	if _, _, err := repository.StoreWebhookEvent(ctx, webhookEventForTest(now.Add(time.Minute), "delivery-rel-2"), updated, nil, nil); err != nil {
		t.Fatalf("store updated relationships: %v", err)
	}
	workItem, err := repository.GetWorkItemProjection(ctx, query.ProviderTargetLookup{
		ProviderSlug:     enum.ProviderSlugGitHub,
		ProviderObjectID: "github:codex-k8s/kodex:issue:7",
	})
	if err != nil {
		t.Fatalf("get work item projection: %v", err)
	}
	relationships, _, err := repository.ListRelationships(ctx, query.RelationshipFilter{
		WorkItemProjectionID: &workItem.ID,
		Sources:              []enum.RelationshipSource{enum.RelationshipSourceWatermark},
	})
	if err != nil {
		t.Fatalf("list relationships: %v", err)
	}
	if len(relationships) != 1 || relationships[0].RelationshipType != "next" || relationships[0].TargetProviderRef != "https://github.com/codex-k8s/kodex/issues/9" {
		t.Fatalf("relationships = %+v, want only updated next relationship", relationships)
	}
}

func webhookEventForTest(receivedAt time.Time, deliveryID string) entity.WebhookEvent {
	return entity.WebhookEvent{
		ID:                   uuid.New(),
		ProviderSlug:         enum.ProviderSlugGitHub,
		DeliveryID:           deliveryID,
		EventName:            "issues",
		RepositoryProviderID: "100",
		ReceivedAt:           receivedAt,
		ProcessingStatus:     enum.WebhookProcessingStatusProcessed,
		PayloadJSON:          []byte(`{"issue":{"id":55,"number":7},"repository":{"id":100}}`),
		RetainUntil:          receivedAt.Add(30 * 24 * time.Hour),
	}
}

func projectionUpdateForTest(workItemID uuid.UUID, commentID uuid.UUID, providerUpdatedAt time.Time, title string, bodyDigest string, summary string, commentDigest string, nextRef string) providerrepo.ProjectionUpdate {
	return providerrepo.ProjectionUpdate{
		WorkItem: &entity.ProviderWorkItemProjection{
			Base:               entity.Base{ID: workItemID, Version: 1, CreatedAt: providerUpdatedAt, UpdatedAt: providerUpdatedAt},
			ProviderSlug:       enum.ProviderSlugGitHub,
			ProviderWorkItemID: "github:codex-k8s/kodex:issue:7",
			RepositoryFullName: "codex-k8s/kodex",
			Kind:               enum.WorkItemKindIssue,
			Number:             7,
			URL:                "https://github.com/codex-k8s/kodex/issues/7",
			Title:              title,
			State:              "open",
			WorkItemType:       "dev",
			LabelsJSON:         []byte(`["type:dev"]`),
			AssigneesJSON:      []byte(`[]`),
			ProjectFieldsJSON:  []byte(`{}`),
			WatermarkStatus:    enum.WorkItemWatermarkStatusValid,
			WatermarkJSON:      []byte(`{"work_type":"dev"}`),
			BodyDigest:         bodyDigest,
			ProviderUpdatedAt:  &providerUpdatedAt,
			SyncedAt:           providerUpdatedAt,
			DriftStatus:        enum.WorkItemDriftStatusFresh,
		},
		Comments: []entity.ProviderCommentProjection{{
			Base:                 entity.Base{ID: commentID, Version: 1, CreatedAt: providerUpdatedAt, UpdatedAt: providerUpdatedAt},
			WorkItemProjectionID: workItemID,
			ProviderCommentID:    "900",
			Kind:                 enum.CommentKindComment,
			AuthorProviderLogin:  "kodex-agent",
			BodyDigest:           commentDigest,
			Summary:              summary,
			ProviderCreatedAt:    &providerUpdatedAt,
			ProviderUpdatedAt:    &providerUpdatedAt,
		}},
		Relationships: []entity.ProviderRelationship{{
			ID:                uuid.New(),
			SourceWorkItemID:  workItemID,
			TargetProviderRef: nextRef,
			RelationshipType:  "next",
			Source:            enum.RelationshipSourceWatermark,
			Confidence:        enum.RelationshipConfidenceConfirmed,
			CreatedAt:         providerUpdatedAt,
		}},
	}
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("KODEX_PROVIDER_HUB_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("set KODEX_PROVIDER_HUB_TEST_DATABASE_DSN to run PostgreSQL repository integration tests")
	}
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "provider_repo_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.WithoutCancel(ctx), "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
	})

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse pool config: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	for _, statement := range migrationtest.GooseUpStatements(t, "../../../../cmd/cli/migrations") {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply provider-hub migration statement %q: %v", statement, err)
		}
	}
	return pool
}

func testOutboxEvent(eventType string, aggregateType string, aggregateID uuid.UUID, occurredAt time.Time) entity.OutboxEvent {
	event := outboxlib.NewEvent(uuid.New(), eventType, providerevents.SchemaVersion, aggregateType, aggregateID, []byte(`{"ok":true}`), occurredAt, 0)
	return outboxlib.RecordFromParts(event, outboxlib.RecordDelivery{}, outboxlib.RecordFailure{})
}
