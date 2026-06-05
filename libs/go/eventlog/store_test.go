package eventlog

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var sqlHeaderPattern = regexp.MustCompile(`^-- name: ([a-z0-9_]+__[a-z0-9_]+(?:_[a-z0-9_]+)*) :(one|many|exec)$`)

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

func TestAppendValidatesEventContract(t *testing.T) {
	t.Parallel()

	store := &Store{db: panicDatabase{}}
	if err := store.Append(context.Background(), AppendParams{Event: Event{ID: uuid.New()}}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("Append() err = %v, want %v", err, ErrInvalidEvent)
	}
}

func TestClaimValidatesLeaseContract(t *testing.T) {
	t.Parallel()

	store := &Store{db: panicDatabase{}}
	now := time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC)
	_, err := store.Claim(context.Background(), ClaimParams{
		ConsumerName: "projection",
		LeaseOwner:   "worker-1",
		Limit:        1,
		Now:          now,
		LockedUntil:  now,
	})
	if !errors.Is(err, ErrInvalidClaim) {
		t.Fatalf("Claim() err = %v, want %v", err, ErrInvalidClaim)
	}
}

func TestDeferValidatesLeaseContract(t *testing.T) {
	t.Parallel()

	store := &Store{db: panicDatabase{}}
	now := time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC)
	err := store.Defer(context.Background(), DeferParams{
		ConsumerName: "projection",
		LeaseOwner:   "worker-1",
		Now:          now,
		LockedUntil:  now,
	})
	if !errors.Is(err, ErrInvalidClaim) {
		t.Fatalf("Defer() err = %v, want %v", err, ErrInvalidClaim)
	}
}

