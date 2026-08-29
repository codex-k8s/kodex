package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	defaultControlPlaneTarget        = "dns:///control-plane.kodex-system.svc:8443"
	defaultControlPlaneTLSServerName = "control-plane.kodex-system.svc.cluster.local"
	defaultExpectedClientSPIFFEID    = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
)

type Config struct {
	RuntimeNamespace            string        `env:"SECRET_BROKER_RUNTIME_NAMESPACE"`
	ClaimantID                  string        `env:"POD_UID"`
	GRPCListen                  string        `env:"SECRET_BROKER_GRPC_LISTEN"`
	TechnicalListen             string        `env:"SECRET_BROKER_TECHNICAL_LISTEN"`
	ServerCertificateFile       string        `env:"SECRET_BROKER_SERVER_CERTIFICATE_FILE"`
	ServerPrivateKeyFile        string        `env:"SECRET_BROKER_SERVER_PRIVATE_KEY_FILE"`
	ClientCAFile                string        `env:"SECRET_BROKER_CLIENT_CA_FILE"`
	ExpectedClientSPIFFEID      string        `env:"SECRET_BROKER_EXPECTED_CLIENT_SPIFFE_ID"`
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
}

func loadConfig() (Config, error) {
	config := Config{
		RuntimeNamespace: "kodex-runtime", GRPCListen: ":8443", TechnicalListen: ":9090",
		ServerCertificateFile:       "/var/run/secrets/kodex/secret-broker/server/tls.crt",
		ServerPrivateKeyFile:        "/var/run/secrets/kodex/secret-broker/server/tls.key",
		ClientCAFile:                "/var/run/config/kodex/secret-broker/client/ca.pem",
		ExpectedClientSPIFFEID:      defaultExpectedClientSPIFFEID,
		ControlPlaneTarget:          defaultControlPlaneTarget,
		ControlPlaneTLSServerName:   defaultControlPlaneTLSServerName,
		ControlPlaneCAFile:          "/var/run/config/kodex/secret-broker/control-plane/ca.pem",
		ControlPlaneCertificateFile: "/var/run/secrets/kodex/secret-broker/workload-tls/tls.crt",
		ControlPlanePrivateKeyFile:  "/var/run/secrets/kodex/secret-broker/workload-tls/tls.key",
		ApplicationGrantFile:        "/var/run/secrets/kodex/secret-broker/application-grant/application-grant.jws",
		RequestTimeout:              5 * time.Second, ShutdownTimeout: 20 * time.Second, MaximumSecretBytes: 512 << 10,
		RecoveryInterval: 30 * time.Second, RecoveryTimeout: 10 * time.Second,
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
	for _, path := range []string{config.ServerCertificateFile, config.ServerPrivateKeyFile, config.ClientCAFile,
		config.ControlPlaneCAFile, config.ControlPlaneCertificateFile, config.ControlPlanePrivateKeyFile, config.ApplicationGrantFile} {
		if !filepath.IsAbs(path) {
			return errors.New("secret broker file path is invalid")
		}
	}
	identity, err := url.Parse(config.ExpectedClientSPIFFEID)
	if err != nil || identity.Scheme != "spiffe" || identity.Host == "" || identity.Path == "" || identity.RawQuery != "" || identity.Fragment != "" {
		return errors.New("secret broker client identity is invalid")
	}
	return nil
}
