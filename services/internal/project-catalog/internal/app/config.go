package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
)

// Config contains process-level project-catalog server configuration.
type Config struct {
	HTTPAddr                  string        `env:"KODEX_PROJECT_CATALOG_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr                  string        `env:"KODEX_PROJECT_CATALOG_GRPC_ADDR" envDefault:":9090"`
	GRPCAuthRequired          bool          `env:"KODEX_PROJECT_CATALOG_GRPC_AUTH_REQUIRED" envDefault:"true"`
	GRPCAuthToken             string        `env:"KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN"`
	GRPCMaxInFlight           int           `env:"KODEX_PROJECT_CATALOG_GRPC_MAX_IN_FLIGHT" envDefault:"128"`
	GRPCMaxConcurrentStreams  uint32        `env:"KODEX_PROJECT_CATALOG_GRPC_MAX_CONCURRENT_STREAMS" envDefault:"128"`
	GRPCUnaryTimeout          time.Duration `env:"KODEX_PROJECT_CATALOG_GRPC_UNARY_TIMEOUT" envDefault:"30s"`
	GRPCKeepaliveTime         time.Duration `env:"KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_TIME" envDefault:"2m"`
	GRPCKeepaliveTimeout      time.Duration `env:"KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_TIMEOUT" envDefault:"20s"`
	GRPCKeepaliveMinTime      time.Duration `env:"KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_MIN_TIME" envDefault:"30s"`
	GRPCPermitWithoutStream   bool          `env:"KODEX_PROJECT_CATALOG_GRPC_PERMIT_WITHOUT_STREAM" envDefault:"false"`
	GRPCMaxRecvMessageBytes   int           `env:"KODEX_PROJECT_CATALOG_GRPC_MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCMaxSendMessageBytes   int           `env:"KODEX_PROJECT_CATALOG_GRPC_MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
	DatabaseDSN               string        `env:"KODEX_PROJECT_CATALOG_DATABASE_DSN,required,notEmpty"`
	DatabaseMaxConns          int32         `env:"KODEX_PROJECT_CATALOG_DATABASE_MAX_CONNS" envDefault:"8"`
	DatabaseMinConns          int32         `env:"KODEX_PROJECT_CATALOG_DATABASE_MIN_CONNS" envDefault:"1"`
	DatabaseMaxConnLifetime   time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
	DatabaseMaxConnIdleTime   time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	DatabaseHealthCheckPeriod time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	DatabasePingTimeout       time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_PING_TIMEOUT" envDefault:"5s"`
	DatabaseRetryMaxAttempts  int           `env:"KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS" envDefault:"6"`
	DatabaseRetryInitialDelay time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_INITIAL_DELAY" envDefault:"500ms"`
	DatabaseRetryMaxDelay     time.Duration `env:"KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_MAX_DELAY" envDefault:"5s"`
	DatabaseRetryJitterRatio  float64       `env:"KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_JITTER_RATIO" envDefault:"0.2"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse project-catalog config from environment: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks configuration invariants that protect runtime boundaries.
func (cfg Config) Validate() error {
	if cfg.GRPCAuthRequired && strings.TrimSpace(cfg.GRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	for _, item := range []struct {
		name  string
		valid bool
	}{
		{name: "KODEX_PROJECT_CATALOG_GRPC_MAX_IN_FLIGHT", valid: cfg.GRPCMaxInFlight > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_MAX_CONCURRENT_STREAMS", valid: cfg.GRPCMaxConcurrentStreams > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_UNARY_TIMEOUT", valid: cfg.GRPCUnaryTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_TIME", valid: cfg.GRPCKeepaliveTime > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_TIMEOUT", valid: cfg.GRPCKeepaliveTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_KEEPALIVE_MIN_TIME", valid: cfg.GRPCKeepaliveMinTime > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_MAX_RECV_MESSAGE_BYTES", valid: cfg.GRPCMaxRecvMessageBytes > 0},
		{name: "KODEX_PROJECT_CATALOG_GRPC_MAX_SEND_MESSAGE_BYTES", valid: cfg.GRPCMaxSendMessageBytes > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_MAX_CONNS", valid: cfg.DatabaseMaxConns > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_MIN_CONNS", valid: cfg.DatabaseMinConns >= 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_MAX_CONN_LIFETIME", valid: cfg.DatabaseMaxConnLifetime > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_MAX_CONN_IDLE_TIME", valid: cfg.DatabaseMaxConnIdleTime > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_HEALTH_CHECK_PERIOD", valid: cfg.DatabaseHealthCheckPeriod > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_PING_TIMEOUT", valid: cfg.DatabasePingTimeout > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS", valid: cfg.DatabaseRetryMaxAttempts > 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_INITIAL_DELAY", valid: cfg.DatabaseRetryInitialDelay >= 0},
		{name: "KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_MAX_DELAY", valid: cfg.DatabaseRetryMaxDelay >= cfg.DatabaseRetryInitialDelay},
	} {
		if !item.valid {
			return fmt.Errorf("%s is invalid", item.name)
		}
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if cfg.DatabaseRetryJitterRatio < 0 || cfg.DatabaseRetryJitterRatio > 1 {
		return fmt.Errorf("KODEX_PROJECT_CATALOG_DATABASE_CONNECT_RETRY_JITTER_RATIO must be between 0 and 1")
	}
	return nil
}

// DatabasePoolSettings converts service config to the shared pgxpool contract.
func (cfg Config) DatabasePoolSettings() postgreslib.PoolSettings {
	settings := postgreslib.PoolRuntimeSettings{}
	settings.DSN = cfg.DatabaseDSN
	settings.MaxConns = cfg.DatabaseMaxConns
	settings.MinConns = cfg.DatabaseMinConns
	settings.MaxConnLifetime = cfg.DatabaseMaxConnLifetime
	settings.MaxConnIdleTime = cfg.DatabaseMaxConnIdleTime
	settings.HealthCheckPeriod = cfg.DatabaseHealthCheckPeriod
	settings.PingTimeout = cfg.DatabasePingTimeout
	settings.ConnectRetryMaxAttempts = cfg.DatabaseRetryMaxAttempts
	settings.ConnectRetryInitialDelay = cfg.DatabaseRetryInitialDelay
	settings.ConnectRetryMaxDelay = cfg.DatabaseRetryMaxDelay
	settings.ConnectRetryJitterRatio = cfg.DatabaseRetryJitterRatio
	return postgreslib.PoolSettingsFromRuntime(settings)
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	runtime := grpcserver.Config{}
	runtime.MaxInFlight = cfg.GRPCMaxInFlight
	runtime.MaxConcurrentStreams = cfg.GRPCMaxConcurrentStreams
	runtime.UnaryTimeout = cfg.GRPCUnaryTimeout
	runtime.KeepaliveTime = cfg.GRPCKeepaliveTime
	runtime.KeepaliveTimeout = cfg.GRPCKeepaliveTimeout
	runtime.KeepaliveMinTime = cfg.GRPCKeepaliveMinTime
	runtime.PermitWithoutStream = cfg.GRPCPermitWithoutStream
	runtime.MaxRecvMessageBytes = cfg.GRPCMaxRecvMessageBytes
	runtime.MaxSendMessageBytes = cfg.GRPCMaxSendMessageBytes
	runtime.AuthRequired = cfg.GRPCAuthRequired
	return runtime
}
