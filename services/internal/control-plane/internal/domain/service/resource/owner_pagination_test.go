package resource

import (
	"context"
	"strings"
	"testing"

	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

type ownerPaginationRepository struct {
	domainrepo.Repository
	resources     []entity.Resource
	resourceActor map[string]string
	incidents     []domainrepo.RuntimeIncident
	incidentActor map[string]string
	lastActor     string
}

func (repository *ownerPaginationRepository) List(_ context.Context, filter query.ResourceFilter) ([]entity.Resource, error) {
	repository.lastActor = filter.ActorID
	return repository.eligibleResources(filter.ActorID, "", filter.AfterID, filter.Limit), nil
}

func (repository *ownerPaginationRepository) Search(_ context.Context, filter query.ResourceSearch) ([]entity.Resource, error) {
	repository.lastActor = filter.ActorID
	return repository.eligibleResources(filter.ActorID, filter.Query, filter.AfterID, filter.Limit), nil
}

func (repository *ownerPaginationRepository) eligibleResources(actorID, search, afterID string, limit int) []entity.Resource {
	result := make([]entity.Resource, 0, limit)
	for _, resource := range repository.resources {
		if resource.ID <= afterID || repository.resourceActor[resource.ID] != actorID ||
			(search != "" && !strings.Contains(strings.ToLower(resource.Name), strings.ToLower(search))) {
			continue
		}
		result = append(result, resource)
		if len(result) == limit {
			break
		}
	}
	return result
}

func (repository *ownerPaginationRepository) ListRuntimeIncidents(_ context.Context, filter query.RuntimeIncidentFilter) ([]domainrepo.RuntimeIncident, error) {
	repository.lastActor = filter.ActorID
	result := make([]domainrepo.RuntimeIncident, 0, filter.Limit)
	for _, incident := range repository.incidents {
		if incident.ID <= filter.AfterID || repository.incidentActor[incident.ID] != filter.ActorID {
			continue
		}
		result = append(result, incident)
		if len(result) == filter.Limit {
			break
		}
	}
	return result, nil
}

func TestOwnerPaginationUsesVerifiedActorBeforePageLimit(t *testing.T) {
	t.Parallel()

	trustedActor := uuid.NewString()
	foreignActor := uuid.NewString()
	organizationID := uuid.NewString()
	projectID := uuid.NewString()
	ids := []string{
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000003",
		"00000000-0000-0000-0000-000000000004",
	}
	repository := &ownerPaginationRepository{
		resourceActor: map[string]string{ids[0]: foreignActor, ids[1]: foreignActor, ids[2]: trustedActor, ids[3]: trustedActor},
		incidentActor: map[string]string{ids[0]: foreignActor, ids[1]: foreignActor, ids[2]: trustedActor, ids[3]: trustedActor},
	}
	for index, id := range ids {
		repository.resources = append(repository.resources, entity.Resource{ID: id, Name: "run visible", OwnerActorID: repository.resourceActor[id], Kind: enum.KindProcessRun})
		repository.incidents = append(repository.incidents, domainrepo.RuntimeIncident{ID: id, ExecutionID: uuid.NewString()})
		_ = index
	}
	service := &Service{repository: repository}

	listPrincipal := ownerPaginationPrincipal(trustedActor, organizationID, projectID, permissionList)
	listed, err := service.List(context.Background(), ListInput{
		Principal: listPrincipal,
		Filter:    query.ResourceFilter{ActorID: foreignActor, Kind: enum.KindProcessRun, Limit: 1},
	})
	if err != nil || len(listed) != 1 || listed[0].ID != ids[2] || repository.lastActor != trustedActor {
		t.Fatalf("List must reach the first eligible row after foreign physical rows: rows=%v actor=%q err=%v", listed, repository.lastActor, err)
	}
	listed, err = service.List(context.Background(), ListInput{
		Principal: listPrincipal,
		Filter:    query.ResourceFilter{Kind: enum.KindProcessRun, AfterID: ids[2], Limit: 1},
	})
	if err != nil || len(listed) != 1 || listed[0].ID != ids[3] {
		t.Fatalf("List cursor must progress through eligible rows: rows=%v err=%v", listed, err)
	}

	searched, err := service.Search(context.Background(), SearchInput{
		Principal: ownerPaginationPrincipal(trustedActor, organizationID, projectID, permissionSearch),
		Filter:    query.ResourceSearch{ActorID: foreignActor, Kind: enum.KindProcessRun, Query: "visible", Limit: 1},
	})
	if err != nil || len(searched) != 1 || searched[0].ID != ids[2] || repository.lastActor != trustedActor {
		t.Fatalf("Search must apply trusted eligibility before LIMIT: rows=%v actor=%q err=%v", searched, repository.lastActor, err)
	}

	incidents, err := service.ListRuntimeIncidents(context.Background(), ListRuntimeIncidentsInput{
		Principal: ownerPaginationPrincipal(trustedActor, organizationID, projectID, permissionRuntimeIncidentRead),
		Filter:    query.RuntimeIncidentFilter{ActorID: foreignActor, Limit: 1},
	})
	if err != nil || len(incidents) != 1 || incidents[0].ID != ids[2] || repository.lastActor != trustedActor {
		t.Fatalf("incident page must exclude another actor before LIMIT: incidents=%v actor=%q err=%v", incidents, repository.lastActor, err)
	}
}

func ownerPaginationPrincipal(actorID, organizationID, projectID, permission string) value.Principal {
	return value.Principal{
		ActorID: actorID, OrganizationID: organizationID, ProjectID: projectID,
		Permission: permission, CorrelationID: uuid.NewString(), PolicyRevision: 1, AuthorityGeneration: 1,
		CallerWorkload: controlAPIGatewayWorkload, CallerSPIFFEID: controlAPIGatewaySPIFFEID,
		AuthoritySource: "OWNER_SESSION", AuthorityReference: uuid.NewString(), AuthorityRevision: 1,
		AuthorityDigest: strings.Repeat("a", 64),
	}
}
