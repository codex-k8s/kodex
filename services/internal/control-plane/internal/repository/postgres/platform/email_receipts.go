package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/emailpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryEmailReceiptGet embeds a FOR UPDATE receipt lookup by same-tenant receipt or invocation ref
// with connection/run/project lineage and invocation state. Resource permissions are checked
// separately; the row lock is retained even for the receipt-view read path.
// queryEmailReceiptGet встраивает FOR UPDATE receipt lookup по receipt либо invocation ref того же
// tenant с connection/run/project lineage и invocation state. Resource permissions проверяются
// отдельно; row lock сохраняется даже для receipt-view read path.
//
//go:embed sql/email_receipt_get.sql
var queryEmailReceiptGet string

// queryEmailDecisionGet embeds the newest reconciliation decision for an exact tenant/receipt,
// optionally restricted to a decision ref, ordered by creation/ref. It does not filter expiry or
// authorize the actor; read and bridge paths apply their own policies afterward.
// queryEmailDecisionGet встраивает newest reconciliation decision точных tenant/receipt с
// необязательным ограничением decision ref в порядке creation/ref. Не фильтрует expiry и не
// авторизует actor; read и bridge paths применяют свои policies после чтения.
//
//go:embed sql/email_decision_get.sql
var queryEmailDecisionGet string

// queryEmailDecisionInsert embeds insertion of a reconciliation decision bound to receipt
// version/digest, outcome, grant ref, actor/note and bounded timestamps. It does not update the
// receipt or re-execute the external email effect.
// queryEmailDecisionInsert встраивает добавление reconciliation decision с привязкой к receipt
// version/digest, outcome, grant ref, actor/note и bounded timestamps. Не меняет receipt и не
// повторяет внешний email effect.
//
//go:embed sql/email_decision_insert.sql
var queryEmailDecisionInsert string

// emailReceiptOwner holds the public receipt plus private receipt UUID, run/project locators and
// invocation state needed for locks, access and reconciliation. These fields come from one owner
// query and do not independently authorize receipt disclosure or effect replay.
// emailReceiptOwner хранит public receipt с private receipt UUID, run/project locators и
// invocation state для locks, access и reconciliation. Поля поступают из одного owner query и
// отдельно не разрешают receipt disclosure либо effect replay.
type emailReceiptOwner struct {
	receipt                                entity.EmailEffectReceipt
	id, runRef, projectID, invocationState string
}

// emailReconciliationSourceClosed accepts only UNKNOWN_OUTCOME, CANCELLED or FAILED as closed
// reconciliation-source states. It performs no lookup, receipt/outcome validation or permission
// check.
// emailReconciliationSourceClosed принимает только UNKNOWN_OUTCOME, CANCELLED либо FAILED как
// закрытые reconciliation-source states. Не выполняет lookup, receipt/outcome validation или
// permission check.
func emailReconciliationSourceClosed(state string) bool {
	return state == "UNKNOWN_OUTCOME" || state == "CANCELLED" || state == "FAILED"
}

// readEmailReceipt locks and scans one tenant-bound receipt with its source lineage, maps no rows
// to NotFound and other scan errors to Unavailable, then hides an authority-project mismatch.
// Early scan errors may carry a partial owner value alongside the error; callers must discard it.
// Resource access remains a separate check and no state is written.
// readEmailReceipt блокирует и читает один tenant-bound receipt с source lineage, отображает no
// rows в NotFound, прочие scan errors — в Unavailable, затем скрывает authority-project mismatch.
// Ранние scan errors могут содержать partial owner value вместе с ошибкой; caller обязан отбросить
// его. Resource access остаётся отдельной проверкой, state не записывается.
func readEmailReceipt(ctx context.Context, tx pgx.Tx, current scope, receiptRef, invocationRef string) (emailReceiptOwner, error) {
	var owner emailReceiptOwner
	r := &owner.receipt
	err := tx.QueryRow(ctx, queryEmailReceiptGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "receipt_ref": receiptRef, "invocation_ref": invocationRef,
	}).Scan(&owner.id, &r.Ref, &r.Version, &r.InvocationRef, &r.ExternalReceiptRef, &r.ExternalReceiptDigest,
		&r.SemanticInputDigest, &r.EffectKey, &r.Outcome, &r.MailboxRef, &r.ConfigurationRevision,
		&r.ConnectionRef, &r.ProjectRef, &r.CreatedAt, &r.UpdatedAt, &owner.runRef, &owner.projectID, &owner.invocationState)
	if errors.Is(err, pgx.ErrNoRows) {
		return owner, errs.ErrNotFound
	}
	if err != nil {
		return owner, errs.ErrUnavailable
	}
	if current.authorityProjectID != "" && current.authorityProjectID != owner.projectID {
		return emailReceiptOwner{}, errs.ErrNotFound
	}
	return owner, nil
}

