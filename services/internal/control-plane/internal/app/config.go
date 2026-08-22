package app

import (
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
)

type Config struct {
	GRPCListen                string        `env:"CONTROL_PLANE_GRPC_LISTEN"`
	TechnicalListen           string        `env:"CONTROL_PLANE_TECHNICAL_LISTEN"`
	ServerCertificateFile     string        `env:"CONTROL_PLANE_TLS_CERTIFICATE_FILE"`
	ServerPrivateKeyFile      string        `env:"CONTROL_PLANE_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile              string        `env:"CONTROL_PLANE_TLS_CLIENT_CA_FILE"`
	PostgresDSNFile           string        `env:"CONTROL_PLANE_POSTGRES_DSN_FILE"`
	PostgresCAFile            string        `env:"CONTROL_PLANE_POSTGRES_CA_FILE"`
	PostgresTLSServerName     string        `env:"CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME"`
	PostgresMaxConnections    int32         `env:"CONTROL_PLANE_POSTGRES_MAX_CONNECTIONS"`
	NATSURL                   string        `env:"CONTROL_PLANE_NATS_URL"`
	NATSTLSServerName         string        `env:"CONTROL_PLANE_NATS_TLS_SERVER_NAME"`
	NATSCAFile                string        `env:"CONTROL_PLANE_NATS_CA_FILE"`
	NATSCredentialsFile       string        `env:"CONTROL_PLANE_NATS_CREDENTIALS_FILE"`
	NATSStream                string        `env:"CONTROL_PLANE_NATS_STREAM"`
	NATSReplicas              int           `env:"CONTROL_PLANE_NATS_REPLICAS"`
	NATSMaxBytes              int64         `env:"CONTROL_PLANE_NATS_MAX_BYTES"`
	InstanceID                string        `env:"POD_UID"`
	DefaultRuntimeProvider    string        `env:"CONTROL_PLANE_DEFAULT_RUNTIME_PROVIDER"`
	DefaultRuntimeModel       string        `env:"CONTROL_PLANE_DEFAULT_RUNTIME_MODEL"`
	AuthorityVerifierSocket   string        `env:"CONTROL_PLANE_AUTHORITY_VERIFIER_SOCKET"`
	AuthorityVerifierUID      uint32        `env:"CONTROL_PLANE_AUTHORITY_VERIFIER_UID"`
	AuthorityVerifierGID      uint32        `env:"CONTROL_PLANE_AUTHORITY_VERIFIER_GID"`
	AuthorityPolicyFile       string        `env:"CONTROL_PLANE_AUTHORITY_POLICY_FILE"`
	ProofSignerFile           string        `env:"CONTROL_PLANE_AUTHORITY_PROOF_SIGNER_FILE"`
	ProofSignerTrustFile      string        `env:"CONTROL_PLANE_AUTHORITY_PROOF_TRUST_FILE"`
	AutomationGrantTrustFile  string        `env:"CONTROL_PLANE_AUTOMATION_GRANT_TRUST_FILE"`
	IntegrationGrantTrustFile string        `env:"CONTROL_PLANE_INTEGRATION_GRANT_TRUST_FILE"`
	RuntimeGrantTrustFile     string        `env:"CONTROL_PLANE_RUNTIME_GRANT_TRUST_FILE"`
	OIDCIssuer                string        `env:"CONTROL_PLANE_OIDC_ISSUER"`
	OIDCAudience              string        `env:"CONTROL_PLANE_OIDC_AUDIENCE"`
	OIDCJWKSURL               string        `env:"CONTROL_PLANE_OIDC_JWKS_URL"`
	OIDCConnectAddress        string        `env:"CONTROL_PLANE_OIDC_CONNECT_ADDRESS"`
	OIDCTLSServerName         string        `env:"CONTROL_PLANE_OIDC_TLS_SERVER_NAME"`
	OIDCCAFile                string        `env:"CONTROL_PLANE_OIDC_CA_FILE"`
	OIDCRefreshInterval       time.Duration `env:"CONTROL_PLANE_OIDC_REFRESH_INTERVAL"`
	StartupTimeout            time.Duration `env:"CONTROL_PLANE_STARTUP_TIMEOUT"`
	ReadinessTimeout          time.Duration `env:"CONTROL_PLANE_READINESS_TIMEOUT"`
	ReadinessInterval         time.Duration `env:"CONTROL_PLANE_READINESS_INTERVAL"`
	ShutdownTimeout           time.Duration `env:"CONTROL_PLANE_SHUTDOWN_TIMEOUT"`
	RelayPollInterval         time.Duration `env:"CONTROL_PLANE_RELAY_POLL_INTERVAL"`
	RelayLeaseDuration        time.Duration `env:"CONTROL_PLANE_RELAY_LEASE_DURATION"`
	RelayPublishTimeout       time.Duration `env:"CONTROL_PLANE_RELAY_PUBLISH_TIMEOUT"`
	RelayFinalizeTimeout      time.Duration `env:"CONTROL_PLANE_RELAY_FINALIZE_TIMEOUT"`
}

func loadConfig() (Config, error) {
	config := Config{
		GRPCListen: ":8443", TechnicalListen: ":9090",
		ServerCertificateFile:   "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.crt",
		ServerPrivateKeyFile:    "/var/run/secrets/mattercodex/control-plane/workload-tls/tls.key",
		ClientCAFile:            "/var/run/config/mattercodex/control-plane/internal-ca/ca.pem",
		PostgresDSNFile:         "/var/run/secrets/mattercodex/control-plane/postgres-runtime/dsn",
		PostgresCAFile:          "/var/run/config/mattercodex/control-plane/postgres/ca.pem",
		PostgresTLSServerName:   "control-plane-postgresql-rw.mattercodex-system.svc.cluster.local",
		PostgresMaxConnections:  16,
		NATSURL:                 "tls://nats.mattercodex-system.svc:4222",
		NATSTLSServerName:       "nats.mattercodex-system.svc.cluster.local",
		NATSCAFile:              "/var/run/config/mattercodex/control-plane/nats/ca.pem",
		NATSCredentialsFile:     "/var/run/secrets/mattercodex/control-plane/nats/user.creds",
		NATSStream:              "CONTROL_PLANE",
		NATSReplicas:            3,
		NATSMaxBytes:            32 << 30,
		DefaultRuntimeProvider:  "openai-codex",
		DefaultRuntimeModel:     "gpt-5",
		AuthorityVerifierSocket: authorityclient.VerifierSocketPath,
		AuthorityVerifierUID:    29002, AuthorityVerifierGID: 29000,
		AuthorityPolicyFile:       "/var/run/config/mattercodex/control-plane/authority/policy.json",
		ProofSignerFile:           "/var/run/secrets/mattercodex/internal-rpc-authority/proof-signer/private.jwk",
		ProofSignerTrustFile:      "/var/run/config/mattercodex/internal-rpc-authority/authority-proof-trust/jwks.json",
		AutomationGrantTrustFile:  "/var/run/config/mattercodex/control-plane/application-grants/control-plane.automation-readiness.public.jwk",
		IntegrationGrantTrustFile: "/var/run/config/mattercodex/control-plane/application-grants/control-plane.integration-readiness.public.jwk",
		RuntimeGrantTrustFile:     "/var/run/config/mattercodex/control-plane/application-grants/control-plane.runtime-readiness.public.jwk",
		OIDCAudience:              "mattercodex-control-api", OIDCCAFile: "/var/run/config/mattercodex/control-plane/oidc/ca.pem",
		OIDCRefreshInterval: 30 * time.Second,
		StartupTimeout:      20 * time.Second, ReadinessTimeout: 2 * time.Second,
		ReadinessInterval: 2 * time.Second, ShutdownTimeout: 10 * time.Second,
		RelayPollInterval: 250 * time.Millisecond, RelayLeaseDuration: 10 * time.Second,
		RelayPublishTimeout: 2 * time.Second, RelayFinalizeTimeout: 2 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, errors.New("parse control-plane environment")
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (config Config) validate() error {
	for _, address := range []string{config.GRPCListen, config.TechnicalListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("control-plane listen address is invalid")
		}
	}
	for _, path := range []string{config.ServerCertificateFile, config.ServerPrivateKeyFile, config.ClientCAFile, config.PostgresDSNFile, config.PostgresCAFile, config.NATSCAFile, config.NATSCredentialsFile, config.AuthorityVerifierSocket, config.AuthorityPolicyFile, config.ProofSignerFile, config.ProofSignerTrustFile, config.AutomationGrantTrustFile, config.IntegrationGrantTrustFile, config.RuntimeGrantTrustFile, config.OIDCCAFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("control-plane file path is invalid")
		}
	}
	if config.PostgresTLSServerName == "" || net.ParseIP(config.PostgresTLSServerName) != nil ||
		config.NATSTLSServerName == "" || net.ParseIP(config.NATSTLSServerName) != nil || config.NATSURL == "" ||
		config.PostgresMaxConnections < 2 || config.PostgresMaxConnections > 64 ||
		config.NATSStream != "CONTROL_PLANE" || config.NATSReplicas < 1 || config.NATSReplicas > 5 || config.NATSMaxBytes < 256<<20 ||
		config.InstanceID == "" || len(config.InstanceID) > 128 ||
		config.DefaultRuntimeProvider != "openai-codex" || !validRuntimeIdentifier(config.DefaultRuntimeModel) ||
		config.AuthorityVerifierUID == 0 || config.AuthorityVerifierGID == 0 ||
		config.OIDCAudience != "mattercodex-control-api" || config.OIDCTLSServerName == "" || net.ParseIP(config.OIDCTLSServerName) != nil ||
		config.OIDCRefreshInterval < 10*time.Second || config.OIDCRefreshInterval > time.Minute ||
		config.StartupTimeout < time.Second || config.StartupTimeout > time.Minute ||
		config.ReadinessTimeout < 100*time.Millisecond || config.ReadinessTimeout > 10*time.Second ||
		config.ReadinessInterval < 500*time.Millisecond || config.ReadinessInterval > time.Minute ||
		config.ShutdownTimeout < time.Second || config.ShutdownTimeout > time.Minute ||
		config.RelayPollInterval < 50*time.Millisecond || config.RelayLeaseDuration < time.Second ||
		config.RelayPublishTimeout <= 0 || config.RelayFinalizeTimeout <= 0 || config.RelayPublishTimeout+config.RelayFinalizeTimeout >= config.RelayLeaseDuration {
		return errors.New("control-plane bounded configuration is invalid")
	}
	issuer, issuerErr := url.Parse(config.OIDCIssuer)
	jwks, jwksErr := url.Parse(config.OIDCJWKSURL)
	connectHost, connectPort, connectErr := net.SplitHostPort(config.OIDCConnectAddress)
	if issuerErr != nil || issuer.Scheme != "https" || issuer.Hostname() != config.OIDCTLSServerName || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" ||
		jwksErr != nil || jwks.Scheme != "https" || jwks.Hostname() != issuer.Hostname() || jwks.User != nil || jwks.RawQuery != "" || jwks.Fragment != "" || jwks.Path == "" ||
		connectErr != nil || connectHost == "" || net.ParseIP(connectHost) != nil || connectPort != "443" {
		return errors.New("control-plane OIDC boundary is invalid")
	}
	if info, err := os.Lstat(filepath.Dir(config.AuthorityVerifierSocket)); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("control-plane authority socket directory is invalid")
	}
	return nil
}

func validRuntimeIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '.' && character != '_' && character != '-' {
			return false
		}
	}
	return true
}
