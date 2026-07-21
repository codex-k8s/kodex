package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveDocumentsKeepV28ForwardOnlyContract(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	for _, relativePath := range []string{
		"docs/operations/deployment-rollbacks.md",
		"docs/runbooks/bot-service.md",
		"docs/roadmap/wave-1-structural-foundation-proposal.md",
	} {
		body := readActiveContractDocument(t, repositoryRoot, relativePath)
		for _, required := range []string{"000025", "000026", "000027", "000028", "000029", "000030", "000032", "manifest", "forward-only", "exact N-1"} {
			if !strings.Contains(body, required) {
				t.Errorf("%s не содержит обязательный rollback contract %q", relativePath, required)
			}
		}
		if !strings.Contains(body, "fence") || !strings.Contains(body, "trigger") || !strings.Contains(body, "PR #74/#75") {
			t.Errorf("%s не сохраняет scoped fence/central trigger и меры PR #74/#75", relativePath)
		}
	}
}

func TestActiveDocumentsKeepPendingCapabilityRetentionContract(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	for _, relativePath := range []string{
		"docs/operations/interaction-capability-retention.md",
		"docs/runbooks/bot-service.md",
		"docs/roadmap/wave-1-structural-foundation-proposal.md",
	} {
		body := readActiveContractDocument(t, repositoryRoot, relativePath)
		for _, required := range []string{"pending", "unused", "consumed", "revoked", "expires_at <"} {
			if !strings.Contains(body, required) {
				t.Errorf("%s не содержит обязательный retention contract %q", relativePath, required)
			}
		}
	}
}

func TestActiveDocumentsKeepMCPTransportBoundary(t *testing.T) {
	repositoryRoot := testRepositoryRoot(t)
	runbook := readActiveContractDocument(t, repositoryRoot, "docs/runbooks/bot-service.md")
	for _, required := range []string{
		"MATTERCODEX_BOT_SERVICE_READ_TIMEOUT",
		"MATTERCODEX_BOT_SERVICE_MAX_MCP_REQUEST_BODY_BYTES",
		"Content-Length",
		"chunked",
	} {
		if !strings.Contains(runbook, required) {
			t.Errorf("runbook не содержит обязательный MCP transport contract %q", required)
		}
	}
}

func readActiveContractDocument(t *testing.T, repositoryRoot string, relativePath string) string {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join(repositoryRoot, relativePath))
	if err != nil {
		t.Fatalf("чтение %s: %v", relativePath, err)
	}
	return string(payload)
}
