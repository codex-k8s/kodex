package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"slices"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	defaultControlPlaneTarget         = "dns:///control-plane.kodex-system.svc:8443"
	defaultControlPlaneTLSServerName  = "control-plane.kodex-system.svc.cluster.local"
	defaultControlAPIClientSPIFFEID   = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
	defaultControlPlaneClientSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	defaultProviderEgressProxy        = "http://egress-gateway.kodex-system.svc.cluster.local:8080"
)

type Config struct {
	RuntimeNamespace            string        `env:"SECRET_BROKER_RUNTIME_NAMESPACE"`
	ClaimantID                  string        `env:"POD_UID"`
	GRPCListen                  string        `env:"SECRET_BROKER_GRPC_LISTEN"`
	TechnicalListen             string        `env:"SECRET_BROKER_TECHNICAL_LISTEN"`
	ServerCertificateFile       string        `env:"SECRET_BROKER_SERVER_CERTIFICATE_FILE"`
	ServerPrivateKeyFile        string        `env:"SECRET_BROKER_SERVER_PRIVATE_KEY_FILE"`
	ClientCAFile                string        `env:"SECRET_BROKER_CLIENT_CA_FILE"`
	ExpectedClientSPIFFEIDs     []string      `env:"SECRET_BROKER_EXPECTED_CLIENT_SPIFFE_IDS" envSeparator:","`
	ControlPlaneTarget          string        `env:"SECRET_BROKER_CONTROL_PLANE_TARGET"`
	ControlPlaneTLSServerName   string        `env:"SECRET_BROKER_CONTROL_PLANE_TLS_SERVER_NAME"`
	ControlPlaneCAFile          string        `env:"SECRET_BROKER_CONTROL_PLANE_CA_FILE"`
	ControlPlaneCertificateFile string        `env:"SECRET_BROKER_CONTROL_PLANE_CERTIFICATE_FILE"`
	ControlPlanePrivateKeyFile  string        `env:"SECRET_BROKER_CONTROL_PLANE_PRIVATE_KEY_FILE"`
	ApplicationGrantFile        string        `env:"SECRET_BROKER_APPLICATION_GRANT_FILE"`
	RequestTimeout              time.Duration `env:"SECRET_BROKER_REQUEST_TIMEOUT"`
	ShutdownTimeout             time.Duration `env:"SECRET_BROKER_SHUTDOWN_TIMEOUT"`
	MaximumSecretBytes          int           `env:"SECRET_BROKER_MAXIMUM_SECRET_BYTES"`
	RecoveryInterval            time.Duration `env:"SECRET_BROKER_RECOVERY_INTERVAL"`
	RecoveryTimeout             time.Duration `env:"SECRET_BROKER_RECOVERY_TIMEOUT"`
	CodexBinary                 string        `env:"SECRET_BROKER_CODEX_BINARY"`
	ProviderAuthorizationRoot   string        `env:"SECRET_BROKER_PROVIDER_AUTHORIZATION_ROOT"`
	ProviderDeviceAuthTTL       time.Duration `env:"SECRET_BROKER_PROVIDER_DEVICE_AUTH_TTL"`
	ProviderHTTPSProxy          string        `env:"HTTPS_PROXY"`
	ProviderHTTPProxy           string        `env:"HTTP_PROXY"`
	AuthorityVerifierSocket     string        `env:"INTERNAL_RPC_AUTHORITY_VERIFIER_SOCKET"`
	AuthorityVerifierUID        uint32        `env:"SECRET_BROKER_AUTHORITY_VERIFIER_UID"`
	AuthorityVerifierGID        uint32        `env:"SECRET_BROKER_AUTHORITY_VERIFIER_GID"`
}

