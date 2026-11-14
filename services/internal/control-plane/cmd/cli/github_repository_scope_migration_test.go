package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestGitHubRepositoryScopeUpgrade applies the exact predecessor and forward-only GitHub scope migration in an isolated database, surfacing native schema failures without contacting GitHub.
// TestGitHubRepositoryScopeUpgrade применяет точный predecessor и forward-only GitHub scope migration в отдельной БД, показывая native schema failures без обращения к GitHub.
func TestGitHubRepositoryScopeUpgrade(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261113000100); err != nil {
		t.Fatal("prepare GitHub scope predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "github-repository-scope", "precondition")
	if _, err := observer.Exec(ctx, "GRANT SELECT ON control_plane.integration_connections TO PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err == nil || !strings.Contains(err.Error(), "GitHub scope exact predecessor catalog changed") {
		t.Fatal("drifted predecessor was accepted", err)
	}
	if _, err := observer.Exec(ctx, "REVOKE SELECT ON control_plane.integration_connections FROM PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err != nil {
		t.Fatal("apply GitHub repository scope", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err != nil {
		t.Fatal("repeat GitHub repository scope", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "github-repository-scope", "postcondition")
	proof, err := os.ReadFile("../../../../../tools/release/github-repository-scope-migration-postcondition.sql")
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
	if _, err := observer.Exec(ctx, "GRANT SELECT ON ALL TABLES IN SCHEMA control_plane,public TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader"); err != nil {
		t.Fatal(err)
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
	for _, backup := range []bool{os.Getenv("KODEX_GITHUB_SCHEMA_CAPTURE_MODE") == "backup"} {
		name := "normal"
		if backup {
			name = "backup"
		}
		t.Run(name, func(t *testing.T) {
			ctx, database, observer := newWorkflowUpgradeDatabase(t)
			if err := goose.UpToContext(ctx, database, "migrations", 20261113000100); err != nil {
				t.Fatal(err)
			}
			if backup {
				if _, err := observer.Exec(ctx, "SET ROLE control_plane_owner; GRANT SELECT ON ALL TABLES IN SCHEMA control_plane,public TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader; RESET ROLE"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := observer.Exec(ctx, "SET search_path=pg_catalog"); err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"before", "after"} {
				if phase == "after" {
					if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err != nil {
						t.Fatal(err)
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
