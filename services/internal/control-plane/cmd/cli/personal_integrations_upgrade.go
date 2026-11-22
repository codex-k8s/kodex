package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/pressly/goose/v3"
)

const (
	// personalIntegrationsUpgradeCommand migrates the stopped normal cohort through the three proven schema heads.
	// personalIntegrationsUpgradeCommand проводит остановленный обычный набор компонентов через три проверенные версии схемы.
	personalIntegrationsUpgradeCommand = "up-personal-integrations"
	// personalIntegrationsBackupUpgradeCommand requires canonical backup grants for every applied stage.
	// personalIntegrationsBackupUpgradeCommand требует канонические права резервного копирования на каждом этапе.
	personalIntegrationsBackupUpgradeCommand = "up-personal-integrations-backup-maintenance"
	// personalIntegrationsBackupUpgradeReceipt confirms canonical backup checks through the final schema.
	// personalIntegrationsBackupUpgradeReceipt подтверждает канонические проверки резервного копирования до итоговой схемы.
	personalIntegrationsBackupUpgradeReceipt = "Personal integrations backup migration completed at version 20261114170000"
)

// personalIntegrationUpgrade binds one adjacent transition to an exact qualification.
// personalIntegrationUpgrade связывает один переход между соседними версиями схемы с точным профилем проверки.
type personalIntegrationUpgrade struct {
	beforeVersion int64
	afterVersion  int64
	backup        bool
}

// personalIntegrationSchemaHeads lists every durable rejoin point of the stopped cohort in order.
// personalIntegrationSchemaHeads перечисляет все точки безопасного повторного входа для остановленного набора компонентов по порядку.
var personalIntegrationSchemaHeads = [...]int64{20261114140000, 20261114150000, 20261114160000, 20261114170000}

// qualificationSQL selects a fixed embedded check for a registered schema head and mode.
// qualificationSQL выбирает закреплённую встроенную проверку зарегистрированной версии схемы и режима.
func (upgrade personalIntegrationUpgrade) qualificationSQL(version int64) string {
	mode := "normal"
	if upgrade.backup {
		mode = "backup"
	}
	return fmt.Sprintf("check_integrations_%d_%s.sql", version, mode)
}

// executePersonalIntegrationUpgrade checks the exact predecessor before one Goose call and rechecks an already committed target without replay.
// executePersonalIntegrationUpgrade проверяет точного предшественника перед одним вызовом Goose и проверяет уже зафиксированную целевую версию без повторного применения.
func executePersonalIntegrationUpgrade(ctx context.Context, database *sql.DB, upgrade personalIntegrationUpgrade, migrate func(context.Context, *sql.DB) error) error {
	version, err := readControlPlaneSchemaHead(ctx, database)
	if err != nil {
		return err
	}
	if version == upgrade.afterVersion {
		return executeWorkspaceBackupStatement(ctx, database, upgrade.qualificationSQL(version))
	}
	if version != upgrade.beforeVersion {
		return errors.New("personal integration upgrade schema is not registered")
	}
	if upgrade.backup {
		return executeBackupUpgradeWithStatement(ctx, database, upgrade.qualificationSQL(version), migrate)
	}
	if err := executeWorkspaceBackupStatement(ctx, database, upgrade.qualificationSQL(version)); err != nil {
		return err
	}
	if err := migrate(ctx, database); err != nil {
		return err
	}
	return executeWorkspaceBackupStatement(ctx, database, upgrade.qualificationSQL(upgrade.afterVersion))
}

// upgradePersonalIntegrationCohort starts at a known committed head and applies subsequent proven transitions without starting consumers in between.
// An error ends the command immediately. A later invocation must qualify the committed head again before performing further migrations.
// upgradePersonalIntegrationCohort начинает с известной зафиксированной версии и выполняет следующие проверенные переходы без промежуточного запуска потребителей.
// Ошибка сразу завершает команду. Следующий вызов заново проверяет зафиксированную версию перед продолжением миграций.
func upgradePersonalIntegrationCohort(ctx context.Context, database *sql.DB, backup bool) error {
	version, err := readControlPlaneSchemaHead(ctx, database)
	if err != nil {
		return err
	}
	final := personalIntegrationSchemaHeads[len(personalIntegrationSchemaHeads)-1]
	if version == final {
		upgrade := personalIntegrationUpgrade{afterVersion: final, backup: backup}
		return executeWorkspaceBackupStatement(ctx, database, upgrade.qualificationSQL(final))
	}
	for index, afterVersion := range personalIntegrationSchemaHeads[1:] {
		beforeVersion := personalIntegrationSchemaHeads[index]
		if version != beforeVersion {
			continue
		}
		upgrade := personalIntegrationUpgrade{beforeVersion: beforeVersion, afterVersion: afterVersion, backup: backup}
		if err := executePersonalIntegrationUpgrade(ctx, database, upgrade, func(ctx context.Context, database *sql.DB) error {
			return goose.UpToContext(ctx, database, "migrations", afterVersion)
		}); err != nil {
			return err
		}
		version = afterVersion
	}
	if version != final {
		return errors.New("personal integration upgrade schema is not registered")
	}
	return nil
}
