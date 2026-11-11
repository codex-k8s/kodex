package providercredential

import (
	"context"
	"errors"

	kubernetesstore "github.com/codex-k8s/kodex/services/internal/secret-broker/internal/kubernetes"
)

// managedRenewalStore separates durable single-effect reservation, metadata reads, completion and
// immutable result recovery; only an acknowledged newly created reservation permits the native refresh
// path.
// managedRenewalStore разделяет durable резервирование одного эффекта, чтение metadata, завершение и
// восстановление immutable результата; native refresh допускается только подтверждённым новым
// резервированием.
type managedRenewalStore interface {
	// BeginProviderCredentialRenewal returns a receipt and whether this call definitively created the
	// reservation; false or an error must never be treated as permission for another OAuth effect.
	// BeginProviderCredentialRenewal возвращает квитанцию и признак доказанного создания резервирования этим
	// вызовом; false или ошибка никогда не означают разрешение ещё одного OAuth эффекта.
	BeginProviderCredentialRenewal(context.Context, kubernetesstore.ProviderRenewalIdentity) (kubernetesstore.ProviderRenewalReceipt, bool, error)
	// ReadProviderCredentialRenewal reads the record bound to the full identity for observation without
	// updating the reservation or executing refresh.
	// ReadProviderCredentialRenewal читает запись, связанную с полной identity, для наблюдения без
	// обновления резервирования или выполнения refresh.
	ReadProviderCredentialRenewal(context.Context, kubernetesstore.ProviderRenewalIdentity) (kubernetesstore.ProviderRenewalReceipt, error)
	// CompleteProviderCredentialRenewal persists the exact produced descriptor against the reservation's API
	// pins after result verification, preserving idempotent completed replay.
	// CompleteProviderCredentialRenewal сохраняет точный produced descriptor с API-привязками резервирования
	// после проверки результата, сохраняя идемпотентность завершённого повтора.
	CompleteProviderCredentialRenewal(context.Context, kubernetesstore.ProviderRenewalReceipt, kubernetesstore.ProviderCredentialDescriptor) (kubernetesstore.ProviderRenewalReceipt, error)
	// RecoverProviderRenewalCredential reads the exact immutable task result and returns metadata plus
	// presence, without recreating credentials or activating them for an account.
	// RecoverProviderRenewalCredential читает точный immutable результат task и возвращает metadata с
	// признаком наличия без повторного создания credentials и их активации для account.
	RecoverProviderRenewalCredential(context.Context, kubernetesstore.ProviderRenewalIdentity) (kubernetesstore.ProviderCredentialDescriptor, bool, error)
}

// ManagedRenewalResult reports observed state, an optional exact credential descriptor and receipt
// UID/version without tokens; observed success does not itself activate the credential in the owner's
// account.
// ManagedRenewalResult сообщает наблюдённое состояние, необязательный точный credential descriptor и
// UID/version квитанции без tokens; наблюдённый успех сам по себе не активирует credential в account
// владельца.
type ManagedRenewalResult struct {
	State                      string
	Credential                 *kubernetesstore.ProviderCredentialDescriptor
	ReceiptUID, ReceiptVersion string
}

// ObserveManagedRenewal reads receipt and immutable output without writing either or starting refresh. A
// published result can report success even if receipt completion ACK was lost; a missing or divergent
// terminal result conflicts. Fresh eligibility for account activation belongs to the task owner.
// ObserveManagedRenewal читает квитанцию и immutable результат без записи и запуска refresh.
// Опубликованный результат может дать успех даже при потерянном ACK завершения квитанции; отсутствующий
// или другой terminal результат даёт конфликт. Свежая допустимость активации account принадлежит task
// owner.
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

// RenewManagedCredential rejects the removed CLI OAuth refresh before reserving or performing an external effect.
// RenewManagedCredential отклоняет удалённый CLI OAuth refresh до резервирования или внешнего эффекта.
func (service *Service) RenewManagedCredential(context.Context, kubernetesstore.ProviderRenewalIdentity) (ManagedRenewalResult, error) {
	return ManagedRenewalResult{}, ErrInvalidInput
}
