package platform

import (
	"context"
	"sort"

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
	predecessors, valid := graphRetryPredecessors(graph, byRef)
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

// Retry lineage подтверждается одновременно owner rows и серверным graph edge.
// Обход ограничен уже прочитанными refs: дополнительные run reads не выполняются.
func graphRetryPredecessors(graph *entity.RunGraph, runs map[string]entity.Run) (map[string]bool, bool) {
	nodes := make(map[string]entity.RunNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes[node.Ref] = node
	}
	links := map[string]string{}
	predecessors := map[string]bool{}
	for _, edge := range graph.Edges {
		if edge.Type != "RETRY_OF" {
			continue
		}
		oldNode, oldOK := nodes[edge.SourceNodeRef]
		newNode, newOK := nodes[edge.TargetNodeRef]
		old, oldRunOK := runs[oldNode.RunRef]
		fresh, newRunOK := runs[newNode.RunRef]
		if !oldOK || !newOK || !oldRunOK || !newRunOK || oldNode.Type != "ROOT_PROCESS" || newNode.Type != "ROOT_PROCESS" || old.RootRunRef != old.Ref || fresh.RootRunRef != fresh.Ref || fresh.RetryOfRunRef != old.Ref || edge.RunRef != fresh.Ref || old.Ref == fresh.Ref || links[fresh.Ref] != "" {
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
