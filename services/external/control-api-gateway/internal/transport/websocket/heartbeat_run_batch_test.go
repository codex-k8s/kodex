package websockettransport

import (
	"context"
	"sync"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type heartbeatRunBatchRecorder struct {
	platformWakeRaceRecorder
	mu                                 sync.Mutex
	runReads, eventReads, catalogReads int
	denyCatalog                        bool
	invalidSnapshot                    bool
}

func (c *heartbeatRunBatchRecorder) Invoke(ctx context.Context, method string, request, response any, options ...grpc.CallOption) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	switch out := response.(type) {
	case *cp.ListRunsResponse:
		c.catalogReads++
		if c.denyCatalog {
			return status.Error(codes.PermissionDenied, "synthetic catalog denied")
		}
		out.Page = &cp.PageInfo{}
	case *cp.ListOwnerGatesResponse:
		out.Page = &cp.PageInfo{}
	case *cp.GetOverviewResponse:
		out.Overview = &cp.Overview{}
	case *cp.ListRunEventsResponse:
		c.eventReads++
		out.CurrentSequence, out.Complete = 7, true
	case *cp.GetRunGraphResponse:
		c.runReads++
		if c.beforeInvoke != nil {
			if err := c.beforeInvoke(ctx, response); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return status.FromContextError(err).Err()
		}
		input := request.(*cp.GetRunGraphRequest)
		value := completeRunSnapshotFixture()
		value.Run.Ref, value.Run.RootRunRef, value.Graph.RunRef = input.RunRef, input.RunRef, input.RunRef
		value.Runs[0] = value.Run
		value.Runs[1].RootRunRef, value.Runs[1].ParentRunRef = input.RunRef, input.RunRef
		if c.invalidSnapshot {
			value.Runs = nil
		}
		proto.Merge(out, value)
		return nil
	default:
		return c.platformWakeRaceRecorder.Invoke(ctx, method, request, response, options...)
	}
	if c.beforeInvoke != nil {
		return c.beforeInvoke(ctx, response)
	}
	return nil
}

func heartbeatRunBatchFixture(t *testing.T) (*sessionMultiplexer, *heartbeatRunBatchRecorder) {
	t.Helper()
	m, base := platformWakeRaceFixture(t, 11)
	c := &heartbeatRunBatchRecorder{platformWakeRaceRecorder: *base}
	m.server.query = cp.NewPlatformQueryServiceClient(c)
	m.runs["run_root0001"] = &runSubscription{rootRef: "run_root0001", requestRef: "request_run001", cursor: 7, available: true}
	return m, c
}

func queueHeartbeatRunWakes(t *testing.T, m *sessionMultiplexer, count int) {
	t.Helper()
	for i := range count {
		signal := platformWakeRaceSignal(t, int64(8+i))
		signal.Kind, signal.EventName = "RUN", "RUN_CHANGED"
		m.platformSignals <- signal
	}
}

func TestHeartbeatCoalescesRunRefreshAndSeparatesBudgets(t *testing.T) {
	m, c := heartbeatRunBatchFixture(t)
	c.beforeInvoke = func(ctx context.Context, response any) error {
		switch response.(type) {
		case *cp.GetRunGraphResponse, *cp.ListRunsResponse, *cp.ListOwnerGatesResponse, *cp.GetOverviewResponse:
		default:
			return nil
		}
		deadline, bounded := ctx.Deadline()
		if !bounded {
			t.Fatal("owner read lost its bounded context")
		}
		remaining := time.Until(deadline)
		if _, run := response.(*cp.GetRunGraphResponse); run {
			if remaining <= heartbeatWakeTimeout || remaining > runSnapshotReadTimeout {
				t.Fatal("full owner read inherited exhausted catalog budget")
			}
		} else if remaining > heartbeatWakeTimeout {
			t.Fatal("catalog read escaped its original budget")
		}
		return nil
	}
	queueHeartbeatRunWakes(t, m, 4)
	if !m.heartbeat(time.Unix(1, 0)) || c.runReads != 1 || c.catalogReads != 1 || m.platformCursor != 11 || len(m.platformSignals) != 0 {
		t.Fatal("heartbeat repeated full graph reads or lost bounded platform prefix")
	}
	frames := platformWakeRaceFrames(&c.platformWakeRaceRecorder)
	deltas, snapshots, runHeartbeats := 0, 0, 0
	for _, frame := range frames {
		switch value := frame.(type) {
		case generated.PlatformSnapshotEnvelope:
			if value.Cursor != int64(8+deltas) || value.Kind != "RUN" || value.Mode != generated.PlatformSnapshotModeDelta {
				t.Fatal("batch changed delta order or kind")
			}
			deltas++
		case generated.RunSnapshotEnvelope:
			snapshots++
		case generated.StreamHeartbeatEnvelope:
			if value.StreamKind == "RUN" {
				runHeartbeats++
			}
		case generated.StreamProblemEnvelope:
			t.Fatal("healthy batch produced a false unavailable result")
		}
	}
	if deltas != 4 || snapshots != 1 || runHeartbeats != 1 {
		t.Fatal("batch lost complete snapshot or confirmed heartbeat")
	}
}

