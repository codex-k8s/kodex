package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryRepositoryBundleWorkGuard takes nonblocking root/invocation barriers before a work mutation; recovery may observe terminal roots but cannot reopen them.
// queryRepositoryBundleWorkGuard берёт nonblocking root/invocation barriers до work mutation; recovery может наблюдать terminal roots, но не открывает их вновь.
//
//go:embed sql/repository_bundle__work_guard.sql
var queryRepositoryBundleWorkGuard string

// queryRepositoryBundleCandidates selects one due proposal with SKIP LOCKED, including expired pre-effect claims and bounded read-only recovery.
// queryRepositoryBundleCandidates выбирает одну due proposal через SKIP LOCKED, включая expired pre-effect claims и bounded read-only recovery.
//
//go:embed sql/repository_bundle__candidates.sql
var queryRepositoryBundleCandidates string

// repositoryBundleTerminal recognizes only durable terminal states; UNKNOWN remains a separate read-only reconciliation state.
// repositoryBundleTerminal распознаёт лишь durable terminal states; UNKNOWN остаётся отдельным read-only reconciliation state.
func repositoryBundleTerminal(state string) bool {
	switch state {
	case "SUCCEEDED", "REJECTED", "CANCELLED", "EXPIRED", "FAILED":
		return true
	default:
		return false
	}
}

// repositoryBundleLeaseMatches binds every work action to the currently issued owner tuple and authoritative expiry; a caller cannot extend it by changing its timestamp.
// repositoryBundleLeaseMatches связывает work action с current owner tuple и authoritative expiry; caller не продлевает её изменением timestamp.
func repositoryBundleLeaseMatches(row repositoryBundleRow, input entity.RepositoryBundleLease, now time.Time) bool {
	return !repositoryBundleTerminal(row.proposal.State) && input.ProposalRef == row.proposal.Ref && input.Attempt == row.lease.Attempt &&
		input.Generation == row.lease.Generation && input.Claimant == row.lease.Claimant && input.Fence != "" && input.Fence == row.lease.Fence &&
		!input.ExpiresAt.IsZero() && !input.ExpiresAt.After(row.lease.ExpiresAt) && now.Before(row.lease.ExpiresAt)
}

// repositoryBundleWorkGuard follows the native root/invocation lock order without waiting on competing graph closure; storage contention is temporary, never a false terminal or new authority.
// repositoryBundleWorkGuard следует native root/invocation lock order без ожидания competing graph closure; storage contention временна и не становится ложным terminal либо новой authority.
func repositoryBundleWorkGuard(ctx context.Context, tx pgx.Tx, current scope, ref string) (string, string, int64, error) {
	var rootState, invocationState string
	var generation int64
	err := tx.QueryRow(ctx, queryRepositoryBundleWorkGuard, pgx.StrictNamedArgs{"organization_id": current.organizationID, "proposal_ref": ref}).Scan(&rootState, &invocationState, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", 0, errs.ErrNotFound
	}
	if err != nil {
		return "", "", 0, errs.ErrUnavailable
	}
	return rootState, invocationState, generation, nil
}

// repositoryBundleWork projects only the frozen definition, private credential descriptor and manifest; archive bytes require a second exact private work read.
// repositoryBundleWork проецирует лишь frozen definition, private credential descriptor и manifest; archive bytes требуют второго exact private work read.
func repositoryBundleWork(row repositoryBundleRow) entity.RepositoryBundlePublicationWork {
	mode := "EXECUTE"
	if row.proposal.State == "UNKNOWN_OUTCOME" {
		mode = "RECOVER_READ_ONLY"
	}
	deadline := row.lease.ExpiresAt
	if mode == "EXECUTE" && row.deadline != nil && row.deadline.Before(deadline) {
		deadline = *row.deadline
	}
	snapshot := row.snapshot
	return entity.RepositoryBundlePublicationWork{Lease: row.lease, Proposal: row.proposal, Mode: mode, DefinitionKey: snapshot.DefinitionKey, DefinitionVersion: snapshot.DefinitionVersion,
		DefinitionDigest: snapshot.DefinitionDigest, DefinitionPackage: snapshot.DefinitionPackage, PublicConfiguration: snapshot.PublicConfiguration, Credential: snapshot.Credential,
		ResourceScope: snapshot.ResourceScope, ResourceScopeDigest: snapshot.ResourceScopeDigest, EffectKey: snapshot.EffectKey, InputDigest: snapshot.InputDigest, Manifest: append([]byte(nil), row.manifest...), CommitTime: row.proposal.CreatedAt, Deadline: deadline, EffectStartedAt: row.started}
}
