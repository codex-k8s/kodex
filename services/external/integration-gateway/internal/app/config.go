// Package app contains integration-gateway process composition and lifecycle.
package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	interactionhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/interactionhub"
	providerhubclient "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/clients/providerhub"
	httptransport "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/transport/http"
)

// Config contains process-level integration-gateway configuration.
type Config struct {
	HTTPAddr         string                 `env:"KODEX_INTEGRATION_GATEWAY_HTTP_ADDR" envDefault:":8080"`
	OpenAPISpecPath  string                 `env:"KODEX_INTEGRATION_GATEWAY_OPENAPI_SPEC_PATH" envDefault:"specs/openapi/integration-gateway.v1.yaml"`
	HTTP             HTTPConfig             `envPrefix:"KODEX_INTEGRATION_GATEWAY_HTTP_"`
	ProviderWebhook  ProviderWebhookConfig  `envPrefix:"KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_"`
	ExternalCallback ExternalCallbackConfig `envPrefix:"KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_"`
	ProviderHub      ProviderHubConfig      `envPrefix:"KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_"`
	InteractionHub   InteractionHubConfig   `envPrefix:"KODEX_INTEGRATION_GATEWAY_INTERACTION_HUB_"`
	SecretResolver   SecretResolverConfig   `envPrefix:"KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_"`
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
	Enabled               bool          `env:"ENABLED" envDefault:"false"`
	AllowedProviderSlugs  []string      `env:"ALLOWED_PROVIDER_SLUGS" envDefault:"github" envSeparator:","`
	GitHubSecretStoreType string        `env:"GITHUB_SECRET_STORE_TYPE"`
	GitHubSecretStoreRef  string        `env:"GITHUB_SECRET_STORE_REF"`
	MaxInFlight           int           `env:"MAX_IN_FLIGHT" envDefault:"32"`
	RateLimitBurst        int           `env:"RATE_LIMIT_BURST" envDefault:"120"`
	RateLimitWindow       time.Duration `env:"RATE_LIMIT_WINDOW" envDefault:"1s"`
	RetryAfter            time.Duration `env:"RETRY_AFTER" envDefault:"1s"`
}

// ExternalCallbackConfig controls generic external channel callback routes.
type ExternalCallbackConfig struct {
	Enabled         bool          `env:"ENABLED" envDefault:"false"`
	AllowedSources  []string      `env:"ALLOWED_SOURCES" envDefault:"channel-package" envSeparator:","`
	SecretStoreType string        `env:"SECRET_STORE_TYPE"`
	SecretStoreRef  string        `env:"SECRET_STORE_REF"`
	MaxInFlight     int           `env:"MAX_IN_FLIGHT" envDefault:"32"`
	RateLimitBurst  int           `env:"RATE_LIMIT_BURST" envDefault:"120"`
	RateLimitWindow time.Duration `env:"RATE_LIMIT_WINDOW" envDefault:"1s"`
	RetryAfter      time.Duration `env:"RETRY_AFTER" envDefault:"1s"`
}

// ProviderHubConfig contains the future owner-service route settings.
type ProviderHubConfig struct {
	GRPCAddr  string        `env:"GRPC_ADDR" envDefault:"provider-hub:9090"`
	AuthToken string        `env:"GRPC_AUTH_TOKEN"`
	Timeout   time.Duration `env:"TIMEOUT" envDefault:"3s"`
}

