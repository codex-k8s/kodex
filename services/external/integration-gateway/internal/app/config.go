package app

import (
	"encoding/hex"
	"errors"
	"net"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	serviceName                      = "integration-gateway"
	platformManagementEgressProxyURL = "http://egress-gateway.mattercodex-system.svc.cluster.local:8080"
	managementEgressNoProxy          = "localhost,127.0.0.1,::1,.svc,.svc.cluster.local"
)

type Config struct {
	DeploymentProfile                 string        `env:"INTEGRATION_GATEWAY_DEPLOYMENT_PROFILE"`
	SecretBackend                     string        `env:"INTEGRATION_GATEWAY_SECRET_BACKEND"`
	OIDCVerifierBackend               string        `env:"INTEGRATION_GATEWAY_OIDC_VERIFIER_BACKEND"`
	HTTPListen                        string        `env:"INTEGRATION_GATEWAY_HTTP_LISTEN"`
	TechnicalListen                   string        `env:"INTEGRATION_GATEWAY_TECHNICAL_LISTEN"`
	ResultRPCListen                   string        `env:"INTEGRATION_GATEWAY_RESULT_RPC_LISTEN"`
	TLSCertificateFile                string        `env:"INTEGRATION_GATEWAY_TLS_CERTIFICATE_FILE"`
	TLSPrivateKeyFile                 string        `env:"INTEGRATION_GATEWAY_TLS_PRIVATE_KEY_FILE"`
	TLSClientCAFile                   string        `env:"INTEGRATION_GATEWAY_TLS_CLIENT_CA_FILE"`
	TLSAllowedClientSPIFFEIDs         string        `env:"INTEGRATION_GATEWAY_TLS_ALLOWED_CLIENT_SPIFFE_IDS"`
	AuthorityVerifierUID              uint32        `env:"INTEGRATION_GATEWAY_AUTHORITY_VERIFIER_UID"`
	AuthorityVerifierGID              uint32        `env:"INTEGRATION_GATEWAY_AUTHORITY_VERIFIER_GID"`
	PostgresDSNFile                   string        `env:"INTEGRATION_GATEWAY_POSTGRES_DSN_FILE"`
	PostgresCAFile                    string        `env:"INTEGRATION_GATEWAY_POSTGRES_CA_FILE"`
	PostgresTLSServerName             string        `env:"INTEGRATION_GATEWAY_POSTGRES_TLS_SERVER_NAME"`
	PostgresMaxConnections            int32         `env:"INTEGRATION_GATEWAY_POSTGRES_MAX_CONNECTIONS"`
	PostgresPrincipalName             string        `env:"INTEGRATION_GATEWAY_POSTGRES_PRINCIPAL_NAME"`
	PostgresPrincipalGeneration       uint64        `env:"INTEGRATION_GATEWAY_POSTGRES_PRINCIPAL_GENERATION"`
	PostgresContextKeyID              string        `env:"INTEGRATION_GATEWAY_POSTGRES_CONTEXT_KEY_ID"`
	PostgresContextKeyFile            string        `env:"INTEGRATION_GATEWAY_POSTGRES_CONTEXT_KEY_FILE"`
	DefinitionDirectory               string        `env:"INTEGRATION_GATEWAY_DEFINITION_DIRECTORY"`
	CredentialDirectory               string        `env:"INTEGRATION_GATEWAY_CREDENTIAL_DIRECTORY"`
	PayloadKeysetFile                 string        `env:"INTEGRATION_GATEWAY_PAYLOAD_KEYSET_FILE"`
	ControlPlaneTarget                string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName         string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile                string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_CA_FILE"`
	ControlPlaneClientCertificateFile string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_CLIENT_CERTIFICATE_FILE"`
	ControlPlaneClientPrivateKeyFile  string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_CLIENT_PRIVATE_KEY_FILE"`
	ControlPlaneApplicationGrantFile  string        `env:"INTEGRATION_GATEWAY_CONTROL_PLANE_APPLICATION_GRANT_FILE"`
	ProviderProxyURL                  string        `env:"INTEGRATION_GATEWAY_PROVIDER_PROXY_URL"`
	ProviderProxyTLSServerName        string        `env:"INTEGRATION_GATEWAY_PROVIDER_PROXY_TLS_SERVER_NAME"`
	ProviderProxyCAFile               string        `env:"INTEGRATION_GATEWAY_PROVIDER_PROXY_CA_FILE"`
	ManagementEgressProxyURL          string        `env:"INTEGRATION_GATEWAY_MANAGEMENT_EGRESS_PROXY_URL"`
	ManagementEgressNoProxy           string        `env:"INTEGRATION_GATEWAY_MANAGEMENT_EGRESS_NO_PROXY"`
	ProviderCatalogFile               string        `env:"INTEGRATION_GATEWAY_PROVIDER_CATALOG_FILE"`
	GitSourceCatalogFile              string        `env:"INTEGRATION_GATEWAY_GIT_SOURCE_CATALOG_FILE"`
	GitExecutable                     string        `env:"INTEGRATION_GATEWAY_GIT_EXECUTABLE"`
	GitTemporaryRoot                  string        `env:"INTEGRATION_GATEWAY_GIT_TEMPORARY_ROOT"`
	GitCAFile                         string        `env:"INTEGRATION_GATEWAY_GIT_CA_FILE"`
	CodexExecutable                   string        `env:"INTEGRATION_GATEWAY_CODEX_EXECUTABLE"`
	CodexTemporaryRoot                string        `env:"INTEGRATION_GATEWAY_CODEX_TEMPORARY_ROOT"`
	CodexCAFile                       string        `env:"INTEGRATION_GATEWAY_CODEX_CA_FILE"`
	VaultAddress                      string        `env:"INTEGRATION_GATEWAY_VAULT_ADDRESS"`
	VaultTLSServerName                string        `env:"INTEGRATION_GATEWAY_VAULT_TLS_SERVER_NAME"`
	VaultCAFile                       string        `env:"INTEGRATION_GATEWAY_VAULT_CA_FILE"`
	VaultRole                         string        `env:"INTEGRATION_GATEWAY_VAULT_ROLE"`
	VaultAuthMount                    string        `env:"INTEGRATION_GATEWAY_VAULT_AUTH_MOUNT"`
	VaultKVMount                      string        `env:"INTEGRATION_GATEWAY_VAULT_KV_MOUNT"`
	VaultCredentialPathPrefix         string        `env:"INTEGRATION_GATEWAY_VAULT_CREDENTIAL_PATH_PREFIX"`
	VaultGitCredentialPathPrefix      string        `env:"INTEGRATION_GATEWAY_VAULT_GIT_CREDENTIAL_PATH_PREFIX"`
	VaultServiceAccountTokenFile      string        `env:"INTEGRATION_GATEWAY_VAULT_SERVICE_ACCOUNT_TOKEN_FILE"`
	KubernetesProviderSecretName      string        `env:"INTEGRATION_GATEWAY_KUBERNETES_PROVIDER_SECRET_NAME"`
	KubernetesProviderSecretDataKey   string        `env:"INTEGRATION_GATEWAY_KUBERNETES_PROVIDER_SECRET_DATA_KEY"`
	GitCredentialAggregateFile        string        `env:"INTEGRATION_GATEWAY_GIT_CREDENTIAL_AGGREGATE_FILE"`
	ProviderReceiptIssuer             string        `env:"INTEGRATION_GATEWAY_PROVIDER_RECEIPT_ISSUER"`
	ProviderReceiptPrivateJWKFile     string        `env:"INTEGRATION_GATEWAY_PROVIDER_RECEIPT_PRIVATE_JWK_FILE"`
	GitReceiptIssuer                  string        `env:"INTEGRATION_GATEWAY_GIT_RECEIPT_ISSUER"`
	GitReceiptPrivateJWKFile          string        `env:"INTEGRATION_GATEWAY_GIT_RECEIPT_PRIVATE_JWK_FILE"`
	OIDCIssuer                        string        `env:"INTEGRATION_GATEWAY_OIDC_ISSUER"`
	OIDCAudience                      string        `env:"INTEGRATION_GATEWAY_OIDC_AUDIENCE"`
	OIDCTLSServerName                 string        `env:"INTEGRATION_GATEWAY_OIDC_TLS_SERVER_NAME"`
	OIDCCAFile                        string        `env:"INTEGRATION_GATEWAY_OIDC_CA_FILE"`
	OIDCProviderSnapshotFile          string        `env:"INTEGRATION_GATEWAY_OIDC_PROVIDER_SNAPSHOT_FILE"`
	OIDCProviderSnapshotSHA256        string        `env:"INTEGRATION_GATEWAY_OIDC_PROVIDER_SNAPSHOT_SHA256"`
	OIDCProviderSnapshotGeneration    uint64        `env:"INTEGRATION_GATEWAY_OIDC_PROVIDER_SNAPSHOT_GENERATION"`
	SessionTTL                        time.Duration `env:"INTEGRATION_GATEWAY_SESSION_TTL"`
	InvocationTTL                     time.Duration `env:"INTEGRATION_GATEWAY_INVOCATION_TTL"`
	RequestDeadline                   time.Duration `env:"INTEGRATION_GATEWAY_REQUEST_DEADLINE"`
	StartupTimeout                    time.Duration `env:"INTEGRATION_GATEWAY_STARTUP_TIMEOUT"`
	ShutdownTimeout                   time.Duration `env:"INTEGRATION_GATEWAY_SHUTDOWN_TIMEOUT"`
	ReadinessInterval                 time.Duration `env:"INTEGRATION_GATEWAY_READINESS_INTERVAL"`
	WorkerInterval                    time.Duration `env:"INTEGRATION_GATEWAY_WORKER_INTERVAL"`
	ManagementLeaseDuration           time.Duration `env:"INTEGRATION_GATEWAY_MANAGEMENT_LEASE_DURATION"`
	AuthorizationTTL                  time.Duration `env:"INTEGRATION_GATEWAY_AUTHORIZATION_TTL"`
	ProviderAuthorizationTimeout      time.Duration `env:"INTEGRATION_GATEWAY_PROVIDER_AUTHORIZATION_TIMEOUT"`
	ProviderAuthorizationPollInterval time.Duration `env:"INTEGRATION_GATEWAY_PROVIDER_AUTHORIZATION_POLL_INTERVAL"`
	GitFetchTimeout                   time.Duration `env:"INTEGRATION_GATEWAY_GIT_FETCH_TIMEOUT"`
	EffectReceiptTTL                  time.Duration `env:"INTEGRATION_GATEWAY_EFFECT_RECEIPT_TTL"`
	MaximumBodyBytes                  int64         `env:"INTEGRATION_GATEWAY_MAXIMUM_BODY_BYTES"`
	MaximumSessionRequests            uint64        `env:"INTEGRATION_GATEWAY_MAXIMUM_SESSION_REQUESTS"`
	MaximumSessionConcurrency         uint32        `env:"INTEGRATION_GATEWAY_MAXIMUM_SESSION_CONCURRENCY"`
	MaximumGlobalConcurrency          int           `env:"INTEGRATION_GATEWAY_MAXIMUM_GLOBAL_CONCURRENCY"`
}

