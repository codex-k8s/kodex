package app

import (
	"errors"
	"time"

	vaultclient "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/client/vault"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
	prototypematerial "github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/repository/kubernetes/prototypematerial"
)

type secretBackend string

const (
	secretBackendVault                     secretBackend = "vault"
	secretBackendDirectProductionPrototype secretBackend = "direct-production-kubernetes-file"
	directProductionPrototypeProfile                     = "direct-production-single-node-prototype"
)

type secretDeliveryCloser interface {
	repository.SecretDelivery
	Close()
}

type staticRoleManagerCloser interface {
	repository.VaultStaticRoleManager
	Close()
}

func selectSecretBackend(value, profile string) (secretBackend, error) {
	backend := secretBackend(value)
	switch backend {
	case secretBackendVault:
		if profile == directProductionPrototypeProfile {
			return "", errors.New("direct-production prototype cannot use Vault secret backend")
		}
	case secretBackendDirectProductionPrototype:
		if profile != directProductionPrototypeProfile {
			return "", errors.New("prototype secret backend requires exact deployment profile")
		}
	default:
		return "", errors.New("secret backend is not registered")
	}
	return backend, nil
}

func newRuntimeSecretDelivery(config Config) (secretDeliveryCloser, error) {
	backend, err := selectSecretBackend(config.SecretBackend, config.DeploymentProfile)
	if err != nil {
		return nil, err
	}
	if backend == secretBackendVault {
		return vaultclient.NewStaticRoleClient(vaultclient.Config{
			Address: config.VaultAddress, TLSServerName: config.VaultTLSServerName,
			CAFile: config.VaultCAFile, AuthMount: "kubernetes",
			AuthRole:                config.VaultAuthRole,
			ServiceAccountTokenFile: config.VaultAuthFile,
			Timeout:                 5 * time.Second,
		})
	}
	primary := map[string]string{
		config.RestoreRoleCredentialVaultPath: "restore-credential.json",
		config.RestoreACKVaultPath:            "restore-ack.json",
		config.ReadbackCredentialVaultPath:    "readback-credential.json",
		config.ReadbackPossessionVaultPath:    "readback-possession.json",
	}
	resolver := map[string]string{}
	if config.Mode == ModeVerifier && config.ResolverEnabled {
		resolver[config.ResolverReadbackCredentialPath] = "readback-credential.json"
		resolver[config.ResolverReadbackPossessionPath] = "readback-possession.json"
		// Resolver разделяет restore role с verifier и не получает второй
		// документ credential/ACK.
	}
	registry, err := prototypematerial.NewWorkloadFileRegistry(primary, resolver)
	if err != nil {
		return nil, err
	}
	return prototypematerial.NewFileDelivery(registry)
}

func newPublisherSecretDelivery(
	config PublisherConfig,
	registry model.DeliveryTargetRegistry,
) (secretDeliveryCloser, error) {
	backend, err := selectSecretBackend(config.SecretBackend, config.DeploymentProfile)
	if err != nil {
		return nil, err
	}
	if backend == secretBackendVault {
		return vaultclient.NewStaticRoleClient(vaultclient.Config{
			Address: config.VaultAddress, TLSServerName: config.VaultTLSServerName,
			CAFile: config.VaultCAFile, AuthMount: "kubernetes",
			AuthRole:                config.VaultAuthRole,
			ServiceAccountTokenFile: config.VaultAuthFile,
			Timeout:                 5 * time.Second,
		})
	}
	deliveryRegistry, err := prototypematerial.NewPublisherRegistry(registry)
	if err != nil {
		return nil, err
	}
	return prototypematerial.NewKubernetesDelivery(
		prototypeKubernetesConfig(
			config.KubernetesAPIAddress,
			config.KubernetesAPITLSServerName,
			config.KubernetesAPICAFile,
			config.KubernetesAPITokenFile,
		),
		deliveryRegistry,
	)
}

func newStaticRoleManager(config ReconcilerConfig) (staticRoleManagerCloser, error) {
	backend, err := selectSecretBackend(config.SecretBackend, config.DeploymentProfile)
	if err != nil {
		return nil, err
	}
	if backend == secretBackendVault {
		return vaultclient.NewStaticRoleClient(vaultclient.Config{
			Address:       config.VaultAddress,
			TLSServerName: config.VaultTLSServerName,
			CAFile:        config.VaultCAFile, AuthMount: "kubernetes",
			AuthRole:                config.VaultAuthRole,
			ServiceAccountTokenFile: config.VaultServiceAccountTokenFile,
			Timeout:                 5 * time.Second,
		})
	}
	return prototypematerial.NewStaticRoleManager(
		prototypeKubernetesConfig(
			config.KubernetesAPIAddress,
			config.KubernetesAPITLSServerName,
			config.KubernetesAPICAFile,
			config.KubernetesAPITokenFile,
		),
		config.SourceRevision,
		config.SourceDigest,
	)
}

func prototypeKubernetesConfig(address, serverName, caFile, tokenFile string) prototypematerial.KubernetesConfig {
	return prototypematerial.KubernetesConfig{
		Address: address, TLSServerName: serverName, CAFile: caFile,
		TokenFile: tokenFile, Namespace: prototypematerial.Namespace, Timeout: 5 * time.Second,
	}
}

func validatePrototypeKubernetesBoundary(address, serverName, caFile, tokenFile string) error {
	if address != "https://kubernetes.default.svc:443" ||
		serverName != "kubernetes.default.svc" ||
		caFile != prototypematerial.KubernetesCAFile ||
		tokenFile != prototypematerial.KubernetesTokenFile {
		return errors.New("prototype Kubernetes API boundary is invalid")
	}
	return nil
}
