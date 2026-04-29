package app

import (
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config contains process-level access-manager server configuration.
type Config struct {
	HTTPAddr string `env:"KODEX_ACCESS_MANAGER_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr string `env:"KODEX_ACCESS_MANAGER_GRPC_ADDR" envDefault:":9090"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err != nil {
		return Config{}, fmt.Errorf("parse access-manager config from environment: %w", err)
	}
	return cfg, nil
}
