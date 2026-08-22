package app

import (
	"errors"
	"time"

	vaultclient "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/client/vault"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

type secretBackend string

const (
	secretBackendVault secretBackend = "vault"
)

type secretDeliveryCloser interface {
	repository.SecretDelivery
	Close()
}

type staticRoleManagerCloser interface {
	repository.VaultStaticRoleManager
	Close()
}

func selectSecretBackend(value string) (secretBackend, error) {
	backend := secretBackend(value)
	if backend != secretBackendVault {
		return "", errors.New("secret backend is not registered")
	}
	return backend, nil
}

func newRuntimeSecretDelivery(config Config) (secretDeliveryCloser, error) {
	if _, err := selectSecretBackend(config.SecretBackend); err != nil {
		return nil, err
	}
	return vaultclient.NewStaticRoleClient(vaultclient.Config{
		Address: config.VaultAddress, TLSServerName: config.VaultTLSServerName,
		CAFile: config.VaultCAFile, AuthMount: "kubernetes",
		AuthRole:                config.VaultAuthRole,
		ServiceAccountTokenFile: config.VaultAuthFile,
		Timeout:                 5 * time.Second,
	})
}

func newPublisherSecretDelivery(
	config PublisherConfig,
	registry model.DeliveryTargetRegistry,
) (secretDeliveryCloser, error) {
	if _, err := selectSecretBackend(config.SecretBackend); err != nil {
		return nil, err
	}
	_ = registry
	return vaultclient.NewStaticRoleClient(vaultclient.Config{
		Address: config.VaultAddress, TLSServerName: config.VaultTLSServerName,
		CAFile: config.VaultCAFile, AuthMount: "kubernetes",
		AuthRole:                config.VaultAuthRole,
		ServiceAccountTokenFile: config.VaultAuthFile,
		Timeout:                 5 * time.Second,
	})
}

func newStaticRoleManager(config ReconcilerConfig) (staticRoleManagerCloser, error) {
	if _, err := selectSecretBackend(config.SecretBackend); err != nil {
		return nil, err
	}
	return vaultclient.NewStaticRoleClient(vaultclient.Config{
		Address:       config.VaultAddress,
		TLSServerName: config.VaultTLSServerName,
		CAFile:        config.VaultCAFile, AuthMount: "kubernetes",
		AuthRole:                config.VaultAuthRole,
		ServiceAccountTokenFile: config.VaultServiceAccountTokenFile,
		Timeout:                 5 * time.Second,
	})
}
