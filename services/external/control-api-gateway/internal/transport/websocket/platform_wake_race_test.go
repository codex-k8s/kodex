package websockettransport

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type platformWakeRaceRecorder struct {
	assistantSnapshotSizeRecorder
	current      int64
	organization string
	cursorError  error
	cursorReads  int
	beforeInvoke func(context.Context, any) error
	runSnapshot  *cp.GetRunGraphResponse
}

func (c *platformWakeRaceRecorder) Invoke(ctx context.Context, method string, request, response any, options ...grpc.CallOption) error {
	if c.beforeInvoke != nil {
		if err := c.beforeInvoke(ctx, response); err != nil {
			return err
		}
	}
	if out, ok := response.(*cp.GetPlatformEventCursorResponse); ok {
		c.cursorReads++
		if c.cursorError != nil {
			return c.cursorError
		}
		out.OrganizationRef, out.CurrentSequence = c.organization, c.current
		return nil
	}
	if out, ok := response.(*cp.GetRunGraphResponse); ok && c.runSnapshot != nil {
		proto.Merge(out, c.runSnapshot)
		return nil
	}
	return c.assistantSnapshotSizeRecorder.Invoke(ctx, method, request, response, options...)
}

func platformWakeRaceFixture(t *testing.T, current int64) (*sessionMultiplexer, *platformWakeRaceRecorder) {
	t.Helper()
	c := &platformWakeRaceRecorder{current: current, organization: "org_fixture01"}
	c.frames = make(chan outboundFrame, 32)
	server := &Server{query: cp.NewPlatformQueryServiceClient(c), assistant: cp.NewSystemAssistantServiceClient(c)}
	return &sessionMultiplexer{server: server, ctx: t.Context(), platformRequestRef: "request_fixture01",
		organizationRef: c.organization, projectRef: "prj_fixture01", platformCursor: 7, platformAvailable: true,
		platformSignals: make(chan platformSignal, 128), outbound: c.frames, overflow: make(chan struct{}, 1),
		localize: func(code string) string { return code }, runs: map[string]*runSubscription{}}, c
}

func platformWakeRaceSignal(t *testing.T, sequence int64) platformSignal {
	t.Helper()
	// Очередь получает только сигнал, прошедший настоящий закрытый decoder.
	envelope := platformBusEnvelope{EventID: "d561fbb0-02c0-4be7-af7c-5998925632bd",
		EventName: "SYSTEM_ASSISTANT_CHANGED", EventVersion: 1, OrganizationRef: "org_fixture01",
		ProjectRef: "prj_fixture01", AggregateRef: "agt_fixture01", AggregateVersion: 1,
		Sequence: sequence, CorrelationRef: "corr_fixture01", OccurredAt: time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)}
	envelope.Data.Kind, envelope.Data.SafeSummary = "SYSTEM_ASSISTANT", "fixture"
	payload, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal("marshal platform wake fixture failed")
	}
	signal, valid := decodePlatformSignal(payload, "org_fixture01")
	if !valid {
		t.Fatal("canonical platform wake fixture was rejected")
	}
	return signal
}

func platformWakeRaceFrames(c *platformWakeRaceRecorder) []any {
	var values []any
	for len(c.frames) > 0 {
		values = append(values, (<-c.frames).value)
	}
	return values
}

func assertPlatformWakeRaceDelta(t *testing.T, value any, sequence int64) {
	t.Helper()
	delta, ok := value.(generated.PlatformSnapshotEnvelope)
	if !ok || delta.Cursor != sequence || delta.Mode != generated.PlatformSnapshotModeDelta ||
		delta.ProjectRef == nil || *delta.ProjectRef != "prj_fixture01" || delta.Kind != "SYSTEM_ASSISTANT" ||
		delta.EventName == nil || *delta.EventName != "SYSTEM_ASSISTANT_CHANGED" || delta.Snapshot.Conversations == nil {
		t.Fatal("queued owner wake did not produce its exact scoped delta")
	}
}