func TestHeartbeatBatchClosedFailures(t *testing.T) {
	for _, condition := range []string{"run-denied", "invalid-snapshot", "catalog-failed-after-prefix", "catalog-denied"} {
		t.Run(condition, func(t *testing.T) {
			m, c := heartbeatRunBatchFixture(t)
			c.invalidSnapshot = condition == "invalid-snapshot"
			c.denyCatalog = condition == "catalog-denied"
			c.beforeInvoke = func(_ context.Context, response any) error {
				if _, run := response.(*cp.GetRunGraphResponse); run && condition == "run-denied" {
					return status.Error(codes.PermissionDenied, "synthetic owner revoked")
				}
				if _, catalog := response.(*cp.ListRunsResponse); catalog && condition == "catalog-failed-after-prefix" && c.catalogReads == 2 {
					return status.Error(codes.Unavailable, "synthetic dependency failed")
				}
				return nil
			}
			if condition == "catalog-failed-after-prefix" {
				for sequence := int64(8); sequence <= 11; sequence++ {
					signal := platformWakeRaceSignal(t, sequence)
					signal.Kind, signal.EventName = "RUN", "RUN_CHANGED"
					if sequence == 9 {
						signal.ProjectRef = ""
					}
					m.platformSignals <- signal
				}
			} else {
				queueHeartbeatRunWakes(t, m, 4)
			}
			if !m.heartbeat(time.Unix(1, 0)) || c.runReads != 1 {
				t.Fatal("failed batch reset owner budget or lost accepted prefix refresh")
			}
			frames := platformWakeRaceFrames(&c.platformWakeRaceRecorder)
			runProblems, platformProblems, runHeartbeats, cursors := 0, 0, 0, 0
			for _, frame := range frames {
				switch value := frame.(type) {
				case generated.StreamProblemEnvelope:
					if value.Code == "RUN_UNAVAILABLE" {
						runProblems++
					}
					if value.Code == "PLATFORM_UNAVAILABLE" {
						platformProblems++
					}
				case generated.StreamHeartbeatEnvelope:
					if value.StreamKind == "RUN" {
						runHeartbeats++
					}
				case generated.PlatformCursorEnvelope:
					cursors++
				}
			}
			if condition == "run-denied" || condition == "invalid-snapshot" {
				if runProblems != 1 || runHeartbeats != 0 || c.eventReads != 0 || m.runs["run_root0001"].available {
					t.Fatal("failed full snapshot was revived by delta-only heartbeat")
				}
			} else if condition == "catalog-failed-after-prefix" {
				if platformProblems != 1 || m.platformCursor != 8 || len(m.platformSignals) != 2 {
					t.Fatal("catalog failure advanced unaccepted cursor or consumed remaining queue")
				}
			} else if cursors != 4 || runProblems != 0 {
				t.Fatal("owner-denied catalog lost cursor-only eligibility semantics")
			}
		})
	}
}

