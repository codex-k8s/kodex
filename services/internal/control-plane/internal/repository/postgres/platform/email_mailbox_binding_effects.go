package platform

import (
	"context"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

type mailboxBindingEffect struct {
	organizationID, connectionRef, connectionID, setID, revisionID string
	expected, version                                              int64
	enabled                                                        bool
}

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