// InteractionHubConfig contains the channel callback owner-service settings.
type InteractionHubConfig struct {
	GRPCAddr  string        `env:"GRPC_ADDR" envDefault:"interaction-hub:9090"`
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
	if err := cfg.ExternalCallback.validate(); err != nil {
		return err
	}
	if err := cfg.SecretResolver.validate(); err != nil {
		return err
	}
	if err := cfg.SecretResolver.validateStore(cfg.ProviderWebhook.Enabled, cfg.ProviderWebhook.GitHubSecretStoreType, "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE"); err != nil {
		return err
	}
	if err := cfg.SecretResolver.validateStore(cfg.ExternalCallback.Enabled, cfg.ExternalCallback.SecretStoreType, "KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_SECRET_STORE_TYPE"); err != nil {
		return err
	}
	if err := cfg.ProviderHub.validate(cfg.ProviderWebhook.Enabled); err != nil {
		return err
	}
	return cfg.InteractionHub.validate(cfg.ExternalCallback.Enabled)
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
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ALLOWED_PROVIDER_SLUGS supports only github for the active provider webhook route")
		}
	}
	if !cfg.Enabled {
		return nil
	}
	if err := validateRouteLimits(
		"KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK",
		cfg.MaxInFlight,
		cfg.RateLimitBurst,
		cfg.RateLimitWindow,
		cfg.RetryAfter,
	); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.GitHubSecretStoreType) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE is required when provider webhook route is enabled")
	}
	if strings.TrimSpace(cfg.GitHubSecretStoreRef) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF is required when provider webhook route is enabled")
	}
	return nil
}

func (cfg ExternalCallbackConfig) validate() error {
	if !cfg.Enabled {
		return nil
	}
	if len(cfg.AllowedSources) == 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_ALLOWED_SOURCES is required")
	}
	for _, source := range cfg.AllowedSources {
		normalized := strings.ToLower(strings.TrimSpace(source))
		if normalized == "" {
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_ALLOWED_SOURCES contains an empty source")
		}
	}
	if err := validateRouteLimits(
		"KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK",
		cfg.MaxInFlight,
		cfg.RateLimitBurst,
		cfg.RateLimitWindow,
		cfg.RetryAfter,
	); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.SecretStoreType) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_SECRET_STORE_TYPE is required when external callback route is enabled")
	}
	if strings.TrimSpace(cfg.SecretStoreRef) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_SECRET_STORE_REF is required when external callback route is enabled")
	}
	return nil
}

func (cfg ProviderHubConfig) validate(required bool) error {
	return validateOwnerGRPCConfig(
		required,
		cfg.GRPCAddr,
		cfg.AuthToken,
		cfg.Timeout,
		"KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB",
		"provider webhook route",
	)
}

func (cfg InteractionHubConfig) validate(required bool) error {
	return validateOwnerGRPCConfig(
		required,
		cfg.GRPCAddr,
		cfg.AuthToken,
		cfg.Timeout,
		"KODEX_INTEGRATION_GATEWAY_INTERACTION_HUB",
		"external callback route",
	)
}

func validateOwnerGRPCConfig(required bool, addr string, authToken string, timeout time.Duration, prefix string, routeName string) error {
	if !required {
		return nil
	}
	if strings.TrimSpace(addr) == "" {
		return fmt.Errorf("%s_GRPC_ADDR is required when %s is enabled", prefix, routeName)
	}
	if strings.TrimSpace(authToken) == "" {
		return fmt.Errorf("%s_GRPC_AUTH_TOKEN is required when %s is enabled", prefix, routeName)
	}
	if timeout <= 0 {
		return fmt.Errorf("%s_TIMEOUT is invalid", prefix)
	}
	return nil
}

func (cfg SecretResolverConfig) validate() error {
	if cfg.MountedKubernetesMaxBytes <= 0 {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_MAX_SECRET_BYTES is invalid")
	}
	if strings.TrimSpace(cfg.VaultAddr) != "" && strings.TrimSpace(cfg.VaultToken) == "" {
		return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_TOKEN is required when Vault address is configured")
	}
	return nil
}

func (cfg SecretResolverConfig) validateStore(required bool, requiredStoreType string, storeTypeEnv string) error {
	if !required {
		return nil
	}
	switch strings.TrimSpace(requiredStoreType) {
	case secretresolver.StoreTypeEnv:
		if !cfg.EnvEnabled {
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_ENV_ENABLED must be true for env edge secret refs")
		}
	case secretresolver.StoreTypeKubernetesMountedSecret:
		if strings.TrimSpace(cfg.MountedKubernetesRoot) == "" {
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_ROOT is required for mounted Kubernetes edge secret refs")
		}
	case secretresolver.StoreTypeVault:
		if strings.TrimSpace(cfg.VaultAddr) == "" {
			return fmt.Errorf("KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_VAULT_ADDR is required for Vault edge secret refs")
		}
	default:
		return fmt.Errorf("%s is unsupported", storeTypeEnv)
	}
	return nil
}

