package platform

import (
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// projectChildRetryGraph omits the unrelated process root from a child retry graph.
// projectChildRetryGraph исключает прежний процесс из графа повторяемого дочернего
// запуска. Точная связь с исходным запуском остаётся в Run.RetryOfRunRef; история
// событий не переписывается. Обычный повтор корня сохраняет графовое ребро.
func projectChildRetryGraph(graph *entity.RunGraph, current, previous entity.Run) error {
	if current.RetryOfRunRef != previous.Ref || current.ProjectRef != previous.ProjectRef {
		return errs.ErrUnavailable
	}
	if previous.RootRunRef == previous.Ref {
		return nil
	}
	nodes := make(map[string]entity.RunNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes[node.Ref] = node
	}
	removed := map[string]bool{}
	edges := make([]entity.RunEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		if edge.Type != "RETRY_OF" || edge.RunRef != current.Ref {
			edges = append(edges, edge)
			continue
		}
		old, fresh := nodes[edge.SourceNodeRef], nodes[edge.TargetNodeRef]
		if old.Type != "ROOT_PROCESS" || old.RunRef != previous.RootRunRef || fresh.Type != "ROOT_PROCESS" || fresh.RunRef != current.Ref {
			return errs.ErrUnavailable
		}
		removed[old.Ref] = true
	}
	for _, edge := range edges {
		if removed[edge.SourceNodeRef] || removed[edge.TargetNodeRef] {
			return errs.ErrUnavailable
		}
	}
	kept := make([]entity.RunNode, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if !removed[node.Ref] {
			kept = append(kept, node)
		}
	}
	graph.Nodes, graph.Edges = kept, edges
	return nil
}
