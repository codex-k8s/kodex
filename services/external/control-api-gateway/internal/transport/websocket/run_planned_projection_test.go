package websockettransport

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
)

func TestPlannedRunGraphRealtimeProjection(t *testing.T) {
	for _, plannedCount := range []int{0, 31} {
		t.Run(fmt.Sprintf("planned_%d", plannedCount), func(t *testing.T) {
			graph := &cp.RunGraph{RunRef: "run_fixture", Revision: 4, Sequence: 231}
			for index := range 37 {
				graph.Nodes = append(graph.Nodes, &cp.RunNode{Ref: fmt.Sprintf("nod_fixture_%d", index), RunRef: graph.RunRef,
					Type: cp.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION, State: cp.RunNodeState_RUN_NODE_STATE_PLANNED, Planned: index < plannedCount})
			}
			projected, err := projectRunGraph(graph, func(value string) string { return value })
			if err != nil || projected.Sequence != 231 || projected.Revision != 4 || len(projected.Nodes) != 37 {
				t.Fatal("planned graph projection lost the authoritative snapshot", err)
			}
			for index, node := range projected.Nodes {
				if node.Planned != graph.Nodes[index].Planned || node.Ref != graph.Nodes[index].Ref {
					t.Fatal("planned graph projection changed a node")
				}
			}
			encoded, err := json.Marshal(generated.RunSnapshotEnvelope{Type: "RUN_GRAPH_SNAPSHOT", StreamKind: "RUN", StreamRef: graph.RunRef, Cursor: 231, Snapshot: projected})
			if err != nil || len(encoded) > maximumFrameBytes || strings.Count(string(encoded), `"planned":true`) != plannedCount {
				t.Fatal("planned snapshot wire lost flags or exceeded its frame budget", err)
			}
		})
	}
}

func TestPlannedRunEventRealtimeProjection(t *testing.T) {
	for _, planned := range []bool{false, true} {
		t.Run(fmt.Sprintf("planned_%t", planned), func(t *testing.T) {
			event := &cp.RunEvent{Ref: "evt_fixture", RunRef: "run_fixture", Sequence: 231,
				Type: cp.RunEventType_RUN_EVENT_TYPE_TURN_PROGRESS, Node: &cp.RunNode{Ref: "nod_fixture", Planned: planned}}
			projected, err := projectRunEvent(event, func(value string) string { return value })
			if err != nil || projected.Node == nil || projected.Sequence != 231 || projected.Node.Planned != planned {
				t.Fatal("planned event projection lost the authoritative flag or cursor", err)
			}
			encoded, err := json.Marshal(generated.RunEventEnvelope{Type: "RUN_EVENT", StreamKind: "RUN", StreamRef: event.RunRef, Cursor: 231, Event: projected})
			if err != nil || strings.Contains(string(encoded), `"planned":true`) != planned {
				t.Fatal("planned event wire lost its flag", err)
			}
		})
	}
}

func TestPlannedRunProjectionStillRejectsUnknownPrivateFields(t *testing.T) {
	for _, node := range []string{`{"planned":true,"privateCredential":"private_fixture"}`, `{"planned":false,"privateCredential":"private_fixture"}`, `{"planned":"true"}`} {
		if _, err := decodeClosed[generated.RunGraph]([]byte(`{"nodes":[` + node + `]}`)); err == nil {
			t.Fatal("graph projection accepted an undeclared private field or mistyped flag")
		}
		if _, err := decodeClosed[generated.RunEvent]([]byte(`{"node":` + node + `}`)); err == nil {
			t.Fatal("event projection accepted an undeclared private field or mistyped flag")
		}
	}
}
