package websockettransport

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func completeRunSnapshotFixture() *cp.GetRunGraphResponse {
	root := &cp.Run{Ref: "run_root0001", RootRunRef: "run_root0001", ProjectRef: "prj_project1", Version: 1,
		SessionRef: "ses_root0001", GraphRevision: 4, LastEventSequence: 7,
		Source: cp.RunSource_RUN_SOURCE_CONTROL_CENTER, Target: &cp.RunTarget{Target: &cp.RunTarget_AgentRef{AgentRef: "agt_fixture1"}},
		SessionReadiness: &cp.RunSessionReadiness{SessionRef: "ses_root0001", StorageState: cp.RunSessionStorageState_RUN_SESSION_STORAGE_STATE_LIVE, Reason: cp.RunSessionReadinessReason_RUN_SESSION_READINESS_REASON_NO_SESSION_BLOCKER}}
	child := proto.Clone(root).(*cp.Run)
	child.Ref, child.ParentRunRef, child.SessionRef = "run_child001", root.Ref, "ses_child001"
	child.SessionReadiness.SessionRef = child.SessionRef
	return &cp.GetRunGraphResponse{Run: root, Graph: &cp.RunGraph{RunRef: root.Ref, Revision: 4, Sequence: 7,
		Nodes: []*cp.RunNode{{Ref: "nod_child001", RunRef: child.Ref}}}, Runs: []*cp.Run{root, child}}
}

func TestCompleteRunSnapshotBindingAndClosedFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*cp.GetRunGraphResponse)
	}{
		{"missing", func(v *cp.GetRunGraphResponse) { v.Runs = nil }},
		{"omitted", func(v *cp.GetRunGraphResponse) { v.Runs = v.Runs[:1] }},
		{"duplicate", func(v *cp.GetRunGraphResponse) { v.Runs[1] = v.Runs[0] }},
		{"duplicate_node", func(v *cp.GetRunGraphResponse) { v.Graph.Nodes = append(v.Graph.Nodes, v.Graph.Nodes[0]) }},
		{"root_body", func(v *cp.GetRunGraphResponse) { v.Run = proto.Clone(v.Run).(*cp.Run); v.Run.Version++ }},
		{"malformed_child_locator", func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[0].ChildRunRefs = []string{"private/foreign"} }},
		{"foreign_child_locator", func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[0].ChildRunRefs = []string{"run_foreign1"} }},
		{"foreign", func(v *cp.GetRunGraphResponse) { v.Runs[1].ProjectRef = "prj_foreign1" }},
		{"unrelated", func(v *cp.GetRunGraphResponse) { v.Runs[1].RootRunRef = "run_foreign1" }},
		{"missing_parent", func(v *cp.GetRunGraphResponse) { v.Runs[1].RootRunRef = v.Runs[1].Ref; v.Runs[1].ParentRunRef = "" }},
		{"foreign_parent", func(v *cp.GetRunGraphResponse) {
			v.Runs[1].RootRunRef = v.Runs[1].Ref
			v.Runs[1].ParentRunRef = "run_foreign1"
		}},
		{"session", func(v *cp.GetRunGraphResponse) { v.Runs[1].SessionReadiness.SessionRef = "ses_foreign1" }},
		{"missing_readiness", func(v *cp.GetRunGraphResponse) { v.Runs[1].SessionReadiness = nil }},
		{"unknown_storage", func(v *cp.GetRunGraphResponse) {
			v.Runs[1].SessionReadiness.StorageState = cp.RunSessionStorageState(999)
		}},
		{"cursor", func(v *cp.GetRunGraphResponse) { v.Graph.Sequence-- }},
		{"size", func(v *cp.GetRunGraphResponse) { v.Runs[1].Title = strings.Repeat("x", maximumFrameBytes) }},
		{"count", func(v *cp.GetRunGraphResponse) {
			for len(v.Runs) <= maximumRunSnapshotItems {
				v.Runs = append(v.Runs, v.Runs[1])
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := completeRunSnapshotFixture()
			tc.mutate(value)
			if _, err := projectCompleteRunSnapshot(value, &runSubscription{rootRef: value.Run.Ref}, nil); err == nil {
				t.Fatal("invalid owner snapshot accepted")
			}
		})
	}
	for _, crossRoot := range []bool{false, true} {
		value := completeRunSnapshotFixture()
		if crossRoot {
			value.Runs[1].RootRunRef = value.Runs[1].Ref
			value.Graph.Nodes[0].RunRef = value.Run.Ref
			value.Graph.Nodes[0].ChildRunRefs = []string{value.Runs[1].Ref}
		}
		got, err := projectCompleteRunSnapshot(value, &runSubscription{rootRef: value.Run.Ref, requestRef: "request_001"}, nil)
		if err != nil || len(got.Runs) != 2 {
			t.Fatal("complete owner snapshot rejected", err)
		}
		encoded, _ := json.Marshal(got)
		if !strings.Contains(string(encoded), "\"runs\"") {
			t.Fatal("required runs field omitted")
		}
	}
	value := completeRunSnapshotFixture()
	value.Graph.Nodes = nil
	value.Runs = value.Runs[:1]
	if got, err := projectCompleteRunSnapshot(value, &runSubscription{rootRef: value.Run.Ref}, nil); err != nil || len(got.Runs) != 1 {
		t.Fatal("empty graph root snapshot rejected", err)
	}
}

