package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecodeContextValidatesDigestAndFingerprint(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextFile, diagnostic := DecodeContext(raw, cfg)
	if !diagnostic.OK() {
		t.Fatalf("DecodeContext() diagnostic = %v", diagnostic)
	}
	if contextFile.AgentRunID != cfg.AgentRunID {
		t.Fatalf("AgentRunID = %q, want %q", contextFile.AgentRunID, cfg.AgentRunID)
	}

	cfg.ContextDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	_, diagnostic = DecodeContext(raw, cfg)
	if diagnostic.Code != "agent_run_context_digest_mismatch" {
		t.Fatalf("digest mismatch code = %q", diagnostic.Code)
	}

	cfg, raw = validConfigAndContext(t)
	cfg.ExpectedMaterializationFingerprint = "materialization:fingerprint:stale"
	_, diagnostic = DecodeContext(raw, cfg)
	if diagnostic.Code != "agent_run_context_fingerprint_mismatch" {
		t.Fatalf("fingerprint mismatch code = %q", diagnostic.Code)
	}
}

func TestDecodeContextRejectsRawFieldsAndKeepsDiagnosticSafe(t *testing.T) {
	cfg, _ := validConfigAndContext(t)
	raw := []byte(`{"agent_run_id":"11111111-1111-1111-1111-111111111111","prompt_body":"do not log this"}`)
	cfg.ContextDigest = SHA256Digest(raw)

	_, diagnostic := DecodeContext(raw, cfg)
	if diagnostic.OK() {
		t.Fatal("DecodeContext() diagnostic = ok, want failure")
	}
	if strings.Contains(strings.ToLower(diagnostic.Summary), "prompt") {
		t.Fatalf("diagnostic summary leaked raw field marker: %q", diagnostic.Summary)
	}
}

func TestDecodeContextRejectsTrailingGarbage(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	raw = append(raw, []byte(" trailing-garbage")...)
	cfg.ContextDigest = SHA256Digest(raw)

	_, diagnostic := DecodeContext(raw, cfg)
	if diagnostic.Code != "agent_run_context_invalid" {
		t.Fatalf("DecodeContext() code = %q", diagnostic.Code)
	}
}

func TestDecodeContextRejectsLargePayload(t *testing.T) {
	cfg, _ := validConfigAndContext(t)
	raw := []byte(strings.Repeat("x", maxContextBytes+1))
	cfg.ContextDigest = SHA256Digest(raw)

	_, diagnostic := DecodeContext(raw, cfg)
	if diagnostic.Code != "agent_run_context_too_large" {
		t.Fatalf("DecodeContext() code = %q", diagnostic.Code)
	}
}

func TestConfigNormalizeRejectsUnsupportedMode(t *testing.T) {
	cfg, _ := validConfigAndContext(t)
	cfg.RunnerMode = "shell"

	_, diagnostic := cfg.Normalize()
	if diagnostic.Code != "unsupported_runner_mode" {
		t.Fatalf("Normalize() code = %q", diagnostic.Code)
	}
}

func TestValidateCodexSessionExecutionSpecAcceptsSafeRefs(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextFile, diagnostic := DecodeContext(raw, cfg)
	if !diagnostic.OK() {
		t.Fatalf("DecodeContext() diagnostic = %v", diagnostic)
	}
	cfg.CodexSessionExecutionSpecJSON = validCodexSessionExecutionSpecJSON(t, cfg)

	spec, diagnostic := ValidateCodexSessionExecutionSpec(cfg, contextFile)
	if !diagnostic.OK() {
		t.Fatalf("ValidateCodexSessionExecutionSpec() diagnostic = %v", diagnostic)
	}
	if spec.InstructionObjectDigest == "" || len(spec.OutputRefs) != 1 || len(spec.ResultRefs) != 1 {
		t.Fatalf("spec = %+v, want normalized execution refs", spec)
	}
}

