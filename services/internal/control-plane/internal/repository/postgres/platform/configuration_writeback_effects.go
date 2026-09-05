package platform

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

const writeBackCommitMessage = "Update managed configuration"

func writeBackMarker(ref string) string { return "kodex-configuration-writeback:" + ref }

func writeBackLeaseMatches(row writeBackRow, lease entity.ConfigurationWriteBackLease, now time.Time) bool {
	return (row.proposal.State == entity.WriteBackClaimed || row.proposal.State == entity.WriteBackStarted || row.proposal.State == entity.WriteBackUnknown) &&
		lease.ProposalRef == row.proposal.Ref && lease.Attempt == row.lease.Attempt && lease.ClaimGeneration == row.lease.ClaimGeneration &&
		lease.Claimant == row.lease.Claimant && lease.Fence != "" && lease.Fence == row.lease.Fence && !lease.ExpiresAt.IsZero() &&
		!lease.ExpiresAt.After(row.lease.ExpiresAt) && now.Before(row.lease.ExpiresAt)
}

func writeBackReceiptKey(lease entity.ConfigurationWriteBackLease, action, effect string) string {
	return fmt.Sprintf("%d/%d/%s/%s", lease.Attempt, lease.ClaimGeneration, action, effect)
}

func writeBackReplay(row writeBackRow, key, digest string) (entity.ConfigurationWriteBack, bool, error) {
	receipt, found := row.receipts[key]
	if !found {
		return entity.ConfigurationWriteBack{}, false, nil
	}
	if receipt.Digest != digest {
		return entity.ConfigurationWriteBack{}, true, errs.ErrConflict
	}
	return receipt.Proposal, true, nil
}

func validateWriteBackEffectInput(row writeBackRow, input platformrepo.ConfigurationWriteBackEffectInput) error {
	p := row.proposal
	if input.Effect != row.effect || input.ContentSHA256 != p.ProposedContentSHA256 || input.ParentCommitSHA != p.BaseCommitSHA ||
		!validSourceCommit(input.CandidateCommitSHA) || !validSourceCommit(input.CandidateTreeSHA) || !validSourceCommit(input.CandidateBlobSHA) ||
		len(input.CandidateCommitSHA) != len(p.BaseCommitSHA) || len(input.CandidateTreeSHA) != len(p.BaseCommitSHA) || len(input.CandidateBlobSHA) != len(p.BaseCommitSHA) {
		return errs.ErrInvalid
	}
	definition, err := integrationpackage.Parse(row.snapshot.Source.DefinitionPackage)
	if err != nil {
		return errs.ErrUnavailable
	}
	inputs := map[string]map[string]any{}
	if row.snapshot.Source.DefinitionKey == "github" {
		if input.Effect == entity.WriteBackBranch {
			if !validSourceCommit(input.BaseBlobSHA) || len(input.BaseBlobSHA) != len(p.BaseCommitSHA) {
				return errs.ErrInvalid
			}
			inputs["github.branch.create"] = map[string]any{"branch": p.ProposalBranch, "sha": p.BaseCommitSHA}
			inputs["github.repository.content.update"] = map[string]any{"branch": p.ProposalBranch, "path": p.Path, "message": writeBackCommitMessage, "content_base64": base64.StdEncoding.EncodeToString([]byte(row.snapshot.ProposedContent)), "sha": input.BaseBlobSHA}
		} else {
			inputs["github.pull_request.create"] = map[string]any{"head": p.ProposalBranch, "base": p.SourceRefName, "title": writeBackCommitMessage, "body": writeBackMarker(p.Ref)}
		}
	} else if row.snapshot.Source.DefinitionKey == "gitlab" {
		if input.Effect == entity.WriteBackBranch {
			inputs["gitlab.branch.create"] = map[string]any{"branch": p.ProposalBranch, "ref": p.BaseCommitSHA}
			inputs["gitlab.commit.create"] = map[string]any{"branch": p.ProposalBranch, "action": "update", "file_path": p.Path, "content": row.snapshot.ProposedContent, "commit_message": writeBackCommitMessage}
		} else {
			inputs["gitlab.merge_request.create"] = map[string]any{"source_branch": p.ProposalBranch, "target_branch": p.SourceRefName, "title": writeBackCommitMessage, "description": writeBackMarker(p.Ref)}
		}
	} else {
		return errs.ErrForbidden
	}
	for operation, values := range inputs {
		capability, ok := definition.Capability(operation)
		if !ok {
			return errs.ErrForbidden
		}
		if _, err := capability.ValidateInput(asJSON(values)); err != nil {
			return errs.ErrForbidden
		}
	}
	return nil
}

