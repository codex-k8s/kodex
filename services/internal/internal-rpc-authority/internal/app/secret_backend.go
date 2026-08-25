package app

import (
	"errors"

	secretclient "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/kubernetessecret"
	projectedsecret "github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/client/projectedsecret"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

type secretBackend string

const secretBackendKubernetes secretBackend = "kubernetes"

type secretDeliveryCloser interface {
	repository.SecretDelivery
	Close()
}

func selectSecretBackend(value string) (secretBackend, error) {
	backend := secretBackend(value)
	if backend != secretBackendKubernetes {
		return "", errors.New("secret backend is not registered")
	}
	return backend, nil
}

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

func newPublisherSecretDelivery(
	config PublisherConfig,
	registry model.DeliveryTargetRegistry,
) (secretDeliveryCloser, error) {
	if _, err := selectSecretBackend(config.SecretBackend); err != nil {
		return nil, err
	}
	return secretclient.NewPublisherDelivery(registry)
}
