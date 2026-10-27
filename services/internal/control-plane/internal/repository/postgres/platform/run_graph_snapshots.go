package platform

import (
	"context"
	"sort"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// maximumGraphRunSnapshots bounds distinct root/node/child run snapshots read for one graph to128.
// It limits projection work, not the number of executable child runs or a caller permission scope.
// maximumGraphRunSnapshots ограничивает число различных снимков root/node/child запусков одного
// графа до128. Ограничивает работу проекции, а не число исполнимых дочерних запусков и не область
// прав вызывающего субъекта.
const maximumGraphRunSnapshots = 128

// attachGraphRuns collects bounded valid node/child refs, sorts them and reads root plus
// run/session readiness within the caller snapshot. It enforces exact project and
// root/parent/retry-predecessor lineage before assigning graph.Runs. Root cursor/version is not a
// SessionStorage version; errors do not publish a partial graph run list or commit.
// attachGraphRuns собирает ограниченные корректные ссылки узлов и дочерних запусков, сортирует и
// читает root и run/session readiness в снимке вызывающего кода. Проверяет точные проект и
// root/parent/retry-predecessor связи до назначения graph.Runs. Версия и курсор root не являются
// версией SessionStorage; ошибки не публикуют частичный список запусков графа и не завершают
// транзакцию.
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

// graphRetryPredecessors validates root-to-root RETRY_OF/CONTINUES edges against already read
// owner run rows, rejecting ambiguous predecessor links, mismatched retry/session/state and
// cycles. Same-run CONTINUES attempt edges are ignored. It returns admitted predecessor refs
// without extra reads or proving the entire graph execution lifecycle.
// graphRetryPredecessors проверяет root-to-root рёбра RETRY_OF/CONTINUES по уже прочитанным
// строкам владельца, отклоняя неоднозначных предшественников, несовпадение retry/session/state и
// циклы. Внутренние CONTINUES рёбра попыток одного запуска пропускаются. Возвращает допустимые
// ссылки предшественников без дополнительных чтений и без доказательства всего жизненного цикла
// графа.
func graphRetryPredecessors(graph *entity.RunGraph, runs map[string]entity.Run) (map[string]bool, bool) {
	nodes := make(map[string]entity.RunNode, len(graph.Nodes))
	for _, node := range graph.Nodes {
		nodes[node.Ref] = node
	}
	links := map[string]string{}
	predecessors := map[string]bool{}
	for _, edge := range graph.Edges {
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
		if !oldOK || !newOK || !oldRunOK || !newRunOK || oldNode.Type != "ROOT_PROCESS" || newNode.Type != "ROOT_PROCESS" || old.RootRunRef != old.Ref || fresh.RootRunRef != fresh.Ref || edge.RunRef != fresh.Ref || old.Ref == fresh.Ref || links[fresh.Ref] != "" {
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
