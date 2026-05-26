// Package app contains governance-manager process composition and lifecycle.
package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
)

// Config contains process-level governance-manager server configuration.
type Config struct {
	HTTPAddr                 string        `env:"KODEX_GOVERNANCE_MANAGER_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr                 string        `env:"KODEX_GOVERNANCE_MANAGER_GRPC_ADDR" envDefault:":9090"`
	GRPCAuthRequired         bool          `env:"KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_REQUIRED" envDefault:"true"`
	GRPCAuthToken            string        `env:"KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN"`
	GRPCMaxConcurrentStreams uint32        `env:"KODEX_GOVERNANCE_MANAGER_GRPC_MAX_CONCURRENT_STREAMS" envDefault:"128"`
	GRPCMaxInFlight          int           `env:"KODEX_GOVERNANCE_MANAGER_GRPC_MAX_IN_FLIGHT" envDefault:"128"`
	GRPCMaxRecvMessageBytes  int           `env:"KODEX_GOVERNANCE_MANAGER_GRPC_MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCMaxSendMessageBytes  int           `env:"KODEX_GOVERNANCE_MANAGER_GRPC_MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
	GRPCKeepaliveMinTime     time.Duration `env:"KODEX_GOVERNANCE_MANAGER_GRPC_KEEPALIVE_MIN_TIME" envDefault:"30s"`
	GRPCKeepaliveTime        time.Duration `env:"KODEX_GOVERNANCE_MANAGER_GRPC_KEEPALIVE_TIME" envDefault:"2m"`
	GRPCKeepaliveTimeout     time.Duration `env:"KODEX_GOVERNANCE_MANAGER_GRPC_KEEPALIVE_TIMEOUT" envDefault:"20s"`
	GRPCPermitWithoutStream  bool          `env:"KODEX_GOVERNANCE_MANAGER_GRPC_PERMIT_WITHOUT_STREAM" envDefault:"false"`
	GRPCUnaryTimeout         time.Duration `env:"KODEX_GOVERNANCE_MANAGER_GRPC_UNARY_TIMEOUT" envDefault:"30s"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load governance-manager config: %w", err)
	}
	return cfg, nil
}

// Validate checks process settings before runtime construction.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_HTTP_ADDR is required")
	}
	if strings.TrimSpace(cfg.GRPCAddr) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_GRPC_ADDR is required")
	}
	if cfg.GRPCAuthRequired && strings.TrimSpace(cfg.GRPCAuthToken) == "" {
		return fmt.Errorf("KODEX_GOVERNANCE_MANAGER_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	if err := cfg.GRPCServerConfig().Validate(); err != nil {
		return err
	}
	return nil
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	return grpcserver.ConfigFromRuntimeValues(cfg.GRPCMaxInFlight, cfg.GRPCMaxConcurrentStreams, cfg.GRPCUnaryTimeout, cfg.GRPCKeepaliveTime, cfg.GRPCKeepaliveTimeout, cfg.GRPCKeepaliveMinTime, cfg.GRPCPermitWithoutStream, cfg.GRPCMaxRecvMessageBytes, cfg.GRPCMaxSendMessageBytes, cfg.GRPCAuthRequired)
}
