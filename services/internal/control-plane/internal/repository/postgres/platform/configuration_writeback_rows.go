package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/configuration_writeback__base.sql
	queryWriteBackBase string
	//go:embed sql/configuration_writeback__insert.sql
	queryWriteBackInsert string
	//go:embed sql/configuration_writeback__lock.sql
	queryWriteBackLock string
	//go:embed sql/configuration_writeback__update.sql
	queryWriteBackUpdate string
	//go:embed sql/configuration_writeback__list.sql
	queryWriteBackList string
	//go:embed sql/configuration_writeback__count.sql
	queryWriteBackCount string
	//go:embed sql/configuration_writeback__candidates.sql
	queryWriteBackCandidates string
)

type writeBackSnapshot struct {
	Proposal                     entity.ConfigurationWriteBack
	Source                       entity.ManagedConfigurationSourceWork
	BaseContent, ProposedContent string
}

type writeBackReceipt struct {
	Digest   string
	Proposal entity.ConfigurationWriteBack
}

type writeBackRow struct {
	id, rootRef, approverRef, organizationRef string
	snapshot                                  writeBackSnapshot
	proposal                                  entity.ConfigurationWriteBack
	lease                                     entity.ConfigurationWriteBackLease
	effect, tree, blob                        string
	started, deadline                         *time.Time
	receipts                                  map[string]writeBackReceipt
}

func writeBackDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func lockWriteBack(ctx context.Context, tx pgx.Tx, current scope, ref string) (writeBackRow, error) {
	var row writeBackRow
	var raw, receipts []byte
	var digest, approval, configurationRef, sourceRef, connectionRef, credentialRef string
	var lease *time.Time
	row.lease.ProposalRef = ref
	p := &row.proposal
	err := tx.QueryRow(ctx, queryWriteBackLock, current.organizationID, ref).Scan(&row.id, &raw, &digest, &approval, &p.Version, &p.State, &row.effect,
		&row.lease.Attempt, &row.lease.ClaimGeneration, &row.lease.Claimant, &row.lease.Fence, &lease,
		&p.CandidateCommitSHA, &row.tree, &row.blob, &row.started, &p.BranchConfirmedAt, &p.PullRequestConfirmedAt, &p.PullRequestRef, &p.PullRequestURL,
		&p.FailureCode, &receipts, &p.ApprovedAt, &row.deadline, &p.ExpiresAt, &p.CompletedAt, &p.CreatedAt,
		&row.rootRef, &row.approverRef, &row.organizationRef, &configurationRef, &sourceRef, &connectionRef, &credentialRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, errs.ErrNotFound
	}
	if err != nil {
		return row, errs.ErrUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&row.snapshot) != nil || decoder.Decode(&struct{}{}) != io.EOF || json.Unmarshal(receipts, &row.receipts) != nil || writeBackDigest(asJSON(row.snapshot)) != digest {
		return row, errs.ErrUnavailable
	}
	immutable := row.snapshot.Proposal
	if immutable.Ref != ref || immutable.ConfigurationRef != configurationRef || immutable.SourceRef != sourceRef || immutable.ConnectionRef != connectionRef ||
		immutable.ApprovalDigest != approval || row.snapshot.Source.CredentialRevision.Ref != credentialRef ||
		writeBackDigest([]byte(row.snapshot.BaseContent)) != immutable.BaseContentSHA256 || writeBackDigest([]byte(row.snapshot.ProposedContent)) != immutable.ProposedContentSHA256 {
		return row, errs.ErrUnavailable
	}
	immutable.Version, immutable.State, immutable.FailureCode = p.Version, p.State, p.FailureCode
	immutable.CandidateCommitSHA, immutable.PullRequestRef, immutable.PullRequestURL = p.CandidateCommitSHA, p.PullRequestRef, p.PullRequestURL
	immutable.CreatedAt, immutable.ExpiresAt, immutable.ApprovedAt, immutable.CompletedAt = p.CreatedAt, p.ExpiresAt, p.ApprovedAt, p.CompletedAt
	immutable.BranchConfirmedAt, immutable.PullRequestConfirmedAt = p.BranchConfirmedAt, p.PullRequestConfirmedAt
	row.proposal = immutable
	if lease != nil {
		row.lease.ExpiresAt = *lease
	}
	return row, nil
}

func saveWriteBack(ctx context.Context, tx pgx.Tx, current scope, row *writeBackRow) error {
	var lease *time.Time
	if !row.lease.ExpiresAt.IsZero() {
		lease = &row.lease.ExpiresAt
	}
	p := &row.proposal
	tag, err := tx.Exec(ctx, queryWriteBackUpdate, pgx.StrictNamedArgs{
		"id": row.id, "organization_id": current.organizationID, "version": p.Version, "state": p.State, "effect": row.effect,
		"attempt": row.lease.Attempt, "claim_generation": row.lease.ClaimGeneration, "claimant": row.lease.Claimant, "fence": row.lease.Fence, "lease": lease,
		"candidate_commit": p.CandidateCommitSHA, "candidate_tree": row.tree, "candidate_blob": row.blob, "effect_started": row.started,
		"branch_confirmed": p.BranchConfirmedAt, "pr_confirmed": p.PullRequestConfirmedAt, "pr_ref": p.PullRequestRef, "pr_url": p.PullRequestURL,
		"failure": p.FailureCode, "receipts": asJSON(row.receipts), "approved": p.ApprovedAt, "approver": row.approverRef, "deadline": row.deadline, "completed": p.CompletedAt,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	p.Version++
	auditRef, err := newRef("aud")
	if err != nil {
		return errs.ErrUnavailable
	}
	_, err = tx.Exec(ctx, queryCommandsExecuteInsertAuditEventsRefProjectIdAction, auditRef, current.organizationID, nil, current.actorID,
		"configuration-writeback."+p.State, "MANAGED_CONFIGURATION_WRITEBACK", p.Ref, "i18n:MANAGED_CONFIGURATION_CHANGED", current.correlationRef)
	if err != nil {
		return errs.ErrUnavailable
	}
	return nil
}

func writeBackTerminal(state string) bool {
	switch state {
	case entity.WriteBackSucceeded, entity.WriteBackRejected, entity.WriteBackCancelled, entity.WriteBackExpired, entity.WriteBackFailed:
		return true
	default:
		return false
	}
}
