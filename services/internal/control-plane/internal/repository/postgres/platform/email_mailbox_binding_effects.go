package platform

import (
	"context"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// mailboxBindingEffect carries server-resolved tenant/connection/configuration IDs and
// expected/current observation versions for one publication effect. It is transaction-local
// metadata, not a caller capability or proof of external application.
// mailboxBindingEffect хранит серверные tenant/connection/configuration IDs и expected/current
// observation versions одного publication effect. Это локальные метаданные транзакции, а не
// capability клиента и не доказательство внешнего применения.
type mailboxBindingEffect struct {
	organizationID, connectionRef, connectionID, setID, revisionID string
	expected, version                                              int64
	enabled                                                        bool
}

// readMailboxBindingEffects locks and scans the publication connection effects in SQL order,
// including observation-version carry. No rows returns a nil slice; query/scan/iteration errors
// discard accumulated effects as Unavailable. It does not itself check tenant/OCC or commit.
// readMailboxBindingEffects блокирует и читает connection effects публикации в SQL-порядке с
// переносом observation version. Без строк возвращает nil slice; ошибки query/scan/iteration
// отбрасывают накопленные effects как Unavailable. Сам не проверяет tenant/OCC и не делает Commit.
func readMailboxBindingEffects(ctx context.Context, tx pgx.Tx, ref string) ([]mailboxBindingEffect, error) {
	rows, err := tx.Query(ctx, queryEmailMailboxPublicationBindings, ref)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	var result []mailboxBindingEffect
	for rows.Next() {
		var item mailboxBindingEffect
		if rows.Scan(&item.organizationID, &item.connectionRef, &item.connectionID, &item.expected, &item.version, &item.setID, &item.revisionID, &item.enabled) != nil {
			return nil, errs.ErrUnavailable
		}
		result = append(result, item)
	}
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	return result, nil
}

// applyMailboxPublicationBindings rechecks each effect tenant/OCC and matches mailbox metadata by
// tenant plus connection; enabled mailbox against a disabled connection conflicts. It updates
// exactly one connection, applies/removes local bindings and emits READY snapshots for nonanchor
// connections using publication correlation. Later errors require caller rollback of earlier
// effects; no Secret delivery or independent commit occurs.
// applyMailboxPublicationBindings перепроверяет tenant/OCC каждого effect и ищет mailbox metadata
// по tenant и connection; enabled mailbox при disabled connection даёт Conflict. Меняет ровно одну
// connection, применяет либо удаляет локальные bindings и пишет READY snapshots для nonanchor
// connections с publication correlation. Поздняя ошибка требует rollback прежних effects
// вызывающим кодом; доставки Secret и отдельного Commit нет.
func (repository *Repository) applyMailboxPublicationBindings(ctx context.Context, tx pgx.Tx, current scope, ref string, configuration api.Configuration, anchor string) error {
	effects, err := readMailboxBindingEffects(ctx, tx, ref)
	if err != nil {
		return err
	}
	for _, effect := range effects {
		if effect.organizationID != current.organizationID || effect.expected != effect.version {
			return errs.ErrConflict
		}
		mailboxRef, sender := "", ""
		for _, mailbox := range configuration.Mailboxes {
			if mailbox.TenantId == current.organizationRef && mailbox.ConnectionId == effect.connectionRef {
				if mailbox.Enabled && !effect.enabled {
					return errs.ErrConflict
				}
				mailboxRef, sender = mailbox.Id, mailbox.Sender
			}
		}
		if effect.setID != "" && (mailboxRef == "" || sender == "") {
			return errs.ErrConflict
		}
		if tag, err := tx.Exec(ctx, queryEmailMailboxConnectionApply, current.organizationID, effect.connectionID, effect.version, mailboxRef, sender); err != nil || tag.RowsAffected() != 1 {
			return errs.ErrConflict
		}
		if effect.setID != "" {
			if _, err := tx.Exec(ctx, queryEmailMailboxBindingApply, current.organizationID, effect.connectionID, effect.setID, effect.revisionID); err != nil {
				return errs.ErrUnavailable
			}
		} else if _, err := tx.Exec(ctx, queryEmailMailboxBindingRemove, current.organizationID, effect.connectionID); err != nil {
			return errs.ErrUnavailable
		}
		if effect.connectionRef != anchor {
			current.correlationRef = ref
			if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", effect.connectionRef, "i18n:EMAIL_MAILBOX_PUBLICATION_READY", effect.version, "READY"); err != nil {
				return err
			}
		}
	}
	return nil
}
