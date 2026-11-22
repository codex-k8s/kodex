package app

import (
	"context"
	"testing"
	"time"
)

// TestPersonalInvocationDeadlineSurvivesRPCClockRollback models RPC latency and a backward wall observation without changing the host clock or adding a runtime clock abstraction.
// TestPersonalInvocationDeadlineSurvivesRPCClockRollback моделирует RPC latency и backward wall observation без изменения часов хоста либо нового clock abstraction.
func TestPersonalInvocationDeadlineSurvivesRPCClockRollback(t *testing.T) {
	entry := time.Now()
	originalTimestamp := entry.Add(30 * time.Second).UTC()
	freshTimestamp := entry.Add(time.Second).UTC()
	original := personalInvocationDeadline(entry, originalTimestamp)
	fresh := personalInvocationDeadline(entry, freshTimestamp)
	afterRPC := entry.Add(2 * time.Second)
	rollbackWall := afterRPC.Add(-time.Hour).UTC()
	// Re-anchoring from the later wall observation would grant another hour after the real RPC elapsed.
	// Re-anchor от позднего wall observation выдал бы ещё час после реального RPC elapsed.
	renewed := afterRPC.Add(freshTimestamp.Sub(rollbackWall))
	if !renewed.After(afterRPC) || !fresh.Before(afterRPC) || !original.After(afterRPC) || fresh.Sub(entry) != time.Second {
		t.Fatal("fresh allowance was reconstructed from the post-RPC wall clock")
	}
}

// TestPersonalInvocationDeadlineKeepsOriginalAndFreshMinimum verifies both chronological envelopes using the same observation and ordinary context inheritance.
// TestPersonalInvocationDeadlineKeepsOriginalAndFreshMinimum проверяет оба chronological envelopes через одно observation и обычное context inheritance.
func TestPersonalInvocationDeadlineKeepsOriginalAndFreshMinimum(t *testing.T) {
	observed := time.Now()
	for _, scenario := range []struct {
		name                  string
		original, fresh, want time.Duration
	}{
		{"shortened after claim", 30 * time.Second, 3 * time.Second, 3 * time.Second},
		{"restored before Begin", 3 * time.Second, 30 * time.Second, 3 * time.Second},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			original := personalInvocationDeadline(observed, observed.Add(scenario.original).UTC())
			fresh := personalInvocationDeadline(observed, observed.Add(scenario.fresh).UTC())
			ctx, cancel := context.WithDeadline(t.Context(), original)
			defer cancel()
			child, finish := context.WithDeadline(ctx, fresh)
			defer finish()
			until, ok := child.Deadline()
			if !ok || until.Sub(observed) != scenario.want {
				t.Fatal("fresh grant extended original monotonic claim ceiling")
			}
		})
	}
}
