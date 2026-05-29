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

func TestRunnerStopsWithExecutionContractDiagnostic(t *testing.T) {
	cfg, raw := validConfigAndContext(t)
	contextPath := writeTempContext(t, raw)
	cfg.ContextPath = contextPath
	cfg.WorkspaceMountPath = filepath.Dir(filepath.Dir(filepath.Dir(contextPath)))
	reporter := &recordingReporter{}

	diagnostic := NewRunnerWithClock(reporter, testLogger(), fixedClock{}).Run(context.Background(), cfg)
	if diagnostic.Code != "agent_execution_contract_unavailable" {
		t.Fatalf("Run() code = %q", diagnostic.Code)
	}
	if reporter.started != 1 {
		t.Fatalf("started reports = %d, want 1", reporter.started)
	}
	if reporter.failed != 1 {
		t.Fatalf("failed reports = %d, want 1", reporter.failed)
	}
	if strings.Contains(strings.ToLower(reporter.failure.Summary), "prompt") {
		t.Fatalf("failure summary leaked raw marker: %q", reporter.failure.Summary)
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
	started int
	failed  int
	failure Diagnostic
}

func (r *recordingReporter) ReportStarted(context.Context, ReportInput) error {
	r.started++
	return nil
}

func (r *recordingReporter) ReportFailed(_ context.Context, _ ReportInput, diagnostic Diagnostic) error {
	r.failed++
	r.failure = diagnostic
	return nil
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