func TestPostgresIntegrationAppendClaimAdvanceAndFanOut(t *testing.T) {
	dsn := os.Getenv("KODEX_EVENTLOG_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("KODEX_EVENTLOG_TEST_DATABASE_DSN is empty")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgres pool: %v", err)
	}
	defer pool.Close()
	applyMigrations(t, ctx, pool)

	store := NewStore(pool)
	now := time.Date(2026, 5, 4, 13, 0, 0, 0, time.UTC)
	first := testEvent(now, "access.user.created")
	second := testEvent(now.Add(time.Second), "access.user.updated")

	if err := store.Append(ctx, AppendParams{Event: first, RecordedAt: now}); err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if err := store.Append(ctx, AppendParams{Event: first, RecordedAt: now.Add(time.Second)}); err != nil {
		t.Fatalf("append duplicate event: %v", err)
	}
	conflictingFirst := first
	conflictingFirst.Payload = []byte(`{"user_id":"changed"}`)
	if err := store.Append(ctx, AppendParams{Event: conflictingFirst, RecordedAt: now.Add(2 * time.Second)}); !errors.Is(err, ErrEventConflict) {
		t.Fatalf("append conflicting duplicate event err = %v, want %v", err, ErrEventConflict)
	}
	if err := store.Append(ctx, AppendParams{Event: second, RecordedAt: now.Add(2 * time.Second)}); err != nil {
		t.Fatalf("append second event: %v", err)
	}

	storedFirst, err := store.GetStoredEvent(ctx, first.ID)
	if err != nil {
		t.Fatalf("get stored first event: %v", err)
	}
	if storedFirst.SequenceID != 1 {
		t.Fatalf("first sequence id = %d, want 1", storedFirst.SequenceID)
	}

	consumerA, err := store.Claim(ctx, ClaimParams{
		ConsumerName: "projection-a",
		LeaseOwner:   "worker-a",
		Limit:        1,
		Now:          now.Add(3 * time.Second),
		LockedUntil:  now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("claim consumer a: %v", err)
	}
	if len(consumerA.Events) != 1 || consumerA.Events[0].ID != first.ID {
		t.Fatalf("consumer a events = %#v, want first event", consumerA.Events)
	}

	lockedAgain, err := store.Claim(ctx, ClaimParams{
		ConsumerName: "projection-a",
		LeaseOwner:   "worker-b",
		Limit:        1,
		Now:          now.Add(4 * time.Second),
		LockedUntil:  now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("claim locked consumer a: %v", err)
	}
	if len(lockedAgain.Events) != 0 {
		t.Fatalf("locked consumer events = %d, want 0", len(lockedAgain.Events))
	}

	consumerB, err := store.Claim(ctx, ClaimParams{
		ConsumerName: "projection-b",
		LeaseOwner:   "worker-b",
		Limit:        2,
		Now:          now.Add(5 * time.Second),
		LockedUntil:  now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("claim consumer b: %v", err)
	}
	if len(consumerB.Events) != 2 {
		t.Fatalf("consumer b events = %d, want 2", len(consumerB.Events))
	}

	if err := store.Advance(ctx, AdvanceParams{
		ConsumerName:   "projection-a",
		LeaseOwner:     "worker-a",
		LastSequenceID: consumerA.Events[0].SequenceID,
		Now:            now.Add(6 * time.Second),
	}); err != nil {
		t.Fatalf("advance consumer a: %v", err)
	}
	nextForA, err := store.Claim(ctx, ClaimParams{
		ConsumerName: "projection-a",
		LeaseOwner:   "worker-a",
		Limit:        1,
		Now:          now.Add(7 * time.Second),
		LockedUntil:  now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("claim next for consumer a: %v", err)
	}
	if len(nextForA.Events) != 1 || nextForA.Events[0].ID != second.ID {
		t.Fatalf("next consumer a events = %#v, want second event", nextForA.Events)
	}
}

func TestPostgresIntegrationRepairLegacyCheckpointRetrySchema(t *testing.T) {
	dsn := os.Getenv("KODEX_EVENTLOG_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("KODEX_EVENTLOG_TEST_DATABASE_DSN is empty")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open postgres pool: %v", err)
	}
	defer pool.Close()

	resetEventLogTables(t, ctx, pool)
	execSQL(t, ctx, pool, legacyEventLogSchemaSQL, "create legacy platform-event-log schema")
	applyMigrationFile(t, ctx, pool, "migrations/20260605090000_platform_event_log_checkpoint_retry_fields.sql")
	applyMigrationFile(t, ctx, pool, "migrations/20260605090000_platform_event_log_checkpoint_retry_fields.sql")
	assertCheckpointRetrySchema(t, ctx, pool)

	var retrySequenceID int64
	var retryAttempt int
	var lastError string
	if err := pool.QueryRow(ctx, `
		SELECT retry_sequence_id, retry_attempt, last_error
		FROM platform_event_consumer_checkpoints
		WHERE consumer_name = 'legacy-consumer'
	`).Scan(&retrySequenceID, &retryAttempt, &lastError); err != nil {
		t.Fatalf("read repaired checkpoint row: %v", err)
	}
	if retrySequenceID != 0 || retryAttempt != 0 || lastError != "" {
		t.Fatalf("repaired checkpoint row = (%d, %d, %q), want zero retry state", retrySequenceID, retryAttempt, lastError)
	}
	if _, err := pool.Exec(ctx, `UPDATE platform_event_consumer_checkpoints SET retry_attempt = -1 WHERE consumer_name = 'legacy-consumer'`); err == nil {
		t.Fatal("negative retry_attempt update succeeded, want check violation")
	}
}

func testEvent(occurredAt time.Time, eventType string) Event {
	return Event{
		ID:            uuid.New(),
		SourceService: "access-manager",
		EventType:     eventType,
		SchemaVersion: 1,
		AggregateType: "user",
		AggregateID:   uuid.New(),
		Payload:       []byte(`{"user_id":"test"}`),
		OccurredAt:    occurredAt,
	}
}

func applyMigrations(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)
	for i := len(files) - 1; i >= 0; i-- {
		content, err := os.ReadFile(files[i])
		if err != nil {
			t.Fatalf("read migration %s: %v", files[i], err)
		}
		execSQL(t, ctx, pool, downMigrationSQL(t, string(content), files[i]), "reset migration "+files[i])
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		execSQL(t, ctx, pool, upMigrationSQL(t, string(content), file), "apply migration "+file)
	}
}

func upMigrationSQL(t *testing.T, content string, file string) string {
	t.Helper()

	upIndex := strings.Index(content, "-- +goose Up")
	downIndex := strings.Index(content, "-- +goose Down")
	if upIndex < 0 || downIndex < 0 || downIndex < upIndex {
		t.Fatalf("invalid goose migration markers in %s", file)
	}
	return content[upIndex+len("-- +goose Up") : downIndex]
}

func downMigrationSQL(t *testing.T, content string, file string) string {
	t.Helper()

	downIndex := strings.Index(content, "-- +goose Down")
	if downIndex < 0 {
		t.Fatalf("invalid goose migration markers in %s", file)
	}
	return content[downIndex+len("-- +goose Down"):]
}

func applyMigrationFile(t *testing.T, ctx context.Context, pool *pgxpool.Pool, file string) {
	t.Helper()

	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read migration %s: %v", file, err)
	}
	execSQL(t, ctx, pool, upMigrationSQL(t, string(content), file), "apply migration "+file)
}

