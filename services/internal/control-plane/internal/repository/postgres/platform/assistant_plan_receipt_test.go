package platform

import "testing"

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
