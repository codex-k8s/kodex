package interaction

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
	interactionevents "github.com/codex-k8s/kodex/libs/go/platformevents/interaction"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/interaction-hub/internal/domain/types/value"
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
		})
	}
}

func TestRepositoryIntegrationThreadMessageAndOutbox(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool := openIntegrationPool(t, ctx)
	repository := NewRepository(pool)
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)

	thread := testThread(now)
	createCommandID := uuid.New()
	createResult := testCommandResult(createCommandID, "thread-create", enum.OperationCreateConversationThread, interactionevents.AggregateThread, thread.ID, "create-fingerprint", now)
	createEvent := testOutboxEvent(interactionevents.EventThreadCreated, interactionevents.AggregateThread, thread.ID, now)
	if err := repository.CreateConversationThreadWithResult(ctx, thread, createResult, createEvent); err != nil {
		t.Fatalf("create thread with result: %v", err)
	}
	storedThread, err := repository.GetConversationThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}
	if storedThread.Scope.Type != enum.ScopeTypeService || storedThread.Version != 1 {
		t.Fatalf("stored thread = %+v, want service scope v1", storedThread)
	}
	replayCommandID := uuid.New()
	storedResult, err := repository.GetCommandResult(ctx, query.CommandIdentity{
		CommandID:      replayCommandID,
		IdempotencyKey: createResult.IdempotencyKey,
		ActorRef:       createResult.ActorRef,
		Operation:      createResult.Operation,
	})
	if err != nil {
		t.Fatalf("get command result by idempotency: %v", err)
	}
	if storedResult.CommandID != createCommandID || storedResult.RequestFingerprint != createResult.RequestFingerprint {
		t.Fatalf("stored result = %+v, want command %s", storedResult, createCommandID)
	}

	message := testMessage(thread.ID, now.Add(time.Minute))
	thread.LatestMessageID = &message.ID
	thread.Version = 2
	thread.UpdatedAt = message.CreatedAt
	messageResult := testCommandResult(uuid.New(), "message-create", enum.OperationRecordConversationMessage, interactionevents.AggregateMessage, message.ID, "message-fingerprint", message.CreatedAt)
	messageEvent := testOutboxEvent(interactionevents.EventMessageRecorded, interactionevents.AggregateMessage, message.ID, message.CreatedAt)
	if err := repository.CreateConversationMessageWithResult(ctx, message, thread, 99, messageResult, messageEvent); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("stale message create err = %v, want %v", err, errs.ErrConflict)
	}
	if err := repository.CreateConversationMessageWithResult(ctx, message, thread, 1, messageResult, messageEvent); err != nil {
		t.Fatalf("create message with result: %v", err)
	}
	storedMessage, err := repository.GetConversationMessage(ctx, message.ID)
	if err != nil {
		t.Fatalf("get message: %v", err)
	}
	if storedMessage.BodyObject.SizeBytes == nil || *storedMessage.BodyObject.SizeBytes != 512 || storedMessage.SafeMetadata["surface"] != "mcp" {
		t.Fatalf("stored message = %+v, want object ref and safe metadata", storedMessage)
	}
	updatedThread, err := repository.GetConversationThread(ctx, thread.ID)
	if err != nil {
		t.Fatalf("get updated thread: %v", err)
	}
	if updatedThread.LatestMessageID == nil || *updatedThread.LatestMessageID != message.ID || updatedThread.Version != 2 {
		t.Fatalf("updated thread = %+v, want latest message %s v2", updatedThread, message.ID)
	}
	messages, page, err := repository.ListConversationMessages(ctx, query.ConversationMessageFilter{ThreadID: thread.ID, Page: value.PageRequest{PageSize: 1}})
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 1 || page.NextPageToken != "" || messages[0].ID != message.ID {
		t.Fatalf("messages = %+v page = %+v, want single message", messages, page)
	}

	claimedEvents, err := repository.ClaimOutboxEvents(ctx, 10, now.Add(2*time.Minute), now.Add(3*time.Minute))
	if err != nil {
		t.Fatalf("claim outbox events: %v", err)
	}
	if len(claimedEvents) != 2 || claimedEvents[0].AttemptCount != 1 {
		t.Fatalf("claimed events = %+v, want two leased events", claimedEvents)
	}
	if err := repository.MarkOutboxEventPublished(ctx, claimedEvents[0].ID, claimedEvents[0].AttemptCount, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("mark first event published: %v", err)
	}
	if err := repository.MarkOutboxEventFailed(ctx, claimedEvents[1].ID, claimedEvents[1].AttemptCount, now.Add(5*time.Minute), "temporary"); err != nil {
		t.Fatalf("mark second event failed: %v", err)
	}
	reclaimedEvents, err := repository.ClaimOutboxEvents(ctx, 10, now.Add(6*time.Minute), now.Add(7*time.Minute))
	if err != nil {
		t.Fatalf("reclaim outbox events: %v", err)
	}
	if len(reclaimedEvents) != 1 || reclaimedEvents[0].ID != claimedEvents[1].ID || reclaimedEvents[0].AttemptCount != 2 {
		t.Fatalf("reclaimed events = %+v, want retry event attempt 2", reclaimedEvents)
	}
	if err := repository.MarkOutboxEventPermanentlyFailed(ctx, reclaimedEvents[0].ID, reclaimedEvents[0].AttemptCount, now.Add(8*time.Minute), "permanent"); err != nil {
		t.Fatalf("mark retry event permanently failed: %v", err)
	}
}