// readEmailDecision reads the latest or exact-ref decision in the caller transaction, returning
// nil without error when absent and Unavailable on backend failure. It sets the existing synthetic
// decision Version to one and returns stored expiry unchanged; freshness and access are not
// checked here.
// readEmailDecision читает latest либо exact-ref decision в caller transaction, возвращая nil без
// ошибки при отсутствии и Unavailable при backend failure. Назначает прежнюю synthetic decision
// Version один и возвращает stored expiry без изменения; freshness и access здесь не проверяются.
func readEmailDecision(ctx context.Context, tx pgx.Tx, current scope, receiptRef, decisionRef string) (*entity.EmailReconciliationDecision, error) {
	var d entity.EmailReconciliationDecision
	err := tx.QueryRow(ctx, queryEmailDecisionGet, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "receipt_ref": receiptRef, "decision_ref": decisionRef,
	}).Scan(&d.Ref, &d.ReceiptRef, &d.ReceiptVersion, &d.ReceiptDigest, &d.InvocationRef, &d.Outcome,
		&d.GrantRef, &d.ActorRef, &d.CreatedAt, &d.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	d.Version = 1
	return &d, nil
}

// emailReceiptAccess requires the supplied integration permission on the receipt connection first,
// then run.view on its authoritative source run. It returns the first access error unchanged and
// does not treat receipt refs, project equality or idempotency as permission.
// emailReceiptAccess сначала требует переданную integration permission на receipt connection,
// затем run.view на authoritative source run. Возвращает первую access error без изменения и не
// считает receipt refs, project equality или idempotency полномочиями.
func (repository *Repository) emailReceiptAccess(ctx context.Context, tx pgx.Tx, current scope, owner emailReceiptOwner, permission string) error {
	if err := repository.requireAccess(ctx, tx, current, permission, entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: owner.receipt.ConnectionRef,
	}); err != nil {
		return err
	}
	return repository.requireAccess(ctx, tx, current, emailpolicy.PermissionRunView, entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: owner.runRef,
	})
}

// GetEmailEffectReceipt resolves current principal scope and reads a locked receipt plus latest
// optional decision within a five-second transaction. Both integration view and source run.view
// are required before commit. It may show an expired stored decision for owner inspection; this
// view neither creates a reconciliation grant nor retries the email effect.
// GetEmailEffectReceipt разрешает current principal scope и читает locked receipt с latest
// optional decision в пятисекундной transaction. До commit требуются integration view и source
// run.view. Может показывать expired stored decision для owner inspection; view не создаёт
// reconciliation grant и не повторяет email effect.
func (repository *Repository) GetEmailEffectReceipt(ctx context.Context, principal value.Principal, invocationRef string) (entity.EmailEffectReceiptView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.EmailEffectReceiptView{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	owner, err := readEmailReceipt(ctx, tx, current, "", invocationRef)
	if err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	if err := repository.emailReceiptAccess(ctx, tx, current, owner, emailpolicy.PermissionView); err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	decision, err := readEmailDecision(ctx, tx, current, owner.receipt.Ref, "")
	if err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.EmailEffectReceiptView{}, errs.ErrUnavailable
	}
	return entity.EmailEffectReceiptView{Receipt: owner.receipt, Decision: decision}, nil
}

