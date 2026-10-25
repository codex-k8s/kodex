package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryEmailMailboxObservationAdvance embeds a receipt for consecutive connection observation
// versions only when enabled active email state and publication high-watermark still match. It
// changes no publication document, digest or credential generation and never adopts earlier drift.
// queryEmailMailboxObservationAdvance встраивает квитанцию последовательных connection observation
// versions только при совпадении enabled active email state и publication high-watermark. Не
// меняет publication document, digest и credential generation и не принимает прежний drift.
//
//go:embed sql/email_mailbox_observation_advance.sql
var queryEmailMailboxObservationAdvance string

// preservesMailboxSpecification recognizes test/start-completion and grant-change transitions that
// alter observation/grant lifecycle while preserving mailbox specification and credentials.
// Callers acquire the publication lock before row locks; this predicate itself takes no lock and
// grants no authority.
// preservesMailboxSpecification распознаёт test/start-completion и grant-change переходы
// observation/grant lifecycle с сохранением mailbox specification и credentials. Caller берёт
// publication lock до row locks; предикат сам не блокирует и не выдаёт полномочия.
func preservesMailboxSpecification(kind command.Kind) bool {
	return kind == command.TestConnection || kind == command.CompleteConnectionTest || kind == command.ChangeIntegrationGrant
}

// advanceMailboxObservation requires a positive previous version and exactly the next version,
// then records eligible observation receipts in the caller transaction. Invalid progression
// conflicts and Exec failures are Unavailable; zero affected rows is allowed and no new mailbox
// document or Secret revision is created.
// advanceMailboxObservation требует положительную прежнюю версию и ровно следующую, затем
// записывает допустимые observation receipts в переданной транзакции. Неверное продвижение даёт
// Conflict, ошибка Exec — Unavailable; ноль изменённых строк допустим, новый mailbox document и
// Secret revision не создаются.
func advanceMailboxObservation(ctx context.Context, tx pgx.Tx, organizationID, connectionID string, before, after int64) error {
	if before < 1 || after != before+1 {
		return errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, queryEmailMailboxObservationAdvance, pgx.StrictNamedArgs{
		"organization_id": organizationID, "connection_id": connectionID, "previous_version": before, "current_version": after,
	}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}
