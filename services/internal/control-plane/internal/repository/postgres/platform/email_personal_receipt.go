package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryEmailPersonalReceipt reads only immutable receipt metadata for the current actor and one closed mailbox operation, without locking receipt before Connection or exposing another actor's response.
// queryEmailPersonalReceipt читает только неизменные receipt metadata текущего actor и одной закрытой mailbox operation, не блокирует receipt перед Connection и не выдаёт ответ другого actor.
//
//go:embed sql/email_personal__receipt.sql
var queryEmailPersonalReceipt string

// GetPersonalEmailMailboxMutationReceipt resolves a historical self mutation under fresh Connection/owner eligibility; missing receipts remain unknown and do not authorize a second request or SMTP send.
// GetPersonalEmailMailboxMutationReceipt разрешает историческую собственную mutation с текущей Connection/owner eligibility; отсутствие receipt остаётся неизвестностью и не разрешает второй запрос либо SMTP send.
func (repository *Repository) GetPersonalEmailMailboxMutationReceipt(ctx context.Context, principal value.Principal, operation, idempotencyKey string) (entity.EmailMailboxMutationReceipt, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var result entity.EmailMailboxMutationReceipt
	kind := map[string]command.Kind{"CREATE": command.CreatePersonalEmailMailboxConnection, "CONFIGURE": command.ConfigureEmailMailboxOwnership, "REVOKE": command.RevokeEmailMailboxOwnership}[operation]
	if kind == "" || len(idempotencyKey) < 8 || len(idempotencyKey) > 128 || strings.ContainsAny(idempotencyKey, "\x00\r\n") {
		return result, errs.ErrInvalid
	}
	if principal.CallerWorkload != "control-api-gateway" || principal.Permission != "platform.email-mailboxes.receipts.read" {
		return result, errs.ErrForbidden
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return result, err
	}
	if current.authorityProjectID != "" {
		return result, errs.ErrForbidden
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return result, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var raw []byte
	err = tx.QueryRow(ctx, queryEmailPersonalReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "operation": "controlplane." + strings.ToLower(string(kind)), "idempotency_key": idempotencyKey}).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, errs.ErrUnavailable
	}
	var stored command.Result
	if json.Unmarshal(raw, &stored) != nil || stored.EmailMailboxOwnership == nil {
		return result, errs.ErrUnavailable
	}
	owner := stored.EmailMailboxOwnership
	if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: owner.ConnectionRef}); err != nil {
		return result, errs.ErrNotFound
	}
	if _, err := requireEmailMailboxManagerTx(ctx, tx, current, owner.ConnectionRef); err != nil {
		return result, err
	}
	result.Operation = operation
	if kind == command.CreatePersonalEmailMailboxConnection {
		if stored.Connection == nil || stored.Connection.Ref != owner.ConnectionRef || stored.Connection.DefinitionKey != "email" {
			return result, errs.ErrUnavailable
		}
		result.Mailbox = &entity.MyEmailMailbox{Ownership: *owner, Name: stored.Connection.Name, Enabled: stored.Connection.Enabled, Available: false}
	} else {
		result.Ownership = owner
	}
	return result, nil
}
