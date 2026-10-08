package websockettransport

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	httpgenerated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/websocket/generated"
	"google.golang.org/protobuf/proto"
)

const maximumRunSnapshotItems = 128

var errRunSnapshotInvalid = errors.New("run snapshot projection is invalid")

func (multiplexer *sessionMultiplexer) readRunSnapshot(ref string) (*controlplanev1.GetRunGraphResponse, error) {
	return multiplexer.readRunSnapshotWithin(multiplexer.ctx, ref)
}

func (multiplexer *sessionMultiplexer) readRunSnapshotWithin(parent context.Context, ref string) (*controlplanev1.GetRunGraphResponse, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	return multiplexer.server.query.GetRunGraph(ctx, &controlplanev1.GetRunGraphRequest{RunRef: ref, IncludeRunSnapshots: true})
}

func (multiplexer *sessionMultiplexer) refreshSubscribedRuns() bool {
	ctx, cancel := context.WithTimeout(multiplexer.ctx, 10*time.Second)
	defer cancel()
	for _, ref := range multiplexer.sortedRunRefs() {
		subscription := multiplexer.runs[ref]
		snapshot, err := multiplexer.readRunSnapshotWithin(ctx, ref)
		if err != nil {
			subscription.available = false
			if !multiplexer.sendStreamProblem(subscription.requestRef, "RUN", ref, subscription.cursor, "RUN_UNAVAILABLE") {
				return false
			}
			continue
		}
		if !multiplexer.sendRunSnapshot(subscription, snapshot) {
			return false
		}
	}
	return true
}

func projectCompleteRunSnapshot(value *controlplanev1.GetRunGraphResponse, subscription *runSubscription, localize func(string) string) (generated.RunSnapshotEnvelope, error) {
	ref := subscription.rootRef
	graph := value.GetGraph()
	root := value.GetRun()
	if root.GetRef() != ref || root.GetRootRunRef() != ref || graph.GetRunRef() != ref ||
		root.GetGraphRevision() != graph.GetRevision() || root.GetLastEventSequence() != graph.GetSequence() ||
		graph.GetSequence() < subscription.cursor || len(value.GetRuns()) < 1 || len(value.GetRuns()) > maximumRunSnapshotItems {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	expected := map[string]bool{ref: true}
	nodeRefs, edgeRefs := map[string]bool{}, map[string]bool{}
	for _, node := range graph.GetNodes() {
		if !safeRef.MatchString(node.GetRunRef()) || !safeRef.MatchString(node.GetRef()) || nodeRefs[node.GetRef()] {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		nodeRefs[node.GetRef()] = true
		expected[node.GetRunRef()] = true
		for _, ref := range node.GetChildRunRefs() {
			if !safeRef.MatchString(ref) {
				return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
			}
			expected[ref] = true
		}
	}
	for _, edge := range graph.GetEdges() {
		if !safeRef.MatchString(edge.GetRef()) || edgeRefs[edge.GetRef()] || !nodeRefs[edge.GetSourceNodeRef()] || !nodeRefs[edge.GetTargetNodeRef()] {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		edgeRefs[edge.GetRef()] = true
	}
	if len(expected) != len(value.GetRuns()) {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	runs := make([]httpgenerated.Run, 0, len(value.GetRuns()))
	refs := make(map[string]bool, len(expected))
	for ref := range expected {
		refs[ref] = true
	}
	predecessors, valid := snapshotRetryPredecessors(graph, value.GetRuns())
	if !valid {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	for _, run := range value.GetRuns() {
		if run.GetRef() == ref && !proto.Equal(root, run) {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		crossRoot := run.GetRef() == run.GetRootRunRef() && run.GetParentRunRef() != run.GetRef() && refs[run.GetParentRunRef()]
		if !expected[run.GetRef()] || (run.GetRootRunRef() != ref && !crossRoot && !predecessors[run.GetRef()]) || run.GetProjectRef() != root.GetProjectRef() {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		delete(expected, run.GetRef())
		projected, err := projectProto[httpgenerated.Run](run, localize)
		readiness := projected.SessionReadiness
		if err != nil || (run.GetSessionRef() != "" && readiness == nil) ||
			(readiness != nil && (string(readiness.SessionRef) != string(projected.SessionRef) || !readiness.Reason.Valid() || !readiness.StorageState.Valid())) {
			return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
		}
		if readiness != nil && readiness.LatestArchiveTask != nil {
			task := readiness.LatestArchiveTask
			if !task.Kind.Valid() || !task.State.Valid() || !task.SafeErrorCode.Valid() || !safeRef.MatchString(string(task.Ref)) || task.Attempt < 0 || task.MaximumAttempts < 1 || task.MaximumAttempts > 5 || task.Attempt > task.MaximumAttempts {
				return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
			}
		}
		runs = append(runs, projected)
	}
	if len(expected) != 0 {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	projected, err := projectRunGraph(graph, localize)
	if err != nil {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	envelope := generated.RunSnapshotEnvelope{
		Type: "RUN_GRAPH_SNAPSHOT", RequestRef: subscription.requestRef, StreamKind: "RUN", StreamRef: ref,
		Cursor: graph.GetSequence(), Snapshot: projected, Runs: runs,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil || len(encoded) > maximumFrameBytes {
		return generated.RunSnapshotEnvelope{}, errRunSnapshotInvalid
	}
	return envelope, nil
}

func snapshotRetryPredecessors(graph *controlplanev1.RunGraph, values []*controlplanev1.Run) (map[string]bool, bool) {
	nodes := map[string]*controlplanev1.RunNode{}
	for _, node := range graph.GetNodes() {
		nodes[node.GetRef()] = node
	}
	runs := map[string]*controlplanev1.Run{}
	for _, run := range values {
		runs[run.GetRef()] = run
	}
	links := map[string]string{}
	previous := map[string]bool{}
	for _, edge := range graph.GetEdges() {
		if edge.GetType() != controlplanev1.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF {
			continue
		}
		oldNode, newNode := nodes[edge.GetSourceNodeRef()], nodes[edge.GetTargetNodeRef()]
		old, fresh := runs[oldNode.GetRunRef()], runs[newNode.GetRunRef()]
		if old == nil || fresh == nil || oldNode.GetType() != controlplanev1.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS || newNode.GetType() != controlplanev1.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS || old.GetRef() != old.GetRootRunRef() || fresh.GetRef() != fresh.GetRootRunRef() || fresh.GetRetryOfRunRef() != old.GetRef() || edge.GetRunRef() != fresh.GetRef() || old.GetRef() == fresh.GetRef() || links[fresh.GetRef()] != "" {
			return nil, false
		}
		links[fresh.GetRef()] = old.GetRef()
		previous[old.GetRef()] = true
	}
	for _, run := range values {
		if run.GetRetryOfRunRef() != "" && runs[run.GetRetryOfRunRef()] != nil && links[run.GetRef()] != run.GetRetryOfRunRef() {
			return nil, false
		}
	}
	for ref := range links {
		seen := map[string]bool{}
		for current := ref; current != ""; current = links[current] {
			if seen[current] {
				return nil, false
			}
			seen[current] = true
		}
	}
	return previous, true
}
