package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryBrokerIdentitySSHFact grants attachment credit only to exact completed private material and a fresh database-derived grant deadline, excluding damaged acknowledgements and quarantined revisions.
// queryBrokerIdentitySSHFact даёт attachment credit лишь exact completed private material и fresh database-derived grant deadline, исключая damaged acknowledgements и quarantined revisions.
//
//go:embed sql/broker_identity__ssh_fact.sql
var queryBrokerIdentitySSHFact string

// querySSHSetupCancelDependency closes setup and attempt authority for an exact current credential owner while retaining immutable material, classified UNKNOWN and unresolved write history.
// querySSHSetupCancelDependency закрывает setup и attempt authority exact current credential owner, сохраняя immutable material, classified UNKNOWN и unresolved write history.
//
//go:embed sql/ssh_setup__cancel_dependency.sql
var querySSHSetupCancelDependency string

// readSSHIdentityAttachmentFactTx returns the original bounded grant deadline; it neither renews a setup nor treats known Kubernetes material as an SSH authentication or operator verification result.
// readSSHIdentityAttachmentFactTx возвращает original bounded grant deadline; setup не продлевается, known Kubernetes material не считается SSH authentication либо operator verification.
func readSSHIdentityAttachmentFactTx(ctx context.Context, tx pgx.Tx, identityID, organizationID, connectionID string) (*time.Time, error) {
	var deadline *time.Time
	if err := tx.QueryRow(ctx, queryBrokerIdentitySSHFact, pgx.StrictNamedArgs{"identity_id": identityID, "organization_id": organizationID, "connection_id": connectionID}).Scan(&deadline); err != nil {
		return nil, errs.ErrUnavailable
	}
	if deadline == nil {
		return nil, errs.ErrConflict
	}
	return deadline, nil
}

// cancelSSHIdentitySetupsTx is a closing-only dependency transition under the existing Connection/binding lock; it never joins an unknown external write or authorizes deleting its material.
// cancelSSHIdentitySetupsTx является closing-only dependency transition под existing Connection/binding lock; unknown external write не объявляется joined, удаление material не разрешается.
func cancelSSHIdentitySetupsTx(ctx context.Context, tx pgx.Tx, organizationID, connectionRef, bindingID string) error {
	if _, err := tx.Exec(ctx, querySSHSetupCancelDependency, pgx.StrictNamedArgs{"organization_id": organizationID, "connection_ref": connectionRef, "binding_id": bindingID}); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// cancelSSHIdentityDependenciesTx closes the original setup and every dependent invocation in the same owner transaction; source mutation never leaves a new execution claim eligible.
// cancelSSHIdentityDependenciesTx закрывает original setup и все dependent invocations в той же owner transaction; source mutation не оставляет new execution claim eligible.
func (repository *Repository) cancelSSHIdentityDependenciesTx(ctx context.Context, tx pgx.Tx, current scope, connectionRef, bindingID string) error {
	if err := cancelSSHIdentitySetupsTx(ctx, tx, current.organizationID, connectionRef, bindingID); err != nil {
		return err
	}
	return repository.closeSSHExecutionDependenciesTx(ctx, tx, current, connectionRef, bindingID, "", "")
}