func TestPlatformHeartbeatProcessesAlreadyQueuedOwnerWakesBeforeGapCheck(t *testing.T) {
	m, c := platformWakeRaceFixture(t, 9)
	m.platformSignals <- platformWakeRaceSignal(t, 8)
	m.platformSignals <- platformWakeRaceSignal(t, 9)
	// Детерминированно выбираем heartbeat, пока оба wake уже готовы в select.
	if !m.heartbeat(time.Unix(1, 0)) {
		t.Fatal("heartbeat failed")
	}
	frames := platformWakeRaceFrames(c)
	for _, frame := range frames {
		if resync, ok := frame.(generated.PlatformResyncEnvelope); ok {
			t.Fatalf("queued contiguous owner wakes caused false resync: cursor=%d queued=%d reason=%s", resync.Cursor, len(m.platformSignals), resync.Reason)
		}
	}
	if len(frames) != 3 || len(m.platformSignals) != 0 || c.cursorReads != 1 || m.platformCursor != 9 {
		t.Fatal("heartbeat did not consume only the bounded queued prefix before one owner cursor read")
	}
	assertPlatformWakeRaceDelta(t, frames[0], 8)
	assertPlatformWakeRaceDelta(t, frames[1], 9)
	if heartbeat, ok := frames[2].(generated.StreamHeartbeatEnvelope); !ok || heartbeat.Cursor != 9 {
		t.Fatal("heartbeat lost the authoritative cursor after queued deltas")
	}
}

func TestPlatformHeartbeatWakeRaceControls(t *testing.T) {
	t.Run("wake-selected-before-heartbeat", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 9)
		for _, sequence := range []int64{8, 9} {
			m.platformSignals <- platformWakeRaceSignal(t, sequence)
		}
		for len(m.platformSignals) > 0 {
			if !m.applyPlatformSignal(<-m.platformSignals) {
				t.Fatal("decoded queued wake failed")
			}
		}
		if !m.heartbeat(time.Unix(1, 0)) {
			t.Fatal("heartbeat failed")
		}
		frames := platformWakeRaceFrames(c)
		if len(frames) != 3 || c.cursorReads != 1 || len(c.requests) != 2 || m.platformCursor != 9 {
			t.Fatal("wake-first ordering repeated reads or lost the exact cursor")
		}
		assertPlatformWakeRaceDelta(t, frames[0], 8)
		assertPlatformWakeRaceDelta(t, frames[1], 9)
		if _, ok := frames[2].(generated.StreamHeartbeatEnvelope); !ok {
			t.Fatal("wake-first heartbeat unexpectedly required resync")
		}
	})
	t.Run("undelivered-or-lost-wake-remains-resync", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 9)
		if !m.heartbeat(time.Unix(1, 0)) {
			t.Fatal("heartbeat failed")
		}
		frames := platformWakeRaceFrames(c)
		if len(frames) != 2 {
			t.Fatal("missing owner wake silently delayed authoritative recovery")
		}
		resync, ok := frames[0].(generated.PlatformResyncEnvelope)
		if !ok || resync.Cursor != 9 || resync.Reason != "AUTHORITATIVE_READ_REQUIRED" || c.cursorReads != 1 {
			t.Fatal("real gap did not preserve its closed resync reason")
		}
	})
	t.Run("noncontiguous-queued-wake-remains-resync", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 9)
		if !m.applyPlatformSignal(platformWakeRaceSignal(t, 9)) {
			t.Fatal("gap recovery failed")
		}
		frames := platformWakeRaceFrames(c)
		if len(frames) != 1 {
			t.Fatal("noncontiguous signal invented an incremental delta")
		}
		if resync, ok := frames[0].(generated.PlatformResyncEnvelope); !ok || resync.Cursor != 9 {
			t.Fatal("noncontiguous signal did not require authoritative recovery")
		}
	})
	for _, condition := range []string{"foreign-owner", "cursor-rollback", "owner-read-denied"} {
		t.Run(condition, func(t *testing.T) {
			m, c := platformWakeRaceFixture(t, 7)
			switch condition {
			case "foreign-owner":
				c.organization = "org_foreign01"
			case "cursor-rollback":
				c.current = 6
			case "owner-read-denied":
				c.cursorError = status.Error(codes.PermissionDenied, "synthetic owner denial")
			}
			if !m.heartbeat(time.Unix(1, 0)) {
				t.Fatal("closed stream problem could not be queued")
			}
			frames := platformWakeRaceFrames(c)
			if len(frames) != 1 || m.platformAvailable || m.platformCursor != 7 || len(c.requests) != 0 || c.cursorReads != 1 {
				t.Fatal("invalid owner cursor advanced state or projected owner data")
			}
			if problem, ok := frames[0].(generated.StreamProblemEnvelope); !ok || problem.Code != "PLATFORM_UNAVAILABLE" || problem.Cursor != 7 {
				t.Fatal("invalid owner cursor lost its closed failure")
			}
		})
	}
}

