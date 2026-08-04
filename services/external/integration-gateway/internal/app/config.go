package app

import (
	"errors"
	"net"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
)

const serviceName = "integration-gateway"

type Config struct {
	HTTPListen                        string        `env:"INTEGRATION_GATEWAY_HTTP_LISTEN"`
	TechnicalListen                   string        `env:"INTEGRATION_GATEWAY_TECHNICAL_LISTEN"`
	ResultRPCListen                   string        `env:"INTEGRATION_GATEWAY_RESULT_RPC_LISTEN"`
	TLSCertificateFile                string        `env:"INTEGRATION_GATEWAY_TLS_CERTIFICATE_FILE"`
	TLSPrivateKeyFile                 string        `env:"INTEGRATION_GATEWAY_TLS_PRIVATE_KEY_FILE"`
	TLSClientCAFile                   string        `env:"INTEGRATION_GATEWAY_TLS_CLIENT_CA_FILE"`
	TLSAllowedClientSPIFFEIDs         string        `env:"INTEGRATION_GATEWAY_TLS_ALLOWED_CLIENT_SPIFFE_IDS"`
	ResultGrantPublicJWKFile          string        `env:"INTEGRATION_GATEWAY_RESULT_GRANT_PUBLIC_JWK_FILE"`
	ResultGrantIssuer                 string        `env:"INTEGRATION_GATEWAY_RESULT_GRANT_ISSUER"`
	ResultGrantSignerGeneration       uint64        `env:"INTEGRATION_GATEWAY_RESULT_GRANT_SIGNER_GENERATION"`
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
	OIDCIssuer                        string        `env:"INTEGRATION_GATEWAY_OIDC_ISSUER"`
	OIDCAudience                      string        `env:"INTEGRATION_GATEWAY_OIDC_AUDIENCE"`
	OIDCTLSServerName                 string        `env:"INTEGRATION_GATEWAY_OIDC_TLS_SERVER_NAME"`
	OIDCCAFile                        string        `env:"INTEGRATION_GATEWAY_OIDC_CA_FILE"`
	SessionTTL                        time.Duration `env:"INTEGRATION_GATEWAY_SESSION_TTL"`
	InvocationTTL                     time.Duration `env:"INTEGRATION_GATEWAY_INVOCATION_TTL"`
	RequestDeadline                   time.Duration `env:"INTEGRATION_GATEWAY_REQUEST_DEADLINE"`
	StartupTimeout                    time.Duration `env:"INTEGRATION_GATEWAY_STARTUP_TIMEOUT"`
	ShutdownTimeout                   time.Duration `env:"INTEGRATION_GATEWAY_SHUTDOWN_TIMEOUT"`
	ReadinessInterval                 time.Duration `env:"INTEGRATION_GATEWAY_READINESS_INTERVAL"`
	WorkerInterval                    time.Duration `env:"INTEGRATION_GATEWAY_WORKER_INTERVAL"`
	MaximumBodyBytes                  int64         `env:"INTEGRATION_GATEWAY_MAXIMUM_BODY_BYTES"`
	MaximumSessionRequests            uint64        `env:"INTEGRATION_GATEWAY_MAXIMUM_SESSION_REQUESTS"`
	MaximumSessionConcurrency         uint32        `env:"INTEGRATION_GATEWAY_MAXIMUM_SESSION_CONCURRENCY"`
	MaximumGlobalConcurrency          int           `env:"INTEGRATION_GATEWAY_MAXIMUM_GLOBAL_CONCURRENCY"`
}

func loadConfig() (Config, error) {
	config := Config{
		HTTPListen: ":8443", TechnicalListen: ":9090", ResultRPCListen: ":9443",
		TLSCertificateFile: "/var/run/secrets/mattercodex/integration-gateway/workload-tls/tls.crt",
		TLSPrivateKeyFile:  "/var/run/secrets/mattercodex/integration-gateway/workload-tls/tls.key",
		TLSClientCAFile:    "/var/run/config/mattercodex/integration-gateway/client-ca/ca.pem",
		TLSAllowedClientSPIFFEIDs: "spiffe://mattercodex.local/ns/mattercodex-system/sa/agent-runner," +
			"spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
		ResultGrantPublicJWKFile:    "/var/run/config/mattercodex/integration-gateway/result-grant/continuation.public-keyset.json",
		ResultGrantIssuer:           "https://control-plane.mattercodex-system.svc.cluster.local/authority/integration-continuation",
		ResultGrantSignerGeneration: 1, AuthorityVerifierUID: 29001, AuthorityVerifierGID: 29000,
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
		OIDCIssuer:                        "https://sso.mattercodex.local", OIDCAudience: "mattercodex-integration-gateway",
		OIDCTLSServerName: "sso.mattercodex.local", OIDCCAFile: "/var/run/config/mattercodex/integration-gateway/oidc/ca.pem",
		SessionTTL: 30 * time.Minute, InvocationTTL: 7 * 24 * time.Hour, RequestDeadline: 30 * time.Second,
		StartupTimeout: 15 * time.Second, ShutdownTimeout: 10 * time.Second, ReadinessInterval: 10 * time.Second,
		WorkerInterval: 250 * time.Millisecond, MaximumBodyBytes: 512 << 10,
		MaximumSessionRequests: 10000, MaximumSessionConcurrency: 4, MaximumGlobalConcurrency: 128,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

func (config Config) validate() error {
	for _, address := range []string{config.HTTPListen, config.TechnicalListen, config.ResultRPCListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("integration gateway listen address is invalid")
		}
	}
	for _, name := range []string{
		config.PostgresTLSServerName, config.ControlPlaneTLSServerName,
		config.ProviderProxyTLSServerName, config.OIDCTLSServerName,
	} {
		if name == "" || net.ParseIP(name) != nil {
			return errors.New("integration gateway TLS server name is invalid")
		}
	}
	for _, path := range []string{
		config.TLSCertificateFile, config.TLSPrivateKeyFile, config.TLSClientCAFile, config.ResultGrantPublicJWKFile, config.PostgresDSNFile,
		config.PostgresCAFile, config.PostgresContextKeyFile, config.DefinitionDirectory,
		config.CredentialDirectory, config.PayloadKeysetFile, config.ControlPlaneCAFile,
		config.ControlPlaneClientCertificateFile, config.ControlPlaneClientPrivateKeyFile,
		config.ControlPlaneApplicationGrantFile, config.ProviderProxyCAFile, config.OIDCCAFile,
	} {
		if !filepath.IsAbs(path) {
			return errors.New("integration gateway runtime path is invalid")
		}
	}
	if config.TLSAllowedClientSPIFFEIDs == "" || config.ResultGrantIssuer == "" || config.ResultGrantSignerGeneration == 0 ||
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
	return nil
}