// authorizeEmailReconciliation requires a gateway reconciliation payload with receipt ref,
// resolves the receipt inside current tenant/project scope, checks integration reconcile plus
// source run.view and demands fresh interactive authentication. Version/digest/outcome/OCC checks
// belong to the subsequent specialized command, not this access helper.
// authorizeEmailReconciliation требует gateway reconciliation payload с receipt ref, разрешает
// receipt внутри current tenant/project scope, проверяет integration reconcile с source run.view и
// требует fresh interactive authentication. Version/digest/outcome/OCC checks принадлежат
// последующей specialized command, а не этому access helper.
func (repository *Repository) authorizeEmailReconciliation(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (emailReceiptOwner, error) {
	payload, ok := input.Payload.(command.EmailReconciliationInput)
	if !ok || payload.ReceiptRef == "" || input.Principal.CallerWorkload != "control-api-gateway" {
		return emailReceiptOwner{}, errs.ErrInvalid
	}
	owner, err := readEmailReceipt(ctx, tx, current, payload.ReceiptRef, "")
	if err != nil {
		return emailReceiptOwner{}, err
	}
	if err := repository.emailReceiptAccess(ctx, tx, current, owner, emailpolicy.PermissionReconcile); err != nil {
		return emailReceiptOwner{}, err
	}
	if err := emailpolicy.RequireFreshAuthentication(input.Principal, time.Now().UTC()); err != nil {
		return emailReceiptOwner{}, err
	}
	return owner, nil
}

// reconcileEmailEffect reauthorizes the owner before validating expected receipt
// digest/outcome/note and exact version. Only an UNKNOWN receipt with a closed source may receive
// a matching-outcome decision; a contradictory prior decision is Conflict. It allocates fresh
// decision/grant refs, inserts bounded metadata and rereads it in the caller transaction,
// returning the command result and summary without committing or performing email I/O. Receipt
// idempotency belongs to the outer command path.
// reconcileEmailEffect заново авторизует owner до проверки expected receipt digest/outcome/note и
// exact version. Только UNKNOWN receipt с закрытым source может получить matching-outcome
// decision; противоречащий previous decision даёт Conflict. Выделяет fresh decision/grant refs,
// добавляет bounded metadata и перечитывает её в caller transaction, возвращая event/audit summary
// intent без commit или email I/O. Receipt idempotency принадлежит outer command path.
func (repository *Repository) reconcileEmailEffect(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	owner, err := repository.authorizeEmailReconciliation(ctx, tx, current, input)
	if err != nil {
		return commandOutcome{}, err
	}
	payload := input.Payload.(command.EmailReconciliationInput)
	if err := emailpolicy.ValidateReconciliation(payload.ExpectedReceiptDigest, payload.Outcome, payload.Note); err != nil {
		return commandOutcome{}, err
	}
	if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != owner.receipt.Version {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if payload.ExpectedReceiptDigest != owner.receipt.ExternalReceiptDigest || owner.receipt.Outcome != emailpolicy.OutcomeUnknown || !emailReconciliationSourceClosed(owner.invocationState) {
		return commandOutcome{}, errs.ErrConflict
	}
	previous, err := readEmailDecision(ctx, tx, current, owner.receipt.Ref, "")
	if err != nil {
		return commandOutcome{}, err
	}
	if previous != nil && previous.Outcome != payload.Outcome {
		return commandOutcome{}, errs.ErrConflict
	}
	ref, err := newRef("emrd")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	grant, err := newRef("emrg")
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, queryEmailDecisionInsert, pgx.StrictNamedArgs{
		"ref": ref, "organization_id": current.organizationID, "receipt_id": owner.id,
		"receipt_version": owner.receipt.Version, "receipt_digest": payload.ExpectedReceiptDigest,
		"outcome": payload.Outcome, "grant_ref": grant, "actor_id": current.actorID, "note": payload.Note,
		"created_at": now, "expires_at": now.Add(emailpolicy.AuthorizationMaximumAge),
	}); err != nil {
		return commandOutcome{}, mapWriteError(err)
	}
	decision, err := readEmailDecision(ctx, tx, current, owner.receipt.Ref, ref)
	if err != nil || decision == nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{EmailDecision: decision}, projectID: owner.projectID,
		projectRef: owner.receipt.ProjectRef, resourceKind: "EMAIL_EFFECT_RECEIPT", resourceRef: owner.receipt.Ref,
		summary: "i18n:EMAIL_EFFECT_RECONCILED"}, nil
}

