package platform

import (
	"context"
	_ "embed"
	"sort"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// querySSHExecutionInvalidate closes exact SSH credential dependencies, their original Gates, notification leases and invocation leases while retaining uncertain effects and immutable receipts.
// querySSHExecutionInvalidate закрывает exact SSH credential dependencies, original Gates, notification leases и invocation leases, сохраняя uncertain effects и immutable receipts.
//
//go:embed sql/ssh_execution__invalidate.sql
var querySSHExecutionInvalidate string

// closeSSHExecutionDependenciesTx shares the canonical continuation/event consumers after one set-based closing update; an exact old revision filter never revokes its successor or declares an attempted command absent.
// closeSSHExecutionDependenciesTx использует canonical continuation/event consumers после одного set-based closing update; exact old revision filter не отзывает successor и не объявляет attempted command отсутствующим.
func (repository *Repository) closeSSHExecutionDependenciesTx(ctx context.Context, tx pgx.Tx, current scope, connectionRef, bindingID, identityID, grantRef string) error {
	rows, err := tx.Query(ctx, querySSHExecutionInvalidate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID,
		"connection_ref": connectionRef, "binding_id": bindingID, "identity_id": identityID, "grant_ref": grantRef})
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	// closingFact carries only stored owner lineage after rows close; no caller-supplied actor or aggregate is promoted to event authority.
	// closingFact содержит лишь stored owner lineage после закрытия rows; caller-supplied actor либо aggregate не становится event authority.
	type closingFact struct{ kind, ref, id, node, project, root, state, rootState, nodeState, organizationRef, actorRef, actorName string }
	facts := []closingFact{}
	for rows.Next() {
		var fact closingFact
		if rows.Scan(&fact.kind, &fact.ref, &fact.id, &fact.node, &fact.project, &fact.root, &fact.state, &fact.rootState, &fact.nodeState, &fact.organizationRef, &fact.actorRef, &fact.actorName) != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		facts = append(facts, fact)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	// Serialize root wait changes with ordinary Gate resolution; an unrelated open Gate keeps its wait.
	// Сериализуем root wait с ordinary Gate resolution; unrelated OPEN Gate сохраняет ожидание.
	rootStates := make(map[string]string)
	var rootIDs []string
	closedGateRoots := make(map[string]bool)
	for _, fact := range facts {
		if fact.kind == "GATE" {
			closedGateRoots[fact.root] = true
		}
		if _, checked := rootStates[fact.root]; !checked {
			rootStates[fact.root] = ""
			rootIDs = append(rootIDs, fact.root)
		}
	}
	sort.Strings(rootIDs)
	for _, rootID := range rootIDs {
		state, err := lockOwnerGateRootTx(ctx, tx, rootID)
		if err != nil {
			return err
		}
		if state == "WAITING_HUMAN" && closedGateRoots[rootID] {
			// The canonical resume query preserves any other OPEN Gate; only our exact closed Gate can remove this wait.
			// Canonical resume query сохраняет любой другой OPEN Gate; ожидание снимается лишь после нашего exact closed Gate.
			tag, err := tx.Exec(ctx, queryRuntimeIntegrationResumeRootRun, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": rootID})
			if err != nil {
				return errs.ErrUnavailable
			}
			if tag.RowsAffected() == 1 {
				state = "RUNNING"
			}
		}
		rootStates[rootID] = state
	}
	for _, fact := range facts {
		source := current
		source.organizationRef = fact.organizationRef
		source.actorRef = fact.actorRef
		source.actorName = fact.actorName
		if fact.kind == "GATE" {
			if _, err := repository.emitRunEvent(ctx, tx, source, fact.project, fact.root, fact.ref, "OWNER_GATE_RESOLVED", fact.node, "", fact.ref, "", "i18n:OWNER_GATE_CANCELLED", rootStates[fact.root], "CANCELLED"); err != nil {
				return err
			}
			continue
		}
		if fact.kind != "INVOCATION" {
			return errs.ErrUnavailable
		}
		if fact.state == "CANCELLED" {
			if _, err := repository.scheduleIntegrationContinuation(ctx, tx, source, fact.id, fact.project); err != nil {
				return err
			}
			// Cancellation has no integration action outcome event; the canonical continuation and exact owner result reader retain CANCELLED without a FAILED/UNKNOWN presentation conversion.
			// Отмена не создаёт integration action outcome event; canonical continuation и exact owner result reader сохраняют CANCELLED без преобразования presentation в FAILED/UNKNOWN.
			continue
		}
		if _, err := repository.emitIntegrationActionEvent(ctx, tx, source, fact.project, fact.root, fact.ref, fact.node, fact.state); err != nil {
			return err
		}
	}
	return nil
}