func loadConfig() (Config, error) {
	config := Config{
		DeploymentProfile: "production", SecretBackend: string(secretBackendVault), OIDCVerifierBackend: string(oidcBackendNetwork),
		HTTPListen: ":8443", TechnicalListen: ":9090", ResultRPCListen: ":9443",
		TLSCertificateFile: "/var/run/secrets/mattercodex/integration-gateway/workload-tls/tls.crt",
		TLSPrivateKeyFile:  "/var/run/secrets/mattercodex/integration-gateway/workload-tls/tls.key",
		TLSClientCAFile:    "/var/run/config/mattercodex/integration-gateway/client-ca/ca.pem",
		TLSAllowedClientSPIFFEIDs: "spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner," +
			"spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
		AuthorityVerifierUID: 29001, AuthorityVerifierGID: 29000,
		PostgresDSNFile:        "/var/run/secrets/mattercodex/integration-gateway/postgres-runtime/dsn",
		PostgresCAFile:         "/var/run/config/mattercodex/integration-gateway/postgres/ca.pem",
		PostgresTLSServerName:  "integration-gateway-postgresql-rw.mattercodex-system.svc.cluster.local",
		PostgresMaxConnections: 16, PostgresPrincipalName: "integration_gateway_runtime_g1",
		PostgresPrincipalGeneration: 1, PostgresContextKeyID: "integration-gateway-db-context-g1",
		PostgresContextKeyFile:            "/var/run/secrets/mattercodex/integration-gateway/postgres-context/key",
		DefinitionDirectory:               "/var/run/config/mattercodex/integration-gateway/definitions",
		CredentialDirectory:               "/var/run/secrets/mattercodex/integration-gateway/credentials",
		PayloadKeysetFile:                 "/var/run/secrets/mattercodex/integration-gateway/payload-keyset/keyset.json",
		ControlPlaneTarget:                "dns:///control-plane.mattercodex-system.svc:8443",
		ControlPlaneTLSServerName:         "control-plane.mattercodex-system.svc.cluster.local",
		ControlPlaneCAFile:                "/var/run/config/mattercodex/integration-gateway/control-plane/ca.pem",
		ControlPlaneClientCertificateFile: "/var/run/secrets/mattercodex/integration-gateway/control-plane-client/tls.crt",
		ControlPlaneClientPrivateKeyFile:  "/var/run/secrets/mattercodex/integration-gateway/control-plane-client/tls.key",
		ControlPlaneApplicationGrantFile:  "/var/run/secrets/mattercodex/integration-gateway/application-grant/readiness.jwt",
		ProviderProxyURL:                  "https://integration-egress-proxy.mattercodex-system.svc:8443",
		ProviderProxyTLSServerName:        "integration-egress-proxy.mattercodex-system.svc.cluster.local",
		ProviderProxyCAFile:               "/var/run/config/mattercodex/integration-gateway/provider-proxy/ca.pem",
		ManagementEgressProxyURL:          platformManagementEgressProxyURL,
		ManagementEgressNoProxy:           managementEgressNoProxy,
		ProviderCatalogFile:               "/var/run/config/mattercodex/integration-gateway/provider-catalog/catalog.json",
		GitSourceCatalogFile:              "/var/run/config/mattercodex/integration-gateway/git-sources/catalog.json",
		GitExecutable:                     "/usr/bin/git",
		GitTemporaryRoot:                  "/var/lib/mattercodex/integration-gateway/git",
		GitCAFile:                         "/etc/ssl/certs/ca-certificates.crt",
		CodexExecutable:                   "/usr/local/bin/codex",
		CodexTemporaryRoot:                "/var/lib/mattercodex/integration-gateway/codex",
		CodexCAFile:                       "/etc/ssl/certs/ca-certificates.crt",
		VaultAddress:                      "https://vault.mattercodex-system.svc:8200",
		VaultTLSServerName:                "vault.mattercodex-system.svc.cluster.local",
		VaultCAFile:                       "/var/run/config/mattercodex/integration-gateway/vault/ca.pem",
		VaultRole:                         "integration-gateway",
		VaultAuthMount:                    "kubernetes",
		VaultKVMount:                      "kv",
		VaultCredentialPathPrefix:         "mattercodex/integration-gateway/provider-credentials",
		VaultGitCredentialPathPrefix:      "mattercodex/integration-gateway/git-credentials",
		VaultServiceAccountTokenFile:      "/var/run/secrets/tokens/vault/token",
		KubernetesProviderSecretName:      "integration-gateway-provider-credentials",
		KubernetesProviderSecretDataKey:   "state.json",
		GitCredentialAggregateFile:        "/var/run/secrets/mattercodex/integration-gateway/git-credentials/state.json",
		ProviderReceiptIssuer:             "https://integration-gateway.mattercodex-system.svc.cluster.local/authority/provider-readback",
		ProviderReceiptPrivateJWKFile:     "/var/run/secrets/mattercodex/integration-gateway/provider-receipt/private.jwk",
		GitReceiptIssuer:                  "https://integration-gateway.mattercodex-system.svc.cluster.local/authority/git-reconciliation",
		GitReceiptPrivateJWKFile:          "/var/run/secrets/mattercodex/integration-gateway/git-receipt/private.jwk",
		OIDCIssuer:                        "https://sso.kodex.works/realms/mattercodex", OIDCAudience: "mattercodex-integration-gateway",
		OIDCTLSServerName: "sso.kodex.works", OIDCCAFile: "/var/run/config/mattercodex/integration-gateway/oidc/ca.pem",
		OIDCProviderSnapshotFile: "/var/run/config/mattercodex/integration-gateway/oidc/provider-snapshot.json",
		SessionTTL:               30 * time.Minute, InvocationTTL: 7 * 24 * time.Hour, RequestDeadline: 30 * time.Second,
		StartupTimeout: 15 * time.Second, ShutdownTimeout: 10 * time.Second, ReadinessInterval: 10 * time.Second,
		WorkerInterval: 250 * time.Millisecond, MaximumBodyBytes: 512 << 10,
		ManagementLeaseDuration: 20 * time.Second, AuthorizationTTL: 15 * time.Minute,
		ProviderAuthorizationTimeout: 15 * time.Minute, ProviderAuthorizationPollInterval: time.Second,
		GitFetchTimeout: 30 * time.Second, EffectReceiptTTL: time.Minute,
		MaximumSessionRequests: 10000, MaximumSessionConcurrency: 4, MaximumGlobalConcurrency: 128,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

func (config Config) validate() error {
	secretSelection, oidcSelection, err := selectBackends(config.DeploymentProfile, config.SecretBackend, config.OIDCVerifierBackend)
	if err != nil {
		return err
	}
	for _, address := range []string{config.HTTPListen, config.TechnicalListen, config.ResultRPCListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("integration gateway listen address is invalid")
		}
	}
	names := []string{
		config.PostgresTLSServerName, config.ControlPlaneTLSServerName,
		config.ProviderProxyTLSServerName,
	}
	if oidcSelection == oidcBackendNetwork {
		names = append(names, config.OIDCTLSServerName)
	}
	if secretSelection == secretBackendVault {
		names = append(names, config.VaultTLSServerName)
	}
	for _, name := range names {
		if name == "" || net.ParseIP(name) != nil {
			return errors.New("integration gateway TLS server name is invalid")
		}
	}
	paths := []string{
		config.TLSCertificateFile, config.TLSPrivateKeyFile, config.TLSClientCAFile, config.PostgresDSNFile,
		config.PostgresCAFile, config.PostgresContextKeyFile, config.DefinitionDirectory,
		config.CredentialDirectory, config.PayloadKeysetFile, config.ControlPlaneCAFile,
		config.ControlPlaneClientCertificateFile, config.ControlPlaneClientPrivateKeyFile,
		config.ControlPlaneApplicationGrantFile, config.ProviderProxyCAFile,
		config.ProviderCatalogFile, config.GitSourceCatalogFile, config.GitExecutable, config.GitTemporaryRoot,
		config.GitCAFile, config.CodexExecutable, config.CodexTemporaryRoot, config.CodexCAFile,
		config.ProviderReceiptPrivateJWKFile,
		config.GitReceiptPrivateJWKFile,
	}
	if secretSelection == secretBackendVault {
		paths = append(paths, config.VaultCAFile, config.VaultServiceAccountTokenFile)
	} else {
		paths = append(paths, config.GitCredentialAggregateFile)
	}
	if oidcSelection == oidcBackendNetwork {
		paths = append(paths, config.OIDCCAFile)
	} else {
		paths = append(paths, config.OIDCProviderSnapshotFile)
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return errors.New("integration gateway runtime path is invalid")
		}
	}
	if config.TLSAllowedClientSPIFFEIDs == "" ||
		config.AuthorityVerifierUID == 0 || config.AuthorityVerifierGID == 0 ||
		config.PostgresMaxConnections < 2 || config.PostgresMaxConnections > 64 ||
		config.PostgresPrincipalName == "" || config.PostgresPrincipalGeneration == 0 || config.PostgresContextKeyID == "" ||
		config.SessionTTL < time.Minute || config.SessionTTL > 24*time.Hour || config.InvocationTTL < time.Minute || config.InvocationTTL > 7*24*time.Hour ||
		config.RequestDeadline < time.Second || config.RequestDeadline > time.Minute || config.StartupTimeout < time.Second ||
		config.ShutdownTimeout < time.Second || config.ReadinessInterval < time.Second || config.WorkerInterval < 50*time.Millisecond ||
		config.MaximumBodyBytes < 1024 || config.MaximumBodyBytes > 1<<20 || config.MaximumSessionRequests == 0 ||
		config.MaximumSessionConcurrency == 0 || config.MaximumSessionConcurrency > 32 ||
		config.MaximumGlobalConcurrency < 1 || config.MaximumGlobalConcurrency > 1024 {
		return errors.New("integration gateway bounded configuration is invalid")
	}
	if config.ManagementEgressProxyURL != platformManagementEgressProxyURL || config.ManagementEgressNoProxy != managementEgressNoProxy || config.VaultCredentialPathPrefix != "mattercodex/integration-gateway/provider-credentials" || config.VaultGitCredentialPathPrefix != "mattercodex/integration-gateway/git-credentials" ||
		config.ProviderReceiptIssuer == "" || config.GitReceiptIssuer == "" || config.ManagementLeaseDuration < 5*time.Second || config.ManagementLeaseDuration > time.Minute ||
		config.AuthorizationTTL < time.Minute || config.AuthorizationTTL > 15*time.Minute || config.ProviderAuthorizationTimeout < time.Minute || config.ProviderAuthorizationTimeout > 20*time.Minute ||
		config.ProviderAuthorizationPollInterval < 100*time.Millisecond || config.ProviderAuthorizationPollInterval > 5*time.Second || config.GitFetchTimeout < time.Second || config.GitFetchTimeout > time.Minute ||
		config.EffectReceiptTTL < 30*time.Second || config.EffectReceiptTTL > 5*time.Minute {
		return errors.New("integration management configuration is invalid")
	}
	if secretSelection == secretBackendVault && (config.VaultAddress == "" || config.VaultRole == "" || config.VaultAuthMount == "" || config.VaultKVMount == "") {
		return errors.New("integration Vault configuration is invalid")
	}
	if secretSelection == secretBackendDirect && (config.KubernetesProviderSecretName != "integration-gateway-provider-credentials" ||
		config.KubernetesProviderSecretDataKey != "state.json" ||
		config.GitCredentialAggregateFile != "/var/run/secrets/mattercodex/integration-gateway/git-credentials/state.json") {
		return errors.New("integration direct Secret registry is invalid")
	}
	if oidcSelection == oidcBackendDirectFile && (config.OIDCProviderSnapshotFile != "/var/run/config/mattercodex/integration-gateway/oidc/provider-snapshot.json" ||
		!validSHA256(config.OIDCProviderSnapshotSHA256) || config.OIDCProviderSnapshotGeneration == 0 ||
		config.OIDCIssuer != "https://sso.kodex.works/realms/mattercodex" || config.OIDCAudience != "mattercodex-integration-gateway") {
		return errors.New("integration direct OIDC registry is invalid")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
