package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRunGraphCursor embeds exact child/root graph counters under organization, same project and
// canonical root identity. It returns only root ref, graph revision and event sequence; resource
// authorization must already be established for the requested run by the caller.
// queryRunGraphCursor встраивает точные child/root graph counters внутри organization, того же
// project и canonical root identity. Возвращает только root ref, graph revision и event sequence;
// вызывающий обязан заранее установить resource authorization запрошенного run.
//
//go:embed sql/run_graph_cursor.sql
var queryRunGraphCursor string

// readRootRunGraphCursor builds a root graph cursor from the already-authorized run lineage. A
// child requires an exact SQL root read under tenant/project/ref pins; a root uses the supplied
// counters. Missing refs, read/ref mismatch, nonpositive revision or negative sequence is
// Unavailable. It neither authorizes another run nor changes child identity, lifecycle,
// permissions or graph state.
// readRootRunGraphCursor строит root graph cursor из lineage уже разрешённого run. Для child
// требует точного SQL-чтения root внутри tenant/project/ref pins; для root использует переданные
// counters. Отсутствующие refs, read/ref mismatch, неположительная revision или отрицательная
// sequence дают Unavailable. Не авторизует другой run и не меняет child identity, lifecycle,
// permissions или graph state.
func readRootRunGraphCursor(ctx context.Context, runner queryRunner, scope scope, run entity.Run) (entity.RunGraph, error) {
	graph := entity.RunGraph{RunRef: run.RootRunRef, Revision: run.GraphRevision, Sequence: run.EventSequence}
	if run.Ref == "" || graph.RunRef == "" {
		return entity.RunGraph{}, errs.ErrUnavailable
	}
	if run.Ref != graph.RunRef {
		var rootRef string
		if err := runner.QueryRow(ctx, queryRunGraphCursor, pgx.StrictNamedArgs{
			"organization_id": scope.organizationID,
			"run_ref":         run.Ref,
			"root_run_ref":    run.RootRunRef,
		}).Scan(&rootRef, &graph.Revision, &graph.Sequence); err != nil || rootRef != graph.RunRef {
			return entity.RunGraph{}, errs.ErrUnavailable
		}
	}
	if graph.Revision < 1 || graph.Sequence < 0 {
		return entity.RunGraph{}, errs.ErrUnavailable
	}
	return graph, nil
}
