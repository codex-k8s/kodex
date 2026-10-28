package websockettransport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

// serverWorkflowRetryOwnerSnapshot models the safe owner response after a child business retry;
// its old technical final and immutable logical anchor are separate nodes in the same run.
// serverWorkflowRetryOwnerSnapshot задаёт безопасный owner response после business retry;
// прежний technical final и immutable logical anchor — отдельные узлы одного run.
func serverWorkflowRetryOwnerSnapshot() *cp.GetRunGraphResponse {
	value := completeRunSnapshotFixture()
	root, old := value.Run, value.Runs[1]
	root.Target = &cp.RunTarget{Target: &cp.RunTarget_WorkflowRef{WorkflowRef: "wfl_fixture1"}}
	root.State = cp.RunState_RUN_STATE_CANCELLED
	old.Ref, old.RootRunRef, old.ParentRunRef, old.Attempt = "run_previous", root.Ref, root.Ref, 1
	old.Source, old.State = cp.RunSource_RUN_SOURCE_AGENT_DELEGATION, cp.RunState_RUN_STATE_SUCCEEDED
	fresh := proto.Clone(old).(*cp.Run)
	fresh.Ref, fresh.SessionRef = "run_current1", "ses_current1"
	fresh.State = cp.RunState_RUN_STATE_CANCELLED
	fresh.SessionReadiness.SessionRef = fresh.SessionRef
	value.Runs = []*cp.Run{root, old, fresh}
	origin := &cp.RunNode{Ref: "nod_origin01", RunRef: old.Ref, ParentNodeRef: "nod_root0001", Type: cp.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION, AgentRef: "agt_fixture1", Attempt: 1, State: cp.RunNodeState_RUN_NODE_STATE_SUCCEEDED,
		WorkflowResults: []*cp.WorkflowAttemptResult{{Attempt: 1, RunRef: old.Ref, Accepted: false}}}
	previous := proto.Clone(origin).(*cp.RunNode)
	previous.Ref, previous.Attempt, previous.WorkflowResults = "nod_previous", 3, nil
	middle := proto.Clone(origin).(*cp.RunNode)
	middle.Ref, middle.Attempt, middle.WorkflowResults, middle.ParentNodeRef = "nod_middle01", 2, nil, origin.Ref
	previous.ParentNodeRef = middle.Ref
	current := proto.Clone(origin).(*cp.RunNode)
	current.Ref, current.RunRef, current.WorkflowResults = "nod_current1", fresh.Ref, nil
	current.State = cp.RunNodeState_RUN_NODE_STATE_CANCELLED
	value.Graph.Nodes = []*cp.RunNode{{Ref: "nod_root0001", RunRef: root.Ref, Type: cp.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS, State: cp.RunNodeState_RUN_NODE_STATE_CANCELLED}, origin, previous, current, middle}
	value.Graph.Edges = []*cp.RunEdge{{Ref: "edg_retry001", RunRef: root.Ref, SourceNodeRef: previous.Ref, TargetNodeRef: current.Ref, Type: cp.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF,
		ServerWorkflowRetry: &cp.WorkflowRetryReadProof{WorkflowVersionRef: "wfv_fixture1", WorkflowVersionDigest: strings.Repeat("a", 64), OriginNodeRef: origin.Ref, StepKey: "work", PreviousBusinessAttempt: 1, BusinessAttempt: 2}}}
	value.Graph.Edges = append(value.Graph.Edges,
		&cp.RunEdge{Ref: "edg_cont0001", RunRef: root.Ref, SourceNodeRef: origin.Ref, TargetNodeRef: middle.Ref, Type: cp.RunEdgeType_RUN_EDGE_TYPE_CONTINUES},
		&cp.RunEdge{Ref: "edg_cont0002", RunRef: root.Ref, SourceNodeRef: middle.Ref, TargetNodeRef: previous.Ref, Type: cp.RunEdgeType_RUN_EDGE_TYPE_CONTINUES})
	return value
}

