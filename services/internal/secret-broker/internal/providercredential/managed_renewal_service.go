package providercredential

import (
	"context"
	"errors"
	"time"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

type managedRenewalStore interface {
	BeginProviderCredentialRenewal(context.Context, kubernetesstore.ProviderRenewalIdentity) (kubernetesstore.ProviderRenewalReceipt, bool, error)
	ReadProviderCredentialRenewal(context.Context, kubernetesstore.ProviderRenewalIdentity) (kubernetesstore.ProviderRenewalReceipt, error)
	CompleteProviderCredentialRenewal(context.Context, kubernetesstore.ProviderRenewalReceipt, kubernetesstore.ProviderCredentialDescriptor) (kubernetesstore.ProviderRenewalReceipt, error)
	RecoverProviderRenewalCredential(context.Context, kubernetesstore.ProviderRenewalIdentity) (kubernetesstore.ProviderCredentialDescriptor, bool, error)
}

type managedRenewer interface {
	renewManagedCredential(context.Context, []byte, func(context.Context, []byte) error) error
}

type ManagedRenewalResult struct {
	State                      string
	Credential                 *kubernetesstore.ProviderCredentialDescriptor
	ReceiptUID, ReceiptVersion string
}

// Вызывается только отдельным WRITE transport после свежего owner task proof.
// Долговечная create-квитанция назначает единственного исполнителя на всех репликах.
func (service *Service) RenewManagedCredential(ctx context.Context, identity kubernetesstore.ProviderRenewalIdentity) (ManagedRenewalResult, error) {
	store, storeOK := service.store.(managedRenewalStore)
	renewer, processOK := service.appServer.(managedRenewer)
	if ctx == nil || ctx.Err() != nil || service.lifecycle.Err() != nil || !storeOK || !processOK || !identity.ExpiresAt.After(time.Now()) {
		return ManagedRenewalResult{}, ErrInvalidInput
	}
	ctx, cancel := context.WithDeadline(ctx, identity.ExpiresAt)
	defer cancel()
	stop := context.AfterFunc(service.lifecycle, cancel)
	defer stop()
	original, err := service.store.ReadProviderCredentialExact(ctx, identity.AccountRef, identity.Previous)
	if err != nil {
		return ManagedRenewalResult{}, ErrConflict
	}
	defer clear(original)
	if _, err := parseManagedRenewalAuth(original); err != nil {
		return ManagedRenewalResult{}, ErrInvalidInput
	}
	receipt, created, err := store.BeginProviderCredentialRenewal(ctx, identity)
	if err != nil {
		return ManagedRenewalResult{}, errManagedRenewalUnknown
	}
	if !created {
		return service.ObserveManagedRenewal(ctx, identity)
	}
	err = renewer.renewManagedCredential(ctx, original, func(persistCtx context.Context, raw []byte) error {
		produced, err := service.store.CreateProviderCredential(persistCtx, identity.TaskRef, identity.AccountRef, raw)
		if err != nil {
			return err
		}
		_, err = store.CompleteProviderCredentialRenewal(persistCtx, receipt, produced)
		return err
	})
	if err != nil {
		// Не повторяем эффект при ошибке/потерянном ACK. Только metadata/immutable READ.
		result, recoveryErr := service.ObserveManagedRenewal(ctx, identity)
		if recoveryErr == nil && result.State == kubernetesstore.ProviderRenewalSucceeded {
			return result, nil
		}
		return ManagedRenewalResult{}, errManagedRenewalUnknown
	}
	return service.ObserveManagedRenewal(ctx, identity)
}

// READ rejoin не меняет receipt и никогда не запускает native refresh.
// После lost ACK существующая exact immutable Secret сама доказывает publication;
// task owner отдельно решает допустимость её активации по своим свежим pins.
func (service *Service) ObserveManagedRenewal(ctx context.Context, identity kubernetesstore.ProviderRenewalIdentity) (ManagedRenewalResult, error) {
	store, ok := service.store.(managedRenewalStore)
	if ctx == nil || !ok || service.lifecycle.Err() != nil || ctx.Err() != nil {
		return ManagedRenewalResult{}, ErrInvalidInput
	}
	receipt, err := store.ReadProviderCredentialRenewal(ctx, identity)
	if err != nil {
		return ManagedRenewalResult{}, err
	}
	produced, found, err := store.RecoverProviderRenewalCredential(ctx, identity)
	if err != nil {
		return ManagedRenewalResult{}, err
	}
	result := ManagedRenewalResult{State: receipt.State, ReceiptUID: receipt.UID, ReceiptVersion: receipt.ResourceVersion}
	if found {
		if receipt.Produced != nil && *receipt.Produced != produced {
			return ManagedRenewalResult{}, ErrConflict
		}
		result.State, result.Credential = kubernetesstore.ProviderRenewalSucceeded, &produced
		return result, nil
	}
	if receipt.State == kubernetesstore.ProviderRenewalSucceeded {
		return ManagedRenewalResult{}, ErrConflict
	}
	return result, errors.Join(errManagedRenewalUnknown, ErrConflict)
}
