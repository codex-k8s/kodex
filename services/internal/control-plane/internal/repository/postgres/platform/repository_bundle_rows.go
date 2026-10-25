package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// repositoryBundleOperation shares the exact typed capability discriminator with the byte-validation contract; it is not an authorization grant.
// repositoryBundleOperation разделяет exact typed capability discriminator с byte-validation контрактом; authorization grant в нём нет.
const repositoryBundleOperation = repositorybundle.Operation

// repositoryBundleSummary is the single safe localized owner-audit message; source bytes and archive names are excluded.
// repositoryBundleSummary — единое безопасное локализованное сообщение owner audit; source bytes и имена архива исключены.
const repositoryBundleSummary = "i18n:REPOSITORY_BUNDLE_CHANGED"

// queryRepositoryBundleOrigin resolves and locks one exact live execution/grant origin before native invocation creation.
// queryRepositoryBundleOrigin разрешает и блокирует exact live execution/grant origin до native invocation creation.
//
//go:embed sql/repository_bundle__origin.sql
var queryRepositoryBundleOrigin string

// queryRepositoryBundleArtifactEligibility rechecks fresh actor/project/CLEAN eligibility while retaining the artifact metadata row.
// queryRepositoryBundleArtifactEligibility повторно проверяет свежие actor/project/CLEAN eligibility, удерживая artifact metadata row.
//
//go:embed sql/repository_bundle__artifact_eligibility.sql
var queryRepositoryBundleArtifactEligibility string

// queryRepositoryBundleIdentity resolves the native invocation and its own gate under the prepared origin tuple.
// queryRepositoryBundleIdentity разрешает native invocation и его Gate в подготовленном origin tuple.
//
//go:embed sql/repository_bundle__identity.sql
var queryRepositoryBundleIdentity string

// queryRepositoryBundleExisting locates only the same immutable execution/key idempotency scope, without creating authority from its result.
// queryRepositoryBundleExisting находит лишь ту же immutable execution/key idempotency scope; результат не создаёт authority.
//
//go:embed sql/repository_bundle__existing.sql
var queryRepositoryBundleExisting string

// queryRepositoryBundleInsert freezes the owner-resolved origin, native gate, exact source revision pins and public proposal in one transaction.
// queryRepositoryBundleInsert закрепляет owner-resolved origin, native Gate, exact source revision pins и public proposal одной транзакцией.
//
//go:embed sql/repository_bundle__insert.sql
var queryRepositoryBundleInsert string

// queryRepositoryBundleLock acquires the proposal owner lock with explicit columns; archive bytes require the separately fenced private read.
// queryRepositoryBundleLock берёт proposal owner lock с явными колонками; archive bytes требуют отдельно fenced private read.
//
//go:embed sql/repository_bundle__lock.sql
var queryRepositoryBundleLock string

// queryRepositoryBundleSave performs one OCC-fenced owner update; immutable source and receipt append-only rules remain database-enforced.
// queryRepositoryBundleSave выполняет один OCC-fenced owner update; immutable source и append-only receipts охраняются БД.
//
//go:embed sql/repository_bundle__save.sql
var queryRepositoryBundleSave string

// queryRepositoryBundleInvocationOperation fences generic completion away from the specialized bundle kind using authoritative invocation state.
// queryRepositoryBundleInvocationOperation закрывает generic completion для specialized bundle kind по authoritative invocation state.
//
//go:embed sql/repository_bundle__invocation_operation.sql
var queryRepositoryBundleInvocationOperation string

// queryRepositoryBundleRecipient limits this specialized capability to ordinary project employees; assistant and workflow catalogs cannot advertise an unimplemented producer route.
// queryRepositoryBundleRecipient ограничивает specialized capability обычными project employees; assistant и workflow catalogs не показывают отсутствующий producer route.
//
//go:embed sql/repository_bundle__recipient.sql
var queryRepositoryBundleRecipient string

// repositoryBundleRecipientEligible shares the exact ordinary-agent metadata policy between grant admission and discovery, while execution still resolves its own fresh lease/origin.
// repositoryBundleRecipientEligible разделяет exact ordinary-agent metadata policy между grant admission и discovery; execution отдельно разрешает fresh lease/origin.
func repositoryBundleRecipientEligible(ctx context.Context, tx pgx.Tx, current scope, kind, ref string) (bool, error) {
	if kind != "AGENT" {
		return false, nil
	}
	var eligible bool
	if tx.QueryRow(ctx, queryRepositoryBundleRecipient, pgx.StrictNamedArgs{"organization_id": current.organizationID, "agent_ref": ref}).Scan(&eligible) != nil {
		return false, errs.ErrUnavailable
	}
	return eligible, nil
}

