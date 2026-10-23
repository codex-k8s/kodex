package app

import (
	"errors"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

const (
	// serviceName identifies this deployable in telemetry and control-plane references.
	// RU: serviceName обозначает этот deployable в telemetry и ссылках control plane.
	serviceName = "artifact-retention"
	// metricsSubsystem keeps its Prometheus metric names in one bounded namespace.
	// RU: metricsSubsystem удерживает имена Prometheus-метрик в одном ограниченном пространстве.
	metricsSubsystem = "artifact_retention"
)

// Config holds the bounded process, PostgreSQL, object-storage and cleanup settings.
// RU: Config хранит ограниченные настройки процесса, PostgreSQL, объектного хранилища и очистки.
type Config struct {
	Environment                    string        `env:"DEPLOYMENT_ENVIRONMENT"`
	TechnicalListen                string        `env:"ARTIFACT_RETENTION_TECHNICAL_LISTEN"`
	InstanceID                     string        `env:"ARTIFACT_RETENTION_INSTANCE_ID"`
	PostgresDSNFile                string        `env:"ARTIFACT_RETENTION_POSTGRES_DSN_FILE"`
	PostgresCAFile                 string        `env:"ARTIFACT_RETENTION_POSTGRES_CA_FILE"`
	PostgresTLSServerName          string        `env:"ARTIFACT_RETENTION_POSTGRES_TLS_SERVER_NAME"`
	PostgresMaxConnections         int32         `env:"ARTIFACT_RETENTION_POSTGRES_MAX_CONNECTIONS"`
	ObjectStorageEndpoint          string        `env:"ARTIFACT_RETENTION_OBJECT_STORAGE_ENDPOINT"`
	ObjectStorageRegion            string        `env:"ARTIFACT_RETENTION_OBJECT_STORAGE_REGION"`
	ObjectStorageBucket            string        `env:"ARTIFACT_RETENTION_OBJECT_STORAGE_BUCKET"`
	ObjectStorageAccessKeyFile     string        `env:"ARTIFACT_RETENTION_OBJECT_STORAGE_ACCESS_KEY_FILE"`
	ObjectStorageSecretKeyFile     string        `env:"ARTIFACT_RETENTION_OBJECT_STORAGE_SECRET_KEY_FILE"`
	ObjectStorageUsePathStyle      bool          `env:"ARTIFACT_RETENTION_OBJECT_STORAGE_USE_PATH_STYLE"`
	ObjectStorageAllowInsecureHTTP bool          `env:"ARTIFACT_RETENTION_OBJECT_STORAGE_ALLOW_INSECURE_LOCAL"`
	StartupTimeout                 time.Duration `env:"ARTIFACT_RETENTION_STARTUP_TIMEOUT"`
	ShutdownTimeout                time.Duration `env:"ARTIFACT_RETENTION_SHUTDOWN_TIMEOUT"`
	OperationTimeout               time.Duration `env:"ARTIFACT_RETENTION_OPERATION_TIMEOUT"`
	PollInterval                   time.Duration `env:"ARTIFACT_RETENTION_POLL_INTERVAL"`
	ReadinessInterval              time.Duration `env:"ARTIFACT_RETENTION_READINESS_INTERVAL"`
	ClaimLease                     time.Duration `env:"ARTIFACT_RETENTION_CLAIM_LEASE"`
	BatchSize                      int           `env:"ARTIFACT_RETENTION_BATCH_SIZE"`
}

// loadConfig applies documented defaults, parses environment overrides and rejects invalid deployment settings.
// RU: loadConfig применяет исходные значения, читает переопределения окружения и отклоняет неверные настройки deploy.
func loadConfig() (Config, error) {
	config := Config{
		TechnicalListen:            ":9090",
		InstanceID:                 "artifact-retention-0",
		PostgresDSNFile:            "/var/run/secrets/kodex/artifact-retention/postgres-runtime/dsn",
		PostgresCAFile:             "/var/run/config/kodex/artifact-retention/postgres/ca.pem",
		PostgresTLSServerName:      "control-plane-postgresql-rw.kodex-system.svc.cluster.local",
		PostgresMaxConnections:     2,
		ObjectStorageRegion:        "us-east-1",
		ObjectStorageBucket:        "kodex-artifacts",
		ObjectStorageAccessKeyFile: "/var/run/secrets/kodex/artifact-retention/object-storage/access-key-id",
		ObjectStorageSecretKeyFile: "/var/run/secrets/kodex/artifact-retention/object-storage/secret-access-key",
		ObjectStorageUsePathStyle:  true,
		StartupTimeout:             30 * time.Second,
		ShutdownTimeout:            20 * time.Second,
		OperationTimeout:           20 * time.Second,
		PollInterval:               30 * time.Second,
		ReadinessInterval:          10 * time.Second,
		ClaimLease:                 2 * time.Minute,
		BatchSize:                  32,
	}
	if err := env.ParseWithOptions(&config, env.Options{}); err != nil {
		return Config{}, err
	}
	if err := config.validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

// validate closes on unsafe transport, nonabsolute protected paths, unbounded timeouts or claim sizes.
// RU: validate отклоняет небезопасный transport, нестрогие защищённые пути, чрезмерные сроки и размеры claim.
func (config Config) validate() error {
	if config.Environment != "staging" && config.Environment != "production" {
		return errors.New("artifact-retention environment is invalid")
	}
	if _, _, err := net.SplitHostPort(config.TechnicalListen); err != nil {
		return errors.New("artifact-retention technical endpoint is invalid")
	}
	for _, path := range []string{config.PostgresDSNFile, config.PostgresCAFile, config.ObjectStorageAccessKeyFile, config.ObjectStorageSecretKeyFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return errors.New("artifact-retention protected file path is invalid")
		}
	}
	if strings.TrimSpace(config.InstanceID) == "" || len(config.InstanceID) > 128 ||
		strings.TrimSpace(config.PostgresTLSServerName) == "" || strings.ContainsAny(config.PostgresTLSServerName, "*/") {
		return errors.New("artifact-retention identity is invalid")
	}
	endpoint, err := url.Parse(config.ObjectStorageEndpoint)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return errors.New("artifact-retention object storage endpoint is invalid")
	}
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && config.ObjectStorageAllowInsecureHTTP) {
		return errors.New("artifact-retention object storage transport is invalid")
	}
	if config.ObjectStorageRegion == "" || config.ObjectStorageBucket == "" ||
		config.PostgresMaxConnections < 1 || config.PostgresMaxConnections > 4 ||
		config.BatchSize < 1 || config.BatchSize > 100 ||
		config.StartupTimeout < 5*time.Second || config.StartupTimeout > 2*time.Minute ||
		config.ShutdownTimeout < 5*time.Second || config.ShutdownTimeout > time.Minute ||
		config.OperationTimeout < time.Second || config.OperationTimeout > time.Minute ||
		config.PollInterval < time.Second || config.PollInterval > 24*time.Hour ||
		config.ReadinessInterval < time.Second || config.ReadinessInterval > time.Minute ||
		config.ClaimLease <= config.OperationTimeout || config.ClaimLease > 10*time.Minute {
		return errors.New("artifact-retention lifecycle configuration is invalid")
	}
	return nil
}
