package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// invalidateSSHComposeDependencies closes only SSH work that uses one exact changed owner dependency.
// invalidateSSHComposeDependencies закрывает лишь SSH work одной exact changed owner dependency.
func (repository *Repository) invalidateSSHComposeDependencies(ctx context.Context, tx pgx.Tx, organizationID, actorID, correlationRef, connectionRef, secretRef, grantRef string) error {
	if connectionRef == "" && secretRef == "" && grantRef == "" {
		return errs.ErrInvalid
	}
	if err := repository.invalidateSSHComposeHealthTests(ctx, tx, organizationID, actorID, correlationRef, connectionRef, secretRef); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, querySSHComposeInvalidate, pgx.StrictNamedArgs{"organization_id": organizationID, "actor_id": actorID, "connection_ref": connectionRef, "secret_ref": secretRef, "grant_ref": grantRef})
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	// pendingFact retains safe owner metadata after closing the query rows before outbox writes.
	// pendingFact сохраняет safe owner metadata после закрытия rows до записи outbox.
	type pendingFact struct{ kind, ref, id, node, project, root, state, rootState, nodeState, organizationRef, actorRef, actorName string }
	var facts []pendingFact
	for rows.Next() {
		var fact pendingFact
		if rows.Scan(&fact.kind, &fact.ref, &fact.id, &fact.node, &fact.project, &fact.root, &fact.state, &fact.rootState, &fact.nodeState, &fact.organizationRef, &fact.actorRef, &fact.actorName) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		facts = append(facts, fact)
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return errs.ErrUnavailable
	}
	for _, fact := range facts {
		current := scope{organizationID: organizationID, organizationRef: fact.organizationRef, actorID: actorID, actorRef: fact.actorRef, actorName: fact.actorName, correlationRef: correlationRef}
		if fact.kind == "GATE" {
			if _, err := repository.emitRunEvent(ctx, tx, current, fact.project, fact.root, fact.ref, "OWNER_GATE_RESOLVED", fact.node, "", fact.ref, "", "i18n:OWNER_GATE_CANCELLED", fact.rootState, "CANCELLED"); err != nil {
				return err
			}
			continue
		}
		if fact.state == "FAILED" {
			if _, err := repository.scheduleIntegrationContinuation(ctx, tx, current, fact.id, fact.project); err != nil {
				return err
			}
		}
		if _, err := repository.emitIntegrationActionEvent(ctx, tx, current, fact.project, fact.root, fact.ref, fact.node, fact.state); err != nil {
			return err
		}
	}
	return nil
}

// invalidateSSHComposeHealthTests revokes stale read-test leases and publishes one exact-version connection event per reset.
// invalidateSSHComposeHealthTests отзывает устаревшие read-test leases и публикует exact-version connection event для каждого reset.
func (repository *Repository) invalidateSSHComposeHealthTests(ctx context.Context, tx pgx.Tx, organizationID, actorID, correlationRef, connectionRef, secretRef string) error {
	if connectionRef == "" && secretRef == "" {
		return nil
	}
	rows, err := tx.Query(ctx, querySSHComposeInvalidateHealth, pgx.StrictNamedArgs{"organization_id": organizationID, "actor_id": actorID, "connection_ref": connectionRef, "secret_ref": secretRef})
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	// resetFact retains only public metadata after query rows close and before outbox writes.
	// resetFact сохраняет лишь public metadata после закрытия query rows до записи outbox.
	type resetFact struct {
		ref, state, organizationRef, actorRef, actorName string
		version                                          int64
	}
	var facts []resetFact
	for rows.Next() {
		var fact resetFact
		if rows.Scan(&fact.ref, &fact.version, &fact.state, &fact.organizationRef, &fact.actorRef, &fact.actorName) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		facts = append(facts, fact)
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return errs.ErrUnavailable
	}
	for _, fact := range facts {
		current := scope{organizationID: organizationID, organizationRef: fact.organizationRef, actorID: actorID, actorRef: fact.actorRef, actorName: fact.actorName, correlationRef: correlationRef}
		if err := repository.emitPlatformEventSnapshot(ctx, tx, current, "INTEGRATION_CONNECTION_CHANGED", "", fact.ref, "i18n:INTEGRATION_CONNECTION_UPDATED", fact.version, fact.state); err != nil {
			return err
		}
	}
	return nil
}
