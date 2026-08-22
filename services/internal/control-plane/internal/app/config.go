package app

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth/authorityclient"
)

type Config struct {
	GRPCListen              string        `env:"CONTROL_PLANE_GRPC_LISTEN"`
	TechnicalListen         string        `env:"CONTROL_PLANE_TECHNICAL_LISTEN"`
	ServerCertificateFile   string        `env:"CONTROL_PLANE_TLS_CERTIFICATE_FILE"`
	ServerPrivateKeyFile    string        `env:"CONTROL_PLANE_TLS_PRIVATE_KEY_FILE"`
	ClientCAFile            string        `env:"CONTROL_PLANE_TLS_CLIENT_CA_FILE"`
	PostgresDSNFile         string        `env:"CONTROL_PLANE_POSTGRES_DSN_FILE"`
	PostgresCAFile          string        `env:"CONTROL_PLANE_POSTGRES_CA_FILE"`
	PostgresTLSServerName   string        `env:"CONTROL_PLANE_POSTGRES_TLS_SERVER_NAME"`
	PostgresMaxConnections  int32         `env:"CONTROL_PLANE_POSTGRES_MAX_CONNECTIONS"`
	NATSURL                 string        `env:"CONTROL_PLANE_NATS_URL"`
	NATSTLSServerName       string        `env:"CONTROL_PLANE_NATS_TLS_SERVER_NAME"`
	NATSCAFile              string        `env:"CONTROL_PLANE_NATS_CA_FILE"`
	NATSCredentialsFile     string        `env:"CONTROL_PLANE_NATS_CREDENTIALS_FILE"`
	NATSStream              string        `env:"CONTROL_PLANE_NATS_STREAM"`
	NATSReplicas            int           `env:"CONTROL_PLANE_NATS_REPLICAS"`
	NATSMaxBytes            int64         `env:"CONTROL_PLANE_NATS_MAX_BYTES"`
	InstanceID              string        `env:"POD_UID"`
	AuthorityVerifierSocket string        `env:"CONTROL_PLANE_AUTHORITY_VERIFIER_SOCKET"`
	AuthorityVerifierUID    uint32        `env:"CONTROL_PLANE_AUTHORITY_VERIFIER_UID"`
	AuthorityVerifierGID    uint32        `env:"CONTROL_PLANE_AUTHORITY_VERIFIER_GID"`
	StartupTimeout          time.Duration `env:"CONTROL_PLANE_STARTUP_TIMEOUT"`
	ReadinessTimeout        time.Duration `env:"CONTROL_PLANE_READINESS_TIMEOUT"`
	ReadinessInterval       time.Duration `env:"CONTROL_PLANE_READINESS_INTERVAL"`
	ShutdownTimeout         time.Duration `env:"CONTROL_PLANE_SHUTDOWN_TIMEOUT"`
	RelayPollInterval       time.Duration `env:"CONTROL_PLANE_RELAY_POLL_INTERVAL"`
	RelayLeaseDuration      time.Duration `env:"CONTROL_PLANE_RELAY_LEASE_DURATION"`
	RelayPublishTimeout     time.Duration `env:"CONTROL_PLANE_RELAY_PUBLISH_TIMEOUT"`
	RelayFinalizeTimeout    time.Duration `env:"CONTROL_PLANE_RELAY_FINALIZE_TIMEOUT"`
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
		AuthorityVerifierSocket: authorityclient.VerifierSocketPath,
		AuthorityVerifierUID:    29002, AuthorityVerifierGID: 29000,
		StartupTimeout: 20 * time.Second, ReadinessTimeout: 2 * time.Second,
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
	for _, path := range []string{config.ServerCertificateFile, config.ServerPrivateKeyFile, config.ClientCAFile, config.PostgresDSNFile, config.PostgresCAFile, config.NATSCAFile, config.NATSCredentialsFile, config.AuthorityVerifierSocket} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("control-plane file path is invalid")
		}
	}
	if config.PostgresTLSServerName == "" || net.ParseIP(config.PostgresTLSServerName) != nil ||
		config.NATSTLSServerName == "" || net.ParseIP(config.NATSTLSServerName) != nil || config.NATSURL == "" ||
		config.PostgresMaxConnections < 2 || config.PostgresMaxConnections > 64 ||
		config.NATSStream != "CONTROL_PLANE" || config.NATSReplicas < 1 || config.NATSReplicas > 5 || config.NATSMaxBytes < 256<<20 ||
		config.InstanceID == "" || len(config.InstanceID) > 128 ||
		config.AuthorityVerifierUID == 0 || config.AuthorityVerifierGID == 0 ||
		config.StartupTimeout < time.Second || config.StartupTimeout > time.Minute ||
		config.ReadinessTimeout < 100*time.Millisecond || config.ReadinessTimeout > 10*time.Second ||
		config.ReadinessInterval < 500*time.Millisecond || config.ReadinessInterval > time.Minute ||
		config.ShutdownTimeout < time.Second || config.ShutdownTimeout > time.Minute ||
		config.RelayPollInterval < 50*time.Millisecond || config.RelayLeaseDuration < time.Second ||
		config.RelayPublishTimeout <= 0 || config.RelayFinalizeTimeout <= 0 || config.RelayPublishTimeout+config.RelayFinalizeTimeout >= config.RelayLeaseDuration {
		return errors.New("control-plane bounded configuration is invalid")
	}
	if info, err := os.Lstat(filepath.Dir(config.AuthorityVerifierSocket)); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("control-plane authority socket directory is invalid")
	}
	return nil
}
