package platform

import (
	"context"
	_ "embed"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// queryIntegrationGitHubBackupFixture recreates only the approved backup-reader role and closed
// workspace/OIDC/personal/counter SELECT grants inside the separately verified disposable fixture.
// queryIntegrationGitHubBackupFixture воспроизводит только согласованную роль backup reader и
// закрытые workspace/OIDC/personal/counter SELECT grants внутри проверенной disposable fixture.
//
//go:embed testdata/sql/integration_github_backup_fixture.sql
var queryIntegrationGitHubBackupFixture string

// queryIntegrationGitHubOldWriteConstraint restores only the old CHECK in an isolated counterfactual schema, proving the new profile cannot accept a catalog-only upgrade.
// queryIntegrationGitHubOldWriteConstraint возвращает только старый CHECK в изолированной counterfactual schema, доказывая отказ нового профиля от обновления одного каталога.
//
//go:embed testdata/sql/integration_github_old_write_constraint_fixture.sql
var queryIntegrationGitHubOldWriteConstraint string

// applyCanonicalBackupFixtureACL provisions the production grant set on the verified disposable clone.
// applyCanonicalBackupFixtureACL применяет production-набор прав к проверенной одноразовой копии БД.
func applyCanonicalBackupFixtureACL(t *testing.T, ctx context.Context, admin *pgx.Conn) {
	t.Helper()
	source, err := os.ReadFile("../../../../../../../deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh")
	if err != nil {
		t.Fatal("read canonical backup ACL source", err)
	}
	const begin = "-- BEGIN CANONICAL CONTROL PLANE BACKUP ACL\n"
	const end = "-- END CANONICAL CONTROL PLANE BACKUP ACL"
	content := string(source)
	if strings.Count(content, begin) != 1 || strings.Count(content, end) != 1 {
		t.Fatal("canonical backup ACL markers changed")
	}
	_, block, _ := strings.Cut(content, begin)
	block, _, found := strings.Cut(block, end)
	const connect = "GRANT CONNECT ON DATABASE control_plane TO kodex_backup_reader;"
	if !found || strings.Count(block, connect) != 1 {
		t.Fatal("canonical backup ACL database grant changed")
	}
	block = strings.Replace(block, connect, "GRANT CONNECT ON DATABASE "+
		pgx.Identifier{admin.Config().Database}.Sanitize()+" TO kodex_backup_reader;", 1)
	if _, err := admin.Exec(ctx, block); err != nil {
		t.Fatal("apply canonical backup ACL to disposable clone", err)
	}
}

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
	version := workspaceComponentSchemaVersion(t, ctx, admin)
	if _, err := admin.Exec(ctx, queryIntegrationGitHubBackupFixture); err != nil {
		t.Fatal("prepare isolated backup SELECT fixture")
	}
	filename, profile, catalogField := "github-autonomous-backup-migration-postcondition.sql", "GITHUB_AUTONOMOUS_PDLC_BACKUP_V1", "githubHistoricalCatalog"
	if version == 20261029000200 {
		filename, profile, catalogField = "github-pr-backup-migration-postcondition.sql", "GITHUB_PR_COLLABORATION_BACKUP_V1", "githubPRCatalog"
	}
	if version == 20261029000300 {
		filename, profile, catalogField = "workflow-pdlc-backup-migration-postcondition.sql", "GITHUB_PDLC_BACKUP_V1", "githubPRCatalog"
	}
	if version == 20261030000100 {
		filename, profile, catalogField = "workflow-content-page-backup-migration-postcondition.sql", "WORKFLOW_CONTENT_PAGE_BACKUP_V1", "githubPRCatalog"
	}
	switch version {
	case 20261105000100:
		filename, profile, catalogField = "platform-oidc-identity-backup-migration-postcondition.sql", "PLATFORM_OIDC_IDENTITY_BACKUP_V1", "githubPRCatalog"
	case 20261106000100:
		filename, profile, catalogField = "personal-credentials-backup-migration-postcondition.sql", "PERSONAL_INTEGRATION_CREDENTIALS_BACKUP_V1", "githubPRCatalog"
	case 20261107000100:
		filename, profile, catalogField = "jira-create-fields-backup-migration-postcondition.sql", "JIRA_CREATE_FIELDS_BACKUP_V1", "githubPRCatalog"
	case 20261109000100, 20261110000100, 20261110000200, 20261111000100, 20261111000200, 20261112000100, 20261113000100:
		filename, profile, catalogField = "integration-resource-policies-backup-migration-postcondition.sql", "INTEGRATION_RESOURCE_POLICIES_BACKUP_V2", "integrationPolicyFunctions"
	case 20261108000100:
		filename, profile, catalogField = "authority-proof-counter-backup-migration-postcondition.sql", "AUTHORITY_PROOF_COUNTER_BACKUP_V1", "githubPRCatalog"
	case workflowFileProvenanceFixtureVersion:
		applyCanonicalBackupFixtureACL(t, ctx, admin)
		filename, profile, catalogField = "workflow-file-provenance-backup-migration.sql", "WORKFLOW_FILE_PROVENANCE_BACKUP_051", "schemaMatches"
	}
	source, err := os.ReadFile("../../../../../../../tools/release/" + filename)
	if err != nil {
		t.Fatal("read shipped GitHub backup schema proof")
	}
	backupField := "backupReaderCatalog"
	releaseCatalog := version == workflowFileProvenanceFixtureVersion
	currentResourcePolicies := version >= 20261109000100 && !releaseCatalog
	if releaseCatalog {
		backupField = "schemaMatches"
	} else if currentResourcePolicies {
		backupField = "backupReaderReady"
	}
	proof, err := readComponentSchemaProof(ctx, admin, string(source))
	if err != nil || proof["profile"] != profile || proof["gooseVersion"] != float64(version) || proof[catalogField] != true || proof[backupField] != true {
		t.Fatal("native GitHub successor did not preserve the full backup schema", err)
	}
	_, artifactPredecessorObserved := proof["artifactPredecessorApplied"].(bool)
	if releaseCatalog && (proof["database"] != runtime.Database || proof["readOnly"] != "on" ||
		proof["serverVersionNum"] != float64(180003) || proof["backupQualification"] != "CANONICAL_BACKUP" ||
		proof["historyMatches"] != true || proof["publicSequenceSafe"] != true || !artifactPredecessorObserved) {
		t.Fatal("exact 051 backup catalog proof identity mismatch")
	}
	if currentResourcePolicies {
		for _, field := range []string{"migrationTablesReady", "integrationPolicyColumns", "integrationPolicyConstraints", "integrationPolicyFunctions", "integrationPolicyGuards", "integrationPolicyPrivileges", "workspaceGuards", "backupReaderConfigured", "backupReaderSafe"} {
			if proof[field] != true {
				t.Fatalf("current backup schema predicate rejected: %s", field)
			}
		}
	}
	if !releaseCatalog && !currentResourcePolicies && version >= 20261029000200 && proof["githubAutonomousWrites"] != true {
		t.Fatal("native GitHub write guards are unconfirmed")
	}
	if !releaseCatalog && !currentResourcePolicies && version >= 20261029000300 && (proof["workflowPDLCCatalog"] != true || proof["workflowPDLCFunctions"] != float64(14)) {
		t.Fatal("native PDLC owner catalog is unconfirmed")
	}
	if !releaseCatalog && !currentResourcePolicies && version >= 20261030000100 && proof["workflowContentPageCatalog"] != true {
		t.Fatal("native content page catalog is unconfirmed")
	}
	// Каждая мутация живёт в собственной rollback transaction, чтобы старый CHECK не
	// маскировал следующие ACL/trigger counterfactuals.
	probes := []struct{ statement, rejectedField string }{
		{"GRANT EXECUTE ON FUNCTION control_plane.integration_github_legacy_capabilities(text,text,text,text) TO PUBLIC", catalogField},
		{"REVOKE SELECT ON control_plane.session_workspace_storage FROM kodex_backup_reader", backupField},
	}
	if releaseCatalog {
		probes = append(probes, struct{ statement, rejectedField string }{"ALTER TABLE control_plane.integration_invocations DISABLE TRIGGER email_mailbox_gate_valid", catalogField}, struct{ statement, rejectedField string }{queryIntegrationGitHubOldWriteConstraint, catalogField})
	} else if currentResourcePolicies {
		probes = append(probes, struct{ statement, rejectedField string }{"ALTER TABLE control_plane.integration_invocations DISABLE TRIGGER email_mailbox_gate_valid", "integrationPolicyGuards"}, struct{ statement, rejectedField string }{queryIntegrationGitHubOldWriteConstraint, "integrationPolicyConstraints"})
	} else if version >= 20261029000200 {
		probes = append(probes,
			struct{ statement, rejectedField string }{"ALTER TABLE control_plane.integration_invocations DISABLE TRIGGER email_mailbox_gate_valid", "githubAutonomousWrites"},
			struct{ statement, rejectedField string }{queryIntegrationGitHubOldWriteConstraint, "githubAutonomousWrites"})
	}
	statement := strings.ReplaceAll(string(source), "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;", "")
	statement = strings.TrimSuffix(strings.TrimSpace(statement), "ROLLBACK;")
	verifyRestoredCatalog := func() {
		t.Helper()
		restored, err := readComponentSchemaProof(ctx, admin, string(source))
		if err != nil || restored["profile"] != profile || restored["gooseVersion"] != float64(version) ||
			restored[catalogField] != true || restored[backupField] != true {
			t.Fatal("backup counterfactual changed the canonical schema", err)
		}
	}
	for _, probe := range probes {
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal("begin isolated backup schema counterfactual")
		}
		if _, err := tx.Exec(ctx, probe.statement); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("prepare isolated backup schema counterfactual")
		}
		proof, proofErr := readComponentSchemaProof(ctx, admin, statement)
		rollbackErr := tx.Rollback(ctx)
		var rejected *pgconn.PgError
		if rollbackErr != nil || admin.PgConn().TxStatus() != 'I' || proofErr == nil && proof[probe.rejectedField] != false ||
			proofErr != nil && (releaseCatalog || !errors.As(proofErr, &rejected) || rejected.Code != "P0001") {
			t.Fatalf("backup schema accepted drift or did not roll back: predicate=%s SQLSTATE=%v rollback=%v", probe.rejectedField, proofErr, rollbackErr)
		}
		if !releaseCatalog {
			verifyRestoredCatalog()
		}
	}
	// On 051 each rollback has an idle-session readback. Recheck the full fingerprint once after all probes.
	// На 051 каждый откат подтверждается состоянием idle. Полный отпечаток перепроверяется после всех сценариев.
	if releaseCatalog {
		verifyRestoredCatalog()
	}
}
