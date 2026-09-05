package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) ResolveEmailAuthorization(ctx context.Context, principal value.Principal, input query.EmailAuthorization) (entity.EmailAuthorization, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.EmailAuthorization{}, err
	}
	return service.repository.ResolveEmailAuthorization(ctx, principal, input)
}

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

func (service *Service) ResolveEmailReconciliation(ctx context.Context, principal value.Principal, receipt, decision, externalRef, digest string) (entity.EmailEffectReceiptView, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	return service.repository.ResolveEmailReconciliation(ctx, principal, receipt, decision, externalRef, digest)
}
