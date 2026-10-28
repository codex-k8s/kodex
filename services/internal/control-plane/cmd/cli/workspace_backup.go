package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/pressly/goose/v3"
)

const (
	// workspaceBackupUpgradeCommand selects the explicit stopped-owner compatibility path, never ordinary up.
	// workspaceBackupUpgradeCommand выбирает явный compatibility path остановленного owner, а не обычный up.
	workspaceBackupUpgradeCommand = "up-workspace-backup-compatible"
	// workspaceBackupRestoreCommand repairs only the closed backup SELECT set after an interrupted maintenance command.
	// workspaceBackupRestoreCommand восстанавливает только закрытый набор backup SELECT после прерванного обслуживания.
	workspaceBackupRestoreCommand = "restore-workspace-backup-acl"
	// workspaceBackupCleanupBudget gives ACL restoration its own bounded context after caller cancellation.
	// workspaceBackupCleanupBudget выделяет восстановлению ACL отдельный ограниченный context после отмены caller.
	workspaceBackupCleanupBudget = 30 * time.Second
)

// workspaceBackupStatements embeds only generated, fixed maintenance SQL and a read-only version selector.
// No caller-supplied query, role, schema or target table can enter this registry.
// workspaceBackupStatements встраивает только generated fixed maintenance SQL и read-only selector версии.
// Caller не может передать в реестр запрос, роль, схему или целевую таблицу.
//
//go:embed workspace_backup_sql/*.sql
var workspaceBackupStatements embed.FS

// validPostgresCommand closes dispatch over the existing diagnostic commands and two exact maintenance operations.
// validPostgresCommand замыкает dispatch на прежние diagnostic commands и две точные maintenance operations.
func validPostgresCommand(command string) bool {
	switch command {
	case "check", "up", "status", workspaceBackupUpgradeCommand, workspaceBackupRestoreCommand:
		return true
	default:
		return false
	}
}

// executeWorkspaceBackupStatement commits one exact ACL transition; failed or unknown commits are never retried.
// Catalog and authority failures roll back the same transaction, including every preceding REVOKE.
// executeWorkspaceBackupStatement фиксирует один exact ACL transition; ошибочный либо неизвестный commit не повторяется.
// Catalog и authority failures откатывают ту же транзакцию, включая все предшествующие REVOKE.
func executeWorkspaceBackupStatement(ctx context.Context, database *sql.DB, filename string) error {
	statement, err := workspaceBackupStatements.ReadFile("workspace_backup_sql/" + filename)
	if err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(statement)); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

// restoreWorkspaceBackupACL admits only known committed schema heads and then validates the actual catalog in the write transaction.
// A late failure preserves the prior grants by rollback; an unknown schema never receives a compatibility fallback.
// restoreWorkspaceBackupACL принимает только известные committed schema heads и проверяет actual catalog в write transaction.
// Поздний отказ сохраняет прежние grants через rollback; неизвестная схема не получает compatibility fallback.
func restoreWorkspaceBackupACL(ctx context.Context, database *sql.DB) error {
	selector, err := workspaceBackupStatements.ReadFile("workspace_backup_sql/version.sql")
	if err != nil {
		return err
	}
	var version int64
	if err := database.QueryRowContext(ctx, string(selector)).Scan(&version); err != nil {
		return err
	}
	switch version {
	case 20261026001500, 20261026001600, 20261026001700, 20261027000100, 20261028000100:
		return executeWorkspaceBackupStatement(ctx, database, fmt.Sprintf("restore_%d.sql", version))
	default:
		return errors.New("workspace backup restore schema is not registered")
	}
}

// upgradeWorkspaceWithBackupCompatibility temporarily removes only the approved backup SELECT set, runs Goose once, and restores it.
// Successful normalization is followed by bounded restoration even when migration fails or the caller cancels.
// An unknown normalization commit stops before Goose; the operator must inspect and use the exact restore command.
// upgradeWorkspaceWithBackupCompatibility временно снимает только разрешённый backup SELECT, запускает Goose один раз и возвращает его.
// После успешной normalization bounded restoration выполняется даже при ошибке migration или отмене caller.
// Неизвестный normalization commit останавливает вход в Goose; оператор проверяет outcome и использует exact restore command.
func upgradeWorkspaceWithBackupCompatibility(ctx context.Context, database *sql.DB) error {
	if err := executeWorkspaceBackupStatement(ctx, database, "normalize.sql"); err != nil {
		return err
	}
	migrationErr := goose.UpToContext(ctx, database, "migrations", 20261028000100)
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), workspaceBackupCleanupBudget)
	defer stop()
	return errors.Join(migrationErr, restoreWorkspaceBackupACL(cleanup, database))
}
