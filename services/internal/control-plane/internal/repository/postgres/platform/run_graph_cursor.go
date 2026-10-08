package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/run_graph_cursor.sql
var queryRunGraphCursor string

// Вызывается только после разрешения requested Run внутри owner/tenant boundary.
// Root берётся из сохранённой lineage, а не из caller payload; читаются только
// счётчики общего графа, без подмены identity, lifecycle и permissions child Run.
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
