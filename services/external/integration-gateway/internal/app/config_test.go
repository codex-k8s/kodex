package app

import (
	"strings"
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("KODEX_INTEGRATION_GATEWAY_HTTP_ADDR", ":8080")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_OPENAPI_SPEC_PATH", "specs/openapi/integration-gateway.v1.yaml")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ENABLED", "false")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ALLOWED_PROVIDER_SLUGS", "github")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.OpenAPISpecPath == "" {
		t.Fatalf("LoadConfig() = %+v, want HTTP and OpenAPI defaults", cfg)
	}
	if cfg.ProviderWebhook.Enabled {
		t.Fatal("provider webhook route enabled by default")
	}
	if len(cfg.ProviderWebhook.AllowedProviderSlugs) != 1 || cfg.ProviderWebhook.AllowedProviderSlugs[0] != "github" {
		t.Fatalf("AllowedProviderSlugs = %#v, want github default", cfg.ProviderWebhook.AllowedProviderSlugs)
	}
	if cfg.ProviderWebhook.MaxInFlight != 32 || cfg.ProviderWebhook.RateLimitBurst != 120 ||
		cfg.ProviderWebhook.RateLimitWindow != time.Second || cfg.ProviderWebhook.RetryAfter != time.Second {
		t.Fatalf("ProviderWebhook limits = %+v, want safe defaults", cfg.ProviderWebhook)
	}
	if cfg.ExternalCallback.Enabled {
		t.Fatal("external callback route enabled by default")
	}
	if len(cfg.ExternalCallback.AllowedSources) != 1 || cfg.ExternalCallback.AllowedSources[0] != "channel-package" {
		t.Fatalf("AllowedSources = %#v, want channel-package default", cfg.ExternalCallback.AllowedSources)
	}
}

func TestLoadConfigEnabledGitHubWebhook(t *testing.T) {
	t.Setenv("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ENABLED", "true")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ALLOWED_PROVIDER_SLUGS", "github")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_TYPE", "env")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF", "KODEX_TEST_GITHUB_WEBHOOK_SECRET")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_GRPC_AUTH_TOKEN", "provider-hub-token")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.ProviderWebhook.Enabled {
		t.Fatal("provider webhook route disabled, want enabled")
	}
	if cfg.ProviderWebhook.GitHubSecretStoreType != "env" || cfg.ProviderWebhook.GitHubSecretStoreRef != "KODEX_TEST_GITHUB_WEBHOOK_SECRET" {
		t.Fatalf("GitHub secret ref = %+v", cfg.ProviderWebhook)
	}
}

func TestLoadConfigEnabledExternalCallback(t *testing.T) {
	t.Setenv("KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_ENABLED", "true")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_ALLOWED_SOURCES", "channel-package")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_SECRET_STORE_TYPE", "env")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_SECRET_STORE_REF", "KODEX_TEST_EXTERNAL_CALLBACK_SECRET")
	t.Setenv("KODEX_INTEGRATION_GATEWAY_INTERACTION_HUB_GRPC_AUTH_TOKEN", "interaction-hub-token")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.ExternalCallback.Enabled {
		t.Fatal("external callback route disabled, want enabled")
	}
	if cfg.ExternalCallback.SecretStoreType != "env" || cfg.ExternalCallback.SecretStoreRef != "KODEX_TEST_EXTERNAL_CALLBACK_SECRET" {
		t.Fatalf("ExternalCallback secret ref = %+v", cfg.ExternalCallback)
	}
}

func TestConfigValidateAllowsDisabledProviderWebhookWithoutToken(t *testing.T) {
	cfg := Config{
		HTTPAddr:        ":8080",
		OpenAPISpecPath: "specs/openapi/integration-gateway.v1.yaml",
		HTTP: HTTPConfig{
			ReadHeaderTimeout: time.Second,
			RequestTimeout:    time.Second,
			ShutdownTimeout:   time.Second,
			ReadinessTimeout:  time.Second,
			MaxBodyBytes:      1024,
		},
		ProviderWebhook: ProviderWebhookConfig{Enabled: false, AllowedProviderSlugs: []string{"github"}},
		SecretResolver:  SecretResolverConfig{EnvEnabled: true, MountedKubernetesMaxBytes: 1024},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigValidateRequiresInteractionHubTokenWhenExternalCallbackEnabled(t *testing.T) {
	cfg := validExternalCallbackConfig()
	cfg.InteractionHub.AuthToken = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want token error")
	}
	if !strings.Contains(err.Error(), "KODEX_INTEGRATION_GATEWAY_INTERACTION_HUB_GRPC_AUTH_TOKEN") {
		t.Fatalf("Validate() error = %v, want interaction-hub token error", err)
	}
}

func TestConfigValidateRequiresExternalCallbackSecretRefWhenEnabled(t *testing.T) {
	cfg := validExternalCallbackConfig()
	cfg.ExternalCallback.SecretStoreRef = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want callback secret ref error")
	}
	if !strings.Contains(err.Error(), "KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_SECRET_STORE_REF") {
		t.Fatalf("Validate() error = %v, want external callback secret ref error", err)
	}
}

