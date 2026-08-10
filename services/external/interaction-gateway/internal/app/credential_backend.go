package app

import (
	"errors"
	"time"

	domaincredential "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/domain/client/credential"
	credentialclient "github.com/codex-k8s/matter-codex/services/external/interaction-gateway/internal/integration/credential"
)

type credentialBackend string

const (
	directProductionPrototypeProfile = "direct-production-single-node-prototype"
	credentialBackendVault           = credentialBackend("vault")
	credentialBackendDirect          = credentialBackend("direct-production-kubernetes-file")
)

type managedCredentialStore interface {
	domaincredential.Store
	Close()
}

func selectCredentialBackend(profile, value string) (credentialBackend, error) {
	backend := credentialBackend(value)
	switch backend {
	case credentialBackendVault:
		if profile == directProductionPrototypeProfile {
			return "", errors.New("direct-production prototype cannot use Vault bot credential backend")
		}
	case credentialBackendDirect:
		if profile != directProductionPrototypeProfile {
			return "", errors.New("direct bot credential backend requires exact deployment profile")
		}
	default:
		return "", errors.New("bot credential backend is not registered")
	}
	return backend, nil
}

func newCredentialStore(config Config) (managedCredentialStore, error) {
	backend, err := selectCredentialBackend(config.DeploymentProfile, config.CredentialBackend)
	if err != nil {
		return nil, err
	}
	if backend == credentialBackendDirect {
		return credentialclient.NewDirect(
			config.DirectCredential.ResourceName, config.DirectCredential.DataKey, config.DirectCredential.Timeout,
		)
	}
	return credentialclient.New(credentialclient.Config{
		Address: config.Credential.Address, TLSServerName: config.Credential.TLSServerName,
		CAFile: config.Credential.CAFile, TokenFile: config.Credential.TokenFile,
		AuthMount: config.Credential.AuthMount, Role: config.Credential.Role,
		Mount: config.Credential.Mount, PathPrefix: config.Credential.PathPrefix, Timeout: config.Credential.Timeout,
	})
}

func defaultDirectCredentialConfig() DirectCredentialConfig {
	return DirectCredentialConfig{
		ResourceName: "interaction-gateway-bot-credentials", DataKey: "state.json", Timeout: 5 * time.Second,
	}
}
