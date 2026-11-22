package main

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pressly/goose/v3"
)

// TestPersonalIntegrationsNormalUpgrade covers a mode mismatch, an unexpected history row and forward rejoin after a committed stage loses its acknowledgement.
// TestPersonalIntegrationsNormalUpgrade проверяет несовпадение режима, лишнюю запись истории и продолжение после потери подтверждения зафиксированного этапа.
func TestPersonalIntegrationsNormalUpgrade(t *testing.T) {
	_, database, observer := newWorkflowUpgradeDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	before := preparePersonalIntegrationPredecessor(t, ctx, database, observer)
	readControlPlaneMigrationProof(t, ctx, observer, "personal-integrations", "precondition")
	if err := applyPostgresMigrations(ctx, database, personalIntegrationsBackupUpgradeCommand); err == nil {
		t.Fatal("unqualified normal predecessor received canonical backup admission")
	}
	var insertedID int64
	if err := observer.QueryRow(ctx, "INSERT INTO public.goose_db_version (version_id,is_applied) VALUES (20261114135000,true) RETURNING id").Scan(&insertedID); err != nil {
		t.Fatal("seed unexpected migration history row", err)
	}
	if err := applyPostgresMigrations(ctx, database, personalIntegrationsUpgradeCommand); err == nil {
		t.Fatal("unexpected history row was not rejected")
	}
	if _, err := observer.Exec(ctx, "DELETE FROM public.goose_db_version WHERE id=$1", insertedID); err != nil {
		t.Fatal("remove synthetic migration history row", err)
	}
	lostACK := errors.New("synthetic migration acknowledgement lost")
	first := personalIntegrationUpgrade{beforeVersion: 20261114140000, afterVersion: 20261114150000}
	if err := executePersonalIntegrationUpgrade(ctx, database, first, func(ctx context.Context, database *sql.DB) error {
		if err := goose.UpToContext(ctx, database, "migrations", first.afterVersion); err != nil {
			return err
		}
		return lostACK
	}); !errors.Is(err, lostACK) {
		t.Fatal("committed stage uncertainty was hidden", err)
	}
	assertPersonalIntegrationCohortRejoin(t, ctx, database, observer, personalIntegrationsUpgradeCommand, before)
}

// TestPersonalIntegrationsBackupUpgrade verifies canonical backup authority across a cancelled caller, a committed intermediate stage and the final rejoin.
// TestPersonalIntegrationsBackupUpgrade проверяет канонические права резервного копирования после отмены вызова, зафиксированного промежуточного этапа и итогового повторного входа.
func TestPersonalIntegrationsBackupUpgrade(t *testing.T) {
	_, database, observer := newWorkflowUpgradeDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	before := preparePersonalIntegrationPredecessor(t, ctx, database, observer)
	if _, err := observer.Exec(ctx, canonicalGitHubBackupInitializer(t)); err != nil {
		t.Fatal("prepare canonical 046 backup privileges", err)
	}
	readControlPlaneMigrationProof(t, ctx, observer, "personal-integrations-backup", "precondition")
	if err := applyPostgresMigrations(ctx, database, personalIntegrationsUpgradeCommand); err == nil {
		t.Fatal("canonical backup schema received ordinary migration admission")
	}
	first := personalIntegrationUpgrade{beforeVersion: 20261114140000, afterVersion: 20261114150000, backup: true}
	interrupted, stop := context.WithCancel(ctx)
	defer stop()
	if err := executePersonalIntegrationUpgrade(interrupted, database, first, func(context.Context, *sql.DB) error {
		stop()
		return context.Canceled
	}); !errors.Is(err, context.Canceled) {
		t.Fatal("backup migration cancellation was hidden", err)
	}
	lostACK := errors.New("synthetic backup migration acknowledgement lost")
	if err := executePersonalIntegrationUpgrade(ctx, database, first, func(ctx context.Context, database *sql.DB) error {
		if err := goose.UpToContext(ctx, database, "migrations", first.afterVersion); err != nil {
			return err
		}
		return lostACK
	}); err != lostACK {
		// errors.Join wraps the original error even when bounded cleanup succeeds.
		// errors.Join оборачивает исходную ошибку даже при успешном ограниченном восстановлении.
		if joined, ok := err.(interface{ Unwrap() []error }); !ok || len(joined.Unwrap()) != 1 || joined.Unwrap()[0] != lostACK {
			t.Fatal("lost acknowledgement or independently failed backup restoration", err)
		}
	}
	assertPersonalIntegrationCohortRejoin(t, ctx, database, observer, personalIntegrationsBackupUpgradeCommand, before)
}