func TestConfigValidateRequiresProviderHubTokenWhenProviderWebhookEnabled(t *testing.T) {
	cfg := Config{
		HTTPAddr:        ":8080",
		OpenAPISpecPath: "specs/openapi/integration-gateway.v1.yaml",
		HTTP: HTTPConfig{
			ReadHeaderTimeout: time.Second,
			RequestTimeout:    time.Second,
			ShutdownTimeout:   time.Second,
			ReadinessTimeout:  time.Second,
			MaxBodyBytes:      1024,
		},
		ProviderWebhook: ProviderWebhookConfig{
			Enabled:               true,
			AllowedProviderSlugs:  []string{"github"},
			GitHubSecretStoreType: "env",
			GitHubSecretStoreRef:  "KODEX_TEST_GITHUB_WEBHOOK_SECRET",
			MaxInFlight:           32,
			RateLimitBurst:        120,
			RateLimitWindow:       time.Second,
			RetryAfter:            time.Second,
		},
		ProviderHub:    ProviderHubConfig{GRPCAddr: "provider-hub:9090", Timeout: time.Second},
		SecretResolver: SecretResolverConfig{EnvEnabled: true, MountedKubernetesMaxBytes: 1024},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want token error")
	}
	if !strings.Contains(err.Error(), "KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_GRPC_AUTH_TOKEN") {
		t.Fatalf("Validate() error = %v, want provider-hub token error", err)
	}
}

func TestConfigValidateRequiresGitHubSecretRefWhenProviderWebhookEnabled(t *testing.T) {
	cfg := validEnabledConfig()
	cfg.ProviderWebhook.GitHubSecretStoreRef = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want github secret ref error")
	}
	if !strings.Contains(err.Error(), "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_GITHUB_SECRET_STORE_REF") {
		t.Fatalf("Validate() error = %v, want github secret ref error", err)
	}
}

func TestConfigValidateRejectsInvalidExternalCallbackLimits(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "max in flight",
			mutate: func(cfg *Config) {
				cfg.ExternalCallback.MaxInFlight = 0
			},
			wantErr: "KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_MAX_IN_FLIGHT",
		},
		{
			name: "rate limit burst",
			mutate: func(cfg *Config) {
				cfg.ExternalCallback.RateLimitBurst = 0
			},
			wantErr: "KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_RATE_LIMIT_BURST",
		},
		{
			name: "rate limit window",
			mutate: func(cfg *Config) {
				cfg.ExternalCallback.RateLimitWindow = 0
			},
			wantErr: "KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_RATE_LIMIT_WINDOW",
		},
		{
			name: "retry after",
			mutate: func(cfg *Config) {
				cfg.ExternalCallback.RetryAfter = 0
			},
			wantErr: "KODEX_INTEGRATION_GATEWAY_EXTERNAL_CALLBACK_RETRY_AFTER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validExternalCallbackConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want limit error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %s", err, tt.wantErr)
			}
		})
	}
}

func TestConfigValidateRejectsInvalidProviderWebhookLimits(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name: "max in flight",
			mutate: func(cfg *Config) {
				cfg.ProviderWebhook.MaxInFlight = 0
			},
			wantErr: "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_MAX_IN_FLIGHT",
		},
		{
			name: "rate limit burst",
			mutate: func(cfg *Config) {
				cfg.ProviderWebhook.RateLimitBurst = 0
			},
			wantErr: "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RATE_LIMIT_BURST",
		},
		{
			name: "rate limit window",
			mutate: func(cfg *Config) {
				cfg.ProviderWebhook.RateLimitWindow = 0
			},
			wantErr: "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RATE_LIMIT_WINDOW",
		},
		{
			name: "retry after",
			mutate: func(cfg *Config) {
				cfg.ProviderWebhook.RetryAfter = 0
			},
			wantErr: "KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_RETRY_AFTER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validEnabledConfig()
			tt.mutate(&cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want limit error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() error = %v, want %s", err, tt.wantErr)
			}
		})
	}
}

