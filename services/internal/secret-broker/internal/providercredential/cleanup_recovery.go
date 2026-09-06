package providercredential

import (
	"context"
	"reflect"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

func (service *Service) CleanupProviderCredentialWithRecovery(ctx context.Context, taskRef, accountRef string, generation int64, descriptor kubernetesstore.ProviderCredentialDescriptor, recovery kubernetesstore.ProviderCleanupRecoveryIdentity) (kubernetesstore.ProviderCredentialCleanupResult, error) {
	identity := func(task string, generation int64) (kubernetesstore.ProviderCleanupReceiptIdentity, error) {
		return kubernetesstore.CredentialCleanupReceiptIdentity(task, accountRef, generation, descriptor)
	}
	return service.recoverProviderCleanup(ctx, taskRef, generation, recovery, identity, func(ctx context.Context) (kubernetesstore.ProviderCredentialCleanupResult, error) {
		return service.CleanupProviderCredential(ctx, recovery.TaskRef, accountRef, recovery.Generation, descriptor)
	})
}

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

// Старый digest включает task/generation: legacy identity строится прежним
// алгоритмом для каждого ограниченного поколения и того же exact target.
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