func TestRunSnapshotRetryLineage(t *testing.T) {
	fixture := func() *cp.GetRunGraphResponse {
		v := completeRunSnapshotFixture()
		v.Runs[1].RootRunRef, v.Runs[1].ParentRunRef, v.Runs[1].State = v.Runs[1].Ref, "", cp.RunState_RUN_STATE_CANCELLED
		v.Run.RetryOfRunRef = v.Runs[1].Ref
		v.Graph.Nodes = []*cp.RunNode{{Ref: "nod_previous", RunRef: v.Runs[1].Ref, Type: cp.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS}, {Ref: "nod_current1", RunRef: v.Run.Ref, Type: cp.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS}}
		v.Graph.Edges = []*cp.RunEdge{{Ref: "edg_retry001", RunRef: v.Run.Ref, SourceNodeRef: "nod_previous", TargetNodeRef: "nod_current1", Type: cp.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF}}
		return v
	}
	v := fixture()
	if _, err := projectCompleteRunSnapshot(v, &runSubscription{rootRef: v.Run.Ref}, nil); err != nil {
		t.Fatal("cancelled retry predecessor rejected", err)
	}
	for name, mutate := range map[string]func(*cp.GetRunGraphResponse){
		"missing":   func(v *cp.GetRunGraphResponse) { v.Graph.Edges = nil },
		"foreign":   func(v *cp.GetRunGraphResponse) { v.Graph.Edges[0].SourceNodeRef = "nod_foreign1" },
		"pin":       func(v *cp.GetRunGraphResponse) { v.Run.RetryOfRunRef = "run_foreign1" },
		"duplicate": func(v *cp.GetRunGraphResponse) { v.Graph.Edges = append(v.Graph.Edges, v.Graph.Edges[0]) },
		"cycle": func(v *cp.GetRunGraphResponse) {
			v.Runs[1].RetryOfRunRef = v.Run.Ref
			v.Graph.Edges = append(v.Graph.Edges, &cp.RunEdge{Ref: "edg_reverse1", RunRef: v.Runs[1].Ref, SourceNodeRef: "nod_current1", TargetNodeRef: "nod_previous", Type: cp.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF})
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := fixture()
			mutate(v)
			if _, err := projectCompleteRunSnapshot(v, &runSubscription{rootRef: v.Run.Ref}, nil); err == nil {
				t.Fatal("unproven retry accepted")
			}
		})
	}
}

type snapshotQueryRecorder struct {
	cp.PlatformQueryServiceClient
	refs []string
	fail bool
}

func (query *snapshotQueryRecorder) GetPlatformEventCursor(context.Context, *cp.GetPlatformEventCursorRequest, ...grpc.CallOption) (*cp.GetPlatformEventCursorResponse, error) {
	return &cp.GetPlatformEventCursorResponse{OrganizationRef: "org_fixture1", CurrentSequence: 10}, nil
}

func (query *snapshotQueryRecorder) GetRunGraph(ctx context.Context, input *cp.GetRunGraphRequest, _ ...grpc.CallOption) (*cp.GetRunGraphResponse, error) {
	if !input.GetIncludeRunSnapshots() {
		return nil, status.Error(codes.Internal, "projection selector missing")
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, status.Error(codes.Internal, "projection deadline missing")
	}
	query.refs = append(query.refs, input.GetRunRef())
	if query.fail {
		return nil, status.Error(codes.PermissionDenied, "not permitted")
	}
	value := completeRunSnapshotFixture()
	if input.GetRunRef() != value.Run.Ref {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return value, nil
}

func TestRunSnapshotRefreshOnlyRegisteredRootsAndRevocation(t *testing.T) {
	query := &snapshotQueryRecorder{}
	mux := &sessionMultiplexer{ctx: t.Context(), server: &Server{query: query}, localize: func(value string) string { return value }, outbound: make(chan outboundFrame, 8), runs: map[string]*runSubscription{
		"run_root0001": {rootRef: "run_root0001", requestRef: "request_001"},
	}}
	if !mux.refreshSubscribedRuns() || len(query.refs) != 1 || query.refs[0] != "run_root0001" {
		t.Fatal("snapshot fanout escaped subscription set")
	}
	query.fail = true
	if !mux.refreshSubscribedRuns() || mux.runs["run_root0001"].available {
		t.Fatal("revoked snapshot stayed available")
	}
}

func TestRunSnapshotRefreshAfterLostOrganizationWake(t *testing.T) {
	query := &snapshotQueryRecorder{}
	mux := &sessionMultiplexer{ctx: t.Context(), organizationRef: "org_fixture1", platformCursor: 7,
		server: &Server{query: query}, localize: func(value string) string { return value }, outbound: make(chan outboundFrame, 8),
		runs: map[string]*runSubscription{"run_root0001": {rootRef: "run_root0001", requestRef: "request_001"}}}
	if !mux.synchronizePlatform() || mux.platformCursor != 10 || len(query.refs) != 1 {
		t.Fatal("lost org wake did not rejoin authoritative subscribed snapshot")
	}
	if !mux.synchronizePlatform() || len(query.refs) != 1 {
		t.Fatal("unchanged org cursor introduced snapshot polling")
	}
}