func TestValidateCodexSessionExecutionSpecRequiresCompleteSpec(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextFile, diagnostic := DecodeContext(raw, cfg)
	if !diagnostic.OK() {
		t.Fatalf("DecodeContext() diagnostic = %v", diagnostic)
	}

	_, diagnostic = ValidateCodexSessionExecutionSpec(cfg, contextFile)
	if diagnostic.Code != "agent_execution_contract_unavailable" {
		t.Fatalf("missing spec code = %q", diagnostic.Code)
	}

	cfg.CodexSessionExecutionSpecJSON = `{"instruction_object_ref":"object://instructions"}`
	_, diagnostic = ValidateCodexSessionExecutionSpec(cfg, contextFile)
	if diagnostic.Code != "agent_execution_contract_unavailable" {
		t.Fatalf("incomplete spec code = %q", diagnostic.Code)
	}
}

func TestValidateCodexSessionExecutionSpecRejectsUnsafeRefsWithoutLeak(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextFile, diagnostic := DecodeContext(raw, cfg)
	if !diagnostic.OK() {
		t.Fatalf("DecodeContext() diagnostic = %v", diagnostic)
	}
	cfg.CodexSessionExecutionSpecJSON = validCodexSessionExecutionSpecJSON(t, cfg)
	cfg.CodexSessionExecutionSpecJSON = strings.ReplaceAll(
		cfg.CodexSessionExecutionSpecJSON,
		"object://instructions/11111111",
		"object://instructions/prompt_body_secret_value",
	)

	_, diagnostic = ValidateCodexSessionExecutionSpec(cfg, contextFile)
	if diagnostic.Code != "agent_execution_contract_unavailable" {
		t.Fatalf("unsafe spec code = %q", diagnostic.Code)
	}
	summary := strings.ToLower(diagnostic.Summary)
	if strings.Contains(summary, "prompt") || strings.Contains(summary, "secret") {
		t.Fatalf("diagnostic summary leaked raw marker: %q", diagnostic.Summary)
	}
}

