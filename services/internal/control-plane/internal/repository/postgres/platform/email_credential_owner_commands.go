package platform

import (
	"context"
	_ "embed"
	"errors"
	"math"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var (
	// queryEmailCredentialOwnerSet advances ownership and Connection versions together, invalidating old descriptor generations without changing their immutable history.
	// queryEmailCredentialOwnerSet совместно повышает версии владельца и Connection, запрещая прежние поколения дескрипторов без изменения их неизменной истории.
	//go:embed sql/email_credential_owner__set.sql
	queryEmailCredentialOwnerSet string
	// queryEmailCredentialOwnerService resolves only an active service subject in the current organization; a payload ref is merely a locator.
	// queryEmailCredentialOwnerService разрешает только активный service subject текущей организации; ref из payload является лишь locator.
	//go:embed sql/email_credential_owner__service.sql
	queryEmailCredentialOwnerService string
	// queryEmailCredentialOwnerInitialize assigns the new Connection creator only when that stored subject is an active user; bootstrap service callers remain unconfigured.
	// queryEmailCredentialOwnerInitialize назначает создателя нового Connection лишь если сохранённый subject является активным пользователем; bootstrap service callers остаются ненастроенными.
	//go:embed sql/email_credential_owner__initialize.sql
	queryEmailCredentialOwnerInitialize string
)

// emailMailboxOwnershipCommand recognizes only the two specialized owner transitions and never maps generic Connection update into mailbox credential authority.
// emailMailboxOwnershipCommand распознаёт только два специализированных перехода owner и не превращает generic Connection update в полномочие на credentials mailbox.
func emailMailboxOwnershipCommand(kind command.Kind) bool {
	return kind == command.ConfigureEmailMailboxOwnership || kind == command.RevokeEmailMailboxOwnership
}

// authorizeEmailMailboxOwnershipTx checks the exact browser operation, current Connection permission and configuring user before any idempotency receipt can be replayed.
// authorizeEmailMailboxOwnershipTx проверяет точную browser operation, текущее право на Connection и настроившего пользователя до повторной выдачи idempotency receipt.
func (repository *Repository) authorizeEmailMailboxOwnershipTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command) error {
	payload, ok := input.Payload.(command.EmailMailboxOwnershipInput)
	if !ok || payload.ConnectionRef == "" || len(payload.ConnectionRef) > 96 || input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion < 1 || *input.Mutation.ExpectedVersion == math.MaxInt64 {
		return errs.ErrInvalid
	}
	permission := "platform.email-mailboxes.ownership.configure"
	if input.Kind == command.RevokeEmailMailboxOwnership {
		permission = "platform.email-mailboxes.ownership.revoke"
		if payload.Policy != "" || payload.ServiceSubjectRef != "" {
			return errs.ErrInvalid
		}
	} else if payload.Policy != entity.IntegrationCredentialInitiatorRequired && payload.Policy != entity.IntegrationCredentialProjectShared ||
		payload.Policy == entity.IntegrationCredentialInitiatorRequired && payload.ServiceSubjectRef != "" ||
		payload.Policy == entity.IntegrationCredentialProjectShared && (payload.ServiceSubjectRef == "" || len(payload.ServiceSubjectRef) > 96) {
		return errs.ErrInvalid
	}
	if input.Principal.CallerWorkload != "control-api-gateway" || input.Principal.Permission != permission || current.authorityProjectID != "" {
		return errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: payload.ConnectionRef}); err != nil {
		return errs.ErrNotFound
	}
	row, err := readEmailCredentialOwnerTx(ctx, tx, current, payload.ConnectionRef, current.actorRef)
	if err != nil {
		return err
	}
	if !row.initiatorActive || row.initiator.SubjectKind != "USER" {
		return errs.ErrForbidden
	}
	if input.Kind == command.ConfigureEmailMailboxOwnership && (payload.Policy == entity.IntegrationCredentialProjectShared || row.owner.Policy == entity.IntegrationCredentialProjectShared && row.state != "UNCONFIGURED") {
		// Self metadata access cannot select or restore a service identity; the caller needs independent organization administration.
		// Собственный metadata access не выбирает и не восстанавливает service identity; нужны независимые права управления организацией.
		if err := repository.requireAccess(ctx, tx, current, "organization.manage", organizationTarget(current.organizationRef)); err != nil {
			return errs.ErrForbidden
		}
	}
	if row.state == "UNCONFIGURED" && input.Kind == command.ConfigureEmailMailboxOwnership {
		return nil
	}
	if row.configuredBy != row.initiator.SubjectID || row.owner.Generation < 1 {
		return errs.ErrNotFound
	}
	return nil
}

