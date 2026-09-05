package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) GetMemoryRecord(ctx context.Context, principal value.Principal, ref string) (entity.KodexMemoryRecord, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.KodexMemoryRecord{}, err
	}
	return service.repository.GetMemoryRecord(ctx, principal, ref)
}

func (service *Service) ListMemoryRecords(ctx context.Context, principal value.Principal, filter query.Filter) ([]entity.KodexMemoryRecord, int64, string, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	if filter.State == "" {
		filter.State = "ACTIVE"
	}
	switch filter.State {
	case "ACTIVE", "ARCHIVED", "EXPIRED", "PURGED":
	default:
		return nil, 0, "", errs.ErrInvalid
	}
	if len(filter.Query) > 256 {
		return nil, 0, "", errs.ErrInvalid
	}
	return service.repository.ListMemoryRecords(ctx, principal, filter)
}

func (service *Service) ListMemoryRecordRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.MemoryRecordRevision, int64, string, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.ListMemoryRecordRevisions(ctx, principal, ref, page)
}
