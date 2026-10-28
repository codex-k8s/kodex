package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryLegacyReworkGraphLinks proves child retry edges from stored owner gates; its private pins never enter a public graph DTO.
// queryLegacyReworkGraphLinks доказывает child retry edges по сохранённым owner Gates; private pins не входят в публичный DTO.
//
//go:embed sql/legacy_workflow_rework__graph_links.sql
var queryLegacyReworkGraphLinks string

// queryLegacyReworkInsertRetry appends an exact physical retry edge without modifying either historical endpoint.
// queryLegacyReworkInsertRetry добавляет точное physical retry ребро без изменения исторических концов.
//
//go:embed sql/legacy_workflow_rework__insert_retry.sql
var queryLegacyReworkInsertRetry string

// legacyReworkGraphLink is an owner-read proof, not authority inferred from a caller's visible edge.
// legacyReworkGraphLink является owner-read доказательством, а не authority из видимого ребра вызывающего.
type legacyReworkGraphLink struct {
	PriorRun, Run, PriorNode, Node string
}

// readLegacyReworkGraphLinksTx fetches private proofs only for a graph containing same-root child retry runs.
// readLegacyReworkGraphLinksTx читает private proofs лишь для графа с same-root child retry runs.
func readLegacyReworkGraphLinksTx(ctx context.Context, tx pgx.Tx, current scope, graph *entity.RunGraph, runs map[string]entity.Run) (map[string]legacyReworkGraphLink, error) {
	needed := false
	for _, run := range runs {
		needed = needed || run.Ref != graph.RunRef && run.RootRunRef == graph.RunRef && run.RetryOfRunRef != ""
	}
	if !needed {
		return nil, nil
	}
	rows, err := tx.Query(ctx, queryLegacyReworkGraphLinks, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_ref": graph.RunRef})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	links := map[string]legacyReworkGraphLink{}
	for rows.Next() {
		var ref string
		var link legacyReworkGraphLink
		if rows.Scan(&ref, &link.PriorRun, &link.Run, &link.PriorNode, &link.Node) != nil {
			return nil, errs.ErrUnavailable
		}
		if _, exists := links[ref]; exists {
			return nil, errs.ErrUnavailable
		}
		links[ref] = link
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return links, nil
}

// validLegacyReworkGraphLink requires exact owner pins and same-root physical retry identity; ordinary child retry remains rejected without a proof.
// validLegacyReworkGraphLink требует точные owner pins и same-root physical retry; без proof обычный child retry отклоняется.
func validLegacyReworkGraphLink(graphRef string, edge entity.RunEdge, oldNode, newNode entity.RunNode, prior, fresh entity.Run, proof legacyReworkGraphLink) bool {
	return edge.Type == "RETRY_OF" && edge.RunRef == graphRef && prior.RootRunRef == graphRef && fresh.RootRunRef == graphRef &&
		prior.Ref != fresh.Ref && prior.Ref != graphRef && fresh.Ref != graphRef && prior.ProjectRef == fresh.ProjectRef &&
		fresh.RetryOfRunRef == prior.Ref && fresh.Attempt == prior.Attempt+1 && prior.Attempt > 0 &&
		oldNode.Type == "AGENT_EXECUTION" && newNode.Type == "AGENT_EXECUTION" && oldNode.State == "SUCCEEDED" &&
		oldNode.AgentRef != "" && oldNode.AgentRef == newNode.AgentRef && oldNode.ParentNodeRef != "" && newNode.ParentNodeRef != "" &&
		proof.PriorRun == prior.Ref && proof.Run == fresh.Ref && proof.PriorNode == oldNode.Ref && proof.Node == newNode.Ref
}
