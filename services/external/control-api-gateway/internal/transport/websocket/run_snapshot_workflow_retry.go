package websockettransport

import (
	"regexp"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
)

// workflowRetryReadDigest and workflowRetryReadStep require the published wire encodings, not
// arbitrary labels; matching these patterns does not prove the owner's private journal.
// workflowRetryReadDigest и workflowRetryReadStep требуют опубликованную wire-форму, не
// произвольные метки; совпадение с шаблоном не доказывает приватный journal владельца.
var (
	workflowRetryReadDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
	workflowRetryReadStep   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
)

// validServerWorkflowRetrySnapshot admits only the owner's typed child-to-child SERVER retry
// projection. It binds safe public evidence to this graph without creating execution authority.
// validServerWorkflowRetrySnapshot допускает только typed owner projection SERVER child retry.
// Безопасные доказательства связываются с данным графом без выдачи полномочий исполнения.
func validServerWorkflowRetrySnapshot(graph *cp.RunGraph, edge *cp.RunEdge, nodes map[string]*cp.RunNode, runs map[string]*cp.Run) bool {
	proof := edge.GetServerWorkflowRetry()
	if proof == nil || len(proof.ProtoReflect().GetUnknown()) != 0 || !safeRef.MatchString(proof.GetWorkflowVersionRef()) ||
		!safeRef.MatchString(proof.GetOriginNodeRef()) || !workflowRetryReadDigest.MatchString(proof.GetWorkflowVersionDigest()) ||
		!workflowRetryReadStep.MatchString(proof.GetStepKey()) || proof.GetPreviousBusinessAttempt() < 1 ||
		proof.GetBusinessAttempt() != proof.GetPreviousBusinessAttempt()+1 || proof.GetBusinessAttempt() > 4 {
		return false
	}
	old, fresh, origin := nodes[edge.GetSourceNodeRef()], nodes[edge.GetTargetNodeRef()], nodes[proof.GetOriginNodeRef()]
	parent := nodes[origin.GetParentNodeRef()]
	root, oldRun, newRun := runs[graph.GetRunRef()], runs[old.GetRunRef()], runs[fresh.GetRunRef()]
	if edge.GetType() != cp.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF || edge.GetRunRef() != graph.GetRunRef() ||
		root.GetRef() != graph.GetRunRef() || root.GetRootRunRef() != graph.GetRunRef() || root.GetTarget().GetWorkflowRef() == "" ||
		old == nil || fresh == nil || origin == nil || old.GetType() != cp.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION ||
		fresh.GetType() != old.GetType() || origin.GetType() != old.GetType() || old.GetAgentRef() == "" ||
		old.GetAgentRef() != fresh.GetAgentRef() || origin.GetAgentRef() != old.GetAgentRef() || fresh.GetAttempt() != 1 ||
		fresh.GetParentNodeRef() != origin.GetParentNodeRef() ||
		parent.GetType() != cp.RunNodeType_RUN_NODE_TYPE_ROOT_PROCESS || parent.GetParentNodeRef() != "" || parent.GetRunRef() != graph.GetRunRef() ||
		oldRun == nil || newRun == nil || oldRun.GetRef() == newRun.GetRef() || oldRun.GetRef() == root.GetRef() || newRun.GetRef() == root.GetRef() {
		return false
	}
	for _, run := range []*cp.Run{oldRun, newRun} {
		if run.GetRootRunRef() != root.GetRef() || run.GetParentRunRef() != root.GetRef() || run.GetProjectRef() != root.GetProjectRef() ||
			run.GetSource() != cp.RunSource_RUN_SOURCE_AGENT_DELEGATION || run.GetTarget().GetAgentRef() != old.GetAgentRef() {
			return false
		}
	}
	if newRun.GetRetryOfRunRef() != "" || newRun.GetAttempt() != 1 {
		return false
	}
	if !serverWorkflowRetrySnapshotSourcePath(graph, edge, nodes) {
		return false
	}
	if len(origin.GetWorkflowResults()) < int(proof.GetPreviousBusinessAttempt()) || len(origin.GetWorkflowResults()) > 4 {
		return false
	}
	if origin.GetWorkflowResults()[0].GetRunRef() != origin.GetRunRef() {
		return false
	}
	matched := 0
	historyRuns := map[string]bool{}
	for index, receipt := range origin.GetWorkflowResults() {
		if receipt.GetAttempt() != int32(index+1) || !safeRef.MatchString(receipt.GetRunRef()) || historyRuns[receipt.GetRunRef()] {
			return false
		}
		historyRuns[receipt.GetRunRef()] = true
		if receipt.GetAttempt() == proof.GetPreviousBusinessAttempt() {
			if receipt.GetRunRef() != old.GetRunRef() {
				return false
			}
			matched++
		}
	}
	return matched == 1
}

