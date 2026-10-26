package websockettransport

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	httptransport "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http"
	"google.golang.org/protobuf/proto"
)

// TestWorkflowHistorySurvivesSnapshotAndEvent proves the actual generated WebSocket models keep
// exactly the public HTTP attempt/evidence fields for planned and completed nodes. It checks
// projection only, not owner provenance or dispatch.
// TestWorkflowHistorySurvivesSnapshotAndEvent доказывает сохранение публичных HTTP-полей попыток
// и результатов в generated WebSocket-моделях для плановых и завершённых узлов. Проверяется
// проекция, а не происхождение доказательств или запуск следующего шага.
func TestWorkflowHistorySurvivesSnapshotAndEvent(t *testing.T) {
	for _, planned := range []bool{true, false} {
		t.Run(map[bool]string{true: "planned", false: "completed"}[planned], func(t *testing.T) {
			fixture := completeRunSnapshotFixture()
			node := fixture.Graph.Nodes[0]
			node.Planned = planned
			node.Type = cp.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION
			node.State = cp.RunNodeState_RUN_NODE_STATE_SUCCEEDED
			if planned {
				node.State = cp.RunNodeState_RUN_NODE_STATE_PLANNED
			} else {
				node.WorkflowResults = []*cp.WorkflowAttemptResult{
					{Attempt: 1, RunRef: node.RunRef, Reason: "MISSING_RESULT"},
					{Attempt: 2, RunRef: node.RunRef, Accepted: true, Results: []*cp.WorkflowAcceptedResult{
						{Key: "document", Kind: "ARTIFACT", ArtifactRef: "art_fixture1", ArtifactRevision: "arv_fixture1", Digest: strings.Repeat("a", 64)},
						{Key: "pull_request", Kind: "PULL_REQUEST", ReceiptRef: "erc_fixture1", Repository: "codex-k8s/test-repo", Number: 3, HeadSha: strings.Repeat("b", 40), Digest: strings.Repeat("c", 64)},
						{Key: "review", Kind: "REVIEW", Verdict: "PASS", HeadSha: strings.Repeat("b", 40), Digest: strings.Repeat("d", 64)},
						{Key: "release", Kind: "DEPLOYMENT", TargetRef: "ssh_fixture1", ReceiptRef: "erc_fixture2", BundleSha256: strings.Repeat("e", 64), ImageDigest: "sha256:" + strings.Repeat("f", 64), HealthOk: true, Digest: strings.Repeat("a", 64)},
					}},
				}
			}
			httpView, err := httptransport.ProtoMap(node)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := projectCompleteRunSnapshot(fixture, &runSubscription{rootRef: fixture.Run.Ref, cursor: 7}, nil)
			if err != nil {
				t.Fatal("workflow snapshot rejected", err)
			}
			event, err := projectRunEvent(&cp.RunEvent{Ref: "evt_fixture1", RunRef: fixture.Run.Ref, Sequence: 8, Node: proto.Clone(node).(*cp.RunNode)}, nil)
			if err != nil || event.Node == nil {
				t.Fatal("workflow event rejected", err)
			}
			for _, value := range []any{snapshot.Snapshot.Nodes[0], event.Node} {
				wire, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var view map[string]any
				if err := json.Unmarshal(wire, &view); err != nil {
					t.Fatal(err)
				}
				if !planned && !reflect.DeepEqual(view["workflowResults"], httpView["workflowResults"]) {
					t.Fatal("HTTP and WebSocket history differ")
				}
				if planned && view["planned"] != true {
					t.Fatal("planned node lost its immutable state")
				}
				if strings.Contains(string(wire), "runtimeRevisionId") {
					t.Fatal("private runtime binding leaked")
				}
			}
			if !planned {
				node.WorkflowResults[1].Results[0].Kind = "UNCHECKED"
				if _, err := projectCompleteRunSnapshot(fixture, &runSubscription{rootRef: fixture.Run.Ref, cursor: 7}, nil); err == nil {
					t.Fatal("unknown evidence kind reached WebSocket snapshot")
				}
			}
		})
	}
}
