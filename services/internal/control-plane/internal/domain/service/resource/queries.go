package resource

import (
	"context"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
)

const (
	permissionSearch       = "controlplane.resource.search"
	permissionAuditRead    = "controlplane.audit.read"
	permissionTombstones   = "controlplane.tombstone.read"
	permissionDiagnostics  = "controlplane.diagnostics.read"
	permissionScheduleRead = "controlplane.schedule.read"
)

func (service *Service) Search(
	ctx context.Context,
	input SearchInput,
) ([]entity.Resource, error) {
	if err := authorize(input.Principal, permissionSearch); err != nil {
		return nil, err
	}
	input.Filter.OrganizationID = input.Principal.OrganizationID
	input.Filter.ProjectID = input.Principal.ProjectID
	input.Filter.ActorID = input.Principal.ActorID
	if input.Principal.ProjectID == "" ||
		input.Filter.Kind == enum.KindProject ||
		input.Filter.Validate() != nil {
		return nil, errs.ErrInvalidInput
	}
	if input.Filter.Kind == enum.KindMemoryRecord {
		hits, err := service.searchEligibleMemory(
			ctx,
			input.Principal,
			domainrepo.MemorySearch{
				Query:        input.Filter.Query,
				States:       input.Filter.States,
				AfterID:      input.Filter.AfterID,
				Limit:        input.Filter.Limit,
				GenericOrder: true,
			},
		)
		if err != nil {
			return nil, err
		}
		resources := make([]entity.Resource, 0, len(hits))
		for _, hit := range hits {
			resources = append(resources, hit.Resource)
		}
		return resources, nil
	}
	return service.repository.Search(ctx, input.Filter)
}

func (service *Service) ListScheduleOccurrences(
	ctx context.Context,
	input ListScheduleOccurrencesInput,
) ([]domainrepo.ScheduleOccurrence, error) {
	if err := authorize(input.Principal, permissionScheduleRead); err != nil {
		return nil, err
	}
	input.Filter.OrganizationID = input.Principal.OrganizationID
	input.Filter.ProjectID = input.Principal.ProjectID
	if input.Principal.ProjectID == "" || input.Filter.Validate() != nil {
		return nil, errs.ErrInvalidInput
	}
	schedule, err := service.repository.Get(
		ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
		input.Filter.ScheduleID, enum.KindSchedule,
	)
	if err != nil {
		return nil, err
	}
	if schedule.OwnerActorID != input.Principal.ActorID {
		return nil, errs.ErrNotFound
	}
	return service.repository.ListScheduleOccurrences(ctx, input.Filter)
}

func (service *Service) ListAudit(
	ctx context.Context,
	input ListAuditInput,
) ([]domainrepo.Audit, error) {
	if err := authorize(input.Principal, permissionAuditRead); err != nil {
		return nil, err
	}
	input.Filter.OrganizationID = input.Principal.OrganizationID
	input.Filter.ProjectID = input.Principal.ProjectID
	if input.Principal.ProjectID == "" || input.Filter.Validate() != nil {
		return nil, errs.ErrInvalidInput
	}
	return service.repository.ListAudit(ctx, input.Filter)
}

func (service *Service) ListTombstones(
	ctx context.Context,
	input ListTombstonesInput,
) ([]domainrepo.Tombstone, error) {
	if err := authorize(input.Principal, permissionTombstones); err != nil {
		return nil, err
	}
	input.Filter.OrganizationID = input.Principal.OrganizationID
	input.Filter.ProjectID = input.Principal.ProjectID
	if input.Principal.ProjectID == "" ||
		input.Filter.Kind == enum.KindProject ||
		input.Filter.Validate() != nil {
		return nil, errs.ErrInvalidInput
	}
	return service.repository.ListTombstones(ctx, input.Filter)
}

func (service *Service) Diagnostics(
	ctx context.Context,
	input DiagnosticsInput,
) (domainrepo.Diagnostics, error) {
	if err := authorize(input.Principal, permissionDiagnostics); err != nil {
		return domainrepo.Diagnostics{}, err
	}
	return service.repository.Diagnostics(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	})
}