// serverWorkflowRetrySnapshotQuery retains the existing query-client fixture but returns the
// exact SERVER owner response for the actual recoverRun snapshot/ready path.
// serverWorkflowRetrySnapshotQuery сохраняет прежний query-client fixture, возвращая точный
// SERVER owner response для настоящего recoverRun snapshot/ready path.
type serverWorkflowRetrySnapshotQuery struct {
	snapshotQueryRecorder
	value *cp.GetRunGraphResponse
}

// GetRunGraph requires the real full-snapshot selector and bounded context before returning a
// cloned owner fixture; it never models a mutation or an external network call.
// GetRunGraph требует настоящий full-snapshot selector и bounded context до выдачи копии
// owner fixture; mutation либо внешний сетевой вызов не моделируются.
func (query *serverWorkflowRetrySnapshotQuery) GetRunGraph(ctx context.Context, input *cp.GetRunGraphRequest, _ ...grpc.CallOption) (*cp.GetRunGraphResponse, error) {
	if !input.GetIncludeRunSnapshots() || input.GetRunRef() != query.value.GetRun().GetRef() {
		return nil, errRunSnapshotInvalid
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return nil, errRunSnapshotInvalid
	}
	return proto.Clone(query.value).(*cp.GetRunGraphResponse), nil
}

// TestServerWorkflowRetryRecoverRunReady drives the actual producer through full owner reread,
// generated snapshot and RUN_READY; invalid proof stays unavailable without a ready frame.
// TestServerWorkflowRetryRecoverRunReady проводит producer через full owner reread, generated
// snapshot и RUN_READY; неверный proof остаётся недоступным без ready frame.
func TestServerWorkflowRetryRecoverRunReady(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		value := serverWorkflowRetryOwnerSnapshot()
		if invalid {
			value.Graph.Edges[0].ServerWorkflowRetry = nil
		}
		query := &serverWorkflowRetrySnapshotQuery{value: value}
		frames := make(chan outboundFrame, 8)
		mux := &sessionMultiplexer{ctx: t.Context(), server: &Server{query: query}, outbound: frames, localize: func(value string) string { return value }}
		subscription := &runSubscription{rootRef: value.Run.Ref, requestRef: "request_001"}
		if !mux.recoverRun(subscription, value, 0) {
			t.Fatal("retry snapshot recovery transport failed")
		}
		snapshots, ready, problems := 0, 0, 0
		for len(frames) > 0 {
			switch (<-frames).value.(type) {
			case generated.RunSnapshotEnvelope:
				snapshots++
			case generated.RunReadyEnvelope:
				ready++
			case generated.StreamProblemEnvelope:
				problems++
			}
		}
		if invalid && (subscription.available || ready != 0 || problems == 0) ||
			!invalid && (!subscription.available || ready != 1 || snapshots < 1 || problems != 0) {
			t.Fatal("SERVER retry projection lost exact readiness outcome")
		}
	}
}