func TestHeartbeatBatchSiblingSharedDeadline(t *testing.T) {
	m, c := heartbeatRunBatchFixture(t)
	m.runs["run_root0002"] = &runSubscription{rootRef: "run_root0002", requestRef: "request_run002", cursor: 7, available: true}
	parent, cancel := context.WithTimeout(m.ctx, 100*time.Millisecond)
	defer cancel()
	expected, _ := parent.Deadline()
	c.beforeInvoke = func(ctx context.Context, response any) error {
		if _, run := response.(*cp.GetRunGraphResponse); run {
			deadline, bounded := ctx.Deadline()
			if !bounded || !deadline.Equal(expected) {
				t.Fatal("sibling owner read restarted the shared parent budget")
			}
		}
		return nil
	}
	if !m.refreshSubscribedRunsWithin(parent) || c.runReads != 2 {
		t.Fatal("shared owner budget lost sibling snapshot")
	}
}

func TestHeartbeatBatchSiblingBudgetAndCancellation(t *testing.T) {
	m, c := heartbeatRunBatchFixture(t)
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	m.ctx = parent
	m.runs["run_root0002"] = &runSubscription{rootRef: "run_root0002", requestRef: "request_run002", cursor: 7, available: true}
	c.beforeInvoke = func(ctx context.Context, response any) error {
		if _, run := response.(*cp.GetRunGraphResponse); run {
			cancel()
			<-ctx.Done()
			return status.FromContextError(ctx.Err()).Err()
		}
		return nil
	}
	queueHeartbeatRunWakes(t, m, 4)
	if m.heartbeat(time.Unix(1, 0)) || c.runReads != 1 || m.runs["run_root0002"].available {
		t.Fatal("cancelled owner batch continued siblings or fabricated availability")
	}
}

func TestHeartbeatBatchDuplicateBudgetAndForeignScope(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		t.Run(map[bool]string{false: "duplicate-and-bounded-prefix", true: "foreign-project-cursor-only"}[foreign], func(t *testing.T) {
			m, c := heartbeatRunBatchFixture(t)
			for _, sequence := range []int64{8, 8, 9, 10, 11} {
				signal := platformWakeRaceSignal(t, sequence)
				signal.Kind, signal.EventName = "RUN", "RUN_CHANGED"
				if foreign {
					signal.ProjectRef = "prj_foreign01"
				}
				m.platformSignals <- signal
			}
			available, ok := m.drainHeartbeatWakes()
			if !available || !ok || m.platformCursor != 10 || len(m.platformSignals) != 1 {
				t.Fatal("duplicate/batch changed exact cursor or consumed beyond prefix budget")
			}
			if foreign && (c.runReads != 0 || c.catalogReads != 0) {
				t.Fatal("foreign-project wake caused scoped owner body reads")
			}
			if !foreign && (c.runReads != 1 || c.catalogReads != 1) {
				t.Fatal("duplicate repeated full owner effects or lost accepted deltas")
			}
		})
	}
}

func TestUnavailableRunRequiresCompleteOwnerSnapshot(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete-recovery", true: "invalid-remains-unavailable"}[invalid], func(t *testing.T) {
			m, c := heartbeatRunBatchFixture(t)
			c.invalidSnapshot = invalid
			subscription := m.runs["run_root0001"]
			subscription.available = false
			if !m.synchronizeRun(subscription) {
				t.Fatal("owner recovery transport failed")
			}
			frames := platformWakeRaceFrames(&c.platformWakeRaceRecorder)
			snapshots, ready := 0, 0
			for _, frame := range frames {
				if _, ok := frame.(generated.RunSnapshotEnvelope); ok {
					snapshots++
				}
				if _, ok := frame.(generated.RunReadyEnvelope); ok {
					ready++
				}
			}
			if c.runReads == 0 {
				t.Fatal("unavailable subscription recovered using delta-only catch-up")
			}
			if invalid && (subscription.available || snapshots != 0 || ready != 0) {
				t.Fatal("invalid complete projection was declared available")
			}
			if !invalid && (!subscription.available || snapshots == 0 || ready != 1) {
				t.Fatal("complete authoritative owner recovery lost readiness")
			}
		})
	}
}
