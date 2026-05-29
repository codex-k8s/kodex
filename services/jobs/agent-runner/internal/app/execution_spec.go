package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	maxCodexSessionExecutionSpecBytes     = 16 * 1024
	maxCodexSessionExecutionRefs          = 8
	maxCodexSessionExecutionSecrets       = 16
	maxCodexSessionExecutionTimeoutSecs   = 24 * 60 * 60
	codeAgentExecutionContractUnavailable = "agent_execution_contract_unavailable"
)

type CodexSessionExecutionSpec struct {
	InstructionObjectRef    string         `json:"instruction_object_ref"`
	InstructionObjectDigest string         `json:"instruction_object_digest"`
	ResultSchemaRef         string         `json:"result_schema_ref"`
	ResultSchemaDigest      string         `json:"result_schema_digest"`
	SessionSnapshotRef      string         `json:"session_snapshot_ref,omitempty"`
	WorkspaceSnapshotRef    string         `json:"workspace_snapshot_ref,omitempty"`
	HookEndpointRef         string         `json:"hook_endpoint_ref"`
	CallbackRefs            []ExecutionRef `json:"callback_refs,omitempty"`
	TimeoutSeconds          int            `json:"timeout_seconds"`
	RunnerProfileRef        string         `json:"runner_profile_ref"`
	RunnerMode              string         `json:"runner_mode"`
	OutputRefs              []ExecutionRef `json:"output_refs,omitempty"`
	ResultRefs              []ExecutionRef `json:"result_refs,omitempty"`
	AllowedSecretRefs       []ExecutionRef `json:"allowed_secret_refs,omitempty"`
}

func ValidateCodexSessionExecutionSpec(cfg Config, contextFile AgentRunContext) (*CodexSessionExecutionSpec, Diagnostic) {
	raw := strings.TrimSpace(cfg.CodexSessionExecutionSpecJSON)
	if raw == "" {
		return nil, executionContractUnavailable("codex session execution spec is not available")
	}
	if len(raw) > maxCodexSessionExecutionSpecBytes {
		return nil, executionContractUnavailable("codex session execution spec is too large")
	}
	decoder := json.NewDecoder(bytes.NewReader([]byte(raw)))
	decoder.DisallowUnknownFields()
	var spec CodexSessionExecutionSpec
	if err := decoder.Decode(&spec); err != nil {
		return nil, executionContractUnavailable("codex session execution spec is invalid")
	}
	var extra json.RawMessage
	err := decoder.Decode(&extra)
	if err == nil || !errors.Is(err, io.EOF) {
		return nil, executionContractUnavailable("codex session execution spec is invalid")
	}
	spec = normalizeCodexSessionExecutionSpec(spec)
	if diagnostic := validateCodexSessionExecutionSpec(spec, cfg, contextFile); !diagnostic.OK() {
		return nil, diagnostic
	}
	return &spec, OKDiagnostic()
}

func normalizeCodexSessionExecutionSpec(spec CodexSessionExecutionSpec) CodexSessionExecutionSpec {
	spec.InstructionObjectRef = strings.TrimSpace(spec.InstructionObjectRef)
	spec.InstructionObjectDigest = strings.TrimSpace(spec.InstructionObjectDigest)
	spec.ResultSchemaRef = strings.TrimSpace(spec.ResultSchemaRef)
	spec.ResultSchemaDigest = strings.TrimSpace(spec.ResultSchemaDigest)
	spec.SessionSnapshotRef = strings.TrimSpace(spec.SessionSnapshotRef)
	spec.WorkspaceSnapshotRef = strings.TrimSpace(spec.WorkspaceSnapshotRef)
	spec.HookEndpointRef = strings.TrimSpace(spec.HookEndpointRef)
	spec.RunnerProfileRef = strings.TrimSpace(spec.RunnerProfileRef)
	spec.RunnerMode = strings.TrimSpace(spec.RunnerMode)
	spec.CallbackRefs = normalizeExecutionRefs(spec.CallbackRefs)
	spec.OutputRefs = normalizeExecutionRefs(spec.OutputRefs)
	spec.ResultRefs = normalizeExecutionRefs(spec.ResultRefs)
	spec.AllowedSecretRefs = normalizeExecutionRefs(spec.AllowedSecretRefs)
	return spec
}