// repositoryBundleSnapshot binds the complete immutable source/package/credential metadata to its safe public proposal; it contains no secret value.
// repositoryBundleSnapshot связывает полные immutable source/package/credential metadata с безопасной public proposal; secret values в нём нет.
type repositoryBundleSnapshot struct {
	SourcePolicy                                       string
	ArchiveSource, ManifestSource                      repositoryBundleSourcePin
	FileReceipts                                       []repositoryBundleFileReceipt
	ResourceScope                                      map[string]string
	ResourceScopeDigest                                string
	Proposal                                           entity.RepositoryBundlePublication
	DefinitionKey, DefinitionVersion, DefinitionDigest string
	DefinitionPackage                                  []byte
	PublicConfiguration                                map[string]any
	Credential                                         entity.IntegrationCredentialRevision
	InputDigest, EffectKey                             string
}

// repositoryBundleReceipt preserves the exact request digest and safe owner projection for one work action across lost acknowledgements and process replacement.
// repositoryBundleReceipt сохраняет exact request digest и safe owner projection одной work action при lost ACK и process replacement.
type repositoryBundleReceipt struct {
	Digest   string
	Proposal entity.RepositoryBundlePublication
}

// repositoryBundleRow keeps locked relational identity, mutable work state and verified immutable snapshot separate; callers own commit and external effects remain forbidden here.
// repositoryBundleRow разделяет locked relational identity, mutable work state и verified immutable snapshot; commit принадлежит caller, внешние effects здесь запрещены.
type repositoryBundleRow struct {
	id, projectID, actorID, rootRunID, runID   string
	connectionID, grantID, credentialID        string
	invocationID, gateID, approverID           string
	invocationLeaseRef                         string
	snapshot                                   repositoryBundleSnapshot
	proposal                                   entity.RepositoryBundlePublication
	lease                                      entity.RepositoryBundleLease
	manifest                                   []byte
	approvedAt, deadline, started, completedAt *time.Time
	receipts                                   map[string]repositoryBundleReceipt
}

// lockRepositoryBundle verifies the complete frozen snapshot and manifest after the scoped owner lock; corrupt data never turns into an empty proposal or implicit latest pins.
// lockRepositoryBundle проверяет полный frozen snapshot и manifest после scoped owner lock; corrupt data не превращаются в пустую proposal либо implicit latest pins.
func lockRepositoryBundle(ctx context.Context, tx pgx.Tx, current scope, ref string) (repositoryBundleRow, error) {
	var row repositoryBundleRow
	var raw, receiptBytes, manifestMetadata []byte
	var archiveRevision, manifestRevision, repositoryRef string
	var digest string
	var expiry *time.Time
	p := &row.proposal
	err := tx.QueryRow(ctx, queryRepositoryBundleLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "proposal_ref": ref}).Scan(
		&row.id, &p.Ref, &raw, &digest, &manifestMetadata, &p.Version, &p.State, &row.lease.Attempt, &row.lease.Generation,
		&row.lease.Claimant, &row.lease.Fence, &expiry, &row.approvedAt, &row.deadline, &p.CandidateCommitSHA, &p.CandidateTreeSHA,
		&row.started, &p.ConfirmedAt, &p.FailureCode, &receiptBytes, &row.completedAt, &p.CreatedAt, &p.ExpiresAt,
		&row.invocationID, &row.gateID, &row.projectID, &row.actorID, &row.rootRunID, &row.runID, &row.connectionID, &row.grantID, &row.credentialID, &row.approverID, &row.invocationLeaseRef, &archiveRevision, &manifestRevision, &repositoryRef,
		&p.ContinuationState, &p.ContinuationCode, &p.ContinuationNodeRef,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil {
		return row, errs.ErrUnavailable
	}
	if !json.Valid(raw) || decodeStrict(raw, &row.snapshot) != nil || repositorybundle.Digest(asJSON(row.snapshot)) != digest ||
		!json.Valid(receiptBytes) || decodeStrict(receiptBytes, &row.receipts) != nil || row.receipts == nil {
		return row, errs.ErrUnavailable
	}
	var typedManifest repositorybundle.Manifest
	if !json.Valid(manifestMetadata) || decodeStrict(manifestMetadata, &typedManifest) != nil {
		return row, errs.ErrUnavailable
	}
	row.manifest = asJSON(typedManifest)
	frozen := row.snapshot.Proposal
	if frozen.Ref != ref || frozen.Version != 1 || frozen.State != "WAITING_APPROVAL" || !frozen.CreatedAt.Equal(p.CreatedAt) || !frozen.ExpiresAt.Equal(p.ExpiresAt) ||
		frozen.ProposalBranch != "kodex/bundle/"+ref || !repositorybundle.ValidDigest(frozen.ApprovalDigest) || repositorybundle.Digest(row.manifest) != frozen.Manifest.SHA256 || int64(len(row.manifest)) != frozen.Manifest.Size {
		return row, errs.ErrUnavailable
	}
	manifest, err := repositorybundle.ParseManifest(row.manifest)
	if err != nil || len(manifest.Files) != int(frozen.FileCount) || manifest.TotalBytes != frozen.TotalBytes {
		return row, errs.ErrUnavailable
	}
	if row.snapshot.SourcePolicy != repositoryBundleContentPolicy || row.snapshot.ArchiveSource.RevisionRef != archiveRevision || row.snapshot.ManifestSource.RevisionRef != manifestRevision || frozen.RepositoryRef != repositoryRef || len(row.snapshot.FileReceipts) != len(manifest.Files) {
		return row, errs.ErrUnavailable
	}
	for index, receipt := range row.snapshot.FileReceipts {
		entry := manifest.Files[index]
		if receipt.Entry != entry || receipt.MediaType == "" || receipt.Scan.Infected || receipt.Scan.Digest != "sha256:"+entry.SHA256 || receipt.Scan.SizeBytes != entry.Bytes || !validArtifactAntivirus(receipt.Scan) {
			return row, errs.ErrUnavailable
		}
	}
	version, state, commit, tree, failure, confirmed := p.Version, p.State, p.CandidateCommitSHA, p.CandidateTreeSHA, p.FailureCode, p.ConfirmedAt
	continuationState, continuationCode, continuationNode := p.ContinuationState, p.ContinuationCode, p.ContinuationNodeRef
	*p = frozen
	p.Version, p.State, p.CandidateCommitSHA, p.CandidateTreeSHA, p.FailureCode, p.ConfirmedAt = version, state, commit, tree, failure, confirmed
	p.ContinuationState, p.ContinuationCode, p.ContinuationNodeRef = continuationState, continuationCode, continuationNode
	row.lease.ProposalRef = ref
	if expiry != nil {
		row.lease.ExpiresAt = *expiry
	}
	return row, nil
}