func loadConfig() (Config, error) {
	config := Config{
		RuntimeNamespace: "kodex-runtime", GRPCListen: ":8443", TechnicalListen: ":9090",
		ServerCertificateFile:       "/var/run/secrets/kodex/secret-broker/server/tls.crt",
		ServerPrivateKeyFile:        "/var/run/secrets/kodex/secret-broker/server/tls.key",
		ClientCAFile:                "/var/run/config/kodex/secret-broker/client/ca.pem",
		ExpectedClientSPIFFEIDs:     []string{defaultControlAPIClientSPIFFEID, defaultControlPlaneClientSPIFFEID},
		ControlPlaneTarget:          defaultControlPlaneTarget,
		ControlPlaneTLSServerName:   defaultControlPlaneTLSServerName,
		ControlPlaneCAFile:          "/var/run/config/kodex/secret-broker/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/kodex/secret-broker/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/kodex/secret-broker/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/kodex/secret-broker/application-grant/application-grant.jws",
		RequestTimeout:              5 * time.Second, ShutdownTimeout: 20 * time.Second, MaximumSecretBytes: 512 << 10,
		RecoveryInterval: 30 * time.Second, RecoveryTimeout: 10 * time.Second,
		CodexBinary: "/usr/local/bin/codex", ProviderAuthorizationRoot: "/var/lib/kodex-provider-auth/sessions",
		ProviderDeviceAuthTTL: 15 * time.Minute, ProviderHTTPSProxy: defaultProviderEgressProxy,
		ProviderHTTPProxy: defaultProviderEgressProxy, AuthorityVerifierSocket: "/run/kodex/internal-rpc-authority/verifier.sock",
		AuthorityVerifierUID: 29002, AuthorityVerifierGID: 29000,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, err
	}
	return config, config.validate()
}

func (config Config) validate() error {
	if config.RuntimeNamespace != "kodex-runtime" || config.ClaimantID == "" || len(config.ClaimantID) > 128 ||
		config.ControlPlaneTarget != defaultControlPlaneTarget ||
		config.ControlPlaneTLSServerName != defaultControlPlaneTLSServerName ||
		config.RequestTimeout < time.Second || config.RequestTimeout > 10*time.Second ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.MaximumSecretBytes < 1<<10 || config.MaximumSecretBytes > 1<<20 ||
		config.RecoveryInterval < 5*time.Second || config.RecoveryInterval > 5*time.Minute ||
		config.RecoveryTimeout < time.Second || config.RecoveryTimeout > 30*time.Second {
		return errors.New("secret broker configuration is invalid")
	}
	for _, character := range config.ClaimantID {
		if character < 0x21 || character > 0x7e {
			return errors.New("secret broker claimant ID is invalid")
		}
	}
	for _, address := range []string{config.GRPCListen, config.TechnicalListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("secret broker listen address is invalid")
		}
	}
	if config.ProviderDeviceAuthTTL < time.Minute || config.ProviderDeviceAuthTTL > time.Hour ||
		config.ProviderHTTPSProxy != defaultProviderEgressProxy || config.ProviderHTTPProxy != defaultProviderEgressProxy ||
		config.AuthorityVerifierUID == 0 || config.AuthorityVerifierGID == 0 || len(config.ExpectedClientSPIFFEIDs) != 2 ||
		!slices.Contains(config.ExpectedClientSPIFFEIDs, defaultControlAPIClientSPIFFEID) ||
		!slices.Contains(config.ExpectedClientSPIFFEIDs, defaultControlPlaneClientSPIFFEID) {
		return errors.New("secret broker provider credential configuration is invalid")
	}
	for _, path := range []string{config.ServerCertificateFile, config.ServerPrivateKeyFile, config.ClientCAFile,
		config.ControlPlaneCAFile, config.ControlPlaneCertificateFile, config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile,
		config.CodexBinary, config.ProviderAuthorizationRoot, config.AuthorityVerifierSocket} {
		if !filepath.IsAbs(path) {
			return errors.New("secret broker file path is invalid")
		}
	}
	for _, rawIdentity := range config.ExpectedClientSPIFFEIDs {
		identity, err := url.Parse(rawIdentity)
		if err != nil || identity.Scheme != "spiffe" || identity.Host == "" || identity.Path == "" || identity.RawQuery != "" || identity.Fragment != "" {
			return errors.New("secret broker client identity is invalid")
		}
	}
	return nil
}
