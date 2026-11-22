package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"

	"github.com/pressly/goose/v3"
)

// artifactRetentionGrantCommand admits only the exact 049 predecessor or this applied successor.
// artifactRetentionGrantCommand допускает только точный predecessor049 либо уже применённый successor.
const artifactRetentionGrantCommand = "up-artifact-retention-grant-catalog"

// artifactRetentionGrantVersion is the forward-only worker registration migration, separate from cohort049.
// artifactRetentionGrantVersion задаёт forward-only регистрацию worker отдельно от cohort049.
const artifactRetentionGrantVersion int64 = 20261121000100

// artifactRetentionGrantCompleted identifies the exact committed Goose successor without disclosing credentials.
// artifactRetentionGrantCompleted обозначает точный применённый successor Goose без раскрытия реквизитов.
const artifactRetentionGrantCompleted = "Artifact retention grant catalog migration completed at version 20261121000100"

// artifactRetentionGrantPostcondition checks the exact two successor constraints and active registration.
// artifactRetentionGrantPostcondition проверяет два точных successor CHECK и активную регистрацию.
//
//go:embed artifact_retention_grant_sql/postcondition.sql
var artifactRetentionGrantPostcondition string

// artifactRetentionGrantReadRole selects the existing owner membership for one read-only transaction.
// artifactRetentionGrantReadRole выбирает существующее owner membership для одной read-only транзакции.
//
//go:embed artifact_retention_grant_sql/read_role.sql
var artifactRetentionGrantReadRole string

// upgradeArtifactRetentionGrant applies only this migration after a closed head check; unknown outcomes
// are returned without retry and a confirmed successor rejoins through Goose's no-op applied-version path.
// upgradeArtifactRetentionGrant применяет только эту миграцию после закрытой проверки head; неизвестный
// исход возвращается без повтора, подтверждённый successor читается через no-op путь применённой версии Goose.
func upgradeArtifactRetentionGrant(ctx context.Context, database *sql.DB) (resultErr error) {
	version, err := goose.GetDBVersionContext(ctx, database)
	if err != nil {
		return err
	}
	if version != 20261114170000 && version != artifactRetentionGrantVersion {
		return errors.New("artifact retention grant catalog schema head rejected")
	}
	if err := goose.UpToContext(ctx, database, "migrations", artifactRetentionGrantVersion); err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	if _, err := tx.ExecContext(ctx, artifactRetentionGrantReadRole); err != nil {
		return err
	}
	var valid bool
	if err := tx.QueryRowContext(ctx, artifactRetentionGrantPostcondition).Scan(&valid); err != nil {
		return err
	}
	if !valid {
		return errors.New("artifact retention grant catalog postcondition rejected")
	}
	return tx.Commit()
}
