package platform

import (
	"context"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetMemoryRecord resolves principal and delegates exact memory-record visibility to the owner
// repository without inferring authority from its reference.
// GetMemoryRecord разрешает principal и передаёт видимость точной записи памяти repository
// владельца без вывода полномочий из ссылки.
func (service *Service) GetMemoryRecord(ctx context.Context, principal value.Principal, ref string) (entity.KodexMemoryRecord, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.KodexMemoryRecord{}, err
	}
	return service.repository.GetMemoryRecord(ctx, principal, ref)
}

// ListMemoryRecords resolves principal, defaults state to ACTIVE and checks its closed lifecycle
// set plus a 256-byte query before owner listing.
// ListMemoryRecords разрешает principal, задаёт ACTIVE по умолчанию и проверяет закрытый набор
// lifecycle-состояний и запрос до 256 байт до списка владельца.
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

// ListMemoryRecordRevisions resolves principal and forwards the memory reference and page to the
// owner so history retains the same resource visibility boundary.
// ListMemoryRecordRevisions разрешает principal и передаёт ссылку памяти и страницу владельцу с
// сохранением той же границы видимости ресурса для истории.
func (service *Service) ListMemoryRecordRevisions(ctx context.Context, principal value.Principal, ref string, page query.Page) ([]entity.MemoryRecordRevision, int64, string, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, 0, "", err
	}
	return service.repository.ListMemoryRecordRevisions(ctx, principal, ref, page)
}
