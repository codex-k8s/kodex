package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3"
)

// testAuthorityProofCounterMigration verifies exact035→036 watermark handover, fail-closed legacy
// allocation, concurrent monotonic revisions, exhausted bounds and idempotent Up. A separate empty
// installation retains its baseline zero watermark; no live database is accepted.
// testAuthorityProofCounterMigration проверяет exact035→036 watermark handover, закрытый отказ
// legacy allocator, concurrent monotonic revisions, exhaustion и idempotent Up. Отдельная новая
// установка сохраняет исходный нулевой watermark; live database не принимается.
func testAuthorityProofCounterMigration(t *testing.T, existing bool) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if goose.UpToContext(ctx, database, "migrations", 20261107000100) != nil {
		t.Fatal("prepare exact proof-counter predecessor")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "jira-create-fields", "postcondition")
	if existing {
		if _, err := observer.Exec(ctx, "UPDATE control_plane.installation SET authority_proof_sequence=41 WHERE singleton"); err != nil {
			t.Fatal("seed synthetic predecessor watermark")
		}
	}
	profile := "authority-proof-counter"
	readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
	if existing {
		// Upgrade проверяет реальный SELECT-only backup profile, fresh — обычный.
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("prepare predecessor backup ACL")
		}
		profile += "-backup"
		readControlPlaneMigrationProof(t, ctx, observer, profile, "precondition")
	}
	if goose.UpToContext(ctx, database, "migrations", 20261108000100) != nil {
		t.Fatal("apply proof-counter migration")
	}
	readControlPlaneMigrationProof(t, ctx, observer, profile, "postcondition")
	var first int64
	if existing {
		if err := observer.QueryRow(ctx, "SELECT revision FROM control_plane.authority_proof_revision_counter WHERE singleton").Scan(&first); err != nil || first != 41 {
			t.Fatal("migration lost predecessor watermark")
		}
	} else {
		if observer.QueryRow(ctx, "SELECT revision FROM control_plane.authority_proof_revision_counter WHERE singleton").Scan(&first) != nil || first != 0 {
			t.Fatal("fresh migration lost the baseline zero watermark")
		}
	}
	// Старая инструкция не может продолжать выдачу параллельно новой.
	_, err := observer.Exec(ctx, "UPDATE control_plane.installation SET authority_proof_sequence=authority_proof_sequence+1 WHERE singleton")
	var rejected *pgconn.PgError
	if !errors.As(err, &rejected) || rejected.Code != "55000" {
		t.Fatal("legacy proof allocator was not gated")
	}
	query, err := os.ReadFile("../../internal/repository/postgres/platform/sql/proof_next_revision.sql")
	if err != nil {
		t.Fatal("read production allocator SQL")
	}
	for i := int64(1); i <= 2; i++ {
		var revision int64
		if observer.QueryRow(ctx, string(query)).Scan(&revision) != nil || revision != first+i {
			t.Fatal("allocator did not advance exact persisted watermark")
		}
	}
	if goose.UpToContext(ctx, database, "migrations", 20261108000100) != nil {
		t.Fatal("repeat Up unexpectedly replayed migration")
	}
	var legacy, platform, current int64
	if observer.QueryRow(ctx, "SELECT authority_proof_sequence,platform_sequence,(SELECT revision FROM control_plane.authority_proof_revision_counter WHERE singleton) FROM control_plane.installation WHERE singleton").Scan(&legacy, &platform, &current) != nil || legacy != first || platform != 0 || current != first+2 {
		t.Fatal("allocation changed legacy/platform counter or replay reset new counter")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err != nil {
			t.Fatal("restore exact proof-counter backup ACL")
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, "authority-proof-counter-backup", "postcondition")
	if _, err := observer.Exec(ctx, "GRANT UPDATE ON control_plane.authority_proof_revision_counter TO kodex_backup_reader"); err != nil {
		t.Fatal("prepare isolated backup authority counterfactual")
	}
	if err := applyPostgresMigrations(ctx, database, workspaceBackupRestoreCommand); err == nil {
		t.Fatal("counter restore accepted backup WRITE")
	}
	if _, err := observer.Exec(ctx, "REVOKE UPDATE ON control_plane.authority_proof_revision_counter FROM kodex_backup_reader"); err != nil {
		t.Fatal("remove isolated backup authority counterfactual")
	}
	readControlPlaneMigrationProof(t, ctx, observer, "authority-proof-counter-backup", "postcondition")
	proofSQL, err := os.ReadFile("../../../../../tools/release/authority-proof-counter-backup-migration-postcondition.sql")
	if err != nil {
		t.Fatal("read counter catalog negative proof")
	}
	// Каждое counterfactual ограничено отдельной rollback transaction той же disposable БД.
	for _, drift := range []string{
		"ALTER TABLE control_plane.installation DISABLE TRIGGER reject_legacy_authority_proof_counter",
		"GRANT DELETE ON control_plane.authority_proof_revision_counter TO control_plane_runtime",
		"GRANT UPDATE ON control_plane.authority_proof_revision_counter TO kodex_backup_reader",
		"ALTER TABLE control_plane.authority_proof_revision_counter ADD COLUMN unapproved boolean",
		"ALTER TABLE control_plane.authority_proof_revision_counter DROP CONSTRAINT authority_proof_revision_counter_revision_check",
		"CREATE INDEX unapproved_counter_index ON control_plane.authority_proof_revision_counter(revision)",
		"DELETE FROM control_plane.authority_proof_revision_counter",
	} {
		tx, err := observer.Begin(ctx)
		if err != nil {
			t.Fatal("begin isolated counter catalog counterfactual")
		}
		if _, err := tx.Exec(ctx, drift); err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal("prepare isolated counter catalog counterfactual")
		}
		statement := strings.ReplaceAll(string(proofSQL), "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;", "")
		statement = strings.TrimSuffix(strings.TrimSpace(statement), "ROLLBACK;")
		_, proofErr := tx.Exec(ctx, statement)
		rollbackErr := tx.Rollback(ctx)
		var rejected *pgconn.PgError
		if !errors.As(proofErr, &rejected) || rejected.Code != "P0001" || rollbackErr != nil {
			t.Fatal("counter catalog accepted drift or did not roll back")
		}
	}
	readControlPlaneMigrationProof(t, ctx, observer, "authority-proof-counter-backup", "postcondition")
	const workers = 16
	poolConfig, err := pgxpool.ParseConfig(observer.Config().ConnString())
	if err != nil {
		t.Fatal("configure independent allocator connections")
	}
	poolConfig.ConnConfig = observer.Config().Copy()
	poolConfig.ConnConfig.User = "control_plane_runtime_g1"
	poolConfig.MaxConns = 4
	allocators, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("open independent allocator connections")
	}
	defer allocators.Close()
	values, failures := make(chan int64, workers), make(chan error, workers)
	var group sync.WaitGroup
	for i := 0; i < workers; i++ {
		group.Go(func() {
			var revision int64
			if err := allocators.QueryRow(ctx, string(query)).Scan(&revision); err != nil {
				failures <- err
			} else {
				values <- revision
			}
		})
	}
	group.Wait()
	close(values)
	if len(failures) != 0 {
		classes := map[string]int{}
		for len(failures) != 0 {
			err := <-failures
			var sqlFailure *pgconn.PgError
			class := "non_sql_failure"
			if errors.As(err, &sqlFailure) {
				class = sqlFailure.Code
			}
			classes[class]++
		}
		t.Fatalf("concurrent proof allocator failed: classes=%v", classes)
	}
	var allocated []int64
	for value := range values {
		allocated = append(allocated, value)
	}
	slices.Sort(allocated)
	if len(allocated) != workers {
		t.Fatal("concurrent proof allocation lost a result")
	}
	for i, value := range allocated {
		if value != first+3+int64(i) {
			t.Fatal("concurrent proof revisions are duplicated or reordered")
		}
	}
	assertAuthorityProofCounterBackupRestore(t, ctx, observer, string(query), first+2+workers)
	var privileges bool
	if observer.QueryRow(ctx, "SELECT has_table_privilege('control_plane_runtime','control_plane.authority_proof_revision_counter','SELECT,UPDATE') AND NOT has_table_privilege('control_plane_runtime','control_plane.authority_proof_revision_counter','INSERT') AND NOT has_table_privilege('control_plane_runtime','control_plane.authority_proof_revision_counter','DELETE') AND NOT has_table_privilege('control_plane_runtime','control_plane.authority_proof_revision_counter','TRUNCATE')").Scan(&privileges) != nil || !privileges {
		t.Fatal("proof counter runtime ACL differs")
	}
	if _, err := observer.Exec(ctx, "UPDATE control_plane.authority_proof_revision_counter SET revision=9007199254740991 WHERE singleton"); err != nil {
		t.Fatal("prepare isolated exhaustion fixture")
	}
	var exhausted int64
	if err := observer.QueryRow(ctx, string(query)).Scan(&exhausted); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("exhausted proof allocator overflowed or returned a revision")
	}
}

