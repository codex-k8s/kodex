package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// queryIntegrationGitHubBackupFixture recreates only the approved backup-reader role and six SELECT grants inside the separately verified disposable fixture.
// queryIntegrationGitHubBackupFixture воспроизводит только согласованную роль backup reader и шесть SELECT grants внутри отдельно проверенной disposable fixture.
//
//go:embed testdata/sql/integration_github_backup_fixture.sql
var queryIntegrationGitHubBackupFixture string

// TestIntegrationGitHubBackupSchemaProofComponent proves the native successor with intact backup SELECT, then rejects a public function grant and a missing table grant without weakening the historical validators.
// TestIntegrationGitHubBackupSchemaProofComponent подтверждает native successor с сохранёнными backup SELECT, затем отклоняет публичное разрешение функции и отсутствие табличного grant, не ослабляя исторические проверки.
func TestIntegrationGitHubBackupSchemaProofComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	runtime, err := pgx.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse isolated GitHub backup fixture")
	}
	admin := evidenceRetentionAdministrator(t, ctx, runtime)
	if _, err := admin.Exec(ctx, queryIntegrationGitHubBackupFixture); err != nil {
		t.Fatal("prepare isolated backup SELECT fixture")
	}
	source, err := os.ReadFile("../../../../../../../tools/release/github-autonomous-backup-migration-postcondition.sql")
	if err != nil {
		t.Fatal("read shipped GitHub backup schema proof")
	}
	read := func() (map[string]any, error) {
		results, err := admin.PgConn().Exec(ctx, string(source)).ReadAll()
		if err != nil {
			return nil, err
		}
		var proof map[string]any
		for _, result := range results {
			if result.Err != nil {
				return nil, result.Err
			}
			if len(result.Rows) == 0 {
				continue
			}
			if proof != nil || len(result.Rows) != 1 || len(result.Rows[0]) != 1 || json.Unmarshal(result.Rows[0][0], &proof) != nil {
				t.Fatal("GitHub backup schema proof shape is invalid")
			}
		}
		return proof, nil
	}
	proof, err := read()
	if err != nil || proof["profile"] != "GITHUB_AUTONOMOUS_PDLC_BACKUP_V1" || proof["gooseVersion"] != float64(20261029000100) || proof["githubHistoricalCatalog"] != true || proof["backupReaderCatalog"] != true {
		t.Fatal("native GitHub successor did not preserve the full backup schema", err)
	}
	if _, err := admin.Exec(ctx, "GRANT EXECUTE ON FUNCTION control_plane.integration_github_legacy_capabilities(text,text,text,text) TO PUBLIC"); err != nil {
		t.Fatal("prepare isolated public function grant")
	}
	proof, err = read()
	if err != nil || proof["githubHistoricalCatalog"] != false {
		t.Fatal("public historical admission function was accepted")
	}
	if _, err := admin.Exec(ctx, "REVOKE EXECUTE ON FUNCTION control_plane.integration_github_legacy_capabilities(text,text,text,text) FROM PUBLIC; REVOKE SELECT ON control_plane.session_workspace_storage FROM kodex_backup_reader"); err != nil {
		t.Fatal("prepare isolated missing backup SELECT")
	}
	if _, err := read(); err == nil {
		t.Fatal("incomplete backup SELECT was accepted")
	}
	// Read-only proof с отказом может оставить transaction aborted; закрываем её до cleanup.
	if _, err := admin.Exec(ctx, "ROLLBACK"); err != nil {
		t.Fatal("close isolated rejected schema proof")
	}
}
