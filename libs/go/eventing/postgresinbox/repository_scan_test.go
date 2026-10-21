package postgresinbox

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// inboxProjectionFixture substitutes only the pgx.Row Scan boundary; no database or transaction is opened.
// inboxProjectionFixture подменяет только границу pgx.Row Scan; база и транзакция не открываются.
type inboxProjectionFixture struct {
	scan func(...any) error
}

// Scan forwards positional destinations to the fixture so tests can verify the exact SQL projection.
// Scan передаёт позиционные получатели fixture для проверки точной SQL-проекции.
func (fixture inboxProjectionFixture) Scan(destinations ...any) error {
	return fixture.scan(destinations...)
}

// TestScanInboxRowKeepsProjectionAndErrors checks all projected columns and preserves a deferred pgx scan failure.
// TestScanInboxRowKeepsProjectionAndErrors проверяет все столбцы проекции и сохраняет отложенную ошибку pgx Scan.
func TestScanInboxRowKeepsProjectionAndErrors(t *testing.T) {
	t.Parallel()
	owner, token, code := "owner", "token", "retry"
	deadline := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	processed := deadline.Add(time.Minute)
	cleanup := deadline.Add(time.Hour)
	terminal := deadline.Add(2 * time.Hour)
	expected := inboxRow{
		EventID: "event", EventDigest: []byte{1, 2}, OrderingKey: "ordering", EventSequence: 3,
		State: stateProcessing, Attempts: 4, MaxAttempts: 5, RepairCount: 6, MaxRepairs: 7,
		LeaseOwner: &owner, LeaseToken: &token, LeaseGeneration: 8, LeaseFence: 9,
		LeaseExpiresAt: &deadline, AvailableAt: deadline, LastErrorCode: &code,
		ProcessedAt: &processed, CleanupAfter: &cleanup, TerminalAt: &terminal,
		AvailableNow: true, LeaseActive: true,
	}
	fixture := inboxProjectionFixture{scan: func(dest ...any) error {
		if len(dest) != 21 {
			t.Fatalf("inbox projection destinations = %d, want 21", len(dest))
		}
		*dest[0].(*string) = expected.EventID
		*dest[1].(*[]byte) = expected.EventDigest
		*dest[2].(*string) = expected.OrderingKey
		*dest[3].(*uint64) = expected.EventSequence
		*dest[4].(*string) = expected.State
		*dest[5].(*uint32) = expected.Attempts
		*dest[6].(*uint32) = expected.MaxAttempts
		*dest[7].(*uint32) = expected.RepairCount
		*dest[8].(*uint32) = expected.MaxRepairs
		*dest[9].(**string) = expected.LeaseOwner
		*dest[10].(**string) = expected.LeaseToken
		*dest[11].(*uint64) = expected.LeaseGeneration
		*dest[12].(*uint64) = expected.LeaseFence
		*dest[13].(**time.Time) = expected.LeaseExpiresAt
		*dest[14].(*time.Time) = expected.AvailableAt
		*dest[15].(**string) = expected.LastErrorCode
		*dest[16].(**time.Time) = expected.ProcessedAt
		*dest[17].(**time.Time) = expected.CleanupAfter
		*dest[18].(**time.Time) = expected.TerminalAt
		*dest[19].(*bool) = expected.AvailableNow
		*dest[20].(*bool) = expected.LeaseActive
		return nil
	}}
	actual, err := scanInboxRow(fixture)
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("scan result differs: err=%v", err)
	}
	actual, err = scanInboxRow(inboxProjectionFixture{scan: func(...any) error { return pgx.ErrNoRows }})
	if !errors.Is(err, pgx.ErrNoRows) || !reflect.DeepEqual(actual, inboxRow{}) {
		t.Fatalf("deferred no-rows outcome changed: err=%v", err)
	}
}
