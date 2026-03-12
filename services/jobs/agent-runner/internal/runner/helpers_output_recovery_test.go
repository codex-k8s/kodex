package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCodexReport_RecoversPRMetadataFromGitHub(t *testing.T) {
	t.Setenv("PATH", buildFakeOutputRecoveryPath(t, fakeOutputRecoveryConfig{
		GHViewOutput: `{"number":354,"url":"https://example.test/pr/354","state":"OPEN"}`,
	}))

	service := NewService(Config{
		RunID:                  "run-1",
		PromptConfig:           PromptConfig{PromptTemplateLocale: promptLocaleRU},
		RepositoryFullName:     "codex-k8s/codex-k8s",
		ExistingPRNumber:       354,
		RunTargetBranch:        "codex/issue-347",
		IssueNumber:            347,
		AgentKey:               "dev",
		ControlPlaneGRPCTarget: "codex-k8s-control-plane:9090",
	}, nil, nil)

	state := codexState{repoDir: t.TempDir()}
	result := runResult{
		targetBranch:     "codex/issue-347",
		existingPRNumber: 354,
		sessionID:        "sess-1",
	}

	report, repairedOutput, err := service.resolveCodexReport(context.Background(), state, &result, filepath.Join(t.TempDir(), "schema.json"), []byte(`{"status":"ok","summary":["done"]}`), true)
	if err != nil {
		t.Fatalf("resolveCodexReport() error = %v", err)
	}
	if len(repairedOutput) != 0 {
		t.Fatalf("expected no repaired output, got %q", string(repairedOutput))
	}
	if report.PRNumber != 354 {
		t.Fatalf("pr_number = %d, want 354", report.PRNumber)
	}
	if report.PRURL != "https://example.test/pr/354" {
		t.Fatalf("pr_url = %q", report.PRURL)
	}
	if report.Summary != "done" {
		t.Fatalf("summary = %q, want done", report.Summary)
	}
}

func TestResolveCodexReport_RepairsStructuredOutputWhenRequiredFieldsMissing(t *testing.T) {
	t.Setenv("PATH", buildFakeOutputRecoveryPath(t, fakeOutputRecoveryConfig{
		GHListOutput:      `[]`,
		CodexRepairOutput: `{"summary":"repaired","branch":"codex/issue-347","pr_number":354,"pr_url":"https://example.test/pr/354","session_id":"sess-1","model":"gpt-5.4","reasoning_effort":"high"}`,
	}))

	service := NewService(Config{
		RunID:              "run-2",
		PromptConfig:       PromptConfig{PromptTemplateLocale: promptLocaleRU},
		RepositoryFullName: "codex-k8s/codex-k8s",
		ExistingPRNumber:   354,
		RunTargetBranch:    "codex/issue-347",
		IssueNumber:        347,
		AgentKey:           "dev",
	}, nil, nil)

	state := codexState{repoDir: t.TempDir()}
	result := runResult{
		targetBranch:     "codex/issue-347",
		existingPRNumber: 0,
		sessionID:        "sess-1",
	}

	report, repairedOutput, err := service.resolveCodexReport(context.Background(), state, &result, filepath.Join(t.TempDir(), "schema.json"), []byte(`{"status":"ok","summary":["done"]}`), true)
	if err != nil {
		t.Fatalf("resolveCodexReport() error = %v", err)
	}
	if strings.TrimSpace(string(repairedOutput)) == "" {
		t.Fatalf("expected repaired output to be present")
	}
	if report.PRNumber != 354 {
		t.Fatalf("pr_number = %d, want 354", report.PRNumber)
	}
	if report.PRURL != "https://example.test/pr/354" {
		t.Fatalf("pr_url = %q", report.PRURL)
	}
	if report.Summary != "repaired" {
		t.Fatalf("summary = %q, want repaired", report.Summary)
	}
}

type fakeOutputRecoveryConfig struct {
	GHViewOutput      string
	GHListOutput      string
	CodexRepairOutput string
}

func buildFakeOutputRecoveryPath(t *testing.T, cfg fakeOutputRecoveryConfig) string {
	t.Helper()

	binDir := t.TempDir()
	ghScriptPath := filepath.Join(binDir, "gh")
	codexScriptPath := filepath.Join(binDir, "codex")

	ghScript := `#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == "pr" && "${2:-}" == "view" ]]; then
  printf '%s' "$FAKE_GH_PR_VIEW_OUTPUT"
  exit 0
fi
if [[ "${1:-}" == "pr" && "${2:-}" == "list" ]]; then
  printf '%s' "$FAKE_GH_PR_LIST_OUTPUT"
  exit 0
fi
printf 'unsupported gh invocation: %s\n' "$*" >&2
exit 1
`
	if err := os.WriteFile(ghScriptPath, []byte(ghScript), 0o755); err != nil {
		t.Fatalf("write fake gh script: %v", err)
	}

	codexScript := `#!/usr/bin/env bash
set -euo pipefail
output_file=""
prev=""
for arg in "$@"; do
  if [[ "$prev" == "--output-last-message" ]]; then
    output_file="$arg"
  fi
  prev="$arg"
done
if [[ -n "$output_file" ]]; then
  printf '%s' "$FAKE_CODEX_REPAIR_OUTPUT" > "$output_file"
fi
printf 'stdout-fallback'
`
	if err := os.WriteFile(codexScriptPath, []byte(codexScript), 0o755); err != nil {
		t.Fatalf("write fake codex script: %v", err)
	}

	t.Setenv("FAKE_GH_PR_VIEW_OUTPUT", cfg.GHViewOutput)
	t.Setenv("FAKE_GH_PR_LIST_OUTPUT", cfg.GHListOutput)
	t.Setenv("FAKE_CODEX_REPAIR_OUTPUT", cfg.CodexRepairOutput)

	return binDir + string(os.PathListSeparator) + os.Getenv("PATH")
}
