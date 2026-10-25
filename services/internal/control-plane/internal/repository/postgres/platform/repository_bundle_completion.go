package platform

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// CompleteRepositoryBundle stores a bounded exact remote readback once. Receipt replay survives lost ACK without requiring a live lease; mismatched candidates or epochs never become success.
// CompleteRepositoryBundle однократно сохраняет bounded exact remote readback. Receipt replay переживает lost ACK без live lease; mismatched candidate либо epoch не становятся success.
func (repository *Repository) CompleteRepositoryBundle(ctx context.Context, principal value.Principal, input port.RepositoryBundleEffect) (entity.RepositoryBundlePublication, error) {
	current, tx, row, rootState, invocationState, err := repository.openRepositoryBundleWork(ctx, principal, "complete", input.Lease.ProposalRef)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	defer rollbackRepositoryBundle(ctx, tx)
	if !validRepositoryBundleEffect(row, input) {
		return entity.RepositoryBundlePublication{}, errs.ErrConflict
	}
	digest := repositoryBundleEffectDigest(input)
	if receipt, exists := row.receipts["COMPLETE"]; exists {
		if receipt.Digest != digest {
			return entity.RepositoryBundlePublication{}, errs.ErrConflict
		}
		if tx.Commit(ctx) != nil {
			return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
		}
		return receipt.Proposal, nil
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
	}
	if !repositoryBundleLeaseMatches(row, input.Lease, now) {
		return entity.RepositoryBundlePublication{}, errs.ErrForbidden
	}
	if row.started == nil || row.proposal.State != "EFFECT_STARTED" && row.proposal.State != "UNKNOWN_OUTCOME" || row.proposal.CandidateCommitSHA != input.CandidateCommitSHA || row.proposal.CandidateTreeSHA != input.CandidateTreeSHA {
		return entity.RepositoryBundlePublication{}, errs.ErrConflict
	}
	// Only past fact is accepted after Begin; revoked write grants cannot erase it, and no authority check here can authorize another push.
	// После Begin принимается лишь прошлый факт; revoke write grant его не стирает, и authority check здесь не разрешает новый push.
	if row.proposal.FailureCode != "AUTHORITY_CHANGED" && (rootState == "RUNNING" || rootState == "WAITING_HUMAN") && invocationState == "RUNNING" && row.invocationLeaseRef != "" {
		result := string(asJSON(map[string]any{"commit_sha": input.CandidateCommitSHA, "tree_sha": input.CandidateTreeSHA, "proposal_branch": row.proposal.ProposalBranch,
			"manifest_sha256": row.proposal.Manifest.SHA256, "file_count": row.proposal.FileCount, "total_bytes": row.proposal.TotalBytes}))
		_, err = repository.completeIntegrationInvocationTx(ctx, tx, current, command.Command{Principal: principal, Payload: command.IntegrationInvocationInput{
			InvocationRef: row.proposal.InvocationRef, LeaseRef: row.invocationLeaseRef, Fence: row.lease.Fence, Generation: row.lease.Generation,
			Success: true, EffectKey: row.snapshot.EffectKey, InputDigest: row.snapshot.InputDigest, ProviderEffectRef: input.CandidateCommitSHA,
			ResultSummary: result, ResponseDigest: repositorybundle.Digest([]byte(result)),
		}}, true)
		if err != nil {
			return entity.RepositoryBundlePublication{}, err
		}
	}
	row.proposal.State, row.proposal.FailureCode, row.proposal.ConfirmedAt, row.completedAt = "SUCCEEDED", "", &now, &now
	row.lease.Claimant, row.lease.Fence, row.lease.ExpiresAt, row.invocationLeaseRef = "", "", time.Time{}, ""
	if err := saveRepositoryBundle(ctx, tx, current, &row); err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	if _, err := repository.scheduleIntegrationContinuation(ctx, tx, current, row.invocationID, row.projectID); err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	row, err = lockRepositoryBundle(ctx, tx, current, row.proposal.Ref)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	receipt := row.proposal
	receipt.Version++
	row.receipts["COMPLETE"] = repositoryBundleReceipt{Digest: digest, Proposal: receipt}
	if err := saveRepositoryBundle(ctx, tx, current, &row); err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
	}
	return row.proposal, nil
}

