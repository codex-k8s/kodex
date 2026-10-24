package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	repository "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// CheckProviderCredentialRenewalReadiness requires the optional renewal repository port and
// delegates readiness for owner-managed renewal state.
// CheckProviderCredentialRenewalReadiness требует необязательный порт repository продления и
// передаёт ему проверку готовности принадлежащего владельцу состояния продления.
func (service *Service) CheckProviderCredentialRenewalReadiness(ctx context.Context) error {
	owner, ok := service.repository.(repository.ProviderCredentialRenewalRepository)
	if !ok {
		return errs.ErrUnavailable
	}
	return owner.CheckProviderCredentialRenewalReadiness(ctx)
}

// AuthorizeProviderCredentialRenewal requires secret-broker, the exact authorize operation and
// absent project scope before resolving principal and delegating task authorization to its owner.
// AuthorizeProviderCredentialRenewal требует secret-broker, точную операцию authorize и отсутствие
// области проекта до разрешения principal и передачи авторизации задачи владельцу.
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

// ValidateProviderCredentialRenewalObservation requires secret-broker and the distinct validate
// operation with no project scope before resolving principal and checking the observed task
// through the owner port.
// ValidateProviderCredentialRenewalObservation требует secret-broker и отдельную операцию validate
// без области проекта до разрешения principal и проверки наблюдаемой задачи через порт владельца.
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
