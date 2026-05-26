package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	grpcserver "github.com/codex-k8s/kodex/libs/go/grpcserver"
)

// Config contains process-level interaction-hub server configuration.
type Config struct {
	HTTPAddr string                `env:"KODEX_INTERACTION_HUB_HTTP_ADDR" envDefault:":8080"`
	GRPCAddr string                `env:"KODEX_INTERACTION_HUB_GRPC_ADDR" envDefault:":9090"`
	GRPC     InteractionGRPCConfig `envPrefix:"KODEX_INTERACTION_HUB_GRPC_"`
}

// InteractionGRPCConfig contains gRPC boundary limits.
type InteractionGRPCConfig struct {
	AuthRequired         bool          `env:"AUTH_REQUIRED" envDefault:"true"`
	AuthToken            string        `env:"AUTH_TOKEN"`
	MaxInFlight          int           `env:"MAX_IN_FLIGHT" envDefault:"128"`
	MaxConcurrentStreams uint32        `env:"MAX_CONCURRENT_STREAMS" envDefault:"128"`
	UnaryTimeout         time.Duration `env:"UNARY_TIMEOUT" envDefault:"30s"`
	KeepaliveTime        time.Duration `env:"KEEPALIVE_TIME" envDefault:"2m"`
	KeepaliveTimeout     time.Duration `env:"KEEPALIVE_TIMEOUT" envDefault:"20s"`
	KeepaliveMinTime     time.Duration `env:"KEEPALIVE_MIN_TIME" envDefault:"30s"`
	PermitWithoutStream  bool          `env:"PERMIT_WITHOUT_STREAM" envDefault:"false"`
	MaxRecvMessageBytes  int           `env:"MAX_RECV_MESSAGE_BYTES" envDefault:"4194304"`
	MaxSendMessageBytes  int           `env:"MAX_SEND_MESSAGE_BYTES" envDefault:"4194304"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load interaction-hub config: %w", err)
	}
	return cfg, nil
}

// Validate checks process settings before runtime construction.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("KODEX_INTERACTION_HUB_HTTP_ADDR is required")
	}
	if strings.TrimSpace(cfg.GRPCAddr) == "" {
		return fmt.Errorf("KODEX_INTERACTION_HUB_GRPC_ADDR is required")
	}
	if cfg.GRPC.AuthRequired && strings.TrimSpace(cfg.GRPC.AuthToken) == "" {
		return fmt.Errorf("KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN is required when gRPC auth is enabled")
	}
	if err := cfg.validateGRPC(); err != nil {
		return err
	}
	return cfg.GRPCServerConfig().Validate()
}

func (cfg Config) validateGRPC() error {
	if err := validatePositiveInt("KODEX_INTERACTION_HUB_GRPC_MAX_IN_FLIGHT", cfg.GRPC.MaxInFlight); err != nil {
		return err
	}
	if cfg.GRPC.MaxConcurrentStreams == 0 {
		return fmt.Errorf("KODEX_INTERACTION_HUB_GRPC_MAX_CONCURRENT_STREAMS is invalid")
	}
	if err := validatePositiveInt("KODEX_INTERACTION_HUB_GRPC_MAX_RECV_MESSAGE_BYTES", cfg.GRPC.MaxRecvMessageBytes); err != nil {
		return err
	}
	if err := validatePositiveInt("KODEX_INTERACTION_HUB_GRPC_MAX_SEND_MESSAGE_BYTES", cfg.GRPC.MaxSendMessageBytes); err != nil {
		return err
	}
	return validateDurationChecks([]durationCheck{
		{name: "KODEX_INTERACTION_HUB_GRPC_UNARY_TIMEOUT", value: cfg.GRPC.UnaryTimeout},
		{name: "KODEX_INTERACTION_HUB_GRPC_KEEPALIVE_TIME", value: cfg.GRPC.KeepaliveTime},
		{name: "KODEX_INTERACTION_HUB_GRPC_KEEPALIVE_TIMEOUT", value: cfg.GRPC.KeepaliveTimeout},
		{name: "KODEX_INTERACTION_HUB_GRPC_KEEPALIVE_MIN_TIME", value: cfg.GRPC.KeepaliveMinTime},
	})
}

// GRPCServerConfig converts service env config to the shared gRPC runtime contract.
func (cfg Config) GRPCServerConfig() grpcserver.Config {
	grpcCfg := cfg.GRPC
	return grpcserver.ConfigFromRuntimeValues(
		grpcCfg.MaxInFlight,
		grpcCfg.MaxConcurrentStreams,
		grpcCfg.UnaryTimeout,
		grpcCfg.KeepaliveTime,
		grpcCfg.KeepaliveTimeout,
		grpcCfg.KeepaliveMinTime,
		grpcCfg.PermitWithoutStream,
		grpcCfg.MaxRecvMessageBytes,
		grpcCfg.MaxSendMessageBytes,
		grpcCfg.AuthRequired,
	)
}

type durationCheck struct {
	name  string
	value time.Duration
}

func validatePositiveInt(envName string, value int) error {
	if value <= 0 {
		return fmt.Errorf("%s is invalid", envName)
	}
	return nil
}

func validateDurationChecks(checks []durationCheck) error {
	for _, check := range checks {
		if check.value <= 0 {
			return fmt.Errorf("%s is invalid", check.name)
		}
	}
	return nil
}