// changeEmailMailboxOwnershipTx fences future effects atomically; configuration requires a disabled drained Connection, while urgent revoke retains in-flight uncertainty and history.
// changeEmailMailboxOwnershipTx атомарно ограждает будущие эффекты; настройка требует отключённого освобождённого Connection, срочный revoke сохраняет неопределённость начатой работы и историю.
func (repository *Repository) changeEmailMailboxOwnershipTx(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.EmailMailboxOwnershipInput)
	if !ok || input.Mutation.ExpectedVersion == nil {
		return commandOutcome{}, errs.ErrInvalid
	}
	row, err := readEmailCredentialOwnerTx(ctx, tx, current, payload.ConnectionRef, current.actorRef)
	if err != nil {
		return commandOutcome{}, err
	}
	if row.connectionVersion != *input.Mutation.ExpectedVersion {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	state, policy, subjectID, subjectKind := "ACTIVE", payload.Policy, row.initiator.SubjectID, "USER"
	if input.Kind == command.RevokeEmailMailboxOwnership {
		if row.state != "ACTIVE" {
			return commandOutcome{}, errs.ErrConflict
		}
		state, policy, subjectID, subjectKind = "REVOKED", row.owner.Policy, row.owner.SubjectID, row.owner.SubjectKind
	} else {
		locked, err := repository.lockIntegrationConnection(ctx, tx, current.organizationID, payload.ConnectionRef)
		if err != nil {
			return commandOutcome{}, err
		}
		effects, grants, err := repository.integrationConnectionDependencies(ctx, tx, locked.id, false)
		if err != nil {
			return commandOutcome{}, err
		}
		var priorPolicy entity.IntegrationCredentialPolicy
		var roots int64
		if tx.QueryRow(ctx, queryIntegrationCredentialPolicyDependencies, pgx.StrictNamedArgs{"organization_id": current.organizationID, "connection_id": locked.id}).Scan(&priorPolicy, &roots) != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if locked.enabled || locked.state != "DISABLED" || effects != 0 || grants != 0 || roots != 0 {
			return commandOutcome{}, errs.ErrConflict
		}
		if policy == entity.IntegrationCredentialProjectShared {
			if err := tx.QueryRow(ctx, queryEmailCredentialOwnerService, pgx.StrictNamedArgs{"organization_id": current.organizationID, "subject_ref": payload.ServiceSubjectRef}).Scan(&subjectID); errors.Is(err, pgx.ErrNoRows) {
				return commandOutcome{}, errs.ErrNotFound
			} else if err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			subjectKind = "SERVICE"
		}
	}
	var version, generation int64
	err = tx.QueryRow(ctx, queryEmailCredentialOwnerSet, pgx.StrictNamedArgs{"connection_id": row.connectionID, "organization_id": current.organizationID, "operator_id": row.initiator.SubjectID, "subject_id": subjectID, "subject_kind": subjectKind, "state": state, "policy": string(policy), "expected_version": row.connectionVersion}).Scan(&version, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	if err := repository.settleEmailPendingTx(ctx, tx, current, payload.ConnectionRef); err != nil {
		return commandOutcome{}, err
	}
	view := entity.EmailMailboxOwnershipView{ConnectionRef: payload.ConnectionRef, ConnectionVersion: version, DeliveryKind: "MAILBOX_CONFIGURATION", Policy: policy, State: state, Generation: generation, OwnerEligible: state == "ACTIVE"}
	if policy == entity.IntegrationCredentialProjectShared {
		view.ServiceSubjectRef = payload.ServiceSubjectRef
		if state == "REVOKED" {
			view.ServiceSubjectRef = row.subjectRef
		}
	}
	return commandOutcome{result: command.Result{EmailMailboxOwnership: &view}, resourceKind: "INTEGRATION_CONNECTION", resourceRef: payload.ConnectionRef, summary: "i18n:EMAIL_MAILBOX_OWNERSHIP_CHANGED", platformEvent: "INTEGRATION_CONNECTION_CHANGED", platformAggregateVersion: version, platformState: state}, nil
}
