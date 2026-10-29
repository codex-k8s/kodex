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

// readWorkspaceVolumeSchemaProof separates the exact actual volume or restore envelope from its versioned inverse014 projection.
// It executes only shipped read-only metadata SQL against an exact disposable loopback clone, never business fixtures or live credentials.
// readWorkspaceVolumeSchemaProof отделяет exact actual volume либо restore envelope от versioned inverse014 projection.
// Исполняет только поставляемый read-only metadata SQL на exact disposable loopback clone, без business fixtures и live credentials.
func readWorkspaceVolumeSchemaProof(t *testing.T, ctx context.Context, conn *pgx.Conn) (map[string]any, map[string]any) {
	t.Helper()
	config := conn.Config()
	if config.Host != "127.0.0.1" && config.Host != "localhost" || config.Port < 1024 ||
		config.User != "postgres" || !strings.HasPrefix(config.Database, "kodex_assistant_test_") {
		t.Fatal("schema proof requires an isolated loopback clone")
	}
	version := workspaceComponentSchemaVersion(t, ctx, conn)
	profile, filename := "WORKSPACE_VOLUME_SSH_V1", "workspace-volume-ssh-migration-postcondition.sql"
	if version == 20261028000100 {
		profile, filename = "WORKSPACE_RESTORE_V2", "workspace-restore-migration-postcondition.sql"
	}
	if version == 20261029000100 {
		profile, filename = "GITHUB_AUTONOMOUS_PDLC_V1", "github-autonomous-migration-postcondition.sql"
	}
	if version == 20261029000200 {
		profile, filename = "GITHUB_PR_COLLABORATION_V1", "github-pr-migration-postcondition.sql"
	}
	if version == 20261029000300 {
		profile, filename = "GITHUB_PDLC_V1", "workflow-pdlc-migration-postcondition.sql"
	}
	source, err := os.ReadFile("../../../../../../../tools/release/" + filename)
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
	if !ok || actual["profile"] != profile || actual["readOnly"] != "on" ||
		actual["gooseVersion"] != float64(version) || projection["database"] != config.Database ||
		projection["gooseVersion"] != float64(20261026001400) || projection["reviewQAEvidenceProfile"] != "GITHUB_REVIEW_QA_V1" {
		t.Fatal("actual workspace schema or inverse predecessor identity mismatch")
	}
	return actual, projection
}

// workspaceComponentSchemaVersion selects only a registered actual schema in an already isolated administrator fixture.
// Unknown successors never inherit acceptance from a historical proof or caller-provided version.
// workspaceComponentSchemaVersion выбирает только зарегистрированную actual schema в уже изолированной admin fixture.
// Неизвестный successor не наследует допуск historical proof либо caller-provided version.
func workspaceComponentSchemaVersion(t *testing.T, ctx context.Context, conn *pgx.Conn) int64 {
	t.Helper()
	configuration := conn.Config()
	if (configuration.Host != "127.0.0.1" && configuration.Host != "localhost") || configuration.Port < 1024 ||
		configuration.User != "postgres" || !strings.HasPrefix(configuration.Database, "kodex_assistant_test_") {
		t.Fatal("schema version requires an isolated administrator fixture")
	}
	var version int64
	if err := conn.QueryRow(ctx, "SELECT max(version_id) FROM public.goose_db_version WHERE is_applied").Scan(&version); err != nil ||
		version != 20261027000100 && version != 20261028000100 && version != 20261029000100 && version != 20261029000200 && version != 20261029000300 {
		t.Fatal("unregistered disposable workspace schema", err)
	}
	return version
}

// assertWorkspaceVolumeSchemaCanonical checks the complete actual envelope without substituting inverse graph counts or versions.
// Counterfactual callers inspect their specific false predicate before invoking this canonical-only assertion.
// assertWorkspaceVolumeSchemaCanonical проверяет полный actual envelope без подмены inverse graph counts и versions.
// Counterfactual callers проверяют свой false predicate до этой проверки исключительно canonical schema.
func assertWorkspaceVolumeSchemaCanonical(t *testing.T, actual map[string]any) {
	t.Helper()
	if (actual["profile"] == "WORKSPACE_RESTORE_V2" || actual["profile"] == "GITHUB_AUTONOMOUS_PDLC_V1" || actual["profile"] == "GITHUB_PR_COLLABORATION_V1" || actual["profile"] == "GITHUB_PDLC_V1") && actual["restoreCatalog"] != true {
		t.Error("actual workspace restore catalog is unconfirmed")
	}
	if actual["profile"] == "GITHUB_AUTONOMOUS_PDLC_V1" && actual["githubHistoricalCatalog"] != true {
		t.Error("actual GitHub admission catalog is unconfirmed")
	}
	if (actual["profile"] == "GITHUB_PR_COLLABORATION_V1" || actual["profile"] == "GITHUB_PDLC_V1") && (actual["githubPRCatalog"] != true || actual["githubAutonomousWrites"] != true) {
		t.Error("actual GitHub PR catalog is unconfirmed")
	}
	if actual["profile"] == "GITHUB_PDLC_V1" && (actual["workflowPDLCCatalog"] != true || actual["workflowPDLCFunctions"] != float64(14)) {
		t.Error("actual PDLC owner catalog is unconfirmed")
	}
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
