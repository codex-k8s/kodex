// Package app contains integration-gateway process composition and lifecycle.
package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
	httptransport "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/transport/http"
)

// Config contains process-level integration-gateway configuration.
type Config struct {
	HTTPAddr        string                `env:"KODEX_INTEGRATION_GATEWAY_HTTP_ADDR" envDefault:":8080"`
	OpenAPISpecPath string                `env:"KODEX_INTEGRATION_GATEWAY_OPENAPI_SPEC_PATH" envDefault:"specs/openapi/integration-gateway.v1.yaml"`
	HTTP            HTTPConfig            `envPrefix:"KODEX_INTEGRATION_GATEWAY_HTTP_"`
	ProviderWebhook ProviderWebhookConfig `envPrefix:"KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_"`
	ProviderHub     ProviderHubConfig     `envPrefix:"KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_"`
	SecretResolver  SecretResolverConfig  `envPrefix:"KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_"`
}

// HTTPConfig contains edge HTTP limits and lifecycle timeouts.
type HTTPConfig struct {
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"5s"`
	RequestTimeout    time.Duration `env:"REQUEST_TIMEOUT" envDefault:"10s"`
	ShutdownTimeout   time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`
	ReadinessTimeout  time.Duration `env:"READINESS_TIMEOUT" envDefault:"2s"`
	MaxBodyBytes      int64         `env:"MAX_BODY_BYTES" envDefault:"1048576"`
}

// ProviderWebhookConfig controls the first provider webhook route.
type ProviderWebhookConfig struct {
	Enabled               bool     `env:"ENABLED" envDefault:"false"`
	AllowedProviderSlugs  []string `env:"ALLOWED_PROVIDER_SLUGS" envDefault:"github" envSeparator:","`
	GitHubSecretStoreType string   `env:"GITHUB_SECRET_STORE_TYPE"`
	GitHubSecretStoreRef  string   `env:"GITHUB_SECRET_STORE_REF"`
}

// ProviderHubConfig contains the future owner-service route settings.
type ProviderHubConfig struct {
	GRPCAddr  string        `env:"GRPC_ADDR" envDefault:"provider-hub:9090"`
	AuthToken string        `env:"GRPC_AUTH_TOKEN"`
	Timeout   time.Duration `env:"TIMEOUT" envDefault:"3s"`
}