func TestPlatformHeartbeatWakeDrainBudget(t *testing.T) {
	t.Run("four-wakes-only-then-existing-gap-recovery", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 12)
		for sequence := int64(8); sequence <= 12; sequence++ {
			m.platformSignals <- platformWakeRaceSignal(t, sequence)
		}
		if !m.heartbeat(time.Unix(1, 0)) {
			t.Fatal("bounded heartbeat failed")
		}
		frames := platformWakeRaceFrames(c)
		if len(frames) != 6 || len(m.platformSignals) != 1 || len(c.requests) != 4 || c.cursorReads != 1 {
			t.Fatal("drain exceeded four wakes or waited for the remaining queue")
		}
		for index := 0; index < 4; index++ {
			assertPlatformWakeRaceDelta(t, frames[index], int64(8+index))
		}
		if resync, ok := frames[4].(generated.PlatformResyncEnvelope); !ok || resync.Cursor != 12 || resync.Reason != "AUTHORITATIVE_READ_REQUIRED" {
			t.Fatal("remaining authoritative gap was hidden by drain budget exhaustion")
		}
		if !m.applyPlatformSignal(<-m.platformSignals) || len(c.frames) != 0 || len(c.requests) != 4 {
			t.Fatal("remaining wake repeated a snapshot after authoritative gap recovery")
		}
	})
	t.Run("new-arrival-does-not-extend-initial-prefix", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 10)
		m.platformSignals <- platformWakeRaceSignal(t, 8)
		injected := false
		c.beforeInvoke = func(_ context.Context, response any) error {
			if _, ok := response.(*cp.GetSystemAssistantResponse); ok && !injected {
				injected = true
				m.platformSignals <- platformWakeRaceSignal(t, 9)
				m.platformSignals <- platformWakeRaceSignal(t, 10)
			}
			return nil
		}
		available, ok := m.drainHeartbeatWakes()
		if !available || !ok || m.platformCursor != 8 || len(m.platformSignals) != 2 || len(c.requests) != 1 || c.cursorReads != 0 {
			t.Fatal("new arrivals extended the captured drain prefix")
		}
		frames := platformWakeRaceFrames(c)
		if len(frames) != 1 {
			t.Fatal("drain fabricated a heartbeat or cursor read")
		}
		assertPlatformWakeRaceDelta(t, frames[0], 8)
	})
}

