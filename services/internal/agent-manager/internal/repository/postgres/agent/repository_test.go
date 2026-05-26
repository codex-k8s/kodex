package agent

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	migrationtest "github.com/codex-k8s/kodex/libs/go/migrationtest"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/value"
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

func TestAgentActivityListSQLUsesKeysetCursor(t *testing.T) {
	t.Parallel()

	query, err := loadQuery("agent_activity__list")
	if err != nil {
		t.Fatalf("load agent activity list query: %v", err)
	}
	if strings.Contains(strings.ToUpper(query), "OFFSET") {
		t.Fatalf("agent activity list query must not use OFFSET:\n%s", query)
	}
	if !strings.Contains(query, "(started_at, id) <") || !strings.Contains(query, "@cursor_started_at") || !strings.Contains(query, "@cursor_id") {
		t.Fatalf("agent activity list query must use keyset cursor by (started_at, id):\n%s", query)
	}
}

func TestListAgentActivitiesKeysetPaginationStableUnderConcurrentInsert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	sessionID := uuid.MustParse("89898989-1111-2222-3333-444444444444")
	baseTime := time.Date(2026, 5, 26, 22, 30, 0, 0, time.UTC)
	insertTestAgentSession(t, ctx, pool, sessionID, baseTime)

	firstID := uuid.MustParse("89898989-1111-2222-3333-000000000003")
	secondID := uuid.MustParse("89898989-1111-2222-3333-000000000002")
	thirdID := uuid.MustParse("89898989-1111-2222-3333-000000000001")
	insertTestAgentActivity(t, ctx, pool, firstID, sessionID, baseTime.Add(3*time.Minute))
	insertTestAgentActivity(t, ctx, pool, secondID, sessionID, baseTime.Add(2*time.Minute))
	insertTestAgentActivity(t, ctx, pool, thirdID, sessionID, baseTime.Add(2*time.Minute))

	activities, page, err := repository.ListAgentActivities(ctx, query.AgentActivityFilter{
		SessionID: sessionID,
		Page:      value.PageRequest{PageSize: 2},
	})
	if err != nil {
		t.Fatalf("list first activity page: %v", err)
	}
	if got := activityIDs(activities); strings.Join(got, ",") != firstID.String()+","+secondID.String() {
		t.Fatalf("first page ids = %v", got)
	}
	if page.NextPageToken == "" {
		t.Fatal("first page next token is empty")
	}

	newerID := uuid.MustParse("89898989-1111-2222-3333-000000000004")
	insertTestAgentActivity(t, ctx, pool, newerID, sessionID, baseTime.Add(4*time.Minute))

	activities, page, err = repository.ListAgentActivities(ctx, query.AgentActivityFilter{
		SessionID: sessionID,
		Page:      value.PageRequest{PageSize: 2, PageToken: page.NextPageToken},
	})
	if err != nil {
		t.Fatalf("list second activity page: %v", err)
	}
	if got := activityIDs(activities); strings.Join(got, ",") != thirdID.String() {
		t.Fatalf("second page ids = %v, want only %s", got, thirdID)
	}
	if page.NextPageToken != "" {
		t.Fatalf("second page next token = %q, want empty", page.NextPageToken)
	}
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("KODEX_AGENT_MANAGER_TEST_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("set KODEX_AGENT_MANAGER_TEST_DATABASE_DSN to run PostgreSQL repository integration tests")
	}
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "agent_repo_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
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
			t.Fatalf("apply agent-manager migration statement %q: %v", statement, err)
		}
	}
	return pool
}

func insertTestAgentSession(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID uuid.UUID, now time.Time) {
	t.Helper()

	_, err := pool.Exec(ctx, `
INSERT INTO agent_manager_sessions (
    id,
    scope_type,
    scope_ref,
    provider_work_item_ref,
    status,
    created_by_actor_ref,
    version,
    created_at,
    updated_at
) VALUES ($1, 'project', 'project:activity-test', 'issue:activity-test', 'open', 'user:owner', 1, $2, $2)
`, sessionID, now)
	if err != nil {
		t.Fatalf("insert test session: %v", err)
	}
}

func insertTestAgentActivity(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, sessionID uuid.UUID, startedAt time.Time) {
	t.Helper()

	_, err := pool.Exec(ctx, `
INSERT INTO agent_manager_agent_activities (
    id,
    session_id,
    activity_kind,
    status,
    started_at,
    safe_summary,
    idempotency_key,
    version,
    created_at,
    updated_at
) VALUES ($1, $2, 'lifecycle', 'started', $3, 'safe activity', $4, 1, $3, $3)
`, id, sessionID, startedAt, "domain.Service.RecordAgentActivity:user:owner:"+id.String())
	if err != nil {
		t.Fatalf("insert test activity %s: %v", id, err)
	}
}

func activityIDs(activities []entity.AgentActivity) []string {
	result := make([]string, 0, len(activities))
	for _, activity := range activities {
		result = append(result, activity.ID.String())
	}
	return result
}
