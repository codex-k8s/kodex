package enum

import "testing"

func TestTurnAttemptStateRegistryIsClosed(t *testing.T) {
	t.Parallel()

	for _, state := range []string{
		"QUEUED", "CLAIMED", "WAITING_OWNER", "WAITING_EXTERNAL",
		"BLOCKED", "EXPIRED", "SUCCEEDED", "FAILED", "CANCELLED",
	} {
		if !TurnAttemptStateValid(state) {
			t.Fatalf("approved TurnAttempt state %s was rejected", state)
		}
	}
	for _, state := range []string{"", "RUNNING", "ACTIVE", "UNKNOWN"} {
		if TurnAttemptStateValid(state) {
			t.Fatalf("unsupported TurnAttempt state %s was accepted", state)
		}
	}
	if TurnAttemptStateFinished("QUEUED") || TurnAttemptStateFinished("CLAIMED") ||
		!TurnAttemptStateFinished("WAITING_OWNER") || !TurnAttemptStateFinished("WAITING_EXTERNAL") {
		t.Fatal("TurnAttempt open/finished partition is inconsistent")
	}
}
