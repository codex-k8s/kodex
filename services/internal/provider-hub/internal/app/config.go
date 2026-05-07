package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
)

// Config contains process-level provider-hub server configuration.
type Config struct {
	HTTPAddr                  string        `env:"KODEX_PROVIDER_HUB_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr                  string        `env:"KODEX_PROVIDER_HUB_GRPC_ADDR" envDefault:":9090"`
	GRPCAuthRequired          bool          `env:"KODEX_PROVIDER_HUB_GRPC_AUTH_REQUIRED" envDefault:"true"`
	GRPCAuthToken             string        `env:"KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN"`
	GRPCMaxInFlight           int           `env:"KODEX_PROVIDER_HUB_GRPC_MAX_IN_FLIGHT" envDefault:"128"`
	GRPCMaxConcurrentStreams  uint32        `env:"KODEX_PROVIDER_HUB_GRPC_MAX_CONCURRENT_STREAMS" envDefault:"128"`
	GRPCUnaryTimeout          time.Duration `env:"KODEX_PROVIDER_HUB_GRPC_UNARY_TIMEOUT" envDefault:"30s"`
	GRPCKeepaliveTime         time.Duration `env:"KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_TIME" envDefault:"2m"`
	GRPCKeepaliveTimeout      time.Duration `env:"KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_TIMEOUT" envDefault:"20s"`
	GRPCKeepaliveMinTime      time.Duration `env:"KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_MIN_TIME" envDefault:"30s"`
	GRPCPermitWithoutStream   bool          `env:"KODEX_PROVIDER_HUB_GRPC_PERMIT_WITHOUT_STREAM" envDefault:"false"`
	GRPCMaxRecvMessageBytes   int           `env:"KODEX_PROVIDER_HUB_GRPC_MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCMaxSendMessageBytes   int           `env:"KODEX_PROVIDER_HUB_GRPC_MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
	DatabaseDSN               string        `env:"KODEX_PROVIDER_HUB_DATABASE_DSN,required,notEmpty"`
	DatabaseMaxConns          int32         `env:"KODEX_PROVIDER_HUB_DATABASE_MAX_CONNS" envDefault:"8"`
	DatabaseMinConns          int32         `env:"KODEX_PROVIDER_HUB_DATABASE_MIN_CONNS" envDefault:"1"`
	DatabaseMaxConnLifetime   time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
	DatabaseMaxConnIdleTime   time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	DatabaseHealthCheckPeriod time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	DatabasePingTimeout       time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_PING_TIMEOUT" envDefault:"5s"`
	DatabaseRetryMaxAttempts  int           `env:"KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS" envDefault:"6"`
	DatabaseRetryInitialDelay time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_INITIAL_DELAY" envDefault:"500ms"`
	DatabaseRetryMaxDelay     time.Duration `env:"KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_MAX_DELAY" envDefault:"5s"`
	DatabaseRetryJitterRatio  float64       `env:"KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_JITTER_RATIO" envDefault:"0.2"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load provider-hub config: %w", err)
	}
	return cfg, nil
}

// Validate checks configuration invariants that protect runtime boundaries.
func (cfg Config) Validate() error {
	if cfg.GRPCAuthRequired && strings.TrimSpace(cfg.GRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_PROVIDER_HUB_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	if err := cfg.validateGRPCSettings(); err != nil {
		return err
	}
	if err := cfg.validateDatabaseSettings(); err != nil {
		return err
	}
	return nil
}

func (cfg Config) validateGRPCSettings() error {
	if err := requirePositive("KODEX_PROVIDER_HUB_GRPC_MAX_IN_FLIGHT", cfg.GRPCMaxInFlight); err != nil {
		return err
	}
	if cfg.GRPCMaxConcurrentStreams == 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_GRPC_MAX_CONCURRENT_STREAMS is invalid")
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_GRPC_UNARY_TIMEOUT", cfg.GRPCUnaryTimeout); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_TIME", cfg.GRPCKeepaliveTime); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_TIMEOUT", cfg.GRPCKeepaliveTimeout); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_GRPC_KEEPALIVE_MIN_TIME", cfg.GRPCKeepaliveMinTime); err != nil {
		return err
	}
	if err := requirePositive("KODEX_PROVIDER_HUB_GRPC_MAX_RECV_MESSAGE_BYTES", cfg.GRPCMaxRecvMessageBytes); err != nil {
		return err
	}
	return requirePositive("KODEX_PROVIDER_HUB_GRPC_MAX_SEND_MESSAGE_BYTES", cfg.GRPCMaxSendMessageBytes)
}

func (cfg Config) validateDatabaseSettings() error {
	if cfg.DatabaseMaxConns <= 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_MAX_CONNS is invalid")
	}
	if cfg.DatabaseMinConns < 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_MIN_CONNS is invalid")
	}
	if cfg.DatabaseMinConns > cfg.DatabaseMaxConns {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_MIN_CONNS must be less than or equal to max conns")
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_DATABASE_MAX_CONN_LIFETIME", cfg.DatabaseMaxConnLifetime); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_DATABASE_MAX_CONN_IDLE_TIME", cfg.DatabaseMaxConnIdleTime); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_DATABASE_HEALTH_CHECK_PERIOD", cfg.DatabaseHealthCheckPeriod); err != nil {
		return err
	}
	if err := requireDuration("KODEX_PROVIDER_HUB_DATABASE_PING_TIMEOUT", cfg.DatabasePingTimeout); err != nil {
		return err
	}
	if err := requirePositive("KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_MAX_ATTEMPTS", cfg.DatabaseRetryMaxAttempts); err != nil {
		return err
	}
	if cfg.DatabaseRetryInitialDelay < 0 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_INITIAL_DELAY is invalid")
	}
	if cfg.DatabaseRetryMaxDelay < cfg.DatabaseRetryInitialDelay {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_MAX_DELAY is invalid")
	}
	if cfg.DatabaseRetryJitterRatio < 0 || cfg.DatabaseRetryJitterRatio > 1 {
		return fmt.Errorf("KODEX_PROVIDER_HUB_DATABASE_CONNECT_RETRY_JITTER_RATIO must be between 0 and 1")
	}
	return nil
}

func requirePositive(name string, value int) error {
	if value <= 0 {
		return fmt.Errorf("%s is invalid", name)
	}
	return nil
}

func requireDuration(name string, value time.Duration) error {
	if value <= 0 {
		return fmt.Errorf("%s is invalid", name)
	}
	return nil
}

// DatabasePoolSettings converts service config to the shared pgxpool contract.
func (cfg Config) DatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettingsFromRuntime(postgreslib.PoolRuntimeSettings{
		DSN:                      cfg.DatabaseDSN,
		MaxConns:                 cfg.DatabaseMaxConns,
		MinConns:                 cfg.DatabaseMinConns,
		MaxConnLifetime:          cfg.DatabaseMaxConnLifetime,
		MaxConnIdleTime:          cfg.DatabaseMaxConnIdleTime,
		HealthCheckPeriod:        cfg.DatabaseHealthCheckPeriod,
		PingTimeout:              cfg.DatabasePingTimeout,
		ConnectRetryMaxAttempts:  cfg.DatabaseRetryMaxAttempts,
		ConnectRetryInitialDelay: cfg.DatabaseRetryInitialDelay,
		ConnectRetryMaxDelay:     cfg.DatabaseRetryMaxDelay,
		ConnectRetryJitterRatio:  cfg.DatabaseRetryJitterRatio,
	})
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	runtime := grpcserver.Config{AuthRequired: cfg.GRPCAuthRequired}
	runtime.MaxInFlight = cfg.GRPCMaxInFlight
	runtime.MaxConcurrentStreams = cfg.GRPCMaxConcurrentStreams
	runtime.UnaryTimeout = cfg.GRPCUnaryTimeout
	runtime.KeepaliveTime = cfg.GRPCKeepaliveTime
	runtime.KeepaliveTimeout = cfg.GRPCKeepaliveTimeout
	runtime.KeepaliveMinTime = cfg.GRPCKeepaliveMinTime
	runtime.PermitWithoutStream = cfg.GRPCPermitWithoutStream
	runtime.MaxRecvMessageBytes = cfg.GRPCMaxRecvMessageBytes
	runtime.MaxSendMessageBytes = cfg.GRPCMaxSendMessageBytes
	return runtime
}