func TestConfigValidateRejectsGitLabProviderWebhookSlugInIGW2(t *testing.T) {
	cfg := validEnabledConfig()
	cfg.ProviderWebhook.AllowedProviderSlugs = []string{"github", "gitlab"}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want unsupported slug error")
	}
	if !strings.Contains(err.Error(), "supports only github") {
		t.Fatalf("Validate() error = %v, want unsupported slug error", err)
	}
}

func TestConfigValidateRequiresMountedRootForMountedWebhookSecretRef(t *testing.T) {
	cfg := validEnabledConfig()
	cfg.ProviderWebhook.GitHubSecretStoreType = "kubernetes_mounted_secret"
	cfg.SecretResolver.MountedKubernetesRoot = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want mounted root error")
	}
	if !strings.Contains(err.Error(), "KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_ROOT") {
		t.Fatalf("Validate() error = %v, want mounted root error", err)
	}
}

func TestConfigValidateRequiresMountedRootForMountedCallbackSecretRef(t *testing.T) {
	cfg := validExternalCallbackConfig()
	cfg.ExternalCallback.SecretStoreType = "kubernetes_mounted_secret"
	cfg.SecretResolver.MountedKubernetesRoot = ""

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want mounted root error")
	}
	if !strings.Contains(err.Error(), "KODEX_INTEGRATION_GATEWAY_SECRET_RESOLVER_MOUNTED_KUBERNETES_ROOT") {
		t.Fatalf("Validate() error = %v, want mounted root error", err)
	}
}

func validEnabledConfig() Config {
	return Config{
		HTTPAddr:        ":8080",
		OpenAPISpecPath: "specs/openapi/integration-gateway.v1.yaml",
		HTTP: HTTPConfig{
			ReadHeaderTimeout: time.Second,
			RequestTimeout:    time.Second,
			ShutdownTimeout:   time.Second,
			ReadinessTimeout:  time.Second,
			MaxBodyBytes:      1024,
		},
		ProviderWebhook: ProviderWebhookConfig{
			Enabled:               true,
			AllowedProviderSlugs:  []string{"github"},
			GitHubSecretStoreType: "env",
			GitHubSecretStoreRef:  "KODEX_TEST_GITHUB_WEBHOOK_SECRET",
			MaxInFlight:           32,
			RateLimitBurst:        120,
			RateLimitWindow:       time.Second,
			RetryAfter:            time.Second,
		},
		ProviderHub: ProviderHubConfig{
			GRPCAddr:  "provider-hub:9090",
			AuthToken: "token",
			Timeout:   time.Second,
		},
		SecretResolver: SecretResolverConfig{
			EnvEnabled:                true,
			MountedKubernetesRoot:     "/var/run/kodex/secrets",
			MountedKubernetesMaxBytes: 1024,
		},
	}
}

func validExternalCallbackConfig() Config {
	return Config{
		HTTPAddr:        ":8080",
		OpenAPISpecPath: "specs/openapi/integration-gateway.v1.yaml",
		HTTP: HTTPConfig{
			ReadHeaderTimeout: time.Second,
			RequestTimeout:    time.Second,
			ShutdownTimeout:   time.Second,
			ReadinessTimeout:  time.Second,
			MaxBodyBytes:      1024,
		},
		ProviderWebhook: ProviderWebhookConfig{
			Enabled:              false,
			AllowedProviderSlugs: []string{"github"},
		},
		ExternalCallback: ExternalCallbackConfig{
			Enabled:         true,
			AllowedSources:  []string{"channel-package"},
			SecretStoreType: "env",
			SecretStoreRef:  "KODEX_TEST_EXTERNAL_CALLBACK_SECRET",
			MaxInFlight:     32,
			RateLimitBurst:  120,
			RateLimitWindow: time.Second,
			RetryAfter:      time.Second,
		},
		InteractionHub: InteractionHubConfig{
			GRPCAddr:  "interaction-hub:9090",
			AuthToken: "token",
			Timeout:   time.Second,
		},
		SecretResolver: SecretResolverConfig{
			EnvEnabled:                true,
			MountedKubernetesRoot:     "/var/run/kodex/secrets",
			MountedKubernetesMaxBytes: 1024,
		},
	}
}
