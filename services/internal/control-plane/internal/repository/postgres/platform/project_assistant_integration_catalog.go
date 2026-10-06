package platform

import (
	"context"
	_ "embed"
	"sort"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/project_assistant_integration_grants__connections.sql
var queryProjectAssistantIntegrationGrantConnections string

func (repository *Repository) projectAssistantIntegrationCatalogTx(ctx context.Context, tx pgx.Tx, current scope, input entity.AssistantConfigurationCatalogRequest) ([]entity.ProjectAssistantIntegrationGrantCatalogEntry, int32, error) {
	rows, err := tx.Query(ctx, queryProjectAssistantIntegrationGrantConnections, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "assistant_ref": input.AssistantRef})
	if err != nil {
		return nil, 0, errs.ErrUnavailable
	}
	type connection struct{ ref, name string }
	connections := []connection{}
	for rows.Next() {
		var item connection
		if rows.Scan(&item.ref, &item.name) != nil {
			rows.Close()
			return nil, 0, errs.ErrUnavailable
		}
		connections = append(connections, item)
	}
	rows.Close()
	if rows.Err() != nil || len(connections) > 100 {
		return nil, 0, errs.ErrUnavailable
	}
	items := []entity.ProjectAssistantIntegrationGrantCatalogEntry{}
	for _, connection := range connections {
		candidates, err := repository.projectAssistantIntegrationGrantCandidatesTx(ctx, tx, current, input.AssistantRef, connection.ref, "", query.Page{Size: 100})
		if err != nil {
			return nil, 0, err
		}
		if candidates.NextPageToken != "" {
			return nil, 0, errs.ErrUnavailable
		}
		for _, candidate := range candidates.Items {
			if input.Query != "" && !strings.Contains(strings.ToLower(connection.name+" "+candidate.Capability.Key+" "+candidate.Capability.Name), strings.ToLower(input.Query)) {
				continue
			}
			items = append(items, entity.ProjectAssistantIntegrationGrantCatalogEntry{ConnectionRef: connection.ref, ConnectionName: connection.name,
				ConnectionVersion: candidates.ConnectionVersion, DefinitionVersion: candidates.DefinitionVersion, DefinitionDigest: candidates.DefinitionDigest, Candidate: candidate})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].ConnectionRef == items[j].ConnectionRef {
			return items[i].Candidate.Capability.Key < items[j].Candidate.Capability.Key
		}
		return items[i].ConnectionRef < items[j].ConnectionRef
	})
	if int(input.Offset) >= len(items) {
		return []entity.ProjectAssistantIntegrationGrantCatalogEntry{}, 0, nil
	}
	items = items[input.Offset:]
	next := int32(0)
	if len(items) > 10 {
		items = items[:10]
		next = input.Offset + 10
	}
	return items, next, nil
}
