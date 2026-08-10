package app

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/authorization/oidc"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/client/secretstore"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/gitsource"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/integration/secret"
)

type secretBackend string
type oidcBackend string

const (
	directProductionPrototypeProfile = "direct-production-single-node-prototype"
	secretBackendVault               = secretBackend("vault")
	secretBackendDirect              = secretBackend("direct-production-kubernetes-file")
	oidcBackendNetwork               = oidcBackend("network")
	oidcBackendDirectFile            = oidcBackend("direct-production-file")
)

func selectBackends(profile, secretValue, oidcValue string) (secretBackend, oidcBackend, error) {
	secrets, oidcVerifier := secretBackend(secretValue), oidcBackend(oidcValue)
	switch secrets {
	case secretBackendVault:
		if profile == directProductionPrototypeProfile {
			return "", "", errors.New("direct-production prototype cannot use Vault secret backend")
		}
	case secretBackendDirect:
		if profile != directProductionPrototypeProfile {
			return "", "", errors.New("direct secret backend requires exact deployment profile")
		}
	default:
		return "", "", errors.New("integration secret backend is not registered")
	}
	switch oidcVerifier {
	case oidcBackendNetwork:
		if profile == directProductionPrototypeProfile {
			return "", "", errors.New("direct-production prototype cannot use network OIDC discovery")
		}
	case oidcBackendDirectFile:
		if profile != directProductionPrototypeProfile {
			return "", "", errors.New("OIDC file backend requires exact deployment profile")
		}
	default:
		return "", "", errors.New("OIDC verifier backend is not registered")
	}
	return secrets, oidcVerifier, nil
}

type managedSecretStore interface {
	secretstore.Store
	Close()
}

func newSecretStores(config Config, catalog *gitsource.Catalog) (managedSecretStore, managedSecretStore, error) {
	backend, _, err := selectBackends(config.DeploymentProfile, config.SecretBackend, config.OIDCVerifierBackend)
	if err != nil {
		return nil, nil, err
	}
	if backend == secretBackendVault {
		provider, err := secret.NewVault(secret.Config{
			Address: config.VaultAddress, TLSServerName: config.VaultTLSServerName, CAFile: config.VaultCAFile,
			Role: config.VaultRole, AuthMount: config.VaultAuthMount, KVMount: config.VaultKVMount,
			PathPrefix: config.VaultCredentialPathPrefix, ServiceAccountTokenFile: config.VaultServiceAccountTokenFile,
			Timeout: 5 * time.Second,
		})
		if err != nil {
			return nil, nil, err
		}
		git, err := secret.NewVault(secret.Config{
			Address: config.VaultAddress, TLSServerName: config.VaultTLSServerName, CAFile: config.VaultCAFile,
			Role: config.VaultRole, AuthMount: config.VaultAuthMount, KVMount: config.VaultKVMount,
			PathPrefix: config.VaultGitCredentialPathPrefix, ServiceAccountTokenFile: config.VaultServiceAccountTokenFile,
			Timeout: 5 * time.Second, ReadOnly: true,
		})
		if err != nil {
			provider.Close()
			return nil, nil, err
		}
		return provider, git, nil
	}
	provider, err := secret.NewKubernetesStore(
		config.KubernetesProviderSecretName, config.KubernetesProviderSecretDataKey,
		config.VaultCredentialPathPrefix, 5*time.Second,
	)
	if err != nil {
		return nil, nil, err
	}
	git, err := secret.NewGitFileStore(config.GitCredentialAggregateFile, catalog.CredentialRegistry())
	if err != nil {
		provider.Close()
		return nil, nil, err
	}
	return provider, git, nil
}

func newOIDCVerifier(ctx context.Context, config Config) (*oidc.Verifier, error) {
	_, backend, err := selectBackends(config.DeploymentProfile, config.SecretBackend, config.OIDCVerifierBackend)
	if err != nil {
		return nil, err
	}
	if backend == oidcBackendNetwork {
		return oidc.New(ctx, oidc.Config{
			Issuer: config.OIDCIssuer, Audience: config.OIDCAudience,
			TLSServerName: config.OIDCTLSServerName, CAFile: config.OIDCCAFile, Timeout: 5 * time.Second,
		})
	}
	return oidc.NewFile(oidc.FileConfig{
		Issuer: config.OIDCIssuer, Audience: config.OIDCAudience, File: config.OIDCProviderSnapshotFile,
		ExpectedSHA256: config.OIDCProviderSnapshotSHA256, ExpectedGeneration: config.OIDCProviderSnapshotGeneration,
	})
}