// assertAuthorityProofCounterBackupRestore dumps only the caller's disposable database and restores
// it into a new database in that same loopback container. Native runtime allocation must continue
// above the persisted watermark. This is counter/schema recovery, not a live distributed DR claim.
// assertAuthorityProofCounterBackupRestore выгружает только disposable БД вызывающего и восстанавливает
// её в новую БД того же loopback контейнера. Native runtime allocation продолжает сохранённый watermark.
// Это проверка counter/schema recovery, а не утверждение о live distributed DR.
func assertAuthorityProofCounterBackupRestore(t *testing.T, ctx context.Context, observer *pgx.Conn, query string, watermark int64) {
	t.Helper()
	config := observer.Config()
	if config.Host != "127.0.0.1" || config.Database != "control_plane" || config.Port < 1024 || config.Password != "" || config.TLSConfig != nil {
		t.Fatal("counter recovery requires the disposable loopback fixture")
	}
	directory := t.TempDir()
	archive := filepath.Join(directory, "counter-schema.dump")
	file, err := os.OpenFile(archive, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil || file.Close() != nil {
		t.Fatal("prepare private disposable counter archive")
	}
	connection := []string{"--host", config.Host, "--port", strconv.Itoa(int(config.Port)), "--username", "postgres", "--no-password"}
	// Только synthetic trust fixture: ни launcher env, ни пароль/DSN в argv не наследуются.
	dump := exec.CommandContext(ctx, "pg_dump", append(connection, "--dbname", "control_plane", "--format=custom", "--file", archive)...)
	dump.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + directory, "PGCONNECT_TIMEOUT=5"}
	if dump.Run() != nil {
		t.Fatal("dump disposable counter schema")
	}
	if _, err := observer.Exec(ctx, "CREATE DATABASE authority_counter_restore OWNER control_plane_owner"); err != nil {
		t.Fatal("create isolated counter recovery database")
	}
	restore := exec.CommandContext(ctx, "pg_restore", append(connection, "--dbname", "authority_counter_restore", "--exit-on-error", archive)...)
	restore.Env = dump.Env
	if restore.Run() != nil {
		t.Fatal("restore disposable counter schema")
	}
	cloneConfig := config.Copy()
	cloneConfig.Database = "authority_counter_restore"
	cloneConfig.User = "control_plane_runtime_g1"
	clone, err := pgx.ConnectConfig(ctx, cloneConfig)
	if err != nil {
		t.Fatal("connect restored counter as native runtime")
	}
	defer func() { _ = clone.Close(ctx) }()
	var next int64
	if clone.QueryRow(ctx, query).Scan(&next) != nil || next != watermark+1 {
		t.Fatal("restored counter reused or reset the persisted watermark")
	}
	_, err = clone.Exec(ctx, "UPDATE control_plane.installation SET authority_proof_sequence=authority_proof_sequence+1 WHERE singleton")
	var rejected *pgconn.PgError
	if !errors.As(err, &rejected) || rejected.Code != "55000" {
		t.Fatal("restored counter lost the legacy allocation gate")
	}
}

