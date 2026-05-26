package ops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

func TestRecordDiagnosticDoesNotEvictInFlightReservation(t *testing.T) {
	t.Parallel()

	repository := NewRepository(Config{Capacity: 1, Retention: time.Hour})
	now := time.Now().UTC()
	admitted := value.OpsFeedEntry{
		EntryID:           uuid.MustParse("11111111-2222-4111-8111-111111111111"),
		EventID:           uuid.MustParse("11111111-2222-4111-8111-222222222222"),
		HookEventName:     hookenum.HookEventPreToolUse,
		EventKind:         string(hookenum.HookEventPreToolUse),
		Status:            hookenum.OpsFeedStatusAccepted,
		PayloadDigest:     "sha256:" + strings.Repeat("a", 64),
		PayloadSizeBucket: "le_1KiB",
		LatencyBucket:     "le_10ms",
		ObservedAt:        now,
		ExpiresAt:         now.Add(time.Hour),
	}
	reservation, err := repository.Admit(context.Background(), admitted)
	if err != nil {
		t.Fatalf("Admit(): %v", err)
	}

	rejected := value.OpsFeedEntry{
		EntryID:           uuid.MustParse("11111111-2222-4111-8111-333333333333"),
		EventID:           uuid.MustParse("11111111-2222-4111-8111-444444444444"),
		HookEventName:     hookenum.HookEventPreToolUse,
		EventKind:         string(hookenum.HookEventPreToolUse),
		Status:            hookenum.OpsFeedStatusRejected,
		RejectReason:      value.OpsDiagnosticRejected,
		PayloadDigest:     "sha256:" + strings.Repeat("b", 64),
		PayloadSizeBucket: "le_1KiB",
		LatencyBucket:     "le_10ms",
		ObservedAt:        now,
		ExpiresAt:         now.Add(time.Hour),
	}
	if err := repository.RecordDiagnostic(context.Background(), rejected); err != nil {
		t.Fatalf("RecordDiagnostic(): %v", err)
	}

	completed := admitted
	completed.RouteResults = []value.OpsRouteResult{
		{
			OwnerTarget:    hookenum.DownstreamOwnerOperationsFeed,
			DeliveryMode:   hookenum.DeliveryModeAsync,
			RouteResult:    hookenum.RouteDeliveryStatusDelivered,
			DiagnosticCode: value.RouteDiagnosticDelivered,
			LatencyBucket:  "le_10ms",
		},
	}
	if err := repository.Complete(context.Background(), reservation, completed); err != nil {
		t.Fatalf("Complete(): %v", err)
	}

	entries := repository.Recent(context.Background(), 10)
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want reserved entry only", len(entries))
	}
	if entries[0].EntryID != reservation.EntryID {
		t.Fatalf("entry id = %s, want reservation %s", entries[0].EntryID, reservation.EntryID)
	}
	snapshot := repository.Snapshot(context.Background())
	if snapshot.Accepted != 1 || snapshot.Dropped != 1 {
		t.Fatalf("snapshot = %+v, want accepted completion and dropped overflow diagnostic", snapshot)
	}
}
