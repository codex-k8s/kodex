package postgresinbox

import (
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TestClaimMutationArgumentsPinsConsumerEventAndLease ensures every fenced inbox update receives the exact claim coordinates.
// TestClaimMutationArgumentsPinsConsumerEventAndLease проверяет точные координаты claim для каждого защищённого обновления inbox.
func TestClaimMutationArgumentsPinsConsumerEventAndLease(t *testing.T) {
	t.Parallel()
	claim := Claim{
		Consumer: Consumer{Name: "consumer", Scope: "scope"}, EventID: "event",
		LeaseOwner: "owner", LeaseToken: "token", LeaseGeneration: 12, LeaseFence: 13,
	}
	claim.EventDigest[0] = 7
	actual := claimMutationArguments(claim)
	expected := pgx.StrictNamedArgs{
		"consumer_name": "consumer", "consumer_scope": "scope", "event_id": "event",
		"event_digest": claim.EventDigest[:], "lease_owner": "owner", "lease_token": "token",
		"lease_generation": uint64(12), "lease_fence": uint64(13),
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("fenced mutation coordinates changed")
	}
	actual["error_code"] = "EFFECT_FAILED"
	if next := claimMutationArguments(claim); len(next) != 8 || next["error_code"] != nil {
		t.Fatal("operation-specific keys leaked between claim mutations")
	}
	for _, operation := range []struct {
		name  string
		query string
		extra pgx.StrictNamedArgs
	}{
		{"complete", rawInboxComplete, pgx.StrictNamedArgs{"retention_seconds": float64(10)}},
		{"retry", rawInboxMarkRetry, pgx.StrictNamedArgs{"backoff_seconds": float64(5), "error_code": "EFFECT_FAILED"}},
		{"dead-letter", rawInboxMarkDeadLetter, pgx.StrictNamedArgs{"error_code": "EFFECT_FAILED"}},
	} {
		arguments := claimMutationArguments(claim)
		for name, value := range operation.extra {
			arguments[name] = value
		}
		if _, _, err := arguments.RewriteQuery(t.Context(), nil, operation.query, nil); err != nil {
			t.Fatalf("%s SQL argument contract changed: %v", operation.name, err)
		}
	}
}

// TestRequireOneClaimMutationKeepsErrorAndStaleMapping checks the old database, zero-row and multi-row outcomes.
// TestRequireOneClaimMutationKeepsErrorAndStaleMapping проверяет прежние исходы ошибки БД, нуля и нескольких строк.
func TestRequireOneClaimMutationKeepsErrorAndStaleMapping(t *testing.T) {
	t.Parallel()
	if err := requireOneClaimMutation(pgconn.NewCommandTag("UPDATE 1"), nil); err != nil {
		t.Fatalf("single fenced row rejected: %v", err)
	}
	for _, command := range []string{"UPDATE 0", "UPDATE 2"} {
		if err := requireOneClaimMutation(pgconn.NewCommandTag(command), nil); !errors.Is(err, ErrStaleClaim) {
			t.Fatalf("%s outcome = %v, want stale claim", command, err)
		}
	}
	cause := errors.New("synthetic database fault")
	err := requireOneClaimMutation(pgconn.NewCommandTag("UPDATE 1"), cause)
	if !errors.Is(err, cause) || err.Error() != errorTextDatabaseOperation {
		t.Fatalf("database failure mapping changed: %v", err)
	}
}