// TestServerWorkflowRetryFullSnapshotProjection exercises the real complete-snapshot projector and
// generated wire; malformed proof, history and owner bindings must remain unavailable.
// TestServerWorkflowRetryFullSnapshotProjection проверяет настоящий complete-snapshot projector
// и generated wire; неверные proof, history и owner bindings остаются недоступными.
func TestServerWorkflowRetryFullSnapshotProjection(t *testing.T) {
	for _, accepted := range []bool{false, true} {
		value := serverWorkflowRetryOwnerSnapshot()
		value.Graph.Nodes[1].WorkflowResults[0].Accepted = accepted
		projected, err := projectCompleteRunSnapshot(value, &runSubscription{rootRef: value.Run.Ref, requestRef: "request_001"}, nil)
		if err != nil {
			t.Fatal("owner-proved server child retry rejected", err)
		}
		raw, err := json.Marshal(projected)
		if err != nil || !strings.Contains(string(raw), `"serverWorkflowRetry"`) || strings.Contains(string(raw), "runtimeRevisionId") || strings.Contains(string(raw), "leaseId") {
			t.Fatal("safe retry read proof lost or private execution pins leaked")
		}
	}
	for name, mutate := range map[string]func(*cp.GetRunGraphResponse){
		"missing technical edge": func(v *cp.GetRunGraphResponse) { v.Graph.Edges = v.Graph.Edges[:2] },
		"duplicate technical edge": func(v *cp.GetRunGraphResponse) {
			edge := proto.Clone(v.Graph.Edges[2]).(*cp.RunEdge)
			edge.Ref = "edg_duplicate1"
			v.Graph.Edges = append(v.Graph.Edges, edge)
		},
		"skipped technical attempt": func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[2].Attempt = 5 },
		"technical parent is root":  func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[2].ParentNodeRef = "nod_root0001" },
		"foreign technical run":     func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[4].RunRef = "run_current1" },
		"foreign technical agent":   func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[4].AgentRef = "agt_foreign1" },
		"missing":                   func(v *cp.GetRunGraphResponse) { v.Graph.Edges[0].ServerWorkflowRetry = nil },
		"unknown proto": func(v *cp.GetRunGraphResponse) {
			v.Graph.Edges[0].ServerWorkflowRetry.ProtoReflect().SetUnknown([]byte{0xa0, 0x06, 0x01})
		},
		"origin": func(v *cp.GetRunGraphResponse) { v.Graph.Edges[0].ServerWorkflowRetry.OriginNodeRef = "nod_foreign1" },
		"digest": func(v *cp.GetRunGraphResponse) {
			v.Graph.Edges[0].ServerWorkflowRetry.WorkflowVersionDigest = strings.Repeat("G", 64)
		},
		"step":            func(v *cp.GetRunGraphResponse) { v.Graph.Edges[0].ServerWorkflowRetry.StepKey = "invalid.step" },
		"attempt":         func(v *cp.GetRunGraphResponse) { v.Graph.Edges[0].ServerWorkflowRetry.BusinessAttempt = 4 },
		"history":         func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[1].WorkflowResults[0].RunRef = "run_foreign1" },
		"missing history": func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[1].WorkflowResults = nil },
		"duplicate history": func(v *cp.GetRunGraphResponse) {
			v.Graph.Nodes[1].WorkflowResults = append(v.Graph.Nodes[1].WorkflowResults, v.Graph.Nodes[1].WorkflowResults[0])
		},
		"parent":             func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[3].ParentNodeRef = "nod_foreign1" },
		"agent":              func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[3].AgentRef = "agt_foreign1" },
		"runtime attempt":    func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[3].Attempt = 2 },
		"root":               func(v *cp.GetRunGraphResponse) { v.Graph.Edges[0].RunRef = "run_foreign1" },
		"project":            func(v *cp.GetRunGraphResponse) { v.Runs[2].ProjectRef = "prj_foreign1" },
		"ordinary retry":     func(v *cp.GetRunGraphResponse) { v.Runs[2].RetryOfRunRef = v.Runs[1].Ref },
		"proof on continues": func(v *cp.GetRunGraphResponse) { v.Graph.Edges[0].Type = cp.RunEdgeType_RUN_EDGE_TYPE_CONTINUES },
		"duplicate slot": func(v *cp.GetRunGraphResponse) {
			e := proto.Clone(v.Graph.Edges[0]).(*cp.RunEdge)
			e.Ref = "edg_retry002"
			v.Graph.Edges = append(v.Graph.Edges, e)
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := serverWorkflowRetryOwnerSnapshot()
			mutate(value)
			if _, err := projectCompleteRunSnapshot(value, &runSubscription{rootRef: value.Run.Ref}, nil); err == nil {
				t.Fatal("unproved server retry passed complete-snapshot projection")
			}
		})
	}
}