func validateRouteLimits(prefix string, maxInFlight int, rateLimitBurst int, rateLimitWindow time.Duration, retryAfter time.Duration) error {
	if maxInFlight <= 0 {
		return fmt.Errorf("%s_MAX_IN_FLIGHT is invalid", prefix)
	}
	if rateLimitBurst <= 0 {
		return fmt.Errorf("%s_RATE_LIMIT_BURST is invalid", prefix)
	}
	if rateLimitWindow <= 0 {
		return fmt.Errorf("%s_RATE_LIMIT_WINDOW is invalid", prefix)
	}
	if retryAfter <= 0 {
		return fmt.Errorf("%s_RETRY_AFTER is invalid", prefix)
	}
	return nil
}

// HTTPRouterConfig converts process config to the HTTP transport runtime contract.
func (cfg Config) HTTPRouterConfig() httptransport.Config {
	return httptransport.Config{
		ServiceName:                     serviceName,
		OpenAPISpecPath:                 strings.TrimSpace(cfg.OpenAPISpecPath),
		RequestTimeout:                  cfg.HTTP.RequestTimeout,
		MaxBodyBytes:                    cfg.HTTP.MaxBodyBytes,
		ProviderWebhookEnabled:          cfg.ProviderWebhook.Enabled,
		AllowedProviderSlugs:            cfg.ProviderWebhook.AllowedProviderSlugs,
		ProviderWebhookMaxInFlight:      cfg.ProviderWebhook.MaxInFlight,
		ProviderWebhookRateLimitBurst:   cfg.ProviderWebhook.RateLimitBurst,
		ProviderWebhookRateLimitWindow:  cfg.ProviderWebhook.RateLimitWindow,
		ProviderWebhookRetryAfter:       cfg.ProviderWebhook.RetryAfter,
		ExternalCallbackEnabled:         cfg.ExternalCallback.Enabled,
		AllowedCallbackSources:          cfg.ExternalCallback.AllowedSources,
		ExternalCallbackMaxInFlight:     cfg.ExternalCallback.MaxInFlight,
		ExternalCallbackRateLimitBurst:  cfg.ExternalCallback.RateLimitBurst,
		ExternalCallbackRateLimitWindow: cfg.ExternalCallback.RateLimitWindow,
		ExternalCallbackRetryAfter:      cfg.ExternalCallback.RetryAfter,
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

// InteractionHubClientConfig converts process config to an interaction-hub client contract.
func (cfg Config) InteractionHubClientConfig() interactionhubclient.Config {
	return interactionhubclient.Config{
		Addr:      cfg.InteractionHub.GRPCAddr,
		AuthToken: cfg.InteractionHub.AuthToken,
		Timeout:   cfg.InteractionHub.Timeout,
	}
}

// GitHubWebhookSecretRef converts process config to a safe secret reference.
func (cfg Config) GitHubWebhookSecretRef() secretresolver.SecretRef {
	return secretresolver.SecretRef{
		StoreType: strings.TrimSpace(cfg.ProviderWebhook.GitHubSecretStoreType),
		StoreRef:  strings.TrimSpace(cfg.ProviderWebhook.GitHubSecretStoreRef),
	}
}

// ExternalCallbackSecretRef converts process config to a safe secret reference.
func (cfg Config) ExternalCallbackSecretRef() secretresolver.SecretRef {
	return secretresolver.SecretRef{
		StoreType: strings.TrimSpace(cfg.ExternalCallback.SecretStoreType),
		StoreRef:  strings.TrimSpace(cfg.ExternalCallback.SecretStoreRef),
	}
}