func testThread(now time.Time) entity.ConversationThread {
	return entity.ConversationThread{
		ID:              uuid.New(),
		Scope:           value.ScopeRef{Type: enum.ScopeTypeService, Ref: "agent-manager"},
		ThreadKind:      enum.ConversationThreadKindUserDialog,
		PrimaryActorRef: "service:agent-manager",
		SourceKind:      enum.ConversationSourceKindService,
		SourceRef:       "run:123",
		Status:          enum.ConversationThreadStatusOpen,
		CorrelationID:   "trace-123",
		RetentionClass:  "standard",
		Version:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func testMessage(threadID uuid.UUID, now time.Time) entity.ConversationMessage {
	size := int64(512)
	return entity.ConversationMessage{
		ID:          uuid.New(),
		ThreadID:    threadID,
		MessageKind: enum.ConversationMessageKindAgentText,
		AuthorRef:   "agent:codex",
		BodySummary: "safe summary",
		BodyObject: value.ObjectRef{
			URI:       "s3://kodex-interactions/messages/1",
			Digest:    "sha256:" + strings.Repeat("a", 64),
			SizeBytes: &size,
		},
		BodyDigest:   "sha256:" + strings.Repeat("b", 64),
		Locale:       "ru",
		SafeMetadata: map[string]string{"surface": "mcp"},
		CreatedAt:    now,
	}
}

func testCommandResult(commandID uuid.UUID, idempotencyKey string, operation enum.Operation, aggregateType string, aggregateID uuid.UUID, fingerprint string, now time.Time) entity.CommandResult {
	return entity.CommandResult{
		Key:                "command:" + commandID.String(),
		CommandID:          commandID,
		IdempotencyKey:     idempotencyKey,
		ActorRef:           "service:interaction-test",
		Operation:          operation,
		AggregateType:      aggregateType,
		AggregateID:        aggregateID,
		RequestFingerprint: fingerprint,
		ResultPayload:      []byte(`{}`),
		CreatedAt:          now,
	}
}

func testOutboxEvent(eventType string, aggregateType string, aggregateID uuid.UUID, now time.Time) entity.OutboxEvent {
	return entity.OutboxEvent{
		Event: outboxlib.NewEvent(uuid.New(), eventType, interactionevents.SchemaVersion, aggregateType, aggregateID, []byte(`{"version":1}`), now, 0),
	}
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("KODEX_INTERACTION_HUB_TEST_DATABASE_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("set KODEX_INTERACTION_HUB_TEST_DATABASE_DSN to run PostgreSQL repository integration tests")
	}
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := "interaction_repo_test_" + strings.ReplaceAll(uuid.NewString(), "-", "_")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.WithoutCancel(ctx), "DROP SCHEMA IF EXISTS "+quotedSchema+" CASCADE")
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
	applyMigrations(t, ctx, pool)
	return pool
}

func applyMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	statements := migrationtest.GooseUpStatements(t, "../../../../cmd/cli/migrations")
	for _, statement := range statements {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("apply migration statement %q: %v", statement, err)
		}
	}
}