func TestRunnerStopsWithExecutionContractDiagnostic(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	reporter := &recordingReporter{}

	diagnostic := NewRunnerWithExecutor(reporter, testLogger(), fixedClock{}, &recordingExecutor{}).Run(context.Background(), cfg)
	if diagnostic.Code != "agent_execution_contract_unavailable" {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
	if reporter.started != 0 {
		t.Fatalf("started reports = %d, want 0", reporter.started)
	}
	if reporter.failed != 1 {
		t.Fatalf("failed reports = %d, want 1", reporter.failed)
	}
	if strings.Contains(strings.ToLower(reporter.failure.Summary), "prompt") {
		t.Fatalf("failure summary leaked raw marker: %q", reporter.failure.Summary)
	}
}

func TestRunnerExecutesCodexWithCheckedWorkspaceInput(t *testing.T) {
	t.Setenv("KODEX_AGENT_RUNNER_AGENT_MANAGER_GRPC_AUTH_TOKEN", "secret_value_should_not_reach_codex")
	t.Setenv("SECRET_VALUE", "secret_value_should_not_reach_codex")
	cfg, raw := validConfigAndContext(t)
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	instructionDigest, schemaDigest := writeExecutionInput(t, cfg.WorkspaceMountPath, []byte("checked instruction text"), []byte(`{"type":"object"}`))
	cfg.CodexSessionExecutionSpecJSON = validWorkspaceCodexSessionExecutionSpecJSON(t, cfg, instructionDigest, schemaDigest, 30)
	reporter := &recordingReporter{}
	executor := NewCodexCLIExecutorForTest(writeFakeCodexExecutable(t, fakeCodexSuccess))

	diagnostic := NewRunnerWithExecutor(reporter, testLogger(), fixedClock{}, executor).Run(context.Background(), cfg)
	if !diagnostic.OK() {
		t.Fatalf("Run() diagnostic = %+v", diagnostic)
	}
	if reporter.started != 1 || reporter.completed != 1 || reporter.failed != 0 {
		t.Fatalf("reports = started:%d completed:%d failed:%d, want start and completion", reporter.started, reporter.completed, reporter.failed)
	}
	if reporter.result.ResultDigest == "" {
		t.Fatal("result digest is empty")
	}
	for _, value := range []string{diagnostic.Summary, reporter.result.SafeSummary} {
		lower := strings.ToLower(value)
		if strings.Contains(lower, "prompt") || strings.Contains(lower, "secret") {
			t.Fatalf("safe result leaked raw output marker: %q", value)
		}
	}
}

func TestRunnerRejectsMissingExecutionInputDigestBeforeCodex(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	instructionDigest, schemaDigest := writeExecutionInput(t, cfg.WorkspaceMountPath, []byte("checked instruction text"), []byte(`{"type":"object"}`))
	cfg.CodexSessionExecutionSpecJSON = validWorkspaceCodexSessionExecutionSpecJSON(t, cfg, instructionDigest, schemaDigest, 30)
	cfg.CodexSessionExecutionSpecJSON = strings.ReplaceAll(cfg.CodexSessionExecutionSpecJSON, instructionDigest, "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	reporter := &recordingReporter{}
	executor := &recordingExecutor{}

	diagnostic := NewRunnerWithExecutor(reporter, testLogger(), fixedClock{}, executor).Run(context.Background(), cfg)
	if diagnostic.Code != "agent_execution_contract_unavailable" {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
	if executor.called != 0 {
		t.Fatalf("executor calls = %d, want none before checked input passes digest", executor.called)
	}
	if reporter.started != 0 || reporter.failed != 1 {
		t.Fatalf("reports = started:%d failed:%d, want only failure", reporter.started, reporter.failed)
	}
}

func TestRunnerDoesNotFallbackForObjectExecutionRefs(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	cfg.CodexSessionExecutionSpecJSON = validCodexSessionExecutionSpecJSON(t, cfg)
	reporter := &recordingReporter{}
	executor := &recordingExecutor{}

	diagnostic := NewRunnerWithExecutor(reporter, testLogger(), fixedClock{}, executor).Run(context.Background(), cfg)
	if diagnostic.Code != "agent_execution_contract_unavailable" {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
	if executor.called != 0 {
		t.Fatalf("executor calls = %d, want none for unsupported object ref", executor.called)
	}
}

func TestRunnerRejectsUnsupportedRunnerProfile(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	cfg.RunnerProfileRef = "runner-profile/codex-agent@custom"
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	instructionDigest, schemaDigest := writeExecutionInput(t, cfg.WorkspaceMountPath, []byte("checked instruction text"), []byte(`{"type":"object"}`))
	cfg.CodexSessionExecutionSpecJSON = validWorkspaceCodexSessionExecutionSpecJSON(t, cfg, instructionDigest, schemaDigest, 30)
	reporter := &recordingReporter{}

	diagnostic := NewRunnerWithExecutor(reporter, testLogger(), fixedClock{}, NewCodexCLIExecutorForTest(writeFakeCodexExecutable(t, fakeCodexSuccess))).Run(context.Background(), cfg)
	if diagnostic.Code != "agent_execution_contract_unavailable" {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
	if reporter.started != 1 || reporter.failed != 1 {
		t.Fatalf("reports = started:%d failed:%d, want start and failure", reporter.started, reporter.failed)
	}
}

func TestRunnerReportsCodexNonZeroExitSafely(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	instructionDigest, schemaDigest := writeExecutionInput(t, cfg.WorkspaceMountPath, []byte("checked instruction text"), []byte(`{"type":"object"}`))
	cfg.CodexSessionExecutionSpecJSON = validWorkspaceCodexSessionExecutionSpecJSON(t, cfg, instructionDigest, schemaDigest, 30)
	reporter := &recordingReporter{}

	diagnostic := NewRunnerWithExecutor(reporter, testLogger(), fixedClock{}, NewCodexCLIExecutorForTest(writeFakeCodexExecutable(t, fakeCodexFailure))).Run(context.Background(), cfg)
	if diagnostic.Code != codeCodexExecutionFailed {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
	if strings.Contains(strings.ToLower(diagnostic.Summary), "prompt") || strings.Contains(strings.ToLower(reporter.failure.Summary), "secret") {
		t.Fatalf("diagnostic leaked raw process output: %+v/%+v", diagnostic, reporter.failure)
	}
}

func TestRunnerReportsCodexTimeout(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	instructionDigest, schemaDigest := writeExecutionInput(t, cfg.WorkspaceMountPath, []byte("checked instruction text"), []byte(`{"type":"object"}`))
	cfg.CodexSessionExecutionSpecJSON = validWorkspaceCodexSessionExecutionSpecJSON(t, cfg, instructionDigest, schemaDigest, 1)
	reporter := &recordingReporter{}

	diagnostic := NewRunnerWithExecutor(reporter, testLogger(), fixedClock{}, NewCodexCLIExecutorForTest(writeFakeCodexExecutable(t, fakeCodexTimeout))).Run(context.Background(), cfg)
	if diagnostic.Code != codeCodexExecutionTimeout {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
}

func TestRunnerRejectsOversizedCodexResult(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	instructionDigest, schemaDigest := writeExecutionInput(t, cfg.WorkspaceMountPath, []byte("checked instruction text"), []byte(`{"type":"object"}`))
	cfg.CodexSessionExecutionSpecJSON = validWorkspaceCodexSessionExecutionSpecJSON(t, cfg, instructionDigest, schemaDigest, 30)
	reporter := &recordingReporter{}

	diagnostic := NewRunnerWithExecutor(reporter, testLogger(), fixedClock{}, NewCodexCLIExecutorForTest(writeFakeCodexExecutable(t, fakeCodexLargeResult))).Run(context.Background(), cfg)
	if diagnostic.Code != codeCodexExecutionResultError {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
}

func TestRunnerReportsCodexCancellation(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	instructionDigest, schemaDigest := writeExecutionInput(t, cfg.WorkspaceMountPath, []byte("checked instruction text"), []byte(`{"type":"object"}`))
	cfg.CodexSessionExecutionSpecJSON = validWorkspaceCodexSessionExecutionSpecJSON(t, cfg, instructionDigest, schemaDigest, 30)
	reporter := &recordingReporter{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	diagnostic := NewRunnerWithExecutor(reporter, testLogger(), fixedClock{}, cancelAwareExecutor{}).Run(ctx, cfg)
	if diagnostic.Code != codeCodexExecutionCancelled {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
}

func TestRunnerReportsSafeFailureForMissingContext(t *testing.T) {
	cfg, _ := validConfigAndContext(t)
	cfg.ContextPath = "/workspace/.kodex/context/agent-run.json"
	reporter := &recordingReporter{}

	diagnostic := NewRunnerWithClock(reporter, testLogger(), fixedClock{}).Run(context.Background(), cfg)
	if diagnostic.Code != "agent_run_context_unavailable" {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
	if reporter.started != 0 {
		t.Fatalf("started reports = %d, want 0", reporter.started)
	}
	if reporter.failed != 1 {
		t.Fatalf("failed reports = %d, want 1", reporter.failed)
	}
}

func TestDiagnosticSanitizesUnsafeSummary(t *testing.T) {
	diagnostic := NewDiagnostic("unsafe", "prompt_body contains secret_value", ExitFailure)
	if strings.Contains(strings.ToLower(diagnostic.Summary), "prompt") || strings.Contains(strings.ToLower(diagnostic.Summary), "secret") {
		t.Fatalf("summary was not sanitized: %q", diagnostic.Summary)
	}
}

type recordingReporter struct {
	started   int
	completed int
	failed    int
	result    CodexExecutionResult
	failure   Diagnostic
}

func (r *recordingReporter) ReportStarted(context.Context, ReportInput) error {
	r.started++
	return nil
}

func (r *recordingReporter) ReportCompleted(_ context.Context, _ ReportInput, result CodexExecutionResult) error {
	r.completed++
	r.result = result
	return nil
}

func (r *recordingReporter) ReportFailed(_ context.Context, _ ReportInput, diagnostic Diagnostic) error {
	r.failed++
	r.failure = diagnostic
	return nil
}

type recordingExecutor struct {
	called     int
	diagnostic Diagnostic
	result     CodexExecutionResult
}

func (e *recordingExecutor) Execute(context.Context, CodexExecutionRequest) (CodexExecutionResult, Diagnostic) {
	e.called++
	if e.diagnostic.Code != "" && !e.diagnostic.OK() {
		return CodexExecutionResult{}, e.diagnostic
	}
	if e.result.ResultDigest == "" {
		e.result = CodexExecutionResult{
			ResultDigest:       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ResultSchemaRef:    "workspace://.kodex/execution/result.schema.json",
			ResultSchemaDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			SafeSummary:        "codex execution completed",
		}
	}
	return e.result, OKDiagnostic()
}

type cancelAwareExecutor struct{}

func (cancelAwareExecutor) Execute(ctx context.Context, _ CodexExecutionRequest) (CodexExecutionResult, Diagnostic) {
	<-ctx.Done()
	return CodexExecutionResult{}, NewDiagnostic(codeCodexExecutionCancelled, "codex execution was cancelled", ExitFailure)
}

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func validConfigAndContext(t *testing.T) (Config, []byte) {
	t.Helper()
	cfg := Config{
		AgentRunID:                         "11111111-1111-1111-1111-111111111111",
		RuntimeJobID:                       "22222222-2222-2222-2222-222222222222",
		SlotID:                             "33333333-3333-3333-3333-333333333333",
		ExpectedMaterializationID:          "44444444-4444-4444-4444-444444444444",
		ExpectedMaterializationFingerprint: "materialization:fingerprint:abc123",
		WorkspaceRef:                       "runtime.workspace/11111111",
		WorkspaceMountRef:                  "pvc/runtime-workspace",
		WorkspaceMountPath:                 "/workspace",
		ContextRef:                         "runtime.context/agent-run.json",
		ContextPath:                        "/workspace/.kodex/context/agent-run.json",
		RunnerProfileRef:                   "runner-profile/codex-agent@v1",
		RunnerMode:                         RunnerModeCodexAgent,
		AllowedSecretRefsJSON:              `[{"kind":"vault_ref","ref":"secret/runtime/agent-runner"}]`,
		ReportingTargetRefsJSON:            `[{"kind":"agent_manager_run","ref":"agent-run/11111111"}]`,
	}
	payload := map[string]any{
		"agent_run_id":               cfg.AgentRunID,
		"agent_session_id":           "55555555-5555-5555-5555-555555555555",
		"flow_version_id":            "66666666-6666-6666-6666-666666666666",
		"stage_id":                   "77777777-7777-7777-7777-777777777777",
		"role_profile_id":            "88888888-8888-8888-8888-888888888888",
		"role_profile_version":       1,
		"role_profile_digest":        "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"prompt_template_version_id": "99999999-9999-9999-9999-999999999999",
		"prompt_template_digest":     "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		"workspace_fingerprint":      cfg.ExpectedMaterializationFingerprint,
		"runtime_profile":            "codex-agent/default",
		"provider_target": map[string]any{
			"work_item_ref":    "provider.work-item/123",
			"pull_request_ref": "provider.pr/456",
		},
		"guidance_packages": []map[string]any{{
			"local_path":               ".kodex/guidance/package-a",
			"package_installation_ref": "package-installation/abc",
			"package_version_ref":      "package-version/def",
			"manifest_digest":          "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			"capability_ref":           "capability/guidance",
		}},
		"allowed_mcp_tools": []string{"governance.risk.get"},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() err = %v", err)
	}
	cfg.ContextDigest = SHA256Digest(raw)
	return cfg, raw
}

func validCodexSessionExecutionSpecJSON(t *testing.T, cfg Config) string {
	t.Helper()
	payload := map[string]any{
		"instruction_object_ref":    "object://instructions/11111111",
		"instruction_object_digest": "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		"result_schema_ref":         "object://schemas/codex-result-v1",
		"result_schema_digest":      "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"workspace_snapshot_ref":    "runtime://workspace-snapshots/11111111",
		"hook_endpoint_ref":         "hook://codex-hook-ingress/agent-runner",
		"callback_refs": []map[string]any{{
			"kind": "agent_run_state",
			"ref":  "agent-manager://runs/" + cfg.AgentRunID,
		}},
		"timeout_seconds":    1800,
		"runner_profile_ref": cfg.RunnerProfileRef,
		"runner_mode":        RunnerModeCodexAgent,
		"output_refs": []map[string]any{{
			"kind": "last_message",
			"ref":  "object://codex-output/last-message",
		}},
		"result_refs": []map[string]any{{
			"kind": "result_metadata",
			"ref":  "object://codex-output/result-metadata",
		}},
		"allowed_secret_refs": []map[string]any{{
			"kind": "runtime_api",
			"ref":  "secret://runtime/agent-token",
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() err = %v", err)
	}
	return string(raw)
}

func validWorkspaceCodexSessionExecutionSpecJSON(t *testing.T, cfg Config, instructionDigest string, schemaDigest string, timeoutSeconds int) string {
	t.Helper()
	payload := map[string]any{
		"instruction_object_ref":    "workspace://.kodex/execution/instruction.txt",
		"instruction_object_digest": instructionDigest,
		"result_schema_ref":         "workspace://.kodex/execution/result.schema.json",
		"result_schema_digest":      schemaDigest,
		"workspace_snapshot_ref":    "runtime://workspace-snapshots/11111111",
		"hook_endpoint_ref":         "hook://codex-hook-ingress/agent-runner",
		"callback_refs": []map[string]any{{
			"kind": "agent_run_state",
			"ref":  "agent-manager://runs/" + cfg.AgentRunID,
		}},
		"timeout_seconds":    timeoutSeconds,
		"runner_profile_ref": cfg.RunnerProfileRef,
		"runner_mode":        RunnerModeCodexAgent,
		"output_refs": []map[string]any{{
			"kind": "last_message",
			"ref":  "object://codex-output/last-message",
		}},
		"result_refs": []map[string]any{{
			"kind": "result_metadata",
			"ref":  "object://codex-output/result-metadata",
		}},
		"allowed_secret_refs": []map[string]any{{
			"kind": "runtime_api",
			"ref":  "secret://runtime/agent-token",
		}},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal() err = %v", err)
	}
	return string(raw)
}

func writeTempContext(t *testing.T, raw []byte) string {
	t.Helper()
	root := t.TempDir()
	contextDir := filepath.Join(root, ".kodex", "context")
	if err := os.MkdirAll(contextDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() err = %v", err)
	}
	contextPath := filepath.Join(contextDir, "agent-run.json")
	if err := os.WriteFile(contextPath, raw, 0o600); err != nil {
		t.Fatalf("WriteFile() err = %v", err)
	}
	return contextPath
}

func writeExecutionInput(t *testing.T, workspaceRoot string, instruction []byte, schema []byte) (string, string) {
	t.Helper()
	executionDir := filepath.Join(workspaceRoot, ".kodex", "execution")
	if err := os.MkdirAll(executionDir, 0o700); err != nil {
		t.Fatalf("MkdirAll() err = %v", err)
	}
	instructionPath := filepath.Join(executionDir, "instruction.txt")
	if err := os.WriteFile(instructionPath, instruction, 0o600); err != nil {
		t.Fatalf("WriteFile(instruction) err = %v", err)
	}
	schemaPath := filepath.Join(executionDir, "result.schema.json")
	if err := os.WriteFile(schemaPath, schema, 0o600); err != nil {
		t.Fatalf("WriteFile(schema) err = %v", err)
	}
	return SHA256Digest(instruction), SHA256Digest(schema)
}

type fakeCodexBehavior string

const (
	fakeCodexSuccess     fakeCodexBehavior = "success"
	fakeCodexFailure     fakeCodexBehavior = "failure"
	fakeCodexTimeout     fakeCodexBehavior = "timeout"
	fakeCodexLargeResult fakeCodexBehavior = "large_result"
)

func writeFakeCodexExecutable(t *testing.T, behavior fakeCodexBehavior) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
set -eu
out=""
schema=""
cd_dir=""
sandbox=""
while [ "$#" -gt 0 ]; do
	case "$1" in
		exec|--json|--ephemeral|-)
			shift
			;;
		--output-last-message)
			out="$2"
			shift 2
			;;
		--output-schema)
			schema="$2"
			shift 2
			;;
		--cd)
			cd_dir="$2"
			shift 2
			;;
		--sandbox)
			sandbox="$2"
			shift 2
			;;
		*)
			exit 42
			;;
	esac
done
if [ -z "$out" ] || [ -z "$schema" ] || [ -z "$cd_dir" ] || [ "$sandbox" != "workspace-write" ]; then
	exit 43
fi
if [ ! -f "$schema" ]; then
	exit 44
fi
if [ "${KODEX_AGENT_RUNNER_AGENT_MANAGER_GRPC_AUTH_TOKEN:-}" != "" ] || [ "${SECRET_VALUE:-}" != "" ]; then
	exit 45
fi
cat >/dev/null
case "` + string(behavior) + `" in
	success)
		printf '{"status":"ok","summary":"prompt_body secret_value should not leak"}' > "$out"
		;;
	failure)
		printf 'prompt_body secret_value should not leak\n' >&2
		exit 7
		;;
	timeout)
		sleep 2
		;;
	large_result)
		dd if=/dev/zero bs=1024 count=65 2>/dev/null | tr '\000' 'x' > "$out"
		;;
esac
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatalf("WriteFile(fake codex) err = %v", err)
	}
	return path
}