// serverWorkflowRetrySnapshotSourcePath binds a technical final to its exact first physical
// business node through a unique backwards same-run CONTINUES chain, bounded by graph size.
// serverWorkflowRetrySnapshotSourcePath связывает technical final с точным первым physical
// business node единственной обратной same-run CONTINUES цепью, ограниченной размером графа.
func serverWorkflowRetrySnapshotSourcePath(graph *cp.RunGraph, retry *cp.RunEdge, nodes map[string]*cp.RunNode) bool {
	proof := retry.GetServerWorkflowRetry()
	if proof == nil {
		return false
	}
	origin, current := nodes[proof.GetOriginNodeRef()], nodes[retry.GetSourceNodeRef()]
	runRef, agentRef := current.GetRunRef(), current.GetAgentRef()
	seen := map[string]bool{}
	for len(seen) < len(nodes) {
		if current == nil || seen[current.GetRef()] || current.GetType() != cp.RunNodeType_RUN_NODE_TYPE_AGENT_EXECUTION ||
			current.GetRunRef() != runRef || current.GetAgentRef() != agentRef || current.GetAttempt() < 1 {
			return false
		}
		seen[current.GetRef()] = true
		incoming := []*cp.RunEdge{}
		for _, candidate := range graph.GetEdges() {
			if candidate.GetType() == cp.RunEdgeType_RUN_EDGE_TYPE_CONTINUES && candidate.GetTargetNodeRef() == current.GetRef() {
				incoming = append(incoming, candidate)
			}
		}
		if current.GetAttempt() == 1 {
			if len(incoming) != 0 || current.GetParentNodeRef() != origin.GetParentNodeRef() {
				return false
			}
			if proof.GetPreviousBusinessAttempt() == 1 {
				return current.GetRef() == origin.GetRef()
			}
			count := 0
			for _, candidate := range graph.GetEdges() {
				if candidate.GetType() != cp.RunEdgeType_RUN_EDGE_TYPE_RETRY_OF || candidate.GetTargetNodeRef() != current.GetRef() {
					continue
				}
				prior := candidate.GetServerWorkflowRetry()
				if prior == nil || candidate.GetRunRef() != graph.GetRunRef() || prior.GetOriginNodeRef() != origin.GetRef() || prior.GetStepKey() != proof.GetStepKey() ||
					prior.GetWorkflowVersionRef() != proof.GetWorkflowVersionRef() || prior.GetWorkflowVersionDigest() != proof.GetWorkflowVersionDigest() ||
					prior.GetBusinessAttempt() != proof.GetPreviousBusinessAttempt() || prior.GetPreviousBusinessAttempt()+1 != prior.GetBusinessAttempt() {
					return false
				}
				count++
			}
			return count == 1
		}
		if len(incoming) != 1 || incoming[0].GetRunRef() != graph.GetRunRef() || incoming[0].GetSourceNodeRef() != current.GetParentNodeRef() {
			return false
		}
		previous := nodes[incoming[0].GetSourceNodeRef()]
		if previous.GetAttempt()+1 != current.GetAttempt() {
			return false
		}
		current = previous
	}
	return false
}
