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

//go:embed sql/email_receipt_get.sql
var queryEmailReceiptGet string

//go:embed sql/email_decision_get.sql
var queryEmailDecisionGet string

//go:embed sql/email_decision_insert.sql
var queryEmailDecisionInsert string

type emailReceiptOwner struct {
	receipt                                entity.EmailEffectReceipt
	id, runRef, projectID, invocationState string
}

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
	if payload.ExpectedReceiptDigest != owner.receipt.ExternalReceiptDigest || owner.receipt.Outcome != emailpolicy.OutcomeUnknown || owner.invocationState != "UNKNOWN_OUTCOME" {
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

func (repository *Repository) ResolveEmailReconciliation(ctx context.Context, principal value.Principal, receiptRef, decisionRef, externalRef, digest string) (entity.EmailEffectReceiptView, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if principal.CallerWorkload != "email-bridge" || principal.ProjectRef != "" {
		return entity.EmailEffectReceiptView{}, errs.ErrForbidden
	}
	if receiptRef == "" || decisionRef == "" || emailpolicy.ValidateExternalReceipt(externalRef, digest) != nil {
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
	if decision == nil || decision.Ref != decisionRef || decision.ReceiptVersion != owner.receipt.Version ||
		decision.ReceiptDigest != digest || owner.receipt.ExternalReceiptDigest != digest || owner.receipt.ExternalReceiptRef != externalRef ||
		owner.receipt.Outcome != emailpolicy.OutcomeUnknown || owner.invocationState != "UNKNOWN_OUTCOME" ||
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
