package platform

import (
	"context"
	_ "embed"
	"encoding/json"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryEmailConfigurationOwners locks the current owner/configurer/Connection tuples of enabled mailbox slots; absent or stale assignments cannot be counted as eligible publication input.
// queryEmailConfigurationOwners блокирует текущие owner/configurer/Connection tuples включённых mailbox slots; отсутствующая либо устаревшая привязка не считается допустимым входом публикации.
//
//go:embed sql/email_configuration__owners.sql
var queryEmailConfigurationOwners string

// validateEmailConfigurationOwnershipTx admits only new enabled mailbox slots with an exact server-assigned owner epoch and fresh credential version, separately from historical accepted startup replay.
// validateEmailConfigurationOwnershipTx допускает только новые включённые mailbox slots с точным назначенным сервером owner epoch и текущей credential version отдельно от исторического принятого startup replay.
func validateEmailConfigurationOwnershipTx(ctx context.Context, tx pgx.Tx, configuration api.Configuration) error {
	expected := make(map[[2]string]bool, len(configuration.Mailboxes))
	for _, mailbox := range configuration.Mailboxes {
		if !mailbox.Enabled {
			continue
		}
		key := [2]string{mailbox.TenantId, mailbox.ConnectionId}
		if mailbox.CredentialOwnerGeneration < 1 || expected[key] {
			return errs.ErrForbidden
		}
		expected[key] = true
	}
	if len(expected) == 0 {
		return nil
	}
	eligible, err := readEmailConfigurationOwnersTx(ctx, tx, configuration)
	if err != nil {
		return err
	}
	if len(eligible) != len(expected) {
		return errs.ErrForbidden
	}
	for key := range expected {
		if !eligible[key] {
			return errs.ErrForbidden
		}
	}
	_, err = emailCredentialDigests(ctx, tx, configuration)
	return err
}

// readEmailConfigurationOwnersTx returns only fresh enabled slots while locking their authoritative subjects and connections; snapshot builders may disable stale slots but cannot reassign their credentials.
// readEmailConfigurationOwnersTx возвращает только текущие включённые slots с блокировкой авторитетных subjects и connections; построитель snapshot может отключить устаревшие slots, но не переназначить их credentials.
func readEmailConfigurationOwnersTx(ctx context.Context, tx pgx.Tx, configuration api.Configuration) (map[[2]string]bool, error) {
	result := make(map[[2]string]bool, len(configuration.Mailboxes))
	if len(configuration.Mailboxes) == 0 {
		return result, nil
	}
	raw, err := json.Marshal(configuration.Mailboxes)
	if err != nil {
		return nil, errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, queryEmailConfigurationOwners, pgx.StrictNamedArgs{"mailboxes": raw})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var organization, connection string
		if rows.Scan(&organization, &connection) != nil {
			return nil, errs.ErrUnavailable
		}
		key := [2]string{organization, connection}
		if result[key] {
			return nil, errs.ErrConflict
		}
		result[key] = true
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}