// SecretResolverConfig contains value-safe secret resolver backend settings.
type SecretResolverConfig struct {
	EnvEnabled                bool   `env:"ENV_ENABLED" envDefault:"true"`
	MountedKubernetesRoot     string `env:"MOUNTED_KUBERNETES_ROOT"`
	MountedKubernetesMaxBytes int64  `env:"MOUNTED_KUBERNETES_MAX_SECRET_BYTES" envDefault:"1048576"`
	VaultAddr                 string `env:"VAULT_ADDR"`
	VaultToken                string `env:"VAULT_TOKEN"`
	VaultNamespace            string `env:"VAULT_NAMESPACE"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load integration-gateway config: %w", err)
	}
	return cfg, nil
}

// Validate checks process settings before runtime construction.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_HTTP_ADDR is required")
	}
	if strings.TrimSpace(cfg.OpenAPISpecPath) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_OPENAPI_SPEC_PATH is required")
	}
	if err := cfg.HTTP.validate(); err != nil {
		return err
	}
	if err := cfg.ProviderWebhook.validate(); err != nil {
		return err
	}
	if err := cfg.SecretResolver.validate(cfg.ProviderWebhook.Enabled, cfg.ProviderWebhook.GitHubSecretStoreType); err != nil {
		return err
	}
	return cfg.ProviderHub.validate(cfg.ProviderWebhook.Enabled)
}

func (cfg HTTPConfig) validate() error {
	if cfg.ReadHeaderTimeout <= 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_HTTP_READ_HEADER_TIMEOUT is invalid")
	}
	if cfg.RequestTimeout <= 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_HTTP_REQUEST_TIMEOUT is invalid")
	}
	if cfg.ShutdownTimeout <= 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_HTTP_SHUTDOWN_TIMEOUT is invalid")
	}
	if cfg.ReadinessTimeout <= 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_HTTP_READINESS_TIMEOUT is invalid")
	}
	if cfg.MaxBodyBytes <= 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_HTTP_MAX_BODY_BYTES is invalid")
	}
	return nil
}

func (cfg ProviderWebhookConfig) validate() error {
	if len(cfg.AllowedProviderSlugs) == 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ALLOWED_PROVIDER_SLUGS is required")
	}
	for _, slug := range cfg.AllowedProviderSlugs {
		normalized := strings.ToLower(strings.TrimSpace(slug))
		if normalized == "" {
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ALLOWED_PROVIDER_SLUGS contains an empty slug")
		}
		if cfg.Enabled && normalized != "github" {
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ALLOWED_PROVIDER_SLUGS supports only github in IGW-2")
		}
	}
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.GitHubSecretStoreType) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE is required when provider webhook route is enabled")
	}
	if strings.TrimSpace(cfg.GitHubSecretStoreRef) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF is required when provider webhook route is enabled")
	}
	return nil
}

func (cfg ProviderHubConfig) validate(required bool) error {
	if !required {
		return nil
	}
	if strings.TrimSpace(cfg.GRPCAddr) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_GRPC_ADDR is required when provider webhook route is enabled")
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_GRPC_AUTH_TOKEN is required when provider webhook route is enabled")
	}
	if cfg.Timeout <= 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_TIMEOUT is invalid")
	}
	return nil
}

func (cfg SecretResolverConfig) validate(required bool, requiredStoreType string) error {
	if cfg.MountedKubernetesMaxBytes <= 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_MAX_SECRET_BYTES is invalid")
	}
	if strings.TrimSpace(cfg.VaultAddr) != "" && strings.TrimSpace(cfg.VaultToken) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_TOKEN is required when Vault address is configured")
	}
	if !required {
		return nil
	}
	switch strings.TrimSpace(requiredStoreType) {
	case secretresolver.StoreTypeEnv:
		if !cfg.EnvEnabled {
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_ENV_ENABLED must be true for env webhook secret refs")
		}
	case secretresolver.StoreTypeKubernetesMountedSecret:
		if strings.TrimSpace(cfg.MountedKubernetesRoot) == "" {
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_ROOT is required for mounted Kubernetes webhook secret refs")
		}
	case secretresolver.StoreTypeVault:
		if strings.TrimSpace(cfg.VaultAddr) == "" {
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_ADDR is required for Vault webhook secret refs")
		}
	default:
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE is unsupported")
	}
	return nil
}

// HTTPRouterConfig converts process config to the HTTP transport runtime contract.
func (cfg Config) HTTPRouterConfig() httptransport.Config {
	return httptransport.Config{
		ServiceName:            serviceName,
		OpenAPISpecPath:        strings.TrimSpace(cfg.OpenAPISpecPath),
		RequestTimeout:         cfg.HTTP.RequestTimeout,
		MaxBodyBytes:           cfg.HTTP.MaxBodyBytes,
		ProviderWebhookEnabled: cfg.ProviderWebhook.Enabled,
		AllowedProviderSlugs:   cfg.ProviderWebhook.AllowedProviderSlugs,
	}
}

// ProviderHubClientConfig converts process config to a provider-hub client contract.
func (cfg Config) ProviderHubClientConfig() providerhubclient.Config {
	return providerhubclient.Config{
		Addr:      cfg.ProviderHub.GRPCAddr,
		AuthToken: cfg.ProviderHub.AuthToken,
		Timeout:   cfg.ProviderHub.Timeout,
	}
}

// GitHubWebhookSecretRef converts process config to a safe secret reference.
func (cfg Config) GitHubWebhookSecretRef() secretresolver.SecretRef {
	return secretresolver.SecretRef{
		StoreType: strings.TrimSpace(cfg.ProviderWebhook.GitHubSecretStoreType),
		StoreRef:  strings.TrimSpace(cfg.ProviderWebhook.GitHubSecretStoreRef),
	}
}
