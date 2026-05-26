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
	defaultSchemaVersion                   = "codex-hook-ingress.normalized-hook-envelope.v1"
	defaultSanitizerContractID             = "codex-hook-ingress.sanitizer.v1"
	defaultSupportedEvents                 = "SessionStart,UserPromptSubmit,PreToolUse,PermissionRequest,PostToolUse,Stop"
	defaultRouteFailurePolicy              = "diagnostic"
	defaultPermissionDecisionFailurePolicy = "fail_closed"
	defaultPreToolUseDecisionFailurePolicy = "no_decision"
	defaultPreToolUseDecisionRiskClasses   = "medium,high,unknown"
)

// Config contains process-level codex-hook-ingress configuration.
type Config struct {
	HTTPAddr                        string        `env:"KODEX_CODEX_HOOK_INGRESS_HTTP_ADDR" envDefault:":8080"`
	ReadinessTimeout                time.Duration `env:"KODEX_CODEX_HOOK_INGRESS_READINESS_TIMEOUT" envDefault:"2s"`
	ShutdownTimeout                 time.Duration `env:"KODEX_CODEX_HOOK_INGRESS_SHUTDOWN_TIMEOUT" envDefault:"10s"`
	SchemaVersion                   string        `env:"KODEX_CODEX_HOOK_INGRESS_SCHEMA_VERSION" envDefault:"codex-hook-ingress.normalized-hook-envelope.v1"`
	SanitizerContractID             string        `env:"KODEX_CODEX_HOOK_INGRESS_SANITIZER_CONTRACT_ID" envDefault:"codex-hook-ingress.sanitizer.v1"`
	SupportedHookEvents             string        `env:"KODEX_CODEX_HOOK_INGRESS_SUPPORTED_HOOK_EVENTS" envDefault:"SessionStart,UserPromptSubmit,PreToolUse,PermissionRequest,PostToolUse,Stop"`
	MaxEnvelopeBytes                int           `env:"KODEX_CODEX_HOOK_INGRESS_MAX_ENVELOPE_BYTES" envDefault:"65536"`
	MaxTextPreviewBytes             int           `env:"KODEX_CODEX_HOOK_INGRESS_MAX_TEXT_PREVIEW_BYTES" envDefault:"4096"`
	MaxBoundedErrorBytes            int           `env:"KODEX_CODEX_HOOK_INGRESS_MAX_BOUNDED_ERROR_BYTES" envDefault:"8192"`
	DisabledRoutes                  string        `env:"KODEX_CODEX_HOOK_INGRESS_DISABLED_ROUTES" envDefault:""`
	RouteFailurePolicy              string        `env:"KODEX_CODEX_HOOK_INGRESS_ROUTE_FAILURE_POLICY" envDefault:"diagnostic"`
	OpsFeedCapacity                 int           `env:"KODEX_CODEX_HOOK_INGRESS_OPS_FEED_CAPACITY" envDefault:"1024"`
	OpsFeedRetention                time.Duration `env:"KODEX_CODEX_HOOK_INGRESS_OPS_FEED_RETENTION" envDefault:"15m"`
	RateLimitWindow                 time.Duration `env:"KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_WINDOW" envDefault:"1m"`
	RateLimitBurst                  int           `env:"KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_BURST" envDefault:"300"`
	DecisionBridgeTimeout           time.Duration `env:"KODEX_CODEX_HOOK_INGRESS_DECISION_BRIDGE_TIMEOUT" envDefault:"30s"`
	PermissionDecisionFailurePolicy string        `env:"KODEX_CODEX_HOOK_INGRESS_PERMISSION_DECISION_FAILURE_POLICY" envDefault:"fail_closed"`
	PreToolUseDecisionFailurePolicy string        `env:"KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_FAILURE_POLICY" envDefault:"no_decision"`
	PreToolUseDecisionRiskClasses   string        `env:"KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_RISK_CLASSES" envDefault:"medium,high,unknown"`
	LogicalTransportReadOnly        bool          `env:"KODEX_CODEX_HOOK_INGRESS_LOGICAL_TRANSPORT_READ_ONLY" envDefault:"true"`
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
	if _, err := cfg.DisabledRouteOwners(); err != nil {
		return err
	}
	if !hookenum.IsRouteFailurePolicy(cfg.RouteFailureMode()) {
		return fmt.Errorf("unsupported KODEX_CODEX_HOOK_INGRESS_ROUTE_FAILURE_POLICY %q", cfg.RouteFailurePolicy)
	}
	if cfg.OpsFeedCapacity <= 0 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_OPS_FEED_CAPACITY must be positive")
	}
	if cfg.OpsFeedRetention <= 0 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_OPS_FEED_RETENTION must be positive")
	}
	if cfg.RateLimitWindow <= 0 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_WINDOW must be positive")
	}
	if cfg.RateLimitBurst <= 0 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_RATE_LIMIT_BURST must be positive")
	}
	if cfg.DecisionBridgeTimeout <= 0 {
		return fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_DECISION_BRIDGE_TIMEOUT must be positive")
	}
	if !hookenum.IsDecisionFailurePolicy(cfg.PermissionDecisionFailureMode()) {
		return fmt.Errorf("unsupported KODEX_CODEX_HOOK_INGRESS_PERMISSION_DECISION_FAILURE_POLICY %q", cfg.PermissionDecisionFailurePolicy)
	}
	if !hookenum.IsDecisionFailurePolicy(cfg.PreToolUseDecisionFailureMode()) {
		return fmt.Errorf("unsupported KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_FAILURE_POLICY %q", cfg.PreToolUseDecisionFailurePolicy)
	}
	if _, err := cfg.PreToolUseRiskClasses(); err != nil {
		return err
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

// DisabledRouteOwners returns owner routes disabled by process config.
func (cfg Config) DisabledRouteOwners() ([]hookenum.DownstreamOwner, error) {
	seen := map[hookenum.DownstreamOwner]struct{}{}
	var owners []hookenum.DownstreamOwner
	for _, raw := range strings.Split(cfg.DisabledRoutes, ",") {
		owner := hookenum.DownstreamOwner(strings.TrimSpace(raw))
		if owner == "" {
			continue
		}
		if !hookenum.IsDownstreamOwner(owner) {
			return nil, fmt.Errorf("unsupported disabled route %q", owner)
		}
		if _, ok := seen[owner]; ok {
			return nil, fmt.Errorf("duplicate disabled route %q", owner)
		}
		seen[owner] = struct{}{}
		owners = append(owners, owner)
	}
	return owners, nil
}

// RouteFailureMode returns the configured safe route failure behavior.
func (cfg Config) RouteFailureMode() hookenum.RouteFailurePolicy {
	policy := strings.TrimSpace(cfg.RouteFailurePolicy)
	if policy == "" {
		policy = defaultRouteFailurePolicy
	}
	return hookenum.RouteFailurePolicy(policy)
}

// PermissionDecisionFailureMode returns safe fallback behavior for PermissionRequest bridge failures.
func (cfg Config) PermissionDecisionFailureMode() hookenum.DecisionFailurePolicy {
	policy := strings.TrimSpace(cfg.PermissionDecisionFailurePolicy)
	if policy == "" {
		policy = defaultPermissionDecisionFailurePolicy
	}
	return hookenum.DecisionFailurePolicy(policy)
}

// PreToolUseDecisionFailureMode returns safe fallback behavior for policy-controlled PreToolUse.
func (cfg Config) PreToolUseDecisionFailureMode() hookenum.DecisionFailurePolicy {
	policy := strings.TrimSpace(cfg.PreToolUseDecisionFailurePolicy)
	if policy == "" {
		policy = defaultPreToolUseDecisionFailurePolicy
	}
	return hookenum.DecisionFailurePolicy(policy)
}

// PreToolUseRiskClasses returns risk classes that require the governance decision bridge.
func (cfg Config) PreToolUseRiskClasses() ([]string, error) {
	seen := map[string]struct{}{}
	var classes []string
	rawClasses := cfg.PreToolUseDecisionRiskClasses
	if strings.TrimSpace(rawClasses) == "" {
		rawClasses = defaultPreToolUseDecisionRiskClasses
	}
	for _, raw := range strings.Split(rawClasses, ",") {
		class := strings.TrimSpace(raw)
		if class == "" {
			continue
		}
		if !knownRiskClass(class) {
			return nil, fmt.Errorf("unsupported KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_RISK_CLASSES value %q", class)
		}
		if _, ok := seen[class]; ok {
			return nil, fmt.Errorf("duplicate KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_RISK_CLASSES value %q", class)
		}
		seen[class] = struct{}{}
		classes = append(classes, class)
	}
	if len(classes) == 0 {
		return nil, fmt.Errorf("KODEX_CODEX_HOOK_INGRESS_PRE_TOOL_USE_DECISION_RISK_CLASSES must not be empty")
	}
	return classes, nil
}

func knownRiskClass(class string) bool {
	switch class {
	case "low", "medium", "high", "unknown":
		return true
	default:
		return false
	}
}
