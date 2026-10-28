package platform

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// readWorkspaceVolumeSchemaProof separates the actual017+SSH envelope from its explicitly versioned inverse014 projection.
// It executes only shipped read-only metadata SQL against an exact disposable loopback clone, never business fixtures or live credentials.
// readWorkspaceVolumeSchemaProof отделяет actual017+SSH envelope от явно версионированной inverse014 projection.
// Исполняет только поставляемый read-only metadata SQL на exact disposable loopback clone, без business fixtures и live credentials.
func readWorkspaceVolumeSchemaProof(t *testing.T, ctx context.Context, conn *pgx.Conn) (map[string]any, map[string]any) {
	t.Helper()
	config := conn.Config()
	if config.Host != "127.0.0.1" && config.Host != "localhost" || config.Port < 1024 ||
		config.User != "postgres" || !strings.HasPrefix(config.Database, "kodex_assistant_test_") {
		t.Fatal("schema proof requires an isolated loopback clone")
	}
	source, err := os.ReadFile("../../../../../../../tools/release/workspace-volume-ssh-migration-postcondition.sql")
	if err != nil {
		t.Fatal("read shipped current workspace schema proof")
	}
	results, err := conn.PgConn().Exec(ctx, string(source)).ReadAll()
	if err != nil {
		t.Fatal("execute read-only current workspace schema proof")
	}
	var actual map[string]any
	for _, result := range results {
		if result.Err != nil {
			t.Fatal("current workspace schema proof statement failed", result.Err)
		}
		if len(result.Rows) == 0 {
			continue
		}
		if actual != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 || len(result.Rows[0][0]) > 1<<20 ||
			json.Unmarshal(result.Rows[0][0], &actual) != nil {
			t.Fatal("current workspace schema proof shape is invalid")
		}
	}
	projection, ok := actual["predecessorProjection"].(map[string]any)
	if !ok || actual["profile"] != "WORKSPACE_VOLUME_SSH_V1" || actual["readOnly"] != "on" ||
		actual["gooseVersion"] != float64(20261027000100) || projection["database"] != config.Database ||
		projection["gooseVersion"] != float64(20261026001400) || projection["reviewQAEvidenceProfile"] != "GITHUB_REVIEW_QA_V1" {
		t.Fatal("actual workspace schema or inverse predecessor identity mismatch")
	}
	return actual, projection
}

// assertWorkspaceVolumeSchemaCanonical checks the complete actual envelope without substituting inverse graph counts or versions.
// Counterfactual callers inspect their specific false predicate before invoking this canonical-only assertion.
// assertWorkspaceVolumeSchemaCanonical проверяет полный actual envelope без подмены inverse graph counts и versions.
// Counterfactual callers проверяют свой false predicate до этой проверки исключительно canonical schema.
func assertWorkspaceVolumeSchemaCanonical(t *testing.T, actual map[string]any) {
	t.Helper()
	for key, expected := range map[string]any{
		"purgeNodes": float64(109), "purgeEdges": float64(303), "workspacePurgeGuard": true,
		"workspaceCatalog": true, "checkpointCatalog": true, "volumeCatalog": true,
		"sshComposeProfile": "SSH_COMPOSE_V1", "sshColumns": true, "sshConstraints": true,
		"sshFunctions": true, "sshTriggers": true, "sshRuntimeColumns": true,
	} {
		if actual[key] != expected {
			t.Errorf("actual workspace schema proof mismatch: field=%s", key)
		}
	}
	for _, key := range []string{"purgeNodeFingerprint", "purgeEdgeFingerprint"} {
		value, ok := actual[key].(string)
		decoded, err := hex.DecodeString(value)
		if !ok || err != nil || len(decoded) != 16 {
			t.Errorf("actual workspace graph fingerprint is invalid: field=%s", key)
		}
	}
}
