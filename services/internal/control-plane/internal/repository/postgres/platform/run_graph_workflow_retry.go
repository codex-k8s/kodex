package platform

import (
	"context"
	_ "embed"
	"encoding/hex"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowSnapshotRetryProofs reads historical owner lineage after canonical graph eligibility;
// completed leases and immutable journal entries are evidence, never renewed execution authority.
// queryWorkflowSnapshotRetryProofs читает историческую owner lineage после допуска к графу;
// completed leases и immutable journal являются доказательствами, не новыми полномочиями исполнения.
//
//go:embed sql/workflow_snapshot__retry_proofs.sql
var queryWorkflowSnapshotRetryProofs string

// attachWorkflowRetryProofs projects only exact owner-proved child retry edges from this read-only
// snapshot. Ordinary graphs issue no extra query; invalid or duplicate owner proof fails closed.
// attachWorkflowRetryProofs проецирует лишь точно доказанные владельцем child retry из read-only
// снимка. Обычный граф не требует нового запроса; неверный или повторный proof закрыто отклоняется.
func attachWorkflowRetryProofs(ctx context.Context, tx pgx.Tx, current scope, graph *entity.RunGraph) error {
	nodes := make(map[string]entity.RunNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes[node.Ref] = node
	}
	candidates := make(map[string]int)
	for index, edge := range graph.Edges {
		old, fresh := nodes[edge.SourceNodeRef], nodes[edge.TargetNodeRef]
		if edge.Type == "RETRY_OF" && old.Type == "AGENT_EXECUTION" && fresh.Type == "AGENT_EXECUTION" &&
			old.RunRef != fresh.RunRef && old.ParentNodeRef != "" && fresh.ParentNodeRef != "" {
			if _, duplicate := candidates[edge.Ref]; duplicate {
				return errs.ErrUnavailable
			}
			candidates[edge.Ref] = index
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, queryWorkflowSnapshotRetryProofs, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_ref": graph.RunRef,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	defer rows.Close()
	seen := make(map[string]bool)
	for rows.Next() {
		var ref string
		var proof entity.WorkflowRetryReadProof
		if rows.Scan(&ref, &proof.WorkflowVersionRef, &proof.WorkflowVersionDigest, &proof.OriginNodeRef,
			&proof.StepKey, &proof.PreviousBusinessAttempt, &proof.BusinessAttempt) != nil {
			return errs.ErrUnavailable
		}
		index, exists := candidates[ref]
		if !exists || seen[ref] || len(seen) >= workflowservice.MaximumSteps*(workflowservice.MaximumAttempts-1) {
			return errs.ErrUnavailable
		}
		seen[ref] = true
		graph.Edges[index].ServerWorkflowRetry = &proof
	}
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// validWorkflowRetrySnapshot binds an owner-issued proof to public graph and run facts without
// replacing private journal verification or granting access to any additional run.
// validWorkflowRetrySnapshot связывает owner proof с публичными graph/run facts, не заменяя
// приватную проверку journal и не выдавая доступа к дополнительным запускам.
func validWorkflowRetrySnapshot(graph *entity.RunGraph, edge entity.RunEdge, nodes map[string]entity.RunNode, runs map[string]entity.Run) bool {
	proof := edge.ServerWorkflowRetry
	if proof == nil || !validOverlayHistoryRef(proof.WorkflowVersionRef) || !validOverlayHistoryRef(proof.OriginNodeRef) ||
		len(proof.WorkflowVersionDigest) != 64 || proof.WorkflowVersionDigest != strings.ToLower(proof.WorkflowVersionDigest) ||
		proof.PreviousBusinessAttempt < 1 || proof.BusinessAttempt != proof.PreviousBusinessAttempt+1 || proof.BusinessAttempt > 4 ||
		len(proof.StepKey) < 1 || len(proof.StepKey) > 96 {
		return false
	}
	if _, err := hex.DecodeString(proof.WorkflowVersionDigest); err != nil {
		return false
	}
	for _, key := range proof.StepKey {
		if !(key >= 'a' && key <= 'z' || key >= 'A' && key <= 'Z' || key >= '0' && key <= '9' || key == '_' || key == '-') {
			return false
		}
	}
	old, fresh, origin := nodes[edge.SourceNodeRef], nodes[edge.TargetNodeRef], nodes[proof.OriginNodeRef]
	parent := nodes[origin.ParentNodeRef]
	root, oldRun, newRun := runs[graph.RunRef], runs[old.RunRef], runs[fresh.RunRef]
	if edge.Type != "RETRY_OF" || edge.RunRef != graph.RunRef || root.Ref != graph.RunRef || root.RootRunRef != graph.RunRef || root.Target.Type != "WORKFLOW" ||
		old.Ref == "" || fresh.Ref == "" || origin.Ref == "" || old.Type != "AGENT_EXECUTION" || fresh.Type != old.Type || origin.Type != old.Type ||
		old.AgentRef == "" || old.AgentRef != fresh.AgentRef || origin.AgentRef != old.AgentRef || fresh.Attempt != 1 ||
		fresh.ParentNodeRef != origin.ParentNodeRef ||
		parent.Type != "ROOT_PROCESS" || parent.ParentNodeRef != "" || parent.RunRef != graph.RunRef ||
		oldRun.Ref == "" || newRun.Ref == "" || oldRun.Ref == newRun.Ref || oldRun.Ref == root.Ref || newRun.Ref == root.Ref {
		return false
	}
	for _, run := range []entity.Run{oldRun, newRun} {
		if run.RootRunRef != root.Ref || run.ParentRunRef != root.Ref || run.ProjectRef != root.ProjectRef ||
			run.Source != "AGENT_DELEGATION" || run.Target.Type != "AGENT" || run.Target.Ref != old.AgentRef {
			return false
		}
	}
	if newRun.RetryOfRunRef != "" || newRun.Attempt != 1 {
		return false
	}
	if !workflowRetrySnapshotSourcePath(graph, edge, nodes) {
		return false
	}
	if len(origin.WorkflowResults) < int(proof.PreviousBusinessAttempt) || len(origin.WorkflowResults) > 4 {
		return false
	}
	if origin.WorkflowResults[0].RunRef != origin.RunRef {
		return false
	}
	matched := 0
	historyRuns := map[string]bool{}
	for index, receipt := range origin.WorkflowResults {
		if receipt.Attempt != int32(index+1) || !validOverlayHistoryRef(receipt.RunRef) || historyRuns[receipt.RunRef] {
			return false
		}
		historyRuns[receipt.RunRef] = true
		if receipt.Attempt == proof.PreviousBusinessAttempt {
			if receipt.RunRef != old.RunRef {
				return false
			}
			matched++
		}
	}
	return matched == 1
}

// workflowRetrySnapshotSourcePath follows only the previous business attempt's exact backwards
// same-run CONTINUES chain. Graph cardinality bounds the walk; prior business anchors need their
// own typed retry edge, rather than a guessed root parent on a technical final.
// workflowRetrySnapshotSourcePath проходит лишь точную обратную same-run CONTINUES цепь прежней
// business attempt. Обход ограничен размером графа; прежние business anchors требуют собственного
// typed retry edge вместо выдуманного root parent у technical final.
func workflowRetrySnapshotSourcePath(graph *entity.RunGraph, retry entity.RunEdge, nodes map[string]entity.RunNode) bool {
	proof := retry.ServerWorkflowRetry
	if proof == nil {
		return false
	}
	origin, current := nodes[proof.OriginNodeRef], nodes[retry.SourceNodeRef]
	runRef, agentRef := current.RunRef, current.AgentRef
	seen := map[string]bool{}
	for len(seen) < len(nodes) {
		if current.Ref == "" || seen[current.Ref] || current.Type != "AGENT_EXECUTION" || current.RunRef != runRef || current.AgentRef != agentRef || current.Attempt < 1 {
			return false
		}
		seen[current.Ref] = true
		incoming := []entity.RunEdge{}
		for _, candidate := range graph.Edges {
			if candidate.Type == "CONTINUES" && candidate.TargetNodeRef == current.Ref {
				incoming = append(incoming, candidate)
			}
		}
		if current.Attempt == 1 {
			if len(incoming) != 0 || current.ParentNodeRef != origin.ParentNodeRef {
				return false
			}
			if proof.PreviousBusinessAttempt == 1 {
				return current.Ref == origin.Ref
			}
			count := 0
			for _, candidate := range graph.Edges {
				if candidate.Type != "RETRY_OF" || candidate.TargetNodeRef != current.Ref {
					continue
				}
				prior := candidate.ServerWorkflowRetry
				if prior == nil || candidate.RunRef != graph.RunRef || prior.OriginNodeRef != origin.Ref || prior.StepKey != proof.StepKey ||
					prior.WorkflowVersionRef != proof.WorkflowVersionRef || prior.WorkflowVersionDigest != proof.WorkflowVersionDigest ||
					prior.BusinessAttempt != proof.PreviousBusinessAttempt || prior.PreviousBusinessAttempt+1 != prior.BusinessAttempt {
					return false
				}
				count++
			}
			return count == 1
		}
		if len(incoming) != 1 || incoming[0].RunRef != graph.RunRef || incoming[0].SourceNodeRef != current.ParentNodeRef {
			return false
		}
		previous := nodes[incoming[0].SourceNodeRef]
		if previous.Attempt+1 != current.Attempt {
			return false
		}
		current = previous
	}
	return false
}