// preparePersonalIntegrationPredecessor starts at the exact 046 schema and adds inactive workspace rows whose content must survive the upgrade.
// preparePersonalIntegrationPredecessor начинает с точной схемы 046 и добавляет неактивные записи workspace, которые должны сохраниться при обновлении.
func preparePersonalIntegrationPredecessor(t *testing.T, ctx context.Context, database *sql.DB, observer *pgx.Conn) string {
	t.Helper()
	if err := goose.UpToContext(ctx, database, "migrations", 20261114140000); err != nil {
		t.Fatal("prepare 046 predecessor", err)
	}
	for _, name := range []string{"testdata/opencode_native_cutover_seed.sql", "testdata/github_backup_retained_workspace.sql"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := observer.Exec(ctx, string(raw)); err != nil {
			t.Fatal("seed inactive retained workspace", err)
		}
	}
	return personalIntegrationRetainedWorkspaceDigest(t, ctx, observer)
}

// personalIntegrationRetainedWorkspaceDigest reads the existing synthetic preservation projection without inspecting workspace files or credentials.
// personalIntegrationRetainedWorkspaceDigest читает прежнюю синтетическую проекцию сохранности без просмотра файлов workspace и учётных данных.
func personalIntegrationRetainedWorkspaceDigest(t *testing.T, ctx context.Context, observer *pgx.Conn) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/sentry_observability_workspace_digest.sql")
	if err != nil {
		t.Fatal(err)
	}
	var digest string
	if err := observer.QueryRow(ctx, string(raw)).Scan(&digest); err != nil {
		t.Fatal("read retained workspace fixture digest", err)
	}
	return digest
}

// assertPersonalIntegrationCohortRejoin checks the remaining stages, exact rejoin without additional ledger entries and preserved workspace rows.
// assertPersonalIntegrationCohortRejoin проверяет оставшиеся этапы, повторный вход без дополнительных записей истории и сохранность workspace.
func assertPersonalIntegrationCohortRejoin(t *testing.T, ctx context.Context, database *sql.DB, observer *pgx.Conn, command, before string) {
	t.Helper()
	if err := applyPostgresMigrations(ctx, database, command); err != nil {
		t.Fatal("continue the stopped cohort from a committed 047 head", err)
	}
	if head, err := readControlPlaneSchemaHead(ctx, database); err != nil || head != 20261114170000 {
		t.Fatal("cohort did not commit the exact 049 schema", err)
	}
	profile := "personal-integrations"
	if command == personalIntegrationsBackupUpgradeCommand {
		profile = "personal-integrations-backup"
	}
	readControlPlaneMigrationProof(t, ctx, observer, profile, "postcondition")
	const historySQL = "SELECT jsonb_agg(jsonb_build_array(id,version_id,is_applied) ORDER BY id)::text FROM public.goose_db_version"
	var historyBefore, historyAfter string
	if err := observer.QueryRow(ctx, historySQL).Scan(&historyBefore); err != nil {
		t.Fatal("read committed migration history", err)
	}
	if err := applyPostgresMigrations(ctx, database, command); err != nil {
		t.Fatal("verify already committed 049 without replay", err)
	}
	if err := observer.QueryRow(ctx, historySQL).Scan(&historyAfter); err != nil || historyBefore != historyAfter {
		t.Fatal("049 rejoin changed committed migration history", err)
	}
	if digest := personalIntegrationRetainedWorkspaceDigest(t, ctx, observer); digest != before {
		t.Fatal("integration upgrade changed inactive retained workspace")
	}
}