func validateCodexSessionExecutionSpec(spec CodexSessionExecutionSpec, cfg Config, contextFile AgentRunContext) Diagnostic {
	requiredRefs := []string{
		spec.InstructionObjectRef,
		spec.InstructionObjectDigest,
		spec.ResultSchemaRef,
		spec.ResultSchemaDigest,
		spec.HookEndpointRef,
		spec.RunnerProfileRef,
	}
	for _, ref := range requiredRefs {
		if !safeRef(ref, true) {
			return executionContractUnavailable("codex session execution spec contains invalid refs")
		}
	}
	if _, ok := parseSHA256Digest(spec.InstructionObjectDigest); !ok {
		return executionContractUnavailable("codex session instruction digest is invalid")
	}
	if _, ok := parseSHA256Digest(spec.ResultSchemaDigest); !ok {
		return executionContractUnavailable("codex session result schema digest is invalid")
	}
	if spec.SessionSnapshotRef == "" && spec.WorkspaceSnapshotRef == "" {
		return executionContractUnavailable("codex session snapshot ref is required")
	}
	if !safeRef(spec.SessionSnapshotRef, false) || !safeRef(spec.WorkspaceSnapshotRef, false) {
		return executionContractUnavailable("codex session snapshot refs are invalid")
	}
	if contextFile.SessionSnapshotRef != "" && spec.SessionSnapshotRef != "" && contextFile.SessionSnapshotRef != spec.SessionSnapshotRef {
		return executionContractUnavailable("codex session snapshot ref does not match run context")
	}
	if spec.TimeoutSeconds <= 0 || spec.TimeoutSeconds > maxCodexSessionExecutionTimeoutSecs {
		return executionContractUnavailable("codex session timeout is invalid")
	}
	if spec.RunnerProfileRef != cfg.RunnerProfileRef || spec.RunnerMode != RunnerModeCodexAgent {
		return executionContractUnavailable("codex session runner profile is invalid")
	}
	if !validateExecutionRefList(spec.CallbackRefs, maxCodexSessionExecutionRefs) ||
		!validateExecutionRefList(spec.OutputRefs, maxCodexSessionExecutionRefs) ||
		!validateExecutionRefList(spec.ResultRefs, maxCodexSessionExecutionRefs) ||
		!validateExecutionRefList(spec.AllowedSecretRefs, maxCodexSessionExecutionSecrets) {
		return executionContractUnavailable("codex session execution refs are invalid")
	}
	if len(spec.CallbackRefs) == 0 || len(spec.OutputRefs) == 0 || len(spec.ResultRefs) == 0 {
		return executionContractUnavailable("codex session callback and output refs are required")
	}
	return OKDiagnostic()
}

func normalizeExecutionRefs(refs []ExecutionRef) []ExecutionRef {
	if len(refs) == 0 {
		return nil
	}
	normalized := make([]ExecutionRef, 0, len(refs))
	for _, ref := range refs {
		normalized = append(normalized, ExecutionRef{
			Kind: strings.TrimSpace(ref.Kind),
			Ref:  strings.TrimSpace(ref.Ref),
		})
	}
	return normalized
}

func validateExecutionRefList(refs []ExecutionRef, limit int) bool {
	if len(refs) > limit {
		return false
	}
	for _, ref := range refs {
		if !safeLabel(ref.Kind, true) || !safeRef(ref.Ref, true) {
			return false
		}
	}
	return true
}

func executionContractUnavailable(summary string) Diagnostic {
	return NewDiagnostic(codeAgentExecutionContractUnavailable, summary, ExitFailure)
}
