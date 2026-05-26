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
	t.Setenv("KODEX_INTEGRATION_GATEWAY_PROVIDER_WEBHOOK_ALLOWED_PROVIDER_SLUGS", "github,gitlab")
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
	if len(cfg.ProviderWebhook.AllowedProviderSlugs) != 2 {
		t.Fatalf("AllowedProviderSlugs = %#v, want default github/gitlab", cfg.ProviderWebhook.AllowedProviderSlugs)
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
		ProviderWebhook: ProviderWebhookConfig{Enabled: true, AllowedProviderSlugs: []string{"github"}},
		ProviderHub:     ProviderHubConfig{GRPCAddr: "provider-hub:9090", Timeout: time.Second},
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want token error")
	}
	if !strings.Contains(err.Error(), "KODEX_INTEGRATION_GATEWAY_PROVIDER_HUB_GRPC_AUTH_TOKEN") {
		t.Fatalf("Validate() error = %v, want provider-hub token error", err)
	}
}
