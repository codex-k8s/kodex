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
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
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
	changedRemaining := int64(4998)
	changedSnapshot := replayedSnapshot
	changedSnapshot.ID = uuid.New()
	changedSnapshot.Remaining = &changedRemaining
	_, err = repository.RecordLimitSnapshot(ctx, changedSnapshot, state)
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("record changed duplicate snapshot err = %v, want %v", err, errs.ErrConflict)
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
