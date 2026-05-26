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
