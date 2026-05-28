package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	interactionhubclient "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/interactionhub"
	httptransport "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/transport/http"
)

type Config struct {
	HTTPAddr        string               `env:"KODEX_STAFF_GATEWAY_HTTP_ADDR" envDefault:":8080"`
	OpenAPISpecPath string               `env:"KODEX_STAFF_GATEWAY_OPENAPI_SPEC_PATH" envDefault:"specs/openapi/staff-gateway.v1.yaml"`
	HTTP            HTTPConfig           `envPrefix:"KODEX_STAFF_GATEWAY_HTTP_"`
	InteractionHub  InteractionHubConfig `envPrefix:"KODEX_STAFF_GATEWAY_INTERACTION_HUB_"`
}

type HTTPConfig struct {
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"5s"`
	RequestTimeout    time.Duration `env:"REQUEST_TIMEOUT" envDefault:"10s"`
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	ReadinessTimeout  time.Duration `env:"READINESS_TIMEOUT" envDefault:"2s"`
	MaxBodyBytes      int64         `env:"MAX_BODY_BYTES" envDefault:"65536"`
}

type InteractionHubConfig struct {
	GRPCAddr  string        `env:"GRPC_ADDR" envDefault:"interaction-hub:9090"`
	AuthToken string        `env:"GRPC_AUTH_TOKEN"`
	Timeout   time.Duration `env:"TIMEOUT" envDefault:"3s"`
}

func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load staff-gateway config: %w", err)
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_HTTP_ADDR is required")
	}
	if strings.TrimSpace(cfg.OpenAPISpecPath) == "" {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_OPENAPI_SPEC_PATH is required")
	}
	if err := cfg.HTTP.validate(); err != nil {
		return err
	}
	return cfg.InteractionHub.validate()
}

func (cfg Config) HTTPRouterConfig() httptransport.Config {
	return httptransport.Config{
		ServiceName:     serviceName,
		OpenAPISpecPath: cfg.OpenAPISpecPath,
		RequestTimeout:  cfg.HTTP.RequestTimeout,
		MaxBodyBytes:    cfg.HTTP.MaxBodyBytes,
	}
}

func (cfg Config) InteractionHubClientConfig() interactionhubclient.Config {
	return interactionhubclient.Config{
		Addr:      cfg.InteractionHub.GRPCAddr,
		AuthToken: cfg.InteractionHub.AuthToken,
		Timeout:   cfg.InteractionHub.Timeout,
	}
}

func (cfg HTTPConfig) validate() error {
	for _, field := range []struct {
		name  string
		value time.Duration
	}{
		{name: "KODEX_STAFF_GATEWAY_HTTP_READ_HEADER_TIMEOUT", value: cfg.ReadHeaderTimeout},
		{name: "KODEX_STAFF_GATEWAY_HTTP_REQUEST_TIMEOUT", value: cfg.RequestTimeout},
		{name: "KODEX_STAFF_GATEWAY_HTTP_SHUTDOWN_TIMEOUT", value: cfg.ShutdownTimeout},
		{name: "KODEX_STAFF_GATEWAY_HTTP_READINESS_TIMEOUT", value: cfg.ReadinessTimeout},
	} {
		if field.value <= 0 {
			return fmt.Errorf("%s is invalid", field.name)
		}
	}
	if cfg.MaxBodyBytes <= 0 {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_HTTP_MAX_BODY_BYTES is invalid")
	}
	return nil
}

func (cfg InteractionHubConfig) validate() error {
	if strings.TrimSpace(cfg.GRPCAddr) == "" {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_INTERACTION_HUB_GRPC_ADDR is required")
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_INTERACTION_HUB_GRPC_AUTH_TOKEN is required")
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_INTERACTION_HUB_TIMEOUT is invalid")
	}
	return nil
}
