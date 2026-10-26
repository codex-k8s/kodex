package platform

import (
	"context"
	"sort"
	"strconv"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

const maximumGraphRunSnapshots = 128

// attachGraphRuns использует тот же owner snapshot, что graph и storage:
// Run.version/root cursor не являются версией зависимого SessionStorage.
func (repository *Repository) attachGraphRuns(ctx context.Context, tx pgx.Tx, current scope, graph *entity.RunGraph) error {
	refs := map[string]bool{graph.RunRef: true}
	for _, node := range graph.Nodes {
		if !validOverlayHistoryRef(node.RunRef) {
			return errs.ErrUnavailable
		}
		refs[node.RunRef] = true
		for _, ref := range node.ChildRunRefs {
			if !validOverlayHistoryRef(ref) {
				return errs.ErrUnavailable
			}
			refs[ref] = true
		}
		if len(refs) > maximumGraphRunSnapshots {
			return errs.ErrUnavailable
		}
	}
	keys := make([]string, 0, len(refs))
	for ref := range refs {
		keys = append(keys, ref)
	}
	sort.Strings(keys)
	var projectRef string
	root, err := repository.readRunWithIncidents(ctx, tx, current, graph.RunRef)
	if err != nil || root.RootRunRef != graph.RunRef || root.Ref != graph.RunRef {
		return errs.ErrUnavailable
	}
	projectRef = root.ProjectRef
	values := make([]entity.Run, 0, len(keys))
	byRef := make(map[string]entity.Run, len(keys))
	for _, ref := range keys {
		item, err := repository.readRunWithIncidents(ctx, tx, current, ref)
		if err != nil {
			return err
		}
		if item.Ref != ref || item.ProjectRef != projectRef {
			return errs.ErrUnavailable
		}
		if err := attachRunSessionReadiness(ctx, tx, current, &item); err != nil {
			return err
		}
		values = append(values, item)
		byRef[item.Ref] = item
	}
	reworkLinks, err := readLegacyReworkGraphLinksTx(ctx, tx, current, graph, byRef)
	if err != nil {
		return err
	}
	predecessors, valid := graphRetryPredecessorsWithRework(graph, byRef, reworkLinks)
	if !valid {
		return errs.ErrUnavailable
	}
	for _, item := range values {
		crossRoot := item.Ref == item.RootRunRef && item.ParentRunRef != item.Ref && refs[item.ParentRunRef]
		if item.RootRunRef != graph.RunRef && !crossRoot && !predecessors[item.Ref] {
			return errs.ErrUnavailable
		}
	}
	graph.Runs = values
	return nil
}

// Retry/continuation lineage подтверждается owner rows и серверным graph edge.
// Обход ограничен уже прочитанными refs: дополнительные run reads не выполняются.
func graphRetryPredecessors(graph *entity.RunGraph, runs map[string]entity.Run) (map[string]bool, bool) {
	return graphRetryPredecessorsWithRework(graph, runs, nil)
}

// graphRetryPredecessorsWithRework keeps the original cross-root checks and admits only private owner-proved legacy child links.
// graphRetryPredecessorsWithRework сохраняет прежние cross-root проверки и допускает лишь private owner-proved legacy child links.
func graphRetryPredecessorsWithRework(graph *entity.RunGraph, runs map[string]entity.Run, reworkLinks map[string]legacyReworkGraphLink) (map[string]bool, bool) {
	nodes := make(map[string]entity.RunNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes[node.Ref] = node
	}
	links := map[string]string{}
	predecessors := map[string]bool{}
	serverTargets, serverSlots := map[string]bool{}, map[string]bool{}
	serverVersion := ""
	for _, edge := range graph.Edges {
		if proof := edge.ServerWorkflowRetry; proof != nil {
			version := proof.WorkflowVersionRef + ":" + proof.WorkflowVersionDigest
			slot := proof.OriginNodeRef + ":" + strconv.FormatInt(int64(proof.BusinessAttempt), 10)
			if !validWorkflowRetrySnapshot(graph, edge, nodes, runs) || serverTargets[edge.TargetNodeRef] || serverSlots[slot] ||
				serverVersion != "" && serverVersion != version {
				return nil, false
			}
			serverVersion = version
			serverTargets[edge.TargetNodeRef], serverSlots[slot] = true, true
			continue
		}
		if edge.Type != "RETRY_OF" && edge.Type != "CONTINUES" {
			continue
		}
		oldNode, oldOK := nodes[edge.SourceNodeRef]
		newNode, newOK := nodes[edge.TargetNodeRef]
		// CONTINUES внутри одного run также связывает agent attempts. Только
		// отдельное root-to-root ребро доказывает predecessor другой root lineage.
		if edge.Type == "CONTINUES" && oldOK && newOK && oldNode.RunRef == newNode.RunRef {
			continue
		}
		old, oldRunOK := runs[oldNode.RunRef]
		fresh, newRunOK := runs[newNode.RunRef]
		if proof, exists := reworkLinks[edge.Ref]; exists {
			if !oldOK || !newOK || !oldRunOK || !newRunOK || links[fresh.Ref] != "" || !validLegacyReworkGraphLink(graph.RunRef, edge, oldNode, newNode, old, fresh, proof) {
				return nil, false
			}
			links[fresh.Ref], predecessors[old.Ref] = old.Ref, true
			continue
		}
		if !oldOK || !newOK || !oldRunOK || !newRunOK || !validPredecessorAnchors(edge.Type, oldNode, newNode, old, fresh) || old.RootRunRef != old.Ref || fresh.RootRunRef != fresh.Ref || edge.RunRef != fresh.Ref || old.Ref == fresh.Ref || links[fresh.Ref] != "" {
			return nil, false
		}
		if (edge.Type == "RETRY_OF" && fresh.RetryOfRunRef != old.Ref) ||
			(edge.Type == "CONTINUES" && (old.SessionRef == "" || fresh.SessionRef != old.SessionRef || old.State != "SUCCEEDED" || fresh.RetryOfRunRef != "")) {
			return nil, false
		}
		links[fresh.Ref] = old.Ref
		predecessors[old.Ref] = true
	}
	for _, run := range runs {
		if run.RetryOfRunRef != "" {
			if _, present := runs[run.RetryOfRunRef]; present && links[run.Ref] != run.RetryOfRunRef {
				return nil, false
			}
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
	return predecessors, true
}

// validPredecessorAnchors accepts ordinary process roots or exact assistant retry anchors. Assistant identity, session and increasing attempt come from owner-read runs; no caller field grants cross-root access.
// validPredecessorAnchors принимает корни обычного процесса либо точные узлы повтора помощника. Помощник, сессия и возрастающая попытка берутся из прочитанных владельцем запусков; поле клиента не даёт межкорневого доступа.
func validPredecessorAnchors(edgeType string, oldNode, newNode entity.RunNode, old, fresh entity.Run) bool {
	if oldNode.Type == "ROOT_PROCESS" && newNode.Type == "ROOT_PROCESS" {
		return true
	}
	if edgeType != "RETRY_OF" || oldNode.Type != "AGENT_EXECUTION" || newNode.Type != "AGENT_EXECUTION" || oldNode.ParentNodeRef != "" || newNode.ParentNodeRef != "" {
		return false
	}
	pin, next := old.AssistantPin, fresh.AssistantPin
	return old.Source == "SYSTEM_ASSISTANT" && fresh.Source == "SYSTEM_ASSISTANT" &&
		old.Target.Type == "SYSTEM_ASSISTANT" && fresh.Target.Type == "SYSTEM_ASSISTANT" &&
		pin != nil && next != nil && *pin == *next && pin.AssistantRef != "" &&
		old.Target.Ref == pin.AssistantRef && fresh.Target.Ref == pin.AssistantRef &&
		oldNode.AgentRef == pin.AssistantRef && newNode.AgentRef == pin.AssistantRef &&
		old.SessionRef != "" && old.SessionRef == fresh.SessionRef && old.Attempt > 0 && fresh.Attempt > old.Attempt && fresh.Attempt-old.Attempt == 1
}
