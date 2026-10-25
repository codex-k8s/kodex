package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryEmailMailboxPublicationRecoveryLock embeds live exact claimant/generation ownership and
// locks PENDING/READY publication plus anchor connection, returning observation-carried expected
// version and expiry. Caller checks changed bindings; payload refs do not establish ownership.
// queryEmailMailboxPublicationRecoveryLock встраивает действующее владение точных
// claimant/generation и блокирует PENDING/READY publication с anchor connection, возвращая
// expected version с observation carry и expiry. Caller проверяет изменённые bindings; refs
// payload не определяют владение.
//
//go:embed sql/email_mailbox_publication_recovery_lock.sql
var queryEmailMailboxPublicationRecoveryLock string

// queryEmailMailboxPublicationFail embeds fenced live-claim retirement: READY becomes SUPERSEDED,
// PENDING becomes FAILED with a safe code, and claimant/lease are cleared. The owner transaction
// must also persist the replacement forward revision.
// queryEmailMailboxPublicationFail встраивает fenced завершение действующего claim: READY
// становится SUPERSEDED, PENDING — FAILED с безопасным code, claimant/lease очищаются. Owner
// transaction также сохраняет новую forward revision.
//
//go:embed sql/email_mailbox_publication_fail.sql
var queryEmailMailboxPublicationFail string

// RecoverEmailMailboxPublication opens a transaction and global publication lock, validates live
// claim ownership and compares anchor/effect versions. Drift or pending expiry retires the old
// publication and creates a higher RECOVERY snapshot excluding changed tenant connections; READY
// cannot race existing pending work. It atomically saves effects/audit/event then commits,
// returning false,nil for no recovery cause. It never reissues the old Secret revision or performs
// external apply.
// RecoverEmailMailboxPublication открывает транзакцию и глобальную publication lock, проверяет
// действующее владение claim и сравнивает anchor/effect versions. Drift либо pending expiry
// завершает прежнюю publication и создаёт более высокий RECOVERY snapshot без изменённых tenant
// connections; READY не конкурирует с уже pending работой. Атомарно сохраняет effects/audit/event
// и делает Commit, возвращая false,nil без причины recovery. Не переиздаёт прежнюю Secret revision
// и не выполняет внешнее применение.
func (repository *Repository) RecoverEmailMailboxPublication(ctx context.Context, work entity.EmailMailboxPublicationWork) (bool, error) {
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, queryEmailMailboxPublicationLock); err != nil {
		return false, errs.ErrUnavailable
	}
	var current scope
	var state, connectionID, connectionRef string
	var expected, version int64
	var enabled bool
	var expiry time.Time
	if err := tx.QueryRow(ctx, queryEmailMailboxPublicationRecoveryLock, work.Ref, work.Claimant, work.ClaimGeneration).Scan(
		&state, &current.organizationID, &current.organizationRef, &connectionID, &connectionRef, &expected, &version, &enabled, &expiry, &current.actorID); err != nil {
		return false, errs.ErrConflict
	}
	code := ""
	changed := map[string]mailboxBindingEffect{}
	effects, err := readMailboxBindingEffects(ctx, tx, work.Ref)
	if err != nil {
		return false, err
	}
	for _, effect := range effects {
		if effect.organizationID != current.organizationID {
			return false, errs.ErrConflict
		}
		if effect.expected != effect.version {
			changed[effect.connectionRef] = effect
		}
	}
	if expected != version {
		changed[connectionRef] = mailboxBindingEffect{organizationID: current.organizationID, connectionRef: connectionRef, connectionID: connectionID, version: version}
	}
	if len(changed) > 0 {
		code = "EMAIL_MAILBOX_CONNECTION_CHANGED"
	} else if state == "PENDING" && !time.Now().Before(expiry) {
		code = "EMAIL_MAILBOX_DELIVERY_EXPIRED"
	}
	if code == "" {
		return false, nil
	}
	var next int64
	var pending bool
	if err := tx.QueryRow(ctx, queryEmailMailboxPublicationNext).Scan(&next, &pending); err != nil {
		return false, errs.ErrUnavailable
	}
	// READY старого поколения не конкурирует с уже созданной новой публикацией.
	if state == "READY" && pending {
		return false, errs.ErrConflict
	}
	configuration := api.Configuration{Version: "email-bridge/v1", Revision: next, ManagedBy: "ui", Source: "control-plane", Mailboxes: []api.Mailbox{}}
	var raw []byte
	var digest string
	err = tx.QueryRow(ctx, queryEmailConfigurationDocumentRead).Scan(&raw, &digest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, errs.ErrUnavailable
	}
	if err == nil {
		var accepted api.Configuration
		if api.Decode(raw, &accepted) != nil || api.Digest(accepted) != digest {
			return false, errs.ErrUnavailable
		}
		for _, mailbox := range accepted.Mailboxes {
			// Изменённая connection требует нового явного bind, а не старых полномочий.
			if _, revoked := changed[mailbox.ConnectionId]; revoked && mailbox.TenantId == current.organizationRef {
				continue
			}
			configuration.Mailboxes = append(configuration.Mailboxes, mailbox)
		}
	}
	if api.ValidateConfiguration(configuration) != nil {
		return false, errs.ErrUnavailable
	}
	tag, err := tx.Exec(ctx, queryEmailMailboxPublicationFail, work.Ref, work.Claimant, work.ClaimGeneration, code)
	if err != nil || tag.RowsAffected() != 1 {
		return false, errs.ErrConflict
	}
	ref, err := newRef("mailpub")
	if err != nil {
		return false, errs.ErrUnavailable
	}
	raw, err = json.Marshal(configuration)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	var created time.Time
	if err := tx.QueryRow(ctx, queryEmailMailboxPublicationInsert, pgx.StrictNamedArgs{
		"ref": ref, "revision": next, "digest": api.Digest(configuration), "document": raw,
		"organization_id": current.organizationID, "connection_id": connectionID, "connection_version": version,
		"configuration_set_id": "", "configuration_revision_id": "", "actor_id": current.actorID, "kind": "RECOVERY",
	}).Scan(&created); err != nil {
		return false, errs.ErrUnavailable
	}
	for _, effect := range changed {
		if _, err := tx.Exec(ctx, queryEmailMailboxPublicationBindingInsert, ref, current.organizationID, effect.connectionID, effect.version, "", ""); err != nil {
			return false, errs.ErrUnavailable
		}
	}
	current.correlationRef = work.Ref
	if err := auditMailboxOwner(ctx, tx, current, connectionRef, "controlplane.email-mailbox.publication-recovery", "i18n:EMAIL_MAILBOX_PUBLICATION_FAILED"); err != nil {
		return false, err
	}
	if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", connectionRef, "i18n:EMAIL_MAILBOX_PUBLICATION_FAILED", version, "PENDING"); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, errs.ErrUnavailable
	}
	return true, nil
}
