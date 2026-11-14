package main

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pressly/goose/v3"
)

// upgradeSentryWithCanonicalBackup owns exactly044→045 and refuses unknown heads; already applied045 is checked without rerunning Goose.
// upgradeSentryWithCanonicalBackup владеет ровно044→045 и отвергает неизвестные heads; уже применённая045 проверяется без повторного Goose.
func upgradeSentryWithCanonicalBackup(ctx context.Context, database *sql.DB) error {
	return executeSentryCanonicalBackupUpgrade(ctx, database, func(ctx context.Context, database *sql.DB) error {
		return goose.UpToContext(ctx, database, "migrations", 20261114130000)
	})
}

// executeSentryCanonicalBackupUpgrade shares the existing stopped-owner cleanup lifecycle but never strips canonical USAGE/sequence ACLs or admits the table-only qualification as a live canonical boundary.
// executeSentryCanonicalBackupUpgrade использует прежний stopped-owner cleanup lifecycle, но не снимает canonical USAGE/sequence ACL и не принимает table-only qualification как живую canonical boundary.
func executeSentryCanonicalBackupUpgrade(ctx context.Context, database *sql.DB, migrate func(context.Context, *sql.DB) error) error {
	selector, err := workspaceBackupStatements.ReadFile("workspace_backup_sql/version.sql")
	if err != nil {
		return err
	}
	var version int64
	if err := database.QueryRowContext(ctx, string(selector)).Scan(&version); err != nil {
		return err
	}
	switch version {
	case 20261114120000:
		return executeBackupUpgradeWithStatement(ctx, database, "check_sentry_20261114120000_backup.sql", migrate)
	case 20261114130000:
		return executeWorkspaceBackupStatement(ctx, database, "check_sentry_20261114130000_backup.sql")
	default:
		return errors.New("Sentry observability backup upgrade schema is not registered")
	}
}