func TestPlatformHeartbeatWakeDrainScopedControls(t *testing.T) {
	t.Run("duplicate-consumes-budget-without-owner-projection", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 8)
		m.platformSignals <- platformWakeRaceSignal(t, 7)
		m.platformSignals <- platformWakeRaceSignal(t, 8)
		if !m.heartbeat(time.Unix(1, 0)) {
			t.Fatal("duplicate wake heartbeat failed")
		}
		frames := platformWakeRaceFrames(c)
		if len(frames) != 2 || len(c.requests) != 1 || c.cursorReads != 1 {
			t.Fatal("duplicate wake repeated owner projection")
		}
		assertPlatformWakeRaceDelta(t, frames[0], 8)
	})
	t.Run("foreign-project-produces-only-owner-cursor", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 8)
		signal := platformWakeRaceSignal(t, 8)
		signal.ProjectRef = "prj_foreign01"
		m.platformSignals <- signal
		if !m.heartbeat(time.Unix(1, 0)) {
			t.Fatal("foreign project cursor heartbeat failed")
		}
		frames := platformWakeRaceFrames(c)
		cursor, ok := frames[0].(generated.PlatformCursorEnvelope)
		if !ok || cursor.Cursor != 8 || cursor.ProjectRef == nil || *cursor.ProjectRef != signal.ProjectRef ||
			len(frames) != 2 || len(c.requests) != 0 || c.cursorReads != 1 {
			t.Fatal("foreign project wake projected content instead of exact owner cursor")
		}
	})
	t.Run("denied-projection-retains-existing-cursor-only-path", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 8)
		c.failure = status.Error(codes.PermissionDenied, "synthetic projection denial")
		m.platformSignals <- platformWakeRaceSignal(t, 8)
		if !m.heartbeat(time.Unix(1, 0)) {
			t.Fatal("denied projection heartbeat failed")
		}
		frames := platformWakeRaceFrames(c)
		if len(frames) != 2 || len(c.requests) != 1 || c.cursorReads != 1 {
			t.Fatal("denied projection was retried or bypassed")
		}
		if cursor, ok := frames[0].(generated.PlatformCursorEnvelope); !ok || cursor.Cursor != 8 {
			t.Fatal("denied projection did not preserve the existing cursor-only result")
		}
	})
	t.Run("noncontiguous-prefix-retains-exact-resync", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 9)
		m.platformSignals <- platformWakeRaceSignal(t, 9)
		if !m.heartbeat(time.Unix(1, 0)) {
			t.Fatal("queued gap heartbeat failed")
		}
		frames := platformWakeRaceFrames(c)
		if len(frames) != 2 || len(c.requests) != 0 {
			t.Fatal("queued gap invented owner delta content")
		}
		if resync, ok := frames[0].(generated.PlatformResyncEnvelope); !ok || resync.Cursor != 9 || resync.Reason != "AUTHORITATIVE_READ_REQUIRED" {
			t.Fatal("queued gap no longer required authoritative recovery")
		}
	})
}

func TestPlatformHeartbeatWakeDrainFailureAndContext(t *testing.T) {
	for _, condition := range []string{"projection-unavailable", "projection-deadline"} {
		t.Run(condition, func(t *testing.T) {
			m, c := platformWakeRaceFixture(t, 9)
			c.beforeInvoke = func(ctx context.Context, response any) error {
				deadline, bounded := ctx.Deadline()
				if !bounded || time.Until(deadline) > heartbeatWakeTimeout || ctx == m.ctx {
					t.Fatal("drain RPC lacks its separate two-second aggregate parent deadline")
				}
				if _, ok := response.(*cp.ListAssistantConversationsResponse); ok {
					if condition == "projection-deadline" {
						return status.Error(codes.DeadlineExceeded, "synthetic projection deadline")
					}
					return status.Error(codes.Unavailable, "synthetic projection unavailable")
				}
				return nil
			}
			m.platformSignals <- platformWakeRaceSignal(t, 8)
			m.platformSignals <- platformWakeRaceSignal(t, 9)
			if !m.heartbeat(time.Unix(1, 0)) {
				t.Fatal("existing closed stream failure could not be delivered")
			}
			frames := platformWakeRaceFrames(c)
			if len(frames) != 1 || m.platformCursor != 7 || m.platformAvailable || len(m.platformSignals) != 1 || c.cursorReads != 0 || m.ctx.Err() != nil {
				t.Fatal("failed projection advanced cursor, continued drain or changed the session context")
			}
			if problem, ok := frames[0].(generated.StreamProblemEnvelope); !ok || problem.Code != "PLATFORM_UNAVAILABLE" || problem.Cursor != 7 {
				t.Fatal("projection failure lost the existing closed platform result")
			}
		})
	}
	t.Run("drain-context-joined-original-context-preserved", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 8)
		original := m.ctx
		var drain context.Context
		c.beforeInvoke = func(ctx context.Context, _ any) error {
			if drain == nil {
				drain = ctx
			}
			return nil
		}
		m.platformSignals <- platformWakeRaceSignal(t, 8)
		available, ok := m.drainHeartbeatWakes()
		if !available || !ok || drain == nil || drain.Err() != context.Canceled || m.ctx != original || original.Err() != nil {
			t.Fatal("drain did not cancel its own context or replaced the original session context")
		}
	})
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel-before-drain", true: "cancel-during-projection"}[during], func(t *testing.T) {
			m, c := platformWakeRaceFixture(t, 8)
			ctx, cancel := context.WithCancel(m.ctx)
			defer cancel()
			m.ctx = ctx
			if during {
				c.beforeInvoke = func(_ context.Context, _ any) error {
					cancel()
					return status.Error(codes.Canceled, "synthetic cancel")
				}
			} else {
				cancel()
			}
			m.platformSignals <- platformWakeRaceSignal(t, 8)
			if m.heartbeat(time.Unix(1, 0)) || c.cursorReads != 0 || m.platformCursor != 7 {
				t.Fatal("cancelled drain continued owner cursor reads or heartbeat")
			}
		})
	}
	t.Run("existing-overflow-closes-before-owner-read", func(t *testing.T) {
		m, c := platformWakeRaceFixture(t, 8)
		m.platformSignals <- platformWakeRaceSignal(t, 8)
		m.signalOverflow()
		written := make(chan outboundFrame, 1)
		go func() {
			frame := <-c.frames
			close(frame.written)
			written <- frame
		}()
		if m.heartbeat(time.Unix(1, 0)) || c.cursorReads != 0 || len(c.requests) != 0 || len(m.platformSignals) != 1 {
			t.Fatal("overflow did not close before consuming queued wakes")
		}
		frame := <-written
		if problem, ok := frame.value.(generated.SessionProblemEnvelope); !ok || problem.Code != "BACKPRESSURE_EXCEEDED" || frame.closeReason != "BACKPRESSURE_EXCEEDED" {
			t.Fatal("overflow lost the existing typed close")
		}
	})
}

