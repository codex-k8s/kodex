package app

import (
	"errors"
	"net"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/authorityclient"
)

const (
	policyTarget     = "dns:///control-plane.kodex-system.svc:8443"
	policySNI        = "control-plane.kodex-system.svc.cluster.local"
	credentialTarget = "dns:///secret-broker.kodex-system.svc:8443"
	credentialSNI    = "secret-broker.kodex-system.svc.cluster.local"
	resolverTarget   = policyTarget
	resolverSNI      = policySNI
)

type Config struct {
	GRPCListen              string        `env:"STT_GRPC_LISTEN"`
	TechnicalListen         string        `env:"STT_TECHNICAL_LISTEN"`
	ServerCertificateFile   string        `env:"STT_SERVER_CERTIFICATE_FILE"`
	ServerPrivateKeyFile    string        `env:"STT_SERVER_PRIVATE_KEY_FILE"`
	ClientCAFile            string        `env:"STT_CLIENT_CA_FILE"`
	WorkloadCertificateFile string        `env:"STT_WORKLOAD_CERTIFICATE_FILE"`
	WorkloadPrivateKeyFile  string        `env:"STT_WORKLOAD_PRIVATE_KEY_FILE"`
	DependencyCAFile        string        `env:"STT_DEPENDENCY_CA_FILE"`
	ApplicationGrantFile    string        `env:"STT_APPLICATION_GRANT_FILE"`
	PolicyTarget            string        `env:"STT_POLICY_TARGET"`
	PolicyTLSServerName     string        `env:"STT_POLICY_TLS_SERVER_NAME"`
	CredentialTarget        string        `env:"STT_CREDENTIAL_TARGET"`
	CredentialTLSServerName string        `env:"STT_CREDENTIAL_TLS_SERVER_NAME"`
	ResolverTarget          string        `env:"STT_AUTHORITY_RESOLVER_TARGET"`
	ResolverTLSServerName   string        `env:"STT_AUTHORITY_RESOLVER_TLS_SERVER_NAME"`
	AuthorityVerifierSocket string        `env:"INTERNAL_RPC_AUTHORITY_VERIFIER_SOCKET"`
	AuthorityVerifierUID    uint32        `env:"STT_AUTHORITY_VERIFIER_UID"`
	AuthorityVerifierGID    uint32        `env:"STT_AUTHORITY_VERIFIER_GID"`
	AuthorityIssuerUID      uint32        `env:"STT_AUTHORITY_ISSUER_UID"`
	AuthorityIssuerGID      uint32        `env:"STT_AUTHORITY_ISSUER_GID"`
	RequestTimeout          time.Duration `env:"STT_REQUEST_TIMEOUT"`
	StartupTimeout          time.Duration `env:"STT_STARTUP_TIMEOUT"`
	ReadinessTimeout        time.Duration `env:"STT_READINESS_TIMEOUT"`
	ShutdownTimeout         time.Duration `env:"STT_SHUTDOWN_TIMEOUT"`
}

func loadConfig() (Config, error) {
	config := Config{
		GRPCListen: ":8443", TechnicalListen: ":9090",
		ServerCertificateFile:   "/var/run/secrets/kodex/stt-tts-service/workload-tls/tls.crt",
		ServerPrivateKeyFile:    "/var/run/secrets/kodex/stt-tts-service/workload-tls/tls.key",
		ClientCAFile:            "/var/run/config/kodex/stt-tts-service/internal-ca/ca.pem",
		WorkloadCertificateFile: "/var/run/secrets/kodex/stt-tts-service/workload-tls/tls.crt",
		WorkloadPrivateKeyFile:  "/var/run/secrets/kodex/stt-tts-service/workload-tls/tls.key",
		DependencyCAFile:        "/var/run/config/kodex/stt-tts-service/internal-ca/ca.pem",
		ApplicationGrantFile:    "/var/run/secrets/kodex/stt-tts-service/application-grant/application-grant.jws",
		PolicyTarget:            policyTarget, PolicyTLSServerName: policySNI,
		CredentialTarget: credentialTarget, CredentialTLSServerName: credentialSNI,
		ResolverTarget: resolverTarget, ResolverTLSServerName: resolverSNI,
		AuthorityVerifierSocket: authorityclient.VerifierSocketPath,
		AuthorityVerifierUID:    29002, AuthorityVerifierGID: 29000,
		AuthorityIssuerUID: 29001, AuthorityIssuerGID: 29000,
		RequestTimeout: 45 * time.Second, StartupTimeout: 30 * time.Second,
		ReadinessTimeout: 5 * time.Second, ShutdownTimeout: 20 * time.Second,
	}
	if err := env.Parse(&config); err != nil {
		return Config{}, errors.New("parse STT environment")
	}
	return config, config.validate()
}

func (config Config) validate() error {
	for _, address := range []string{config.GRPCListen, config.TechnicalListen} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return errors.New("STT listen address is invalid")
		}
	}
	if config.PolicyTarget != policyTarget || config.PolicyTLSServerName != policySNI ||
		config.CredentialTarget != credentialTarget || config.CredentialTLSServerName != credentialSNI ||
		config.ResolverTarget != resolverTarget || config.ResolverTLSServerName != resolverSNI ||
		config.AuthorityVerifierSocket != authorityclient.VerifierSocketPath ||
		config.AuthorityVerifierUID != 29002 || config.AuthorityVerifierGID != 29000 ||
		config.AuthorityIssuerUID != 29001 || config.AuthorityIssuerGID != 29000 ||
		config.RequestTimeout < time.Second || config.RequestTimeout > time.Minute ||
		config.StartupTimeout < 5*time.Second || config.StartupTimeout > time.Minute ||
		config.ReadinessTimeout < time.Second || config.ReadinessTimeout > 10*time.Second ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute {
		return errors.New("STT configuration is invalid")
	}
	for _, path := range []string{config.ServerCertificateFile, config.ServerPrivateKeyFile, config.ClientCAFile,
		config.WorkloadCertificateFile, config.WorkloadPrivateKeyFile, config.DependencyCAFile,
		config.ApplicationGrantFile, config.AuthorityVerifierSocket} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("STT file path is invalid")
		}
	}
	return nil
}
