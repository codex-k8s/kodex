package platform

import (
	"context"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// kubernetesHealthOwner admits only the private broker method and leaves human authority to owner rows.
// An invocation binding cannot be repurposed as a health test to bypass effect admission.
// kubernetesHealthOwner допускает только private broker method, human authority определяется owner rows.
// Invocation binding не может стать health test для обхода effect admission.
func (service *Service) kubernetesHealthOwner(ctx context.Context, principal value.Principal, permission string, binding value.ManagedKubernetesBinding) (platformrepo.KubernetesIdentityHealthRepository, value.Principal, error) {
	principal, err := service.principal(ctx, principal)
	if err != nil {
		return nil, principal, err
	}
	if principal.CallerWorkload != "secret-broker" || principal.Permission != permission || principal.ProjectRef != "" {
		return nil, principal, errs.ErrForbidden
	}
	if binding.Validate() != nil || binding.ConnectionTestRef == "" || binding.InvocationRef != "" {
		return nil, principal, errs.ErrInvalid
	}
	repository, ok := service.repository.(platformrepo.KubernetesIdentityHealthRepository)
	if !ok {
		return nil, principal, errs.ErrUnavailable
	}
	return repository, principal, nil
}

// BeginManagedKubernetesHealthProbe consumes a health claim without entering mutation or stream paths.
// The repository resolves the exact current actor, configuration, target and immutable broker revision.
// BeginManagedKubernetesHealthProbe расходует health claim вне mutation и stream paths.
// Repository разрешает exact current actor, configuration, target и immutable broker revision.
func (service *Service) BeginManagedKubernetesHealthProbe(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding) (entity.KubernetesIdentityHealthAdmission, error) {
	repository, principal, err := service.kubernetesHealthOwner(ctx, principal, "platform.runtime.kubernetes.health.begin", binding)
	if err != nil {
		return entity.KubernetesIdentityHealthAdmission{}, err
	}
	return repository.BeginManagedKubernetesHealthProbe(ctx, principal, binding)
}

// CompleteManagedKubernetesHealthProbe rejects ambiguous authentication facts before owner processing.
// Observed subject pins are required only for authenticated results and carry no permission assertion.
// CompleteManagedKubernetesHealthProbe отвергает ambiguous authentication facts до owner processing.
// Observed subject pins обязательны только для authenticated result и не утверждают permissions.
func (service *Service) CompleteManagedKubernetesHealthProbe(ctx context.Context, principal value.Principal, binding value.ManagedKubernetesBinding, input command.KubernetesIdentityHealthCompletion) (entity.KubernetesIdentityHealthReceipt, error) {
	repository, principal, err := service.kubernetesHealthOwner(ctx, principal, "platform.runtime.kubernetes.health.complete", binding)
	if err != nil {
		return entity.KubernetesIdentityHealthReceipt{}, err
	}
	if !managedkubernetes.ValidLocator(input.AttemptRef) || !managedkubernetes.ValidDigest(input.SnapshotDigest) {
		return entity.KubernetesIdentityHealthReceipt{}, errs.ErrInvalid
	}
	switch input.Outcome {
	case entity.PersonalIntegrationCredentialAuthenticated:
		if !managedkubernetes.ValidDigest(input.ObservedSubjectDigest) {
			return entity.KubernetesIdentityHealthReceipt{}, errs.ErrInvalid
		}
	case entity.PersonalIntegrationCredentialAuthenticationRejected, entity.PersonalIntegrationCredentialVerificationUnavailable:
		if input.ObservedSubjectDigest != "" {
			return entity.KubernetesIdentityHealthReceipt{}, errs.ErrInvalid
		}
	default:
		return entity.KubernetesIdentityHealthReceipt{}, errs.ErrInvalid
	}
	return repository.CompleteManagedKubernetesHealthProbe(ctx, principal, binding, input)
}
