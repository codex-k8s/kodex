// Package app contains codex-hook-ingress process composition and lifecycle.
package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"

	hookenum "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/enum"
)

const (
	defaultSchemaVersion       = "codex-hook-ingress.normalized-hook-envelope.v1"
	defaultSanitizerContractID = "codex-hook-ingress.sanitizer.v1"
	defaultSupportedEvents     = "SessionStart,UserPromptSubmit,PreToolUse,PermissionRequest,PostToolUse,Stop"
)

// Config contains process-level codex-hook-ingress configuration.
type Config struct {
	HTTPAddr                 string        `env:"KODEX_CODEX_HOOK_INGRESS_HTTP_ADDR" envDefault:":8080"`
	ReadinessTimeout         time.Duration `env:"KODEX_CODEX_HOOK_INGRESS_READINESS_TIMEOUT" envDefault:"2s"`
	ShutdownTimeout          time.Duration `env:"KODEX_CODEX_HOOK_INGRESS_SHUTDOWN_TIMEOUT" envDefault:"10s"`
	SchemaVersion            string        `env:"KODEX_CODEX_HOOK_INGRESS_SCHEMA_VERSION" envDefault:"codex-hook-ingress.normalized-hook-envelope.v1"`
	SanitizerContractID      string        `env:"KODEX_CODEX_HOOK_INGRESS_SANITIZER_CONTRACT_ID" envDefault:"codex-hook-ingress.sanitizer.v1"`
	SupportedHookEvents      string        `env:"KODEX_CODEX_HOOK_INGRESS_SUPPORTED_HOOK_EVENTS" envDefault:"SessionStart,UserPromptSubmit,PreToolUse,PermissionRequest,PostToolUse,Stop"`
	MaxEnvelopeBytes         int           `env:"KODEX_CODEX_HOOK_INGRESS_MAX_ENVELOPE_BYTES" envDefault:"65536"`
	MaxTextPreviewBytes      int           `env:"KODEX_CODEX_HOOK_INGRESS_MAX_TEXT_PREVIEW_BYTES" envDefault:"4096"`
	MaxBoundedErrorBytes     int           `env:"KODEX_CODEX_HOOK_INGRESS_MAX_BOUNDED_ERROR_BYTES" envDefault:"8192"`
	LogicalTransportReadOnly bool          `env:"KODEX_CODEX_HOOK_INGRESS_LOGICAL_TRANSPORT_READ_ONLY" envDefault:"true"`
}

// LoadConfig reads process configuration from environment variables.
func LoadConfig() (Config, error) {
	cfg, err := env.ParseAs[Config]()
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		return Config{}, fmt.Errorf("load codex-hook-ingress config: %w", err)
	}
	return cfg, nil
}

// Validate checks process settings before runtime construction.
func (cfg Config) Validate() error {
	if strings.TrimSpace(cfg.HTTPAddr) == "" {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_HTTP_ADDR is required")
	}
	if cfg.ReadinessTimeout <= 0 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_READINESS_TIMEOUT must be positive")
	}
	if cfg.ShutdownTimeout <= 0 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_SHUTDOWN_TIMEOUT must be positive")
	}
	if strings.TrimSpace(cfg.SchemaVersion) != defaultSchemaVersion {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_SCHEMA_VERSION must stay %s", defaultSchemaVersion)
	}
	if strings.TrimSpace(cfg.SanitizerContractID) != defaultSanitizerContractID {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_SANITIZER_CONTRACT_ID must stay %s", defaultSanitizerContractID)
	}
	if cfg.MaxEnvelopeBytes != 65536 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_MAX_ENVELOPE_BYTES must stay 65536 while JSON Schema v1 is source of truth")
	}
	if cfg.MaxTextPreviewBytes != 4096 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_MAX_TEXT_PREVIEW_BYTES must stay 4096 while JSON Schema v1 is source of truth")
	}
	if cfg.MaxBoundedErrorBytes != 8192 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_MAX_BOUNDED_ERROR_BYTES must stay 8192 while JSON Schema v1 is source of truth")
	}
	events, err := cfg.SupportedEvents()
	if err != nil {
		return err
	}
	if len(events) != len(hookenum.SupportedHookEvents()) {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_SUPPORTED_HOOK_EVENTS must contain exactly the MVP hook set")
	}
	return nil
}

// SupportedEvents returns the configured MVP hook set in schema order.
func (cfg Config) SupportedEvents() ([]hookenum.HookEventName, error) {
	seen := map[hookenum.HookEventName]struct{}{}
	var events []hookenum.HookEventName
	for _, raw := range strings.Split(cfg.SupportedHookEvents, ",") {
		event := hookenum.HookEventName(strings.TrimSpace(raw))
		if event == "" {
			continue
		}
		if event == hookenum.HookEventName("PreCompact") || event == hookenum.HookEventName("PostCompact") {
			return nil, fmt.Errorf("compact checkpoints are internal session events, not Codex hooks")
		}
		if !hookenum.IsSupportedHookEvent(event) {
			return nil, fmt.Errorf("unsupported hook event %q", event)
		}
		if _, ok := seen[event]; ok {
			return nil, fmt.Errorf("duplicate hook event %q", event)
		}
		seen[event] = struct{}{}
		events = append(events, event)
	}
	for _, required := range hookenum.SupportedHookEvents() {
		if _, ok := seen[required]; !ok {
			return nil, fmt.Errorf("missing hook event %q", required)
		}
	}
	return events, nil
}
