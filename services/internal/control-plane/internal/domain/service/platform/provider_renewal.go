package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

func (service *Service) CheckProviderCredentialRenewalReadiness(ctx context.Context) error {
	owner, ok := service.repository.(repository.ProviderCredentialRenewalRepository)
	if !ok {
		return errs.ErrUnavailable
	}
	return owner.CheckProviderCredentialRenewalReadiness(ctx)
}

func (service *Service) AuthorizeProviderCredentialRenewal(ctx context.Context, principal value.Principal, task repository.ProviderCredentialRenewalTask) error {
	if principal.CallerWorkload != "secret-broker" || principal.Permission != repository.ProviderCredentialRenewalAuthorizeOperation || principal.ProjectRef != "" {
		return errs.ErrForbidden
	}
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return err
	}
	owner, ok := service.repository.(repository.ProviderCredentialRenewalRepository)
	if !ok {
		return errs.ErrUnavailable
	}
	return owner.AuthorizeProviderCredentialRenewal(ctx, principal, task)
}

func (service *Service) ValidateProviderCredentialRenewalObservation(ctx context.Context, principal value.Principal, task repository.ProviderCredentialRenewalTask) error {
	if principal.CallerWorkload != "secret-broker" || principal.Permission != repository.ProviderCredentialRenewalValidateOperation || principal.ProjectRef != "" {
		return errs.ErrForbidden
	}
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return err
	}
	owner, ok := service.repository.(repository.ProviderCredentialRenewalRepository)
	if !ok {
		return errs.ErrUnavailable
	}
	return owner.ValidateProviderCredentialRenewalObservation(ctx, principal, task)
}