func (repository *Repository) BeginConfigurationWriteBackEffect(ctx context.Context, principal value.Principal, input platformrepo.ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, bool, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ConfigurationWriteBack{}, false, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ConfigurationWriteBack{}, false, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := lockWriteBack(ctx, tx, current, input.Lease.ProposalRef)
	if err != nil {
		return entity.ConfigurationWriteBack{}, false, err
	}
	key, digest := writeBackReceiptKey(input.Lease, "BEGIN", input.Effect), writeBackDigest(asJSON(input))
	if result, found, err := writeBackReplay(row, key, digest); found {
		return result, true, err
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.ConfigurationWriteBack{}, false, errs.ErrUnavailable
	}
	if !writeBackLeaseMatches(row, input.Lease, now) || row.proposal.State != entity.WriteBackClaimed || row.started != nil || row.deadline == nil || !now.Before(*row.deadline) {
		return entity.ConfigurationWriteBack{}, false, errs.ErrForbidden
	}
	if err := repository.writeBackEligibility(ctx, tx, current, row, false); err != nil {
		return entity.ConfigurationWriteBack{}, false, err
	}
	if err := validateWriteBackEffectInput(row, input); err != nil {
		return entity.ConfigurationWriteBack{}, false, err
	}
	if row.effect == entity.WriteBackPullRequest && (row.proposal.BranchConfirmedAt == nil || input.CandidateCommitSHA != row.proposal.CandidateCommitSHA || input.CandidateTreeSHA != row.tree || input.CandidateBlobSHA != row.blob) {
		return entity.ConfigurationWriteBack{}, false, errs.ErrConflict
	}
	row.proposal.State, row.proposal.CandidateCommitSHA = entity.WriteBackStarted, input.CandidateCommitSHA
	row.tree, row.blob, row.started = input.CandidateTreeSHA, input.CandidateBlobSHA, &now
	receipt := row.proposal
	receipt.Version++
	row.receipts[key] = writeBackReceipt{Digest: digest, Proposal: receipt}
	if err := saveWriteBack(ctx, tx, current, &row); err != nil {
		return entity.ConfigurationWriteBack{}, false, err
	}
	return row.proposal, false, tx.Commit(ctx)
}

func validWriteBackPullRequest(row writeBackRow, ref, rawURL string) bool {
	id, err := strconv.ParseInt(ref, 10, 32)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != ref {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return false
	}
	if row.snapshot.Source.DefinitionKey == "github" {
		return u.Host == "github.com" && u.Path == "/"+row.proposal.RepositoryRef+"/pull/"+ref
	}
	base, ok := row.snapshot.Source.PublicConfiguration["base_url"].(string)
	if !ok {
		return false
	}
	b, err := url.Parse(base)
	return err == nil && b.Scheme == "https" && b.User == nil && u.Host == b.Host && u.Path == strings.TrimSuffix(b.Path, "/")+"/"+row.proposal.RepositoryRef+"/-/merge_requests/"+ref
}

func (repository *Repository) CompleteConfigurationWriteBackEffect(ctx context.Context, principal value.Principal, input platformrepo.ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ConfigurationWriteBack{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := lockWriteBack(ctx, tx, current, input.Lease.ProposalRef)
	if err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	key, digest := writeBackReceiptKey(input.Lease, "COMPLETE", input.Effect), writeBackDigest(asJSON(input))
	if result, found, err := writeBackReplay(row, key, digest); found {
		return result, err
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.ConfigurationWriteBack{}, errs.ErrUnavailable
	}
	if !writeBackLeaseMatches(row, input.Lease, now) || row.started == nil || row.effect != input.Effect || input.CandidateCommitSHA != row.proposal.CandidateCommitSHA || input.ContentSHA256 != row.proposal.ProposedContentSHA256 {
		return entity.ConfigurationWriteBack{}, errs.ErrForbidden
	}
	if input.Effect == entity.WriteBackBranch {
		if input.PullRequestRef != "" || input.PullRequestURL != "" {
			return entity.ConfigurationWriteBack{}, errs.ErrInvalid
		}
		row.proposal.BranchConfirmedAt = &now
		row.effect, row.started = entity.WriteBackPullRequest, nil
		row.proposal.State, row.proposal.FailureCode = entity.WriteBackQueued, ""
		if row.deadline == nil || !now.Before(*row.deadline) || repository.writeBackEligibility(ctx, tx, current, row, false) != nil {
			row.proposal.State, row.proposal.FailureCode, row.proposal.CompletedAt = entity.WriteBackFailed, "AUTHORITY_CHANGED", &now
		}
	} else if input.Effect == entity.WriteBackPullRequest {
		if row.proposal.BranchConfirmedAt == nil || !validWriteBackPullRequest(row, input.PullRequestRef, input.PullRequestURL) {
			return entity.ConfigurationWriteBack{}, errs.ErrInvalid
		}
		row.proposal.PullRequestRef, row.proposal.PullRequestURL = input.PullRequestRef, input.PullRequestURL
		row.proposal.PullRequestConfirmedAt, row.proposal.CompletedAt = &now, &now
		row.proposal.State, row.proposal.FailureCode = entity.WriteBackSucceeded, ""
	} else {
		return entity.ConfigurationWriteBack{}, errs.ErrInvalid
	}
	row.lease.Fence, row.lease.Claimant, row.lease.ExpiresAt = "", "", time.Time{}
	receipt := row.proposal
	receipt.Version++
	row.receipts[key] = writeBackReceipt{Digest: digest, Proposal: receipt}
	if err := saveWriteBack(ctx, tx, current, &row); err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	return row.proposal, tx.Commit(ctx)
}
