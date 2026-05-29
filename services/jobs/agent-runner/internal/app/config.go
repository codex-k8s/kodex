package app

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/google/uuid"
)

const (
	CommandRun              = "run"
	RunnerModeCodexAgent    = "codex_agent"
	defaultWorkspacePath    = "/workspace"
	defaultContextPath      = "/workspace/.kodex/context/agent-run.json"
	maxSafeRefBytes         = 512
	maxSafeLabelBytes       = 64
	maxExecutionRefs        = 16
	defaultReporterTimeout  = 3 * time.Second
	envAgentManagerGRPCAddr = "KODEX_AGENT_RUNNER_AGENT_MANAGER_GRPC_ADDR"
	envAgentManagerToken    = "KODEX_AGENT_RUNNER_AGENT_MANAGER_GRPC_AUTH_TOKEN"
)

type Config struct {
	AgentRunID                         string `env:"KODEX_AGENT_RUN_ID"`
	RuntimeJobID                       string `env:"KODEX_RUNTIME_JOB_ID"`
	SlotID                             string `env:"KODEX_RUNTIME_SLOT_ID"`
	ExpectedMaterializationID          string `env:"KODEX_RUNTIME_MATERIALIZATION_ID"`
	ExpectedMaterializationFingerprint string `env:"KODEX_RUNTIME_MATERIALIZATION_FINGERPRINT"`
	WorkspaceRef                       string `env:"KODEX_RUNTIME_WORKSPACE_REF"`
	WorkspaceMountRef                  string `env:"KODEX_RUNTIME_WORKSPACE_MOUNT_REF"`
	WorkspaceMountPath                 string `env:"KODEX_RUNTIME_WORKSPACE_MOUNT_PATH" envDefault:"/workspace"`
	ContextRef                         string `env:"KODEX_AGENT_RUN_CONTEXT_REF"`
	ContextDigest                      string `env:"KODEX_AGENT_RUN_CONTEXT_DIGEST"`
	ContextPath                        string `env:"KODEX_AGENT_RUN_CONTEXT_PATH" envDefault:"/workspace/.kodex/context/agent-run.json"`
	RunnerProfileRef                   string `env:"KODEX_AGENT_RUNNER_PROFILE_REF"`
	RunnerMode                         string `env:"KODEX_AGENT_RUNNER_MODE"`
	AllowedSecretRefsJSON              string `env:"KODEX_AGENT_RUN_ALLOWED_SECRET_REFS_JSON" envDefault:"[]"`
	ReportingTargetRefsJSON            string `env:"KODEX_AGENT_RUN_REPORTING_TARGET_REFS_JSON" envDefault:"[]"`
	AgentManager                       ReporterConfig
	AllowedSecretRefs                  []ExecutionRef `env:"-"`
	ReportingTargetRefs                []ExecutionRef `env:"-"`
}

type ReporterConfig struct {
	GRPCAddr  string        `env:"KODEX_AGENT_RUNNER_AGENT_MANAGER_GRPC_ADDR"`
	AuthToken string        `env:"KODEX_AGENT_RUNNER_AGENT_MANAGER_GRPC_AUTH_TOKEN"`
	Timeout   time.Duration `env:"KODEX_AGENT_RUNNER_AGENT_MANAGER_TIMEOUT" envDefault:"3s"`
}

type ExecutionRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