// TestAuthorityProofCounterMigration owns one disposable container/database and verifies upgrade
// from the exact populated predecessor; no other scenario reuses its migrated state.
// TestAuthorityProofCounterMigration владеет одной disposable БД и проверяет upgrade populated
// predecessor; другие сценарии не используют уже мигрированное состояние.
func TestAuthorityProofCounterMigration(t *testing.T) {
	testAuthorityProofCounterMigration(t, true)
}

// TestAuthorityProofCounterFreshInstall runs separately in a fresh disposable database, proving
// the baseline zero watermark remains usable without copying state from the upgrade fixture.
// TestAuthorityProofCounterFreshInstall запускается отдельно в свежей disposable БД и проверяет
// исходный zero watermark без копирования состояния из upgrade fixture.
func TestAuthorityProofCounterFreshInstall(t *testing.T) {
	testAuthorityProofCounterMigration(t, false)
}

// TestAuthorityProofCounterOwnerRegressionSeed creates only the empty exact successor schema for
// the isolated owner regression harness. Bounds/legacy fixtures are deliberately not copied into
// its template, and this seed does not claim complete release/schema qualification.
// TestAuthorityProofCounterOwnerRegressionSeed создаёт только пустую successor schema для
// isolated owner regression. Bounds/legacy fixtures не копируются в template; seed не объявляет
// complete release/schema qualification.
func TestAuthorityProofCounterOwnerRegressionSeed(t *testing.T) {
	ctx, database, observer := newWorkflowUpgradeDatabase(t)
	if err := goose.UpToContext(ctx, database, "migrations", 20261108000100); err != nil {
		t.Fatal("prepare empty authority-counter owner regression schema")
	}
	var revision int64
	if err := observer.QueryRow(ctx, "SELECT revision FROM control_plane.authority_proof_revision_counter WHERE singleton").Scan(&revision); err != nil || revision != 0 {
		t.Fatal("owner regression template is not an empty zero-watermark successor")
	}
}
