package app

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"

	postgreslib "github.com/codex-k8s/kodex/libs/go/postgres"
)

// Config contains process-level access-manager server configuration.
type Config struct {
	HTTPAddr                  string        `env:"KODEX_ACCESS_MANAGER_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr                  string        `env:"KODEX_ACCESS_MANAGER_GRPC_ADDR" envDefault:":9090"`
	DatabaseDSN               string        `env:"KODEX_ACCESS_MANAGER_DATABASE_DSN" envDefault:"postgres://kodex:kodex@postgres:5432/kodex_access_manager?sslmode=disable"`
	DatabaseMaxConns          int32         `env:"KODEX_ACCESS_MANAGER_DATABASE_MAX_CONNS" envDefault:"8"`
	DatabaseMinConns          int32         `env:"KODEX_ACCESS_MANAGER_DATABASE_MIN_CONNS" envDefault:"1"`
	DatabaseMaxConnLifetime   time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_MAX_CONN_LIFETIME" envDefault:"1h"`
	DatabaseMaxConnIdleTime   time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_MAX_CONN_IDLE_TIME" envDefault:"15m"`
	DatabaseHealthCheckPeriod time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_HEALTH_CHECK_PERIOD" envDefault:"30s"`
	DatabasePingTimeout       time.Duration `env:"KODEX_ACCESS_MANAGER_DATABASE_PING_TIMEOUT" envDefault:"5s"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse access-manager config from environment: %w", err)
	}
	return cfg, nil
}

// DatabasePoolSettings converts service config to the shared pgxpool contract.
func (cfg Config) DatabasePoolSettings() postgreslib.PoolSettings {
	return postgreslib.PoolSettings{
		DSN:               cfg.DatabaseDSN,
		MaxConns:          cfg.DatabaseMaxConns,
		MinConns:          cfg.DatabaseMinConns,
		MaxConnLifetime:   cfg.DatabaseMaxConnLifetime,
		MaxConnIdleTime:   cfg.DatabaseMaxConnIdleTime,
		HealthCheckPeriod: cfg.DatabaseHealthCheckPeriod,
		PingTimeout:       cfg.DatabasePingTimeout,
	}
}