// repositoryBundleAudit records one closed-state owner action in the same transaction without source bytes, credential material or provider error text.
// repositoryBundleAudit фиксирует одну closed-state owner action в той же транзакции без source bytes, credential material и provider error text.
func repositoryBundleAudit(ctx context.Context, tx pgx.Tx, current scope, projectID, actorID, ref, state string) error {
	auditRef, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, projectID, actorID, "repository-bundle."+state, "REPOSITORY_BUNDLE_PUBLICATION", ref, repositoryBundleSummary, current.correlationRef); err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

// saveRepositoryBundle persists the complete mutable owner projection with one version fence and one audit record; no external effect is performed or retried here.
// saveRepositoryBundle сохраняет полную mutable owner projection с одним version fence и одной audit record; external effect здесь не выполняется и не повторяется.
func saveRepositoryBundle(ctx context.Context, tx pgx.Tx, current scope, row *repositoryBundleRow) error {
	var leaseExpiry any
	if !row.lease.ExpiresAt.IsZero() {
		leaseExpiry = row.lease.ExpiresAt
	}
	p := &row.proposal
	tag, err := tx.Exec(ctx, queryRepositoryBundleSave, pgx.StrictNamedArgs{"organization_id": current.organizationID, "id": row.id, "expected_version": p.Version, "state": p.State,
		"approver_id": row.approverID, "approved_at": row.approvedAt, "deadline": row.deadline, "attempt": row.lease.Attempt, "generation": row.lease.Generation, "claimant": row.lease.Claimant,
		"fence": row.lease.Fence, "lease_expires_at": leaseExpiry, "invocation_lease_ref": row.invocationLeaseRef, "commit_sha": p.CandidateCommitSHA, "tree_sha": p.CandidateTreeSHA, "started_at": row.started, "confirmed_at": p.ConfirmedAt,
		"failure_code": p.FailureCode, "receipts": asJSON(row.receipts), "completed_at": row.completedAt,
		"continuation_state": p.ContinuationState, "continuation_code": p.ContinuationCode, "continuation_node_ref": p.ContinuationNodeRef})
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	p.Version++
	return repositoryBundleAudit(ctx, tx, current, row.projectID, row.actorID, p.Ref, p.State)
}
