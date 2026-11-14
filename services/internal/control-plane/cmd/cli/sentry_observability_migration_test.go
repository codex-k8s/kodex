package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestSentryObservabilityUpgrade applies the exact predecessor and forward-only Sentry observability migration in an isolated database, surfacing native schema failures without contacting Sentry observability.
// TestSentryObservabilityUpgrade применяет точный predecessor и forward-only Sentry observability migration в отдельной БД, показывая native schema failures без обращения к Sentry observability.
func TestSentryObservabilityUpgrade(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err != nil {
		t.Fatal("prepare Sentry observability predecessor", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "sentry-observability", "precondition")
	if _, err := observer.Exec(ctx, "GRANT SELECT ON control_plane.integration_connections TO PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114130000); err == nil || !strings.Contains(err.Error(), "Sentry observability exact predecessor catalog changed") {
		t.Fatal("drifted predecessor was accepted", err)
	}
	if _, err := observer.Exec(ctx, "REVOKE SELECT ON control_plane.integration_connections FROM PUBLIC"); err != nil {
		t.Fatal(err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114130000); err != nil {
		t.Fatal("apply Sentry observability", err)
	}
	if err := goose.UpToContext(ctx, database, "migrations", 20261114130000); err != nil {
		t.Fatal("repeat Sentry observability", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "sentry-observability", "postcondition")
	proof, err := os.ReadFile("../../../../../tools/release/sentry-observability-migration-postcondition.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		"ALTER TABLE control_plane.integration_personal_credentials DISABLE TRIGGER USER",
		"GRANT UPDATE ON control_plane.repository_bundle_publications TO kodex_backup_reader",
		"ALTER TABLE control_plane.integration_definitions ALTER COLUMN adapter SET DEFAULT 'SENTRY'",
	} {
		assertControlPlaneCatalogMutationRejected(t, ctx, observer, proof, mutation, "Sentry observability schema catalog changed")
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
			t.Fatal("restore exact045 backup ACL", err)
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, "sentry-observability-backup", "postcondition")
	if err := goose.DownContext(ctx, database, "migrations"); err == nil {
		t.Fatal("forward-only Sentry observability migration accepted Down")
	}
	var shape bool
	if err := observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='control_plane.integration_definitions'::regclass AND position('SENTRY' IN pg_get_constraintdef(oid))>0 AND position('OBSERVABILITY' IN pg_get_constraintdef(oid))>0) AND EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='control_plane.integration_personal_invocation_credentials'::regclass AND position('LOKI_RULER_MUTATION_V1' IN pg_get_constraintdef(oid))>0)`).Scan(&shape); err != nil || !shape {
		t.Fatal("Sentry observability schema is incomplete", err)
	}
}

// TestSentryObservabilityCatalogCapture exports only schema fingerprints from disposable normal/backup fixtures when an explicit private capture directory is supplied.
// TestSentryObservabilityCatalogCapture сохраняет только schema fingerprints отдельных normal/backup fixtures при явном приватном каталоге capture.
func TestSentryObservabilityCatalogCapture(t *testing.T) {
	directory := os.Getenv("KODEX_SENTRY_OBSERVABILITY_SCHEMA_CAPTURE_DIRECTORY")
	if directory == "" {
		t.Skip("schema capture directory is not requested")
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("schema capture directory must be a private regular directory")
	}
	query, err := os.ReadFile("../../../../../tools/release/sentry-observability-catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	restoreQuery, err := os.ReadFile("../../../../../tools/release/sentry-observability-restore-catalog.sql")
	if err != nil {
		t.Fatal(err)
	}
	name := os.Getenv("KODEX_SENTRY_OBSERVABILITY_SCHEMA_CAPTURE_MODE")
	if name != "normal" && name != "backup" && name != "table-only-backup" {
		t.Fatal("schema capture mode is invalid")
	}
	for _, name := range []string{name} {
		t.Run(name, func(t *testing.T) {
			ctx, database, observer := newWorkflowUpgradeDatabase(t)
			if err := goose.UpToContext(ctx, database, "migrations", 20261114120000); err != nil {
				t.Fatal(err)
			}
			if name == "backup" {
				if _, err := observer.Exec(ctx, canonicalGitHubBackupInitializer(t)); err != nil {
					t.Fatal(err)
				}
			}
			if name == "table-only-backup" {
				if _, err := observer.Exec(ctx, "SET ROLE control_plane_owner; GRANT SELECT ON ALL TABLES IN SCHEMA control_plane,public TO kodex_backup_reader; ALTER DEFAULT PRIVILEGES FOR ROLE control_plane_owner IN SCHEMA control_plane GRANT SELECT ON TABLES TO kodex_backup_reader; RESET ROLE"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := observer.Exec(ctx, "SET search_path=pg_catalog"); err != nil {
				t.Fatal(err)
			}
			for _, phase := range []string{"before", "after"} {
				if phase == "after" {
					if err := goose.UpToContext(ctx, database, "migrations", 20261114130000); err != nil {
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