func execSQL(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, label string) {
	t.Helper()

	if !hasExecutableSQL(sql) {
		return
	}
	if _, err := pool.Exec(ctx, sql, pgx.QueryExecModeSimpleProtocol); err != nil {
		t.Fatalf("%s: %v", label, err)
	}
}

func hasExecutableSQL(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "--") {
			return true
		}
	}
	return false
}

func resetEventLogTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	execSQL(t, ctx, pool, `
DROP TABLE IF EXISTS platform_event_consumer_checkpoints;
DROP TABLE IF EXISTS platform_event_log;
`, "reset platform-event-log tables")
}

func assertCheckpointRetrySchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	rows, err := pool.Query(ctx, `
		SELECT column_name, is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_name = 'platform_event_consumer_checkpoints'
		  AND column_name IN ('retry_sequence_id', 'retry_attempt', 'last_error')
	`)
	if err != nil {
		t.Fatalf("read checkpoint columns: %v", err)
	}
	defer rows.Close()

	type column struct {
		nullable string
		def      string
	}
	columns := make(map[string]column, 3)
	for rows.Next() {
		var name string
		var c column
		if err := rows.Scan(&name, &c.nullable, &c.def); err != nil {
			t.Fatalf("scan checkpoint column: %v", err)
		}
		columns[name] = c
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate checkpoint columns: %v", err)
	}
	expected := map[string]string{
		"retry_sequence_id": "0",
		"retry_attempt":     "0",
		"last_error":        "''::text",
	}
	for name, defaultValue := range expected {
		c, ok := columns[name]
		if !ok {
			t.Fatalf("checkpoint column %s is missing", name)
		}
		if c.nullable != "NO" {
			t.Fatalf("checkpoint column %s nullable = %s, want NO", name, c.nullable)
		}
		if c.def != defaultValue {
			t.Fatalf("checkpoint column %s default = %q, want %q", name, c.def, defaultValue)
		}
	}
}

const legacyEventLogSchemaSQL = `
CREATE TABLE platform_event_log (
    sequence_id bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY,
    event_id uuid NOT NULL UNIQUE,
    source_service text NOT NULL,
    event_type text NOT NULL,
    schema_version integer NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id uuid NOT NULL,
    payload jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    recorded_at timestamptz NOT NULL,
    CONSTRAINT platform_event_log_source_service_chk CHECK (source_service <> ''),
    CONSTRAINT platform_event_log_event_type_chk CHECK (event_type <> ''),
    CONSTRAINT platform_event_log_schema_version_chk CHECK (schema_version > 0),
    CONSTRAINT platform_event_log_aggregate_type_chk CHECK (aggregate_type <> '')
);

CREATE TABLE platform_event_consumer_checkpoints (
    consumer_name text PRIMARY KEY,
    last_sequence_id bigint NOT NULL DEFAULT 0,
    lease_owner text NOT NULL DEFAULT '',
    locked_until timestamptz,
    updated_at timestamptz NOT NULL,
    CONSTRAINT platform_event_consumer_name_chk CHECK (consumer_name <> ''),
    CONSTRAINT platform_event_consumer_last_sequence_chk CHECK (last_sequence_id >= 0),
    CONSTRAINT platform_event_consumer_lease_consistency_chk
        CHECK ((lease_owner = '' AND locked_until IS NULL) OR (lease_owner <> '' AND locked_until IS NOT NULL))
);

INSERT INTO platform_event_consumer_checkpoints (consumer_name, updated_at)
VALUES ('legacy-consumer', '2026-06-05 09:00:00+00');
`

type panicDatabase struct{}

func (panicDatabase) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("database must not be called")
}

func (panicDatabase) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("database must not be called")
}
