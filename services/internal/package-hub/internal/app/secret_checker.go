package app

import (
	"strings"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
)

func newSecretChecker(cfg Config) (secretresolver.Checker, error) {
	backends := make(map[string]secretresolver.Backend)
	if cfg.SecretEnvBackendEnabled {
		backends[secretresolver.StoreTypeEnv] = secretresolver.NewEnvBackend()
	}
	if strings.TrimSpace(cfg.SecretMountedKubernetesRoot) != "" {
		backend, err := secretresolver.NewMountedKubernetesBackend(secretresolver.MountedKubernetesBackendConfig{
			Root:           cfg.SecretMountedKubernetesRoot,
			MaxSecretBytes: cfg.SecretMountedKubernetesMaxBytes,
		})
		if err != nil {
			return nil, err
		}
		backends[secretresolver.StoreTypeKubernetesMountedSecret] = backend
	}
	vaultAddr := strings.TrimSpace(cfg.VaultAddr)
	if vaultAddr != "" {
		vaultBackend, err := secretresolver.NewVaultBackendFromClientConfig(secretresolver.VaultClientConfig{
			Addr:      vaultAddr,
			Token:     cfg.VaultToken,
			Namespace: cfg.VaultNamespace,
		})
		if err != nil {
			return nil, err
		}
		backends[secretresolver.StoreTypeVault] = vaultBackend
	}
	return secretresolver.NewMux(backends)
}
