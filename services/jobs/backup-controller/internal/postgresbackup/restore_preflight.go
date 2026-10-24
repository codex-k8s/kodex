package postgresbackup

import (
	"context"
	"errors"

	"github.com/codex-k8s/kodex/services/jobs/backup-controller/internal/configspec"
	"github.com/jackc/pgx/v5"
)

// CheckRestoreTargets proves all target TLS connections, supported server versions and database absence before any restore effect; it reuses the existing named queries and never creates a database.
// CheckRestoreTargets проверяет TLS, версии серверов и отсутствие всех целевых БД до эффектов; использует штатные именованные запросы и не создаёт БД.
func (manager *Manager) CheckRestoreTargets(ctx context.Context, targets []configspec.RestoreDatabase) error {
	if manager == nil || len(targets) == 0 {
		return errors.New("restore target set is empty")
	}
	for _, target := range targets {
		connection, err := connect(ctx, databaseFromTarget(target, target.AdminDatabase))
		if err != nil {
			return errors.New("restore target TLS connection is unavailable")
		}
		var version string
		err = connection.QueryRow(ctx, serverVersionSQL).Scan(&version)
		var exists bool
		if err == nil && supportedServerVersion(version) {
			err = connection.QueryRow(ctx, databaseExistsSQL, pgx.StrictNamedArgs{"database": target.Database}).Scan(&exists)
		} else {
			err = errors.New("restore target server is unsupported")
		}
		closeErr := connection.Close(ctx)
		if err != nil || closeErr != nil {
			return errors.New("restore target state is unavailable")
		}
		if exists {
			return errors.New("restore target database already exists")
		}
	}
	return nil
}