// FailRepositoryBundle preserves every begun effect as UNKNOWN and closes only a known pre-Begin failure. Retrying this report never issues a second write or removes source artifacts.
// FailRepositoryBundle сохраняет любой begun effect как UNKNOWN и закрывает лишь известный pre-Begin failure. Повтор отчёта не выдаёт второй write и не удаляет source artifacts.
func (repository *Repository) FailRepositoryBundle(ctx context.Context, principal value.Principal, input entity.RepositoryBundleLease, code string) (entity.RepositoryBundlePublication, error) {
	current, tx, row, _, _, err := repository.openRepositoryBundleWork(ctx, principal, "fail", input.ProposalRef)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	defer rollbackRepositoryBundle(ctx, tx)
	if !validRepositoryBundleFailure(code) {
		return entity.RepositoryBundlePublication{}, errs.ErrInvalid
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
	}
	if !repositoryBundleLeaseMatches(row, input, now) {
		return entity.RepositoryBundlePublication{}, errs.ErrForbidden
	}
	if row.started == nil {
		if err := repository.finishRepositoryBundleUnstarted(ctx, tx, current, &row, code, code == "DEADLINE_EXCEEDED", now); err != nil {
			return entity.RepositoryBundlePublication{}, err
		}
	} else {
		if row.invocationLeaseRef != "" {
			if _, err := tx.Exec(ctx, queryRepositoryBundleClearInvocationLease, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": row.invocationID,
				"lease_ref": row.invocationLeaseRef, "fence_digest": repositorybundle.Digest([]byte(input.Fence)), "generation": input.Generation}); err != nil {
				return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
			}
		}
		row.proposal.State, row.proposal.FailureCode = "UNKNOWN_OUTCOME", "OUTCOME_UNCONFIRMED"
		row.lease.Claimant, row.lease.Fence, row.lease.ExpiresAt, row.invocationLeaseRef = "", "", time.Time{}, ""
		if err := saveRepositoryBundle(ctx, tx, current, &row); err != nil {
			return entity.RepositoryBundlePublication{}, err
		}
	}
	if tx.Commit(ctx) != nil {
		return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
	}
	return row.proposal, nil
}

// validRepositoryBundleFailure accepts only closed diagnostic classes; provider text, object bytes and secrets cannot enter owner audit through this field.
// validRepositoryBundleFailure принимает лишь закрытые diagnostic classes; provider text, object bytes и secrets через поле в owner audit не попадают.
func validRepositoryBundleFailure(code string) bool {
	switch code {
	case "UNAVAILABLE", "AUTHORITY_CHANGED", "CONTENT_INVALID", "BASE_INVALID", "DEADLINE_EXCEEDED", "BRANCH_CONFLICT", "OUTCOME_UNCONFIRMED":
		return true
	default:
		return false
	}
}

// ReadRepositoryBundle reads only the pinned existing S3 version for a current pre-Begin work epoch, then rechecks fresh owner eligibility before returning bytes. UNKNOWN recovery uses manifest metadata and has no source-body read permission.
// ReadRepositoryBundle читает лишь pinned existing S3 version для current pre-Begin work epoch, затем повторяет fresh owner eligibility до выдачи bytes. UNKNOWN recovery использует manifest metadata и не получает source-body read permission.
func (repository *Repository) ReadRepositoryBundle(ctx context.Context, principal value.Principal, input entity.RepositoryBundleLease) (port.ArtifactDownload, error) {
	pin, source, err := repository.checkRepositoryBundleContentRead(ctx, principal, input)
	if err != nil {
		return port.ArtifactDownload{}, err
	}
	object, err := repository.objects.Get(ctx, source.Receipt.Key, source.Receipt.VersionID)
	if err != nil {
		return port.ArtifactDownload{}, mapObjectStorageError(err)
	}
	defer func() { _ = object.Body.Close() }()
	if object.VersionID != source.Receipt.VersionID || object.ETag != source.Receipt.ETag || object.Digest != source.Receipt.Digest || object.SizeBytes != source.Receipt.SizeBytes {
		return port.ArtifactDownload{}, errs.ErrConflict
	}
	raw, err := io.ReadAll(io.LimitReader(object.Body, pin.Size+1))
	if err != nil || int64(len(raw)) != pin.Size || repositorybundle.Digest(raw) != pin.SHA256 {
		clear(raw)
		return port.ArtifactDownload{}, errs.ErrConflict
	}
	if err := object.Body.Close(); err != nil {
		clear(raw)
		return port.ArtifactDownload{}, errs.ErrUnavailable
	}
	currentPin, currentSource, err := repository.checkRepositoryBundleContentRead(ctx, principal, input)
	if err != nil {
		clear(raw)
		return port.ArtifactDownload{}, err
	}
	if currentPin != pin || currentSource != source {
		clear(raw)
		return port.ArtifactDownload{}, errs.ErrConflict
	}
	return port.ArtifactDownload{Artifact: entity.Artifact{Ref: pin.Ref, Revision: pin.Revision, Version: pin.Version, Digest: "sha256:" + pin.SHA256, SizeBytes: pin.Size, ScanState: "CLEAN"}, Reader: &repositoryBundleReader{Reader: bytes.NewReader(raw), owned: raw}}, nil
}

