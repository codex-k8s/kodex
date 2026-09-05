package receipt

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/repository/receipt"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/owner_receipt__remember.sql
var rememberSQL string

//go:embed sql/owner_receipt__pending.sql
var pendingSQL string

//go:embed sql/owner_receipt__claim.sql
var claimReconciliationSQL string

//go:embed sql/owner_receipt__commit.sql
var commitReconciliationSQL string

var _ port.ReconciliationRepository = (*Repository)(nil)

func (r *Repository) Remember(ctx context.Context, scope port.Scope, record port.Record, owner port.OwnerReceipt, after time.Time) error {
	if owner.Ref == "" || owner.Version < 1 || owner.Invocation == "" || owner.Connection == "" || owner.Mailbox != scope.Mailbox || owner.ExternalRef != record.ID || owner.ExternalDigest != record.ExternalDigest(scope) || owner.InputDigest != record.Digest || owner.EffectKey != record.Key || owner.ConfigurationRevision != record.Audit.ConfigurationRevision || owner.Outcome != record.Outcome() || after.IsZero() {
		return errs.Invalid
	}
	tag, err := r.Pool.Exec(ctx, rememberSQL, pgx.StrictNamedArgs{"tenant": scope.Tenant, "mailbox": scope.Mailbox, "id": record.ID, "input": record.Digest, "owner_ref": owner.Ref, "version": owner.Version, "invocation": owner.Invocation, "connection": owner.Connection, "external_digest": owner.ExternalDigest, "outcome": owner.Outcome, "after": after})
	if err != nil {
		return errs.Unavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.Conflict
	}
	return nil
}

func (r *Repository) Pending(ctx context.Context, batch int) ([]port.Pending, error) {
	if batch < 1 || batch > 64 {
		return nil, errs.Invalid
	}
	rows, err := r.Pool.Query(ctx, pendingSQL, pgx.StrictNamedArgs{"batch": batch})
	if err != nil {
		return nil, errs.Unavailable
	}
	defer rows.Close()
	result := make([]port.Pending, 0, batch)
	for rows.Next() {
		var p port.Pending
		x, o := &p.Record, &p.Owner
		if err := rows.Scan(&p.Scope.Tenant, &p.Scope.Mailbox, &x.ID, &x.Key, &x.Digest, &x.Status, &x.Resource, &x.Audit.Actor, &x.Audit.Agent, &x.Audit.Grant, &x.Audit.Operation, &x.Audit.ConfigurationRevision, &x.Audit.CredentialGeneration, &x.Audit.GateApproved, &o.Ref, &o.Version, &o.Invocation, &o.Connection, &o.ExternalDigest); err != nil {
			return nil, errs.Unavailable
		}
		o.ExternalRef, o.InputDigest, o.EffectKey, o.Mailbox, o.ConfigurationRevision, o.Outcome = x.ID, x.Digest, x.Key, p.Scope.Mailbox, x.Audit.ConfigurationRevision, port.Unknown
		result = append(result, p)
	}
	if rows.Err() != nil {
		return nil, errs.Unavailable
	}
	return result, nil
}

func (r *Repository) ClaimReconciliation(ctx context.Context, p port.Pending, interval time.Duration) (bool, error) {
	if interval < 5*time.Second || interval > 5*time.Minute {
		return false, errs.Invalid
	}
	tag, err := r.Pool.Exec(ctx, claimReconciliationSQL, pgx.StrictNamedArgs{"tenant": p.Scope.Tenant, "mailbox": p.Scope.Mailbox, "id": p.Record.ID, "owner": p.Owner.Ref, "version": p.Owner.Version, "digest": p.Owner.ExternalDigest, "seconds": interval.Seconds()})
	if err != nil {
		return false, errs.Unavailable
	}
	return tag.RowsAffected() == 1, nil
}

func (r *Repository) CommitReconciliation(ctx context.Context, p port.Pending, d port.Decision) (bool, error) {
	if !d.ValidFor(p, time.Now()) {
		return false, errs.Denied
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	ctx, expires := context.WithDeadline(ctx, d.ExpiresAt.Add(-100*time.Millisecond))
	defer expires()
	var changed, replay bool
	err := r.Pool.QueryRow(ctx, commitReconciliationSQL, pgx.StrictNamedArgs{"tenant": p.Scope.Tenant, "mailbox": p.Scope.Mailbox, "id": p.Record.ID, "input": p.Record.Digest, "owner": p.Owner.Ref, "owner_version": p.Owner.Version, "invocation": p.Owner.Invocation, "connection": p.Owner.Connection, "digest": p.Owner.ExternalDigest, "decision": d.Ref, "decision_version": d.Version, "outcome": d.Outcome, "actor": d.Actor, "grant": d.Grant, "expires": d.ExpiresAt}).Scan(&changed, &replay)
	if err != nil {
		return false, errs.Unavailable
	}
	if !changed && !replay {
		return false, errs.Conflict
	}
	return changed, nil
}
