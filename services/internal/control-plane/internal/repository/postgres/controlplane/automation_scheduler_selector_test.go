package controlplane

import (
	"strings"
	"testing"
)

func TestAutomationSelectorsApplyEligibilityBeforeLimit(t *testing.T) {
	assertBefore := func(name, query, predicate, limit string) {
		t.Helper()
		predicateIndex := strings.Index(query, predicate)
		limitIndex := strings.LastIndex(query, limit)
		if predicateIndex < 0 || limitIndex < 0 || predicateIndex >= limitIndex {
			t.Fatalf("%s applies LIMIT before eligibility predicate %q", name, predicate)
		}
	}
	assertBefore("due FORBID", sqlScheduleDue, "open_occurrence.state = ANY", "LIMIT @limit")
	assertBefore("due historical open run", sqlScheduleDue, "open_run.state = ANY", "LIMIT @limit")
	assertBefore("occurrence active schedule", sqlScheduleOccurrenceNext,
		"schedule.state = 'ACTIVE'", "LIMIT 1")
	assertBefore("occurrence open graph", sqlScheduleOccurrenceNext,
		"open_run.state = ANY", "LIMIT 1")
	assertBefore("occurrence FIFO predecessor", sqlScheduleOccurrenceNext,
		"predecessor.state = 'QUEUED'", "LIMIT 1")
}