// checkRepositoryBundleContentRead validates the exact active epoch, fresh grants/CLEAN visibility and unchanged immutable source receipt under owner locks. It performs no external object mutation and cannot authorize a post-Begin download.
// checkRepositoryBundleContentRead проверяет exact active epoch, fresh grants/CLEAN visibility и прежнюю immutable source receipt под owner locks. External object mutation не выполняется, post-Begin download не разрешается.
func (repository *Repository) checkRepositoryBundleContentRead(ctx context.Context, principal value.Principal, input entity.RepositoryBundleLease) (entity.RepositoryBundleArtifactPin, repositoryBundleSourcePin, error) {
	current, tx, row, _, _, err := repository.openRepositoryBundleWork(ctx, principal, "content.read", input.ProposalRef)
	if err != nil {
		return entity.RepositoryBundleArtifactPin{}, repositoryBundleSourcePin{}, err
	}
	defer rollbackRepositoryBundle(ctx, tx)
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.RepositoryBundleArtifactPin{}, repositoryBundleSourcePin{}, errs.ErrUnavailable
	}
	if !repositoryBundleLeaseMatches(row, input, now) || row.proposal.State != "CLAIMED" || row.started != nil {
		return entity.RepositoryBundleArtifactPin{}, repositoryBundleSourcePin{}, errs.ErrForbidden
	}
	if err := repository.requireRepositoryBundleAuthority(ctx, tx, current, row, true); err != nil {
		return entity.RepositoryBundleArtifactPin{}, repositoryBundleSourcePin{}, err
	}
	source, err := resolveRepositoryBundleSource(ctx, tx, current, row.projectID, row.proposal.Archive)
	if err != nil {
		return entity.RepositoryBundleArtifactPin{}, repositoryBundleSourcePin{}, err
	}
	if source != row.snapshot.ArchiveSource {
		return entity.RepositoryBundleArtifactPin{}, repositoryBundleSourcePin{}, errs.ErrConflict
	}
	if err := repositoryBundleAudit(ctx, tx, current, row.projectID, row.actorID, row.proposal.Ref, "CONTENT_READ"); err != nil {
		return entity.RepositoryBundleArtifactPin{}, repositoryBundleSourcePin{}, err
	}
	if tx.Commit(ctx) != nil {
		return entity.RepositoryBundleArtifactPin{}, repositoryBundleSourcePin{}, errs.ErrUnavailable
	}
	return row.proposal.Archive, source, nil
}

// repositoryBundleReader owns an in-memory immutable source copy and clears it on close; it never deletes the original CLEAN artifact or any committed ledger bytes.
// repositoryBundleReader владеет in-memory immutable source copy и очищает её при close; original CLEAN artifact и committed ledger bytes не удаляет.
type repositoryBundleReader struct {
	*bytes.Reader
	owned []byte
}

// Close clears only this transient memory copy and prevents subsequent reads through the returned reader.
// Close очищает лишь transient memory copy и запрещает последующее чтение через выданный reader.
func (reader *repositoryBundleReader) Close() error {
	clear(reader.owned)
	reader.owned = nil
	reader.Reader = bytes.NewReader(nil)
	return nil
}

var _ io.ReadCloser = (*repositoryBundleReader)(nil)
var _ port.RepositoryBundleOwner = (*Repository)(nil)
