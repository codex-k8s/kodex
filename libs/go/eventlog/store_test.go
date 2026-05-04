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
		for _, statement := range splitSQLStatements(downMigrationSQL(t, string(content), files[i])) {
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatalf("reset migration %s statement %q: %v", files[i], statement, err)
			}
		}
	}
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		for _, statement := range splitSQLStatements(upMigrationSQL(t, string(content), file)) {
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatalf("apply migration %s statement %q: %v", file, statement, err)
			}
		}
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

func splitSQLStatements(content string) []string {
	parts := strings.Split(content, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		statement := strings.TrimSpace(part)
		if statement != "" {
			statements = append(statements, statement)
		}
	}
	return statements
}

type panicDatabase struct{}

func (panicDatabase) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	panic("database must not be called")
}

func (panicDatabase) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("database must not be called")
}

func (panicDatabase) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("database must not be called")
}
