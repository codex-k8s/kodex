package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRepositoryBundleGate distinguishes the exact typed bundle gate from other integration producers without inventing a standalone approval authority.
// queryRepositoryBundleGate отличает exact typed bundle Gate от других integration producers без отдельной выдуманной approval authority.
//
//go:embed sql/repository_bundle__gate.sql
var queryRepositoryBundleGate string

// queryRepositoryBundleGateOperation reads only the established invocation table so unrelated legacy gate decisions do not depend on the new bundle table during schema-first delivery.
// queryRepositoryBundleGateOperation читает лишь прежнюю invocation table, поэтому посторонние legacy Gate decisions не зависят от новой bundle table при schema-first delivery.
//
//go:embed sql/repository_bundle__gate_operation.sql
var queryRepositoryBundleGateOperation string

// queryRepositoryBundleAuthority retains exact current root/grant/connection/credential metadata without waiting on an inverted owner lock; payload identifiers cannot replace this rule.
// queryRepositoryBundleAuthority удерживает exact current root/grant/connection/credential metadata без ожидания обратного owner lock; payload identifiers не заменяют это правило.
//
//go:embed sql/repository_bundle__authority.sql
var queryRepositoryBundleAuthority string

// requireRepositoryBundleAuthority validates the frozen publication against current native authority before approval, claim and Begin. Read-only recovery deliberately does not obtain new write authority through it.
// requireRepositoryBundleAuthority сверяет frozen publication с current native authority до approval, claim и Begin. Read-only recovery намеренно не получает через неё новую write authority.
func (repository *Repository) requireRepositoryBundleAuthority(ctx context.Context, tx pgx.Tx, current scope, row repositoryBundleRow, approved bool) error {
	var actorRef string
	err := tx.QueryRow(ctx, queryRepositoryBundleAuthority, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": row.id, "approved": approved,
		"connection_version": row.proposal.ConnectionVersion, "definition_version": row.snapshot.DefinitionVersion, "definition_digest": row.snapshot.DefinitionDigest,
		"grant_version": row.proposal.GrantVersion, "grant_ref": row.proposal.GrantRef}).Scan(&actorRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrForbidden
	}
	if err != nil {
		return errs.ErrUnavailable
	}
	initiator := current
	initiator.actorID, initiator.actorRef = row.actorID, actorRef
	if repository.requireAccess(ctx, tx, initiator, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: row.proposal.ConnectionRef}) != nil {
		return errs.ErrForbidden
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, row.proposal.ConnectionRef, row.snapshot.DefinitionKey, row.snapshot.DefinitionVersion, row.snapshot.DefinitionDigest)
	if err != nil {
		return err
	}
	capability, found := definition.Capability(repositoryBundleOperation)
	if !found || capability.Operation != repositoryBundleOperation || capability.Risk != "WRITE" || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" ||
		!definition.HasNetworkDestination(integrationpackage.NetworkDestination{Key: "github_git", Source: "STATIC", Hostname: "github.com", Port: 443, TLS: "REQUIRED"}) {
		return errs.ErrForbidden
	}
	return requireRepositoryBundleArtifactEligibility(ctx, tx, current, row.projectID, row.actorID, row.proposal)
}

// applyRepositoryBundleGateDecision participates in the existing native ResolveOwnerGate transaction after authority/OCC/decision checks. Reject/cancel close the proposal; approve repeats complete manifest eligibility before queueing any work.
// applyRepositoryBundleGateDecision участвует в прежней native ResolveOwnerGate transaction после authority/OCC/decision checks. Reject/cancel закрывают proposal; approve повторяет complete manifest eligibility до постановки work.
func (repository *Repository) applyRepositoryBundleGateDecision(ctx context.Context, tx pgx.Tx, current scope, gateID, invocationID, rootRunID, projectID, decision string) error {
	var operation, ref string
	pins := pgx.StrictNamedArgs{"organization_id": current.organizationID, "gate_id": gateID, "invocation_id": invocationID}
	err := tx.QueryRow(ctx, queryRepositoryBundleGateOperation, pins).Scan(&operation)
	if err != nil {
		return errs.ErrUnavailable
	}
	if operation != repositoryBundleOperation {
		return nil
	}
	if tx.QueryRow(ctx, queryRepositoryBundleGate, pins).Scan(&operation, &ref) != nil {
		return errs.ErrUnavailable
	}
	if ref == "" {
		return errs.ErrUnavailable
	}
	row, err := lockRepositoryBundle(ctx, tx, current, ref)
	if err != nil {
		return err
	}
	if row.gateID != gateID || row.invocationID != invocationID || row.rootRunID != rootRunID || row.projectID != projectID || row.proposal.State != "WAITING_APPROVAL" {
		return errs.ErrConflict
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return errs.ErrUnavailable
	}
	switch decision {
	case "APPROVE":
		if !now.Before(row.proposal.ExpiresAt) {
			return errs.ErrConflict
		}
		if err := repository.requireRepositoryBundleAuthority(ctx, tx, current, row, false); err != nil {
			return err
		}
		if err := requireRepositoryBundleArtifactEligibility(ctx, tx, current, row.projectID, current.actorID, row.proposal); err != nil {
			return err
		}
		deadline := now.Add(15 * time.Minute)
		row.proposal.State, row.approverID, row.approvedAt, row.deadline = "QUEUED", current.actorID, &now, &deadline
	case "REJECT":
		row.proposal.State, row.completedAt = "REJECTED", &now
	case "CANCEL":
		row.proposal.State, row.completedAt = "CANCELLED", &now
	default:
		return errs.ErrForbidden
	}
	return saveRepositoryBundle(ctx, tx, current, &row)
}
