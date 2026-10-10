package websockettransport

import (
	"context"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
)

func TestHeartbeatMixedCatalogReadsHaveIndependentDeadlines(t *testing.T) {
	m, c := heartbeatRunBatchFixture(t)
	m.server.assistant = cp.NewSystemAssistantServiceClient(c)
	var reads []context.Context
	var deadlines []time.Time
	c.beforeInvoke = func(ctx context.Context, response any) error {
		switch response.(type) {
		case *cp.GetSystemAssistantResponse, *cp.ListRunsResponse:
			deadline, bounded := ctx.Deadline()
			if !bounded || time.Until(deadline) <= 1800*time.Millisecond || time.Until(deadline) > heartbeatWakeTimeout {
				t.Fatal("fresh owner catalog read did not receive its bounded two-second budget")
			}
			reads, deadlines = append(reads, ctx), append(deadlines, deadline)
		}
		return nil
	}
	m.platformSignals <- platformWakeRaceSignal(t, 8)
	for _, sequence := range []int64{9, 10} {
		signal := platformWakeRaceSignal(t, sequence)
		signal.Kind, signal.EventName = "RUN", "RUN_CHANGED"
		m.platformSignals <- signal
	}
	available, ok := m.drainHeartbeatWakes()
	if !available || !ok || !m.platformAvailable || m.platformCursor != 10 || c.catalogReads != 1 || c.runReads != 1 {
		t.Fatal("mixed catalog batch lost readiness, exact cursor or continuous RUN reuse")
	}
	if len(reads) != 2 || reads[0] == reads[1] || !deadlines[1].After(deadlines[0]) {
		t.Fatal("later fresh owner catalog inherited the preceding catalog deadline")
	}
	for _, ctx := range reads {
		if ctx.Err() != context.Canceled {
			t.Fatal("completed owner catalog context was not joined")
		}
	}
	if m.ctx.Err() != nil {
		t.Fatal("catalog cleanup canceled the session context")
	}
	deltas := 0
	for _, frame := range platformWakeRaceFrames(&c.platformWakeRaceRecorder) {
		if _, failed := frame.(generated.StreamProblemEnvelope); failed {
			t.Fatal("mixed catalog batch emitted a dependency failure")
		}
		if delta, valid := frame.(generated.PlatformSnapshotEnvelope); valid {
			if delta.Cursor != int64(8+deltas) {
				t.Fatal("mixed catalog batch changed authoritative delta order")
			}
			deltas++
		}
	}
	if deltas != 3 {
		t.Fatal("mixed catalog batch omitted an owner delta")
	}
}

func TestHeartbeatCatalogReadsCannotExtendShortSessionParent(t *testing.T) {
	m, c := heartbeatRunBatchFixture(t)
	m.server.assistant = cp.NewSystemAssistantServiceClient(c)
	parent, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	m.ctx = parent
	parentDeadline, _ := parent.Deadline()
	var reads []context.Context
	c.beforeInvoke = func(ctx context.Context, response any) error {
		switch response.(type) {
		case *cp.GetSystemAssistantResponse, *cp.ListRunsResponse, *cp.GetRunGraphResponse:
			deadline, bounded := ctx.Deadline()
			if !bounded || !deadline.Equal(parentDeadline) {
				t.Fatal("catalog or subscribed-run read extended the shorter session deadline")
			}
			reads = append(reads, ctx)
		}
		return nil
	}
	m.platformSignals <- platformWakeRaceSignal(t, 8)
	signal := platformWakeRaceSignal(t, 9)
	signal.Kind, signal.EventName = "RUN", "RUN_CHANGED"
	m.platformSignals <- signal
	if available, ok := m.drainHeartbeatWakes(); !available || !ok || len(reads) != 3 || m.platformCursor != 9 {
		t.Fatal("short parent lost the bounded mixed catalog or exact run refresh")
	}
	for _, ctx := range reads {
		if ctx.Err() != context.Canceled {
			t.Fatal("short-parent read context was not released")
		}
	}
	if parent.Err() != nil {
		t.Fatal("completed reads canceled their session parent")
	}
}

func TestHeartbeatMixedCatalogPrefixHasFixedAggregateBound(t *testing.T) {
	if maximumHeartbeatWakes != 4 || heartbeatWakeTimeout != 2*time.Second || heartbeatWakeDrainTimeout != 8*time.Second {
		t.Fatal("catalog prefix changed its approved per-read or aggregate bound")
	}
	m, c := heartbeatRunBatchFixture(t)
	m.server.assistant = cp.NewSystemAssistantServiceClient(c)
	var reads []context.Context
	started := time.Now()
	c.beforeInvoke = func(ctx context.Context, response any) error {
		switch response.(type) {
		case *cp.GetSystemAssistantResponse, *cp.ListRunsResponse:
			deadline, bounded := ctx.Deadline()
			if !bounded || deadline.After(started.Add(heartbeatWakeDrainTimeout)) {
				t.Fatal("catalog read exceeded the fixed aggregate deadline")
			}
			reads = append(reads, ctx)
		}
		return nil
	}
	for index := range maximumHeartbeatWakes + 1 {
		signal := platformWakeRaceSignal(t, int64(8+index))
		if index%2 == 1 {
			signal.Kind, signal.EventName = "RUN", "RUN_CHANGED"
		}
		m.platformSignals <- signal
	}
	if available, ok := m.drainHeartbeatWakes(); !available || !ok || len(reads) != maximumHeartbeatWakes || len(m.platformSignals) != 1 || m.platformCursor != 11 {
		t.Fatal("mixed catalog drain extended its initial bounded prefix")
	}
	for index, ctx := range reads {
		if ctx.Err() != context.Canceled || (index > 0 && ctx == reads[index-1]) {
			t.Fatal("fresh mixed catalog reused or leaked the preceding read context")
		}
	}
}
