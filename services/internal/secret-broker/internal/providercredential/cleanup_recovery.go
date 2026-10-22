package providercredential

import (
	"context"
	"reflect"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// CleanupProviderCredentialWithRecovery derives current and origin receipt identities for the same exact
// credential descriptor, then delegates bounded recovery and any necessary effect to the original
// task/generation. Authority for both identities must already be established by the transport owner.
// CleanupProviderCredentialWithRecovery выводит текущую и исходную receipt identity для одного точного
// credential descriptor, затем передаёт ограниченное recovery и необходимый эффект исходным
// task/generation. Полномочия обеих identity заранее устанавливает transport owner.
func (service *Service) CleanupProviderCredentialWithRecovery(ctx context.Context, taskRef, accountRef string, generation int64, descriptor kubernetesstore.ProviderCredentialDescriptor, recovery kubernetesstore.ProviderCleanupRecoveryIdentity) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	identity := func(task string, generation int64) (kubernetesstore.ProviderCleanupReceiptIdentity, error) {
		return kubernetesstore.CredentialCleanupReceiptIdentity(task, accountRef, generation, descriptor)
	}
	return service.recoverProviderCleanup(ctx, taskRef, generation, recovery, identity, func(ctx context.Context) (kubernetesstore.ProviderCredentialCleanupResult, error) {
		return service.CleanupProviderCredential(ctx, recovery.TaskRef, accountRef, recovery.Generation, descriptor)
	})
}

// CleanupAuthorizationWithRecovery rebuilds receipt identities with current or origin task/generation
// while preserving every authorization target pin; the effect, if needed, uses the origin identity
// rather than silently changing the target.
// CleanupAuthorizationWithRecovery перестраивает receipt identity с текущими либо исходными
// task/generation, сохраняя все привязки authorization цели; необходимый эффект использует исходную
// identity без неявной смены цели.
func (service *Service) CleanupAuthorizationWithRecovery(ctx context.Context, target kubernetesstore.ProviderAuthorizationCleanupTarget, recovery kubernetesstore.ProviderCleanupRecoveryIdentity) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	identity := func(task string, generation int64) (kubernetesstore.ProviderCleanupReceiptIdentity, error) {
		copy := target
		copy.TaskRef, copy.Generation = task, generation
		return kubernetesstore.AuthorizationCleanupReceiptIdentity(copy)
	}
	return service.recoverProviderCleanup(ctx, target.TaskRef, target.Generation, recovery, identity, func(ctx context.Context) (kubernetesstore.ProviderCredentialCleanupResult, error) {
		target.TaskRef, target.Generation = recovery.TaskRef, recovery.Generation
		return service.CleanupAuthorization(ctx, target)
	})
}

// recoverProviderCleanup validates the bounded origin, reads current/origin and allowed historical
// receipts and rejects conflicting replacement outcomes. Only absence of every result invokes the
// effect; origin is durably completed before current so lost acknowledgements preserve recovery
// provenance.
// recoverProviderCleanup проверяет ограниченную исходную identity, читает текущую, исходную и
// разрешённые исторические квитанции и отклоняет противоречивые replacement исходы. Только отсутствие
// всех результатов вызывает эффект; origin сохраняется до current, сохраняя происхождение recovery при
// потере ответа.
func (service *Service) recoverProviderCleanup(ctx context.Context, task string, generation int64, recovery kubernetesstore.ProviderCleanupRecoveryIdentity,
	identity func(string, int64) (kubernetesstore.ProviderCleanupReceiptIdentity, error), effect func(context.Context) (kubernetesstore.ProviderCredentialCleanupResult, error),
) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	if !recovery.Valid(task, generation) {
		return kubernetesstore.ProviderCredentialCleanupResult{}, ErrInvalidInput
	}
	current, err := identity(task, generation)
	if err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	origin, err := identity(recovery.TaskRef, recovery.Generation)
	if err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, deviceAuthCleanupBudget)
	defer cancel()
	var result kubernetesstore.ProviderCredentialCleanupResult
	found := false
	read := func(id kubernetesstore.ProviderCleanupReceiptIdentity) (bool, error) {
		stored, exists, err := service.store.ReadProviderCleanupReceipt(ctx, id)
		if err != nil || !exists {
			return exists, err
		}
		if found && !reflect.DeepEqual(result.ProducedCredential, stored.ProducedCredential) {
			return false, kubernetesstore.ErrProviderCredentialCleanupConflict
		}
		result, found = stored, true
		return true, nil
	}
	if _, err := read(current); err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	originFound, err := read(origin)
	if err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	if !originFound {
		for legacy := int64(1); legacy <= recovery.LegacyLastGeneration; legacy++ {
			id, err := identity(recovery.TaskRef, legacy)
			if err != nil {
				return kubernetesstore.ProviderCredentialCleanupResult{}, err
			}
			if _, err := read(id); err != nil {
				return kubernetesstore.ProviderCredentialCleanupResult{}, err
			}
		}
	}
	if !found {
		result, err = effect(ctx)
		if err != nil {
			return kubernetesstore.ProviderCredentialCleanupResult{}, err
		}
	}
	// Origin переживает replacement; current receipt подтверждает только текущий
	// запрос. Lost ACK не вызывает повторного внешнего эффекта в этом вызове.
	if _, err := service.store.CompleteProviderCleanupReceipt(ctx, origin, result.ProducedCredential); err != nil {
		return kubernetesstore.ProviderCredentialCleanupResult{}, err
	}
	return service.store.CompleteProviderCleanupReceipt(ctx, current, result.ProducedCredential)
}
