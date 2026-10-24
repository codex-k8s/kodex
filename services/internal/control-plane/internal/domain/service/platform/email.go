package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// ResolveEmailAuthorization resolves current principal and delegates exact email operation/input
// authority to the owning repository without inferring access from payload references.
// ResolveEmailAuthorization разрешает текущий principal и передаёт точные полномочия почтовой
// операции и ввода repository владельца без вывода доступа из ссылок payload.
func (service *Service) ResolveEmailAuthorization(ctx context.Context, principal value.Principal, input query.EmailAuthorization) (entity.EmailAuthorization, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.EmailAuthorization{}, err
	}
	return service.repository.ResolveEmailAuthorization(ctx, principal, input)
}

// GetEmailEffectReceipt rejects an empty invocation, resolves principal and reads the
// authoritative effect receipt without executing or retrying a mail operation.
// GetEmailEffectReceipt отклоняет пустой invocation, разрешает principal и читает авторитетную
// квитанцию эффекта без исполнения либо повтора почтовой операции.
func (service *Service) GetEmailEffectReceipt(ctx context.Context, principal value.Principal, invocation string) (entity.EmailEffectReceiptView, error) {
	if invocation == "" {
		return entity.EmailEffectReceiptView{}, errs.ErrInvalid
	}
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	return service.repository.GetEmailEffectReceipt(ctx, principal, invocation)
}

// ResolveEmailReconciliation resolves principal and forwards exact receipt, decision, external
// reference and digest to owner-side reconciliation resolution.
// ResolveEmailReconciliation разрешает principal и передаёт точные receipt, решение, внешнюю
// ссылку и дайджест владельцу для разрешения сверки.
func (service *Service) ResolveEmailReconciliation(ctx context.Context, principal value.Principal, receipt, decision, externalRef, digest string) (entity.EmailEffectReceiptView, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	return service.repository.ResolveEmailReconciliation(ctx, principal, receipt, decision, externalRef, digest)
}
