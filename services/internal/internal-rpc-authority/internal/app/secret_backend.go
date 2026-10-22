package app

import (
	"errors"

	secretclient "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/kubernetessecret"
	projectedsecret "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/projectedsecret"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

// secretBackend names the closed material-delivery backend selected by app configuration. It is a profile discriminator rather than a dynamic provider URL or caller-controlled credential source.
// secretBackend именует закрытое хранилище доставки материалов, выбранное конфигурацией приложения. Это селектор профиля, а не динамический URL провайдера или источник реквизитов, управляемый вызывающим.
type secretBackend string

// secretBackendKubernetes is the only registered material-delivery profile; runtime receives projected Kubernetes material while publisher uses the registry-bound Kubernetes adapter.
// secretBackendKubernetes — единственный зарегистрированный профиль доставки материалов; runtime получает проецируемые материалы Kubernetes, publisher использует привязанный к реестру адаптер Kubernetes.
const secretBackendKubernetes secretBackend = "kubernetes"

// secretDeliveryCloser combines the existing typed SecretDelivery port with explicit composition-root cleanup. Embedding this interface supplies no additional Secret scope, publisher capability or authority by itself.
// secretDeliveryCloser объединяет существующий типизированный порт SecretDelivery с явной очисткой в composition root. Само встраивание интерфейса не даёт дополнительную область Secret, возможность publisher или полномочия.
type secretDeliveryCloser interface {
	repository.SecretDelivery
	// Close releases the selected delivery adapter's owned resources during startup failure or normal shutdown; it does not authorize secret publication or change the caller's material scope.
	// Close освобождает ресурсы выбранного адаптера доставки при стартовой ошибке или штатном завершении; публикация Secret не разрешается и область материалов вызывающего не меняется.
	Close()
}

// selectSecretBackend accepts exactly the kubernetes discriminator and returns a fixed registration error for every other string. There is no automatic fallback to a removed backend or arbitrary file path.
// selectSecretBackend принимает только селектор kubernetes и возвращает фиксированную ошибку регистрации для остальных строк. Автоматического перехода к удалённому хранилищу или произвольному файловому пути нет.
func selectSecretBackend(value string) (secretBackend, error) {
	backend := secretBackend(value)
	if backend != secretBackendKubernetes {
		return "", errors.New("secret backend is not registered")
	}
	return backend, nil
}

// newRuntimeSecretDelivery validates the sole backend and passes the six configured readback/restore/resolver credential and possession references to the projected runtime adapter. It does not construct the publisher's Kubernetes write client or grant permission to material outside those bindings.
// newRuntimeSecretDelivery проверяет единственное хранилище и передаёт шесть настроенных ссылок credential и possession readback, restore и resolver проецируемому runtime-адаптеру. Клиент записи Kubernetes издателя не создаётся, права на материалы за пределами привязок не выдаются.
func newRuntimeSecretDelivery(config Config) (secretDeliveryCloser, error) {
	if _, err := selectSecretBackend(config.SecretBackend); err != nil {
		return nil, err
	}
	return projectedsecret.NewRuntimeDelivery(
		config.ReadbackCredentialSecret,
		config.ReadbackPossessionSecret,
		config.RestoreRoleCredentialSecret,
		config.RestoreACKSecret,
		config.ResolverReadbackCredentialSecret,
		config.ResolverReadbackPossessionSecret,
	)
}

// newPublisherSecretDelivery validates the registered backend and constructs the publisher delivery adapter from the exact typed target registry. Registry membership and adapter checks remain distinct from merely choosing the kubernetes discriminator.
// newPublisherSecretDelivery проверяет зарегистрированное хранилище и создаёт адаптер доставки издателя из точного типизированного реестра целей. Вхождение в реестр и проверки адаптера остаются отдельными от выбора селектора kubernetes.
func newPublisherSecretDelivery(
	config PublisherConfig,
	registry model.DeliveryTargetRegistry,
) (secretDeliveryCloser, error) {
	if _, err := selectSecretBackend(config.SecretBackend); err != nil {
		return nil, err
	}
	return secretclient.NewPublisherDelivery(registry)
}