func LoadConfig() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (cfg Config) Normalize() (Config, Diagnostic) {
	cfg.AgentRunID = strings.TrimSpace(cfg.AgentRunID)
	cfg.RuntimeJobID = strings.TrimSpace(cfg.RuntimeJobID)
	cfg.SlotID = strings.TrimSpace(cfg.SlotID)
	cfg.ExpectedMaterializationID = strings.TrimSpace(cfg.ExpectedMaterializationID)
	cfg.ExpectedMaterializationFingerprint = strings.TrimSpace(cfg.ExpectedMaterializationFingerprint)
	cfg.WorkspaceRef = strings.TrimSpace(cfg.WorkspaceRef)
	cfg.WorkspaceMountRef = strings.TrimSpace(cfg.WorkspaceMountRef)
	cfg.WorkspaceMountPath = firstNonEmpty(strings.TrimSpace(cfg.WorkspaceMountPath), defaultWorkspacePath)
	cfg.ContextRef = strings.TrimSpace(cfg.ContextRef)
	cfg.ContextDigest = strings.TrimSpace(cfg.ContextDigest)
	cfg.ContextPath = firstNonEmpty(strings.TrimSpace(cfg.ContextPath), defaultContextPath)
	cfg.RunnerProfileRef = strings.TrimSpace(cfg.RunnerProfileRef)
	cfg.RunnerMode = strings.TrimSpace(cfg.RunnerMode)
	cfg.AgentManager.GRPCAddr = strings.TrimSpace(cfg.AgentManager.GRPCAddr)
	cfg.AgentManager.AuthToken = strings.TrimSpace(cfg.AgentManager.AuthToken)
	if cfg.AgentManager.Timeout <= 0 {
		cfg.AgentManager.Timeout = defaultReporterTimeout
	}
	if _, err := uuid.Parse(cfg.AgentRunID); err != nil {
		return cfg, NewDiagnostic("agent_run_id_invalid", "agent_run_id is invalid", ExitFailure)
	}
	if _, err := uuid.Parse(cfg.RuntimeJobID); err != nil {
		return cfg, NewDiagnostic("runtime_job_id_invalid", "runtime job id is invalid", ExitFailure)
	}
	if _, err := uuid.Parse(cfg.SlotID); err != nil {
		return cfg, NewDiagnostic("slot_id_invalid", "slot id is invalid", ExitFailure)
	}
	if _, err := uuid.Parse(cfg.ExpectedMaterializationID); err != nil {
		return cfg, NewDiagnostic("materialization_id_invalid", "workspace materialization id is invalid", ExitFailure)
	}
	if cfg.RunnerMode != RunnerModeCodexAgent {
		return cfg, NewDiagnostic("unsupported_runner_mode", "agent-runner supports only codex_agent mode", ExitFailure)
	}
	for _, value := range []string{
		cfg.ExpectedMaterializationFingerprint,
		cfg.WorkspaceRef,
		cfg.WorkspaceMountRef,
		cfg.ContextRef,
		cfg.ContextDigest,
		cfg.RunnerProfileRef,
	} {
		if !safeRef(value, true) {
			return cfg, NewDiagnostic("agent_run_ref_invalid", "agent_run execution refs are invalid", ExitFailure)
		}
	}
	if _, ok := parseSHA256Digest(cfg.ContextDigest); !ok {
		return cfg, NewDiagnostic("agent_run_context_digest_invalid", "agent_run context digest must be sha256", ExitFailure)
	}
	if !validContextPath(cfg.WorkspaceMountPath, cfg.ContextPath) {
		return cfg, NewDiagnostic("agent_run_context_path_invalid", "agent_run context path is outside the workspace contract", ExitFailure)
	}
	allowed, diagnostic := parseExecutionRefs(cfg.AllowedSecretRefsJSON, maxExecutionRefs)
	if !diagnostic.OK() {
		return cfg, diagnostic
	}
	reporting, diagnostic := parseExecutionRefs(cfg.ReportingTargetRefsJSON, maxExecutionRefs)
	if !diagnostic.OK() {
		return cfg, diagnostic
	}
	cfg.AllowedSecretRefs = allowed
	cfg.ReportingTargetRefs = reporting
	if diagnostic := cfg.AgentManager.validate(); !diagnostic.OK() {
		return cfg, diagnostic
	}
	return cfg, OKDiagnostic()
}

func (cfg ReporterConfig) Enabled() bool {
	return strings.TrimSpace(cfg.GRPCAddr) != "" && strings.TrimSpace(cfg.AuthToken) != ""
}

func (cfg ReporterConfig) validate() Diagnostic {
	addr := strings.TrimSpace(cfg.GRPCAddr)
	token := strings.TrimSpace(cfg.AuthToken)
	if addr == "" && token == "" {
		return OKDiagnostic()
	}
	if addr == "" {
		return NewDiagnostic(CodeInvalidConfiguration, envAgentManagerGRPCAddr+" is required when agent-manager reporting is enabled", ExitFailure)
	}
	if token == "" {
		return NewDiagnostic(CodeInvalidConfiguration, envAgentManagerToken+" is required when agent-manager reporting is enabled", ExitFailure)
	}
	if cfg.Timeout <= 0 {
		return NewDiagnostic(CodeInvalidConfiguration, "agent-manager reporting timeout is invalid", ExitFailure)
	}
	return OKDiagnostic()
}

func parseExecutionRefs(raw string, limit int) ([]ExecutionRef, Diagnostic) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, OKDiagnostic()
	}
	var refs []ExecutionRef
	decoder := json.NewDecoder(strings.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&refs); err != nil {
		return nil, NewDiagnostic("agent_run_execution_refs_invalid", "agent_run execution refs are invalid", ExitFailure)
	}
	if len(refs) > limit {
		return nil, NewDiagnostic("agent_run_execution_refs_too_large", "agent_run execution refs input is too large", ExitFailure)
	}
	for _, ref := range refs {
		if !safeLabel(ref.Kind, true) || !safeRef(ref.Ref, true) {
			return nil, NewDiagnostic("agent_run_execution_refs_invalid", "agent_run execution refs are invalid", ExitFailure)
		}
	}
	return refs, OKDiagnostic()
}

func validContextPath(workspacePath string, contextPath string) bool {
	workspace := filepath.Clean(firstNonEmpty(strings.TrimSpace(workspacePath), defaultWorkspacePath))
	context := filepath.Clean(firstNonEmpty(strings.TrimSpace(contextPath), defaultContextPath))
	return context == filepath.Join(workspace, ".kodex", "context", "agent-run.json")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
