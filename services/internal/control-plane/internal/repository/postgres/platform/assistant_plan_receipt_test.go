package platform

import "testing"

// TestAssistantPlanReceiptMatchesCurrentTerminalState checks APPLIED/REJECTED receipts and
// STALE→CONFLICT correspondence while rejecting mismatched or revalidated VALID/INVALID state
// against an old conflict. It tests the pure state matcher without loading a receipt or
// authorizing a command.
// TestAssistantPlanReceiptMatchesCurrentTerminalState проверяет APPLIED/REJECTED receipts и
// соответствие STALE→CONFLICT, отклоняя несовпадение и revalidated VALID/INVALID с прежним
// conflict. Проверяет чистый state matcher без чтения receipt и авторизации команды.
func TestAssistantPlanReceiptMatchesCurrentTerminalState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		state   string
		outcome string
		want    bool
	}{
		{name: "applied", state: "APPLIED", outcome: "APPLIED", want: true},
		{name: "rejected", state: "REJECTED", outcome: "REJECTED", want: true},
		{name: "conflict", state: "STALE", outcome: "CONFLICT", want: true},
		{name: "revalidated invalid plan ignores old conflict", state: "INVALID", outcome: "CONFLICT"},
		{name: "revalidated valid plan ignores old conflict", state: "VALID", outcome: "CONFLICT"},
		{name: "terminal mismatch", state: "APPLIED", outcome: "CONFLICT"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := assistantPlanReceiptMatchesState(test.state, test.outcome); got != test.want {
				t.Fatalf("assistantPlanReceiptMatchesState(%q, %q) = %v, want %v", test.state, test.outcome, got, test.want)
			}
		})
	}
}

// TestAssistantPlanStateHasReceipt checks only APPLIED/REJECTED/STALE require terminal receipt
// handling and DRAFT/VALID/INVALID do not. The predicate test does not prove a durable receipt
// exists or permit replay.
// TestAssistantPlanStateHasReceipt проверяет terminal receipt handling лишь для
// APPLIED/REJECTED/STALE и его отсутствие для DRAFT/VALID/INVALID. Тест предиката не доказывает
// существование durable receipt и не разрешает replay.
func TestAssistantPlanStateHasReceipt(t *testing.T) {
	t.Parallel()

	for _, state := range []string{"APPLIED", "REJECTED", "STALE"} {
		if !assistantPlanStateHasReceipt(state) {
			t.Fatalf("assistantPlanStateHasReceipt(%q) = false, want true", state)
		}
	}
	for _, state := range []string{"DRAFT", "VALID", "INVALID"} {
		if assistantPlanStateHasReceipt(state) {
			t.Fatalf("assistantPlanStateHasReceipt(%q) = true, want false", state)
		}
	}
}
