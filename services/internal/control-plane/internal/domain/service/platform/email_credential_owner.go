package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// GetPersonalEmailMailboxMutationReceipt resolves the authenticated actor before reading one typed historical setup result; absence never proves that an in-flight command did not commit.
// GetPersonalEmailMailboxMutationReceipt разрешает аутентифицированного actor до чтения одного typed исторического setup результата; отсутствие не доказывает, что выполняющийся command не совершил commit.
func (service *Service) GetPersonalEmailMailboxMutationReceipt(ctx context.Context, principal value.Principal, operation, idempotencyKey string) (entity.EmailMailboxMutationReceipt, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.EmailMailboxMutationReceipt{}, err
	}
	return service.repository.GetPersonalEmailMailboxMutationReceipt(ctx, principal, operation, idempotencyKey)
}

// GetEmailMailboxOwnership resolves the transport principal before requesting safe dedicated mailbox metadata; neither a connection ref nor a generic NONE delivery profile authorizes access.
// GetEmailMailboxOwnership разрешает transport principal до запроса безопасной отдельной metadata mailbox; ref подключения и generic NONE delivery profile не разрешают доступ.
func (service *Service) GetEmailMailboxOwnership(ctx context.Context, principal value.Principal, connectionRef string) (entity.EmailMailboxOwnershipView, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.EmailMailboxOwnershipView{}, err
	}
	if !validMailboxQuery(connectionRef, query.Page{}) {
		return entity.EmailMailboxOwnershipView{}, errs.ErrInvalid
	}
	return service.repository.GetEmailMailboxOwnership(ctx, principal, connectionRef)
}

// ListMyEmailMailboxes delegates current self filtering to the mailbox owner after principal resolution; it never merges generic token catalog pages or caches positive eligibility.
// ListMyEmailMailboxes после разрешения principal передаёт фильтрацию текущего собственного набора владельцу mailbox; не объединяет страницы generic token catalog и не кеширует положительную eligibility.
func (service *Service) ListMyEmailMailboxes(ctx context.Context, principal value.Principal, search string, page query.Page) ([]entity.MyEmailMailbox, string, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, "", err
	}
	if len(page.Token) > 4096 || page.Size < 0 || page.Size > 100 {
		return nil, "", errs.ErrInvalid
	}
	return service.repository.ListMyEmailMailboxes(ctx, principal, search, page)
}