// ResolveEmailReconciliation accepts only unscoped email-bridge reads with a valid external
// receipt binding, then reads the receipt and latest decision under a five-second transaction. An
// explicit decision ref must match that latest row; all receipt version/digest/external-ref,
// UNKNOWN/closed-state, nonempty grant and unexpired pins must match. It rechecks access using the
// decision actor ref and commits before returning. Missing/implicit-expired decisions are
// NotFound, mismatches are Forbidden; it does not consume a grant or resend mail.
// ResolveEmailReconciliation принимает только unscoped email-bridge reads с допустимой external
// receipt binding, затем читает receipt и latest decision в пятисекундной transaction. Explicit
// decision ref обязан совпасть с latest row; должны совпасть все receipt
// version/digest/external-ref, UNKNOWN/closed-state, nonempty grant и unexpired pins. Повторно
// проверяет access с decision actor ref и требует commit до возврата. Missing/implicit-expired
// decisions дают NotFound, mismatches — Forbidden; grant не потребляет и mail повторно не
// отправляет.
func (repository *Repository) ResolveEmailReconciliation(ctx context.Context, principal value.Principal, receiptRef, decisionRef, externalRef, digest string) (entity.EmailEffectReceiptView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if principal.CallerWorkload != "email-bridge" || principal.ProjectRef != "" {
		return entity.EmailEffectReceiptView{}, errs.ErrForbidden
	}
	if receiptRef == "" || emailpolicy.ValidateExternalReceipt(externalRef, digest) != nil {
		return entity.EmailEffectReceiptView{}, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	tx, err := repository.pool.Begin(ctx)
	if err != nil {
		return entity.EmailEffectReceiptView{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	owner, err := readEmailReceipt(ctx, tx, current, receiptRef, "")
	if err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	decision, err := readEmailDecision(ctx, tx, current, receiptRef, "")
	if err != nil {
		return entity.EmailEffectReceiptView{}, err
	}
	if decision == nil || decisionRef == "" && !decision.ExpiresAt.After(time.Now().UTC()) {
		return entity.EmailEffectReceiptView{}, errs.ErrNotFound
	}
	if decisionRef != "" && decision.Ref != decisionRef || decision.ReceiptVersion != owner.receipt.Version ||
		decision.ReceiptDigest != digest || owner.receipt.ExternalReceiptDigest != digest || owner.receipt.ExternalReceiptRef != externalRef ||
		owner.receipt.Outcome != emailpolicy.OutcomeUnknown || !emailReconciliationSourceClosed(owner.invocationState) ||
		!decision.ExpiresAt.After(time.Now().UTC()) || decision.GrantRef == "" {
		return entity.EmailEffectReceiptView{}, errs.ErrForbidden
	}
	actorScope := current
	actorScope.actorRef = decision.ActorRef
	if err := repository.emailReceiptAccess(ctx, tx, actorScope, owner, emailpolicy.PermissionReconcile); err != nil {
		return entity.EmailEffectReceiptView{}, errs.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.EmailEffectReceiptView{}, errs.ErrUnavailable
	}
	return entity.EmailEffectReceiptView{Receipt: owner.receipt, Decision: decision}, nil
}
