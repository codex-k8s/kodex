package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	agentmanagerclient "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/agentmanager"
	"github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/clientruntime"
	governanceclient "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/governance"
	interactionhubclient "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/interactionhub"
	projectcatalogclient "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/clients/projectcatalog"
	httptransport "github.com/codex-k8s/kodex/services/staff/staff-gateway/internal/transport/http"
)

type Config struct {
	HTTPAddr        string               `env:"KODEX_STAFF_GATEWAY_HTTP_ADDR" envDefault:":8080"`
	OpenAPISpecPath string               `env:"KODEX_STAFF_GATEWAY_OPENAPI_SPEC_PATH" envDefault:"specs/openapi/staff-gateway.v1.yaml"`
	HTTP            HTTPConfig           `envPrefix:"KODEX_STAFF_GATEWAY_HTTP_"`
	InteractionHub  InteractionHubConfig `envPrefix:"KODEX_STAFF_GATEWAY_INTERACTION_HUB_"`
	AgentManager    AgentManagerConfig   `envPrefix:"KODEX_STAFF_GATEWAY_AGENT_MANAGER_"`
	Governance      GovernanceConfig     `envPrefix:"KODEX_STAFF_GATEWAY_GOVERNANCE_MANAGER_"`
	ProjectCatalog  ProjectCatalogConfig `envPrefix:"KODEX_STAFF_GATEWAY_PROJECT_CATALOG_"`
	SelfDeploy      SelfDeployConfig     `envPrefix:"KODEX_STAFF_GATEWAY_SELF_DEPLOY_"`
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

type AgentManagerConfig struct {
	GRPCAddr  string        `env:"GRPC_ADDR" envDefault:"agent-manager:9090"`
	AuthToken string        `env:"GRPC_AUTH_TOKEN"`
	Timeout   time.Duration `env:"TIMEOUT" envDefault:"3s"`
}

type GovernanceConfig struct {
	GRPCAddr  string        `env:"GRPC_ADDR" envDefault:"governance-manager:9090"`
	AuthToken string        `env:"GRPC_AUTH_TOKEN"`
	Timeout   time.Duration `env:"TIMEOUT" envDefault:"3s"`
}

type ProjectCatalogConfig struct {
	GRPCAddr  string        `env:"GRPC_ADDR" envDefault:"project-catalog:9090"`
	AuthToken string        `env:"GRPC_AUTH_TOKEN"`
	Timeout   time.Duration `env:"TIMEOUT" envDefault:"3s"`
}

type SelfDeployConfig struct {
	ProjectRef string `env:"PROJECT_REF"`
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
	if err := cfg.InteractionHub.validate(); err != nil {
		return err
	}
	if err := cfg.AgentManager.validate(cfg.InteractionHub.AuthToken); err != nil {
		return err
	}
	if err := cfg.Governance.validate(); err != nil {
		return err
	}
	return cfg.ProjectCatalog.validate()
}

func (cfg Config) HTTPRouterConfig() httptransport.Config {
	return httptransport.Config{
		ServiceName:          serviceName,
		OpenAPISpecPath:      cfg.OpenAPISpecPath,
		RequestTimeout:       cfg.HTTP.RequestTimeout,
		MaxBodyBytes:         cfg.HTTP.MaxBodyBytes,
		SelfDeployProjectRef: strings.TrimSpace(cfg.SelfDeploy.ProjectRef),
	}
}

func (cfg Config) InteractionHubClientConfig() interactionhubclient.Config {
	return interactionhubclient.Config{
		Addr:      cfg.InteractionHub.GRPCAddr,
		AuthToken: cfg.InteractionHub.AuthToken,
		Timeout:   cfg.InteractionHub.Timeout,
	}
}

func (cfg Config) AgentManagerClientConfig() agentmanagerclient.Config {
	return fallbackClientConfig(cfg.AgentManager.GRPCAddr, cfg.AgentManager.AuthToken, cfg.InteractionHub.AuthToken, cfg.AgentManager.Timeout)
}

func (cfg Config) GovernanceClientConfig() governanceclient.Config {
	return governanceclient.Config{
		Addr:      cfg.Governance.GRPCAddr,
		AuthToken: cfg.Governance.AuthToken,
		Timeout:   cfg.Governance.Timeout,
	}
}

func (cfg Config) ProjectCatalogClientConfig() projectcatalogclient.Config {
	return projectcatalogclient.Config{
		Addr:      cfg.ProjectCatalog.GRPCAddr,
		AuthToken: strings.TrimSpace(cfg.ProjectCatalog.AuthToken),
		Timeout:   cfg.ProjectCatalog.Timeout,
	}
}

func fallbackClientConfig(addr string, authToken string, fallbackAuthToken string, timeout time.Duration) clientruntime.Config {
	token := strings.TrimSpace(authToken)
	if token == "" {
		token = strings.TrimSpace(fallbackAuthToken)
	}
	return clientruntime.Config{Addr: addr, AuthToken: token, Timeout: timeout}
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
	return validateRequiredClientConfig("INTERACTION_HUB", cfg.GRPCAddr, cfg.AuthToken, cfg.Timeout)
}

func (cfg AgentManagerConfig) validate(fallbackAuthToken string) error {
	return validateFallbackClientConfig("AGENT_MANAGER", cfg.GRPCAddr, cfg.AuthToken, fallbackAuthToken, cfg.Timeout)
}

func (cfg GovernanceConfig) validate() error {
	return validateRequiredClientConfig("GOVERNANCE_MANAGER", cfg.GRPCAddr, cfg.AuthToken, cfg.Timeout)
}

func (cfg ProjectCatalogConfig) validate() error {
	return validateRequiredClientConfig("PROJECT_CATALOG", cfg.GRPCAddr, cfg.AuthToken, cfg.Timeout)
}

func validateFallbackClientConfig(envPrefix string, grpcAddr string, authToken string, fallbackAuthToken string, timeout time.Duration) error {
	if strings.TrimSpace(grpcAddr) == "" {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_%s_GRPC_ADDR is required", envPrefix)
	}
	if strings.TrimSpace(authToken) == "" && strings.TrimSpace(fallbackAuthToken) == "" {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_%s_GRPC_AUTH_TOKEN is required", envPrefix)
	}
	if timeout <= 0 {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_%s_TIMEOUT is invalid", envPrefix)
	}
	return nil
}

func validateRequiredClientConfig(envPrefix string, grpcAddr string, authToken string, timeout time.Duration) error {
	if strings.TrimSpace(grpcAddr) == "" {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_%s_GRPC_ADDR is required", envPrefix)
	}
	if strings.TrimSpace(authToken) == "" {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_%s_GRPC_AUTH_TOKEN is required", envPrefix)
	}
	if timeout <= 0 {
		return fmt.Errorf("KODEX_STAFF_GATEWAY_%s_TIMEOUT is invalid", envPrefix)
	}
	return nil
}