func TestPlatformHeartbeatWakeDrainRunRefreshDeadline(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(map[bool]string{false: "gap-refresh-has-separate-owner-budget", true: "run-deadline-remains-closed"}[denied], func(t *testing.T) {
			m, c := platformWakeRaceFixture(t, 9)
			c.runSnapshot = completeRunSnapshotFixture()
			root := c.runSnapshot.Run.Ref
			subscription := &runSubscription{rootRef: root, cursor: 7, requestRef: "request_run001", available: true}
			m.runs[root] = subscription
			reads := 0
			c.beforeInvoke = func(ctx context.Context, response any) error {
				deadline, bounded := ctx.Deadline()
				if _, ok := response.(*cp.GetRunGraphResponse); ok {
					if !bounded || time.Until(deadline) <= heartbeatWakeTimeout || time.Until(deadline) > runSnapshotReadTimeout {
						t.Fatal("run refresh lost its separate bounded owner-read budget")
					}
					reads++
					if denied {
						return status.Error(codes.DeadlineExceeded, "synthetic run deadline")
					}
				} else if !bounded || time.Until(deadline) > heartbeatWakeTimeout {
					t.Fatal("gap cursor read escaped the catalog drain budget")
				}
				return nil
			}
			m.platformSignals <- platformWakeRaceSignal(t, 9)
			available, ok := m.drainHeartbeatWakes()
			if !available || !ok || reads != 1 || c.cursorReads != 1 || m.platformCursor != 9 || m.ctx.Err() != nil {
				t.Fatal("bounded gap recovery changed the owner cursor or session lifetime")
			}
			frames := platformWakeRaceFrames(c)
			if len(frames) != 2 {
				t.Fatal("gap recovery lost its paired platform/run result")
			}
			if resync, ok := frames[0].(generated.PlatformResyncEnvelope); !ok || resync.Cursor != 9 || resync.Reason != "AUTHORITATIVE_READ_REQUIRED" {
				t.Fatal("gap recovery lost the authoritative resync")
			}
			if denied {
				if problem, ok := frames[1].(generated.StreamProblemEnvelope); !ok || problem.Code != "RUN_UNAVAILABLE" || subscription.available {
					t.Fatal("run deadline was hidden or treated as a completed snapshot")
				}
			} else if snapshot, ok := frames[1].(generated.RunSnapshotEnvelope); !ok || snapshot.StreamRef != root || len(snapshot.Runs) != 2 {
				t.Fatal("bounded run refresh lost the complete authoritative lineage")
			}
		})
	}
}
