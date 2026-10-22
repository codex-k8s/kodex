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

// completeRunSnapshotFixture constructs a synthetic root/child response with matching graph revision, event sequence and distinct session-readiness bindings; it is not a live execution or authority grant.
// completeRunSnapshotFixture создаёт тестовый ответ корня и ребёнка с согласованными ревизией графа, sequence события и раздельной готовностью сессий; это не настоящий запуск и не grant полномочий.
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

// TestCompleteRunSnapshotBindingAndClosedFailures independently mutates missing/duplicate/foreign runs, graph references, root body, session readiness, cursor and size/count bounds, requiring closed projection failure while valid child/cross-root/root-only fixtures remain accepted.
// TestCompleteRunSnapshotBindingAndClosedFailures независимо меняет отсутствующие, повторные и чужие запуски, ссылки графа, тело корня, готовность сессии, курсор и пределы размера и количества, требуя закрытого отказа; допустимые ребёнок, межкорневой ребёнок и одиночный корень остаются принятыми.
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

// TestRunSnapshotRetryLineage accepts a synthetic canceled retry predecessor only with its exact root-process edge/pin, then rejects missing/foreign/duplicate/cyclic lineage. It does not execute a retry or prove all predecessor-state policy.
// TestRunSnapshotRetryLineage принимает тестового отменённого retry-предшественника с точными корневым ребром и привязкой, затем отклоняет отсутствующие, чужие, повторные и циклические связи. Retry не выполняется, полная политика состояния предшественника не доказывается.
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

// snapshotQueryRecorder provides a fixed owner cursor and one complete synthetic root graph while recording requested refs and optionally denying reads; embedded client methods are not a real server.
// snapshotQueryRecorder предоставляет фиксированный курсор владельца и один полный тестовый граф корня, запоминая ссылки и при необходимости запрещая чтения; встроенные методы клиента не являются настоящим сервером.
type snapshotQueryRecorder struct {
	cp.PlatformQueryServiceClient
	refs []string
	fail bool
}

// TestRunSnapshotContinuationLineage requires the exact CONTINUES edge from a succeeded root sharing the session and rejects missing edge, wrong session/state/node, retry pin or duplicate lineage without creating runs.
// TestRunSnapshotContinuationLineage требует точное CONTINUES от успешного корня с общей сессией и отклоняет отсутствие ребра, неверные сессию, состояние, узел, retry-привязку и повтор связи без создания запусков.
func TestRunSnapshotContinuationLineage(t *testing.T) {
	fixture := func() *cp.GetRunGraphResponse {
		v := completeRunSnapshotFixture()
		v.Runs[1].RootRunRef, v.Runs[1].ParentRunRef, v.Runs[1].State = v.Runs[1].Ref, "", cp.RunState_RUN_STATE_SUCCEEDED
		v.Runs[1].SessionRef, v.Runs[1].SessionReadiness.SessionRef = v.Run.SessionRef, v.Run.SessionRef
		v.Graph.Nodes = []*cp.RunNode{{Ref: "nod_previous", RunRef: v.Runs[1].Ref, Type: cp.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS}, {Ref: "nod_current1", RunRef: v.Run.Ref, Type: cp.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS}}
		v.Graph.Edges = []*cp.RunEdge{{Ref: "edg_continue1", RunRef: v.Run.Ref, SourceNodeRef: "nod_previous", TargetNodeRef: "nod_current1", Type: cp.RunEdgeType_RUN_EDGE_TYPE_CONTINUES}}
		return v
	}
	v := fixture()
	if _, err := projectCompleteRunSnapshot(v, &runSubscription{rootRef: v.Run.Ref}, nil); err != nil {
		t.Fatal("exact continuation predecessor rejected", err)
	}
	for name, mutate := range map[string]func(*cp.GetRunGraphResponse){
		"missing":   func(v *cp.GetRunGraphResponse) { v.Graph.Edges = nil },
		"session":   func(v *cp.GetRunGraphResponse) { v.Runs[1].SessionRef = "ses_foreign1" },
		"state":     func(v *cp.GetRunGraphResponse) { v.Runs[1].State = cp.RunState_RUN_STATE_RUNNING },
		"node":      func(v *cp.GetRunGraphResponse) { v.Graph.Nodes[0].Type = cp.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION },
		"retry":     func(v *cp.GetRunGraphResponse) { v.Run.RetryOfRunRef = v.Runs[1].Ref },
		"duplicate": func(v *cp.GetRunGraphResponse) { v.Graph.Edges = append(v.Graph.Edges, v.Graph.Edges[0]) },
	} {
		t.Run(name, func(t *testing.T) {
			v := fixture()
			mutate(v)
			if _, err := projectCompleteRunSnapshot(v, &runSubscription{rootRef: v.Run.Ref}, nil); err == nil {
				t.Fatal("unproven continuation accepted")
			}
		})
	}
}

// GetPlatformEventCursor returns a fixed synthetic organization and sequence ten, ignoring request/context rather than consulting an event store.
// GetPlatformEventCursor возвращает фиксированные тестовые организацию и sequence десять, игнорируя запрос и контекст вместо обращения к event store.
func (query *snapshotQueryRecorder) GetPlatformEventCursor(context.Context, *cp.GetPlatformEventCursorRequest, ...grpc.CallOption) (*cp.GetPlatformEventCursorResponse, error) {
	return &cp.GetPlatformEventCursorResponse{OrganizationRef: "org_fixture1", CurrentSequence: 10}, nil
}

// GetRunGraph requires IncludeRunSnapshots and a bounded context, records the requested ref and returns the sole fixture graph or configured permission/not-found failures without network access.
// GetRunGraph требует IncludeRunSnapshots и ограниченный контекст, запоминает ссылку и возвращает единственный тестовый граф либо настроенные запрет и отсутствие без сети.
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

// TestRunSnapshotRefreshOnlyRegisteredRootsAndRevocation checks refresh reads only the registered root and marks it unavailable after the synthetic owner starts denying access, without broad run enumeration.
// TestRunSnapshotRefreshOnlyRegisteredRootsAndRevocation проверяет чтение только зарегистрированного корня и его недоступность после тестового отказа владельца без общего перечисления запусков.
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

// TestRunSnapshotRefreshAfterLostOrganizationWake advances the fake authoritative organization cursor to trigger one subscribed-root rejoin, then proves an unchanged cursor does not repeatedly reread snapshots.
// TestRunSnapshotRefreshAfterLostOrganizationWake продвигает тестовый авторитетный курсор организации для единственного перечитывания зарегистрированного корня, затем доказывает отсутствие повторных снимков при неизменном курсоре.
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
