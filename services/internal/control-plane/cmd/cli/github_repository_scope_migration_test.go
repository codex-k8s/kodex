package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

// TestGitHubRepositoryScopeUpgrade applies the exact predecessor and forward-only GitHub scope migration in an isolated database, surfacing native schema failures without contacting GitHub.
// TestGitHubRepositoryScopeUpgrade применяет точный predecessor и forward-only GitHub scope migration в отдельной БД, показывая native schema failures без обращения к GitHub.
func TestGitHubRepositoryScopeUpgrade(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	ctx, cancelTest := context.WithTimeout(t.Context(), 600*time.Second)
	defer cancelTest()
	if err := goose.UpToContext(ctx, database, "migrations", 20261113000100); err != nil {
		t.Fatal("prepare GitHub scope predecessor", err)
	}
	profile := "github-repository-scope"
	if os.Getenv("KODEX_GITHUB_UPGRADE_MODE") == "backup" {
		profile += "-backup"
		if _, err := observer.Exec(ctx, canonicalGitHubBackupInitializer(t)); err != nil {
			t.Fatal(err)
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
	if strings.HasSuffix(profile, "-backup") {
		precondition, err := os.ReadFile("../../../../../tools/release/" + profile + "-migration-precondition.sql")
		if err != nil {
			t.Fatal(err)
		}
		for _, mutation := range []string{
			"REVOKE USAGE ON SCHEMA control_plane FROM kodex_backup_reader",
			"GRANT UPDATE ON SEQUENCE control_plane.runtime_secret_drafts_generation_seq TO kodex_backup_reader",
			"ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT UPDATE ON SEQUENCES TO kodex_backup_reader",
		} {
			assertControlPlaneCatalogMutationRejected(t, ctx, observer, precondition, mutation, "GitHub repository scope schema catalog changed")
		}
	}
	if _, err := observer.Exec(ctx, "GRANT SELECT ON control_plane.integration_connections TO PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(profile, "-backup") {
		if err := executeWorkspaceBackupStatement(ctx, database, "normalize_20261113000100.sql"); err == nil || !strings.Contains(err.Error(), "GitHub backup normalize catalog changed") {
			t.Fatal("normalization accepted drifted backup authority", err)
		}
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err == nil || !strings.Contains(err.Error(), "GitHub scope exact predecessor catalog changed") {
		t.Fatal("drifted predecessor was accepted", err)
	}
	if _, err := observer.Exec(ctx, "REVOKE SELECT ON control_plane.integration_connections FROM PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(profile, "-backup") {
		backupConfig := observer.Config().Copy()
		backupConfig.User = "kodex_backup_reader"
		reader, err := pgx.ConnectConfig(ctx, backupConfig)
		if err != nil {
			t.Fatal("connect disposable backup consumer", err)
		}
		err = executeWorkspaceBackupStatement(ctx, database, "normalize_20261113000100.sql")
		if closeErr := reader.Close(ctx); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err == nil || !strings.Contains(err.Error(), "GitHub backup normalize owner is not idle") {
			t.Fatal("active backup consumer did not close normalization", err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
		// A normalization acknowledgement can be lost after the database commits; explicit read/rejoin restores043 without starting Goose.
		// Подтверждение normalization может потеряться после commit БД; явный read/rejoin восстанавливает043 без запуска Goose.
		if err := executeWorkspaceBackupStatement(ctx, database, "normalize_20261113000100.sql"); err != nil {
			t.Fatal(err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("recover normalized043 without replay", err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
		failure := errors.New("synthetic caller cancellation")
		for _, scenario := range []string{"failed-goose", "cancelled-caller"} {
			callCtx, cancel := context.WithCancel(ctx)
			err := executeBackupUpgradeWithStatement(callCtx, database, "normalize_20261113000100.sql", func(callbackCtx context.Context, db *sql.DB) error {
				if scenario == "cancelled-caller" {
					cancel()
					return failure
				}
				return goose.UpToContext(callbackCtx, db, "missing-fixture-migrations", 20261114120000)
			})
			cancel()
			if err == nil {
				t.Fatal("backup maintenance did not report the failed attempt")
			}
			readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
		}
		// Goose commits successfully but its caller loses the acknowledgement; cleanup still restores exact044 and a later command rejoins without replay.
		// Goose успешно фиксируется, но caller теряет подтверждение; cleanup возвращает exact044, а следующая команда присоединяется без replay.
		lostACK := errors.New("synthetic migration acknowledgement lost")
		err = executeBackupUpgradeWithStatement(ctx, database, "normalize_20261113000100.sql", func(callbackCtx context.Context, db *sql.DB) error {
			if err := goose.UpToContext(callbackCtx, db, "migrations", 20261114120000); err != nil {
				return err
			}
			return lostACK
		})
		if !errors.Is(err, lostACK) {
			t.Fatal("migration lost acknowledgement was not preserved", err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, profile, "postcondition")
		if err := applyPostgresMigrations(ctx, database, githubBackupUpgradeCommand); err != nil {
			t.Fatal("rejoin applied044 without replay", err)
		}
	} else {
		if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err != nil {
			t.Fatal("apply GitHub repository scope", err)
		}
		if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err != nil {
			t.Fatal("repeat GitHub repository scope", err)
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, profile, "postcondition")
	proof, err := os.ReadFile("../../../../../tools/release/" + profile + "-migration-postcondition.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		"ALTER TABLE control_plane.integration_personal_credentials DISABLE TRIGGER USER",
		"GRANT UPDATE ON control_plane.repository_bundle_publications TO kodex_backup_reader",
		"ALTER TABLE control_plane.integration_personal_credentials ALTER COLUMN github_discovery SET NOT NULL",
	} {
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, proof, mutation, "GitHub repository scope schema catalog changed")
	}
	if _, err := observer.Exec(ctx, "CREATE VIEW public.synthetic_backup_view AS SELECT 1 AS safe_fixture"); err != nil {
		t.Fatal(err)
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err == nil {
		t.Fatal("backup restore expanded read authority to an unknown public view")
	}
	if _, err := observer.Exec(ctx, "DROP VIEW public.synthetic_backup_view"); err != nil {
		t.Fatal(err)
	}
	for repeat := 0; repeat < 2; repeat++ {
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("restore exact044 backup ACL", err)
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, "github-repository-scope-backup", "postcondition")
	for _, mutation := range []struct{ apply, undo string }{
		{"GRANT UPDATE ON SEQUENCE control_plane.runtime_secret_drafts_generation_seq TO kodex_backup_reader", "REVOKE UPDATE ON SEQUENCE control_plane.runtime_secret_drafts_generation_seq FROM kodex_backup_reader"},
		{"GRANT SELECT ON SEQUENCE control_plane.oidc_identity_session_revisions TO kodex_backup_reader WITH GRANT OPTION", "REVOKE GRANT OPTION FOR SELECT ON SEQUENCE control_plane.oidc_identity_session_revisions FROM kodex_backup_reader"},
		{"REVOKE USAGE ON SCHEMA control_plane FROM kodex_backup_reader", "GRANT USAGE ON SCHEMA control_plane TO kodex_backup_reader"},
		{"ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT UPDATE ON SEQUENCES TO kodex_backup_reader", "ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane REVOKE UPDATE ON SEQUENCES FROM kodex_backup_reader"},
	} {
		if _, err := observer.Exec(ctx, mutation.apply); err != nil {
			t.Fatal(err)
		}
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err == nil {
			t.Fatal("restore accepted changed backup authority")
		}
		if _, err := observer.Exec(ctx, mutation.undo); err != nil {
			t.Fatal(err)
		}
		readControlPlaneMigrationProof(t, ctx, observer, "github-repository-scope-backup", "postcondition")
	}

	if err := goose.DownContext(ctx, database, "migrations"); err == nil {
		t.Fatal("forward-only GitHub migration accepted Down")
	}
	var shape bool
	if err := observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='control_plane' AND table_name='repository_bundle_publications' AND column_name='personal_credential_revision_id') AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='repository_bundle_personal_credential_guard' AND tgenabled='O')`).Scan(&shape); err != nil || !shape {
		t.Fatal("GitHub scope schema is incomplete", err)
	}
}

// TestGitHubRepositoryScopeCatalogCapture exports only schema fingerprints from disposable normal/backup fixtures when an explicit private capture directory is supplied.
// TestGitHubRepositoryScopeCatalogCapture сохраняет только schema fingerprints отдельных normal/backup fixtures при явном приватном каталоге capture.
func TestGitHubRepositoryScopeCatalogCapture(t *testing.T) {
	directory := os.Getenv("KODEX_GITHUB_SCHEMA_CAPTURE_DIRECTORY")
	if directory == "" {
		t.Skip("schema capture directory is not requested")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("schema capture directory must be a private regular directory")
	}
	query, err := os.ReadFile("../../../../../tools/release/github-repository-scope-catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	restoreQuery, err := os.ReadFile("../../../../../tools/release/github-repository-scope-restore-catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{os.Getenv("KODEX_GITHUB_SCHEMA_CAPTURE_MODE")} {
		if name != "normal" && name != "backup" && name != "table-only-backup" {
			t.Fatal("schema capture mode is invalid")
		}
		t.Run(name, func(t *testing.T) {
			ctx, database, observer := newWorkflowUpgradeDatabase(t)
			if err := goose.UpToContext(ctx, database, "migrations", 20261113000100); err != nil {
				t.Fatal(err)
			}
			if name == "backup" {
				if _, err := observer.Exec(ctx, canonicalGitHubBackupInitializer(t)); err != nil {
					t.Fatal(err)
				}
			}
			if name == "table-only-backup" {
				if _, err := observer.Exec(ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA public,control_plane TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := observer.Exec(ctx, "SET search_path=pg_catalog"); err != nil {
				t.Fatal(err)
			}
			phases := []string{"before", "after"}
			for _, phase := range phases {
				if phase == "after" {
					var upgradeErr error
					if name == "backup" {
						upgradeErr = applyPostgresMigrations(ctx, database, githubBackupUpgradeCommand)
					} else {
						upgradeErr = goose.UpToContext(ctx, database, "migrations", 20261114120000)
					}
					if upgradeErr != nil {
						t.Fatal(upgradeErr)
					}
				}
				var raw []byte
				if err := observer.QueryRow(ctx, string(query)).Scan(&raw); err != nil {
					t.Fatal("read full schema catalog", err)
				}
				var normalized []byte
				if err := observer.QueryRow(ctx, string(restoreQuery)).Scan(&normalized); err != nil {
					t.Fatal("read normalized restore catalog", err)
				}
				var value, mapNormalized map[string]any
				if json.Unmarshal(raw, &value) != nil || json.Unmarshal(normalized, &mapNormalized) != nil {
					t.Fatal("schema capture is invalid")
				}
				value["restoreCatalogDigest"] = mapNormalized["catalogDigest"]
				initializerDigest := sha256.Sum256([]byte(canonicalGitHubBackupInitializer(t)))
				value["backupInitializerSHA256"] = hex.EncodeToString(initializerDigest[:])
				raw, err = json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, name+"-"+phase+".json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

// canonicalGitHubBackupInitializer reads the exact approved initializer SQL without loading credentials or executing shell code; unique markers keep fixture and restore authority tied to the production source.
// canonicalGitHubBackupInitializer читает точный SQL утверждённого initializer без credentials и исполнения shell; уникальные маркеры связывают fixture и restore authority с production source.
func canonicalGitHubBackupInitializer(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../../../../../deploy/k8s/base/platform-state/postgresql/reconcile-runtime-credentials.sh")
	if err != nil {
		t.Fatal(err)
	}
	const begin = "-- BEGIN CANONICAL CONTROL PLANE BACKUP ACL\n"
	const end = "-- END CANONICAL CONTROL PLANE BACKUP ACL"
	text := string(raw)
	if strings.Count(text, begin) != 1 || strings.Count(text, end) != 1 {
		t.Fatal("canonical backup initializer markers changed")
	}
	_, block, _ := strings.Cut(text, begin)
	block, _, found := strings.Cut(block, end)
	if !found || strings.TrimSpace(block) == "" {
		t.Fatal("canonical backup initializer is empty")
	}
	return block
}

// TestGitHubRepositoryScopePublicSequenceRestore proves that canonical backup restoration cannot grant authority to a foreign public sequence and preserves the registered Goose identity sequence.
// TestGitHubRepositoryScopePublicSequenceRestore доказывает, что canonical backup restore не выдаёт права посторонней public sequence и сохраняет зарегистрированную identity sequence Goose.
func TestGitHubRepositoryScopePublicSequenceRestore(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err != nil {
		t.Fatal(err)
	}
	if _, err := observer.Exec(ctx, "SET ROLE control_plane_owner;CREATE SEQUENCE public.synthetic_foreign_backup_sequence;RESET ROLE"); err != nil {
		t.Fatal(err)
	}
	restoreErr := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand)
	var granted bool
	if err := observer.QueryRow(ctx, "SELECT has_sequence_privilege('kodex_backup_reader','public.synthetic_foreign_backup_sequence','SELECT') OR has_sequence_privilege('kodex_backup_reader','public.synthetic_foreign_backup_sequence','USAGE')").Scan(&granted); err != nil {
		t.Fatal(err)
	}
	if restoreErr == nil || granted {
		t.Fatalf("foreign public sequence passed restore; granted=%t", granted)
	}
	if _, err := observer.Exec(ctx, "DROP SEQUENCE public.synthetic_foreign_backup_sequence"); err != nil {
		t.Fatal(err)
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
		t.Fatal("restore registered public identity sequence", err)
	}
	var canonical bool
	if err := observer.QueryRow(ctx, "SELECT has_sequence_privilege('kodex_backup_reader','public.goose_db_version_id_seq','SELECT') AND has_sequence_privilege('kodex_backup_reader','public.goose_db_version_id_seq','USAGE') AND NOT has_sequence_privilege('kodex_backup_reader','public.goose_db_version_id_seq','UPDATE')").Scan(&canonical); err != nil || !canonical {
		t.Fatal("canonical Goose sequence authority changed", err)
	}
}

// TestGitHubBackupRetainedWorkspace preserves inactive durable workspace records through canonical043 restoration and immutable044 maintenance, without interpreting historical BLOCKED metadata as writer quiescence.
// TestGitHubBackupRetainedWorkspace сохраняет неактивные durable workspace records при canonical043 restore и неизменной044 maintenance, не трактуя исторический BLOCKED как writer quiescence.
func TestGitHubBackupRetainedWorkspace(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261113000100); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"testdata/opencode_native_cutover_seed.sql", "testdata/github_backup_retained_workspace.sql"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = observer.Exec(ctx, "SET ROLE control_plane_owner;"+string(raw)+"RESET ROLE;"); err != nil {
			t.Fatal("seed retained workspace fixture", err)
		}
	}
	if _, err := observer.Exec(ctx, canonicalGitHubBackupInitializer(t)); err != nil {
		t.Fatal(err)
	}
	const rowsDigest = "SELECT encode(sha256(convert_to(jsonb_agg(to_jsonb(workspace) ORDER BY id)::text,'UTF8')),'hex') FROM control_plane.session_workspace_storage workspace"
	var before, after string
	if err := observer.QueryRow(ctx, rowsDigest).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
		t.Fatal("canonical043 restore rejected retained inactive workspace", err)
	}
	if err := observer.QueryRow(ctx, rowsDigest).Scan(&after); err != nil || before != after {
		t.Fatal("restore043 changed durable workspace records", err)
	}
	proof, err := os.ReadFile("../../../../../tools/release/github-repository-scope-backup-migration-precondition.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		"SET LOCAL session_replication_role=replica;INSERT INTO control_plane.session_workspace_volume_tasks(ref,volume_id,volume_generation,origin_node_id,root_run_id,origin_actor_id,kind,state,frozen_input) VALUES('svt_active_counterfactual',gen_random_uuid(),1,gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),'CREATE_WORKSPACE_VOLUME','READY','{}')",
		"SET LOCAL session_replication_role=replica;INSERT INTO control_plane.session_workspace_volume_attempts(task_id,attempt,lease_ref,fence_digest,workload_instance,input_snapshot,input_digest,grant_state,expires_at) VALUES(gen_random_uuid(),1,'lea_active_expired_counterfactual',repeat('a',64),'synthetic-workload','{}',encode(sha256(convert_to('{}','UTF8')),'hex'),'ACTIVE',clock_timestamp()-interval '1 hour')",
	} {
		// Orphan counterfactuals exist only inside the disposable rollback transaction; no live trigger or FK is altered.
		// Orphan counterfactual существуют только в disposable rollback transaction; живые triggers и FK не меняются.
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, proof, mutation, "GitHub backup workspace owner is not idle")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "github-repository-scope-backup", "precondition")
	if err := applyPostgresMigrations(ctx, database, githubBackupUpgradeCommand); err != nil {
		t.Fatal("upgrade retained durable workspace", err)
	}
	if err := observer.QueryRow(ctx, rowsDigest).Scan(&after); err != nil || before != after {
		t.Fatal("maintenance044 changed durable workspace records", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "github-repository-scope-backup", "postcondition")
}
