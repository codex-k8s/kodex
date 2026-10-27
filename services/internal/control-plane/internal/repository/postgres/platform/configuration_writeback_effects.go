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
)

// writeBackCommitMessage fixes the provider-facing commit/PR/MR message used by approved effect
// validation and worker instructions.
// writeBackCommitMessage задаёт фиксированное сообщение commit/PR/MR для провайдера, используемое
// проверкой одобренного эффекта и инструкциями worker.
const writeBackCommitMessage = "Update managed configuration"

// writeBackMarker creates the stable proposal marker for provider receipt discovery; the marker is
// not an authorization token.
// writeBackMarker создаёт устойчивый маркер предложения для поиска квитанции провайдера; маркер не
// является токеном полномочий.
func writeBackMarker(ref string) string { return "kodex-configuration-writeback:" + ref }

// writeBackLeaseMatches requires a claimed/started/unknown proposal, exact
// proposal/attempt/generation/claimant/nonempty fence and a live stored expiry. A nonzero
// submitted expiry may be older than the stored expiry, including already past; deadline and actor
// eligibility are checked elsewhere.
// writeBackLeaseMatches требует claimed/started/unknown proposal, точные
// proposal/attempt/generation/claimant/непустой fence и живой сохранённый expiry. Ненулевой
// переданный expiry может быть старше сохранённого, включая уже прошедший; deadline и actor
// eligibility проверяются отдельно.
func writeBackLeaseMatches(row writeBackRow, lease entity.ConfigurationWriteBackLease, now time.Time) bool {
	return (row.proposal.State == entity.WriteBackClaimed || row.proposal.State == entity.WriteBackStarted || row.proposal.State == entity.WriteBackUnknown) &&
		lease.ProposalRef == row.proposal.Ref && lease.Attempt == row.lease.Attempt && lease.ClaimGeneration == row.lease.ClaimGeneration &&
		lease.Claimant == row.lease.Claimant && lease.Fence != "" && lease.Fence == row.lease.Fence && !lease.ExpiresAt.IsZero() &&
		!lease.ExpiresAt.After(row.lease.ExpiresAt) && now.Before(row.lease.ExpiresAt)
}

// writeBackReceiptKey joins attempt, generation, action and effect into the historical receipt
// key; claimant/fence/content binding belongs to the separately stored digest.
// writeBackReceiptKey объединяет attempt, generation, action и effect в ключ исторической
// квитанции; привязка claimant/fence/content принадлежит отдельно сохранённому digest.
func writeBackReceiptKey(lease entity.ConfigurationWriteBackLease, action, effect string) string {
	return fmt.Sprintf("%d/%d/%s/%s", lease.Attempt, lease.ClaimGeneration, action, effect)
}

// writeBackReplay returns the stored proposal for an exact receipt digest, Conflict for an
// existing key with another digest, and a miss otherwise. It does not refresh authority, lease or
// deadline and must not authorize a resend.
// writeBackReplay возвращает сохранённое proposal при точном digest квитанции, Conflict при
// существующем ключе с другим digest, иначе отсутствие записи. Не обновляет authority, lease или
// deadline и не разрешает повторную внешнюю отправку.
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

// validateWriteBackEffectInput checks effect/content/parent and fixed-length commit/tree/blob pins
// against the snapshot, then validates the actual pinned package inputs for GitHub or GitLab
// operations. Branch input additionally binds the GitHub base blob; unsupported providers or
// missing/invalid capabilities close without external calls.
// validateWriteBackEffectInput сверяет effect/content/parent и pins commit/tree/blob фиксированной
// длины со snapshot, затем проверяет ввод операций GitHub или GitLab по фактическому закреплённому
// package. Для ветки дополнительно проверяет GitHub base blob; неподдерживаемый провайдер или
// отсутствующие/невалидные capabilities закрывают путь без внешних вызовов.
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

// BeginConfigurationWriteBackEffect replays an exact BEGIN receipt before fresh checks, otherwise
// requires the live claimed lease, no started effect, live deadline, current eligibility and
// validated candidate pins. It records EFFECT_STARTED and a response receipt atomically with
// audit/event; the boolean means prior receipt found, including a conflicting one, and never
// starts the provider call here.
// BeginConfigurationWriteBackEffect воспроизводит точную BEGIN receipt до свежих проверок, иначе
// требует живой claimed lease, отсутствие начатого эффекта, живой deadline, текущую eligibility и
// проверенные candidate pins. Атомарно записывает EFFECT_STARTED и квитанцию ответа с audit/event;
// bool означает найденную прежнюю квитанцию, включая конфликтную, а вызов провайдера здесь не
// запускается.
func (repository *Repository) BeginConfigurationWriteBackEffect(ctx context.Context, principal value.Principal, input platformrepo.ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, bool, error) {
	current, tx, err := repository.beginWriteBackWorkTransaction(ctx, principal)
	if err != nil {
		return entity.ConfigurationWriteBack{}, false, err
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
	if err := repository.saveWriteBack(ctx, tx, current, &row, true); err != nil {
		return entity.ConfigurationWriteBack{}, false, err
	}
	return row.proposal, false, tx.Commit(ctx)
}

// validWriteBackPullRequest requires a canonical positive int32 reference and exact HTTPS
// repository receipt URL without userinfo, query, fragment or RawPath. GitHub uses its fixed host;
// the other branch matches the configured HTTPS base host/path. This validates syntax, not a
// provider readback.
// validWriteBackPullRequest требует каноническую положительную int32 ссылку и точный HTTPS URL
// квитанции репозитория без userinfo, query, fragment или RawPath. GitHub использует фиксированный
// host; другая ветка сверяет настроенные HTTPS base host/path. Проверяет синтаксис, а не readback
// провайдера.
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

// CompleteConfigurationWriteBackEffect replays the completion receipt first, otherwise verifies
// the live fenced started effect and candidate/content pins. Branch confirmation queues PR
// creation only while deadline/authority remain valid; PR confirmation succeeds without a new
// eligibility check. It clears the claim and saves receipt/audit/event, allowing read-only
// recovery to record a known effect without resending or publishing runtime configuration.
// CompleteConfigurationWriteBackEffect сначала воспроизводит completion receipt, иначе сверяет
// живой fenced started effect и candidate/content pins. Подтверждение ветки ставит создание PR в
// очередь лишь при живых deadline/authority; подтверждение PR завершается без новой eligibility
// check. Очищает claim и сохраняет receipt/audit/event, позволяя read-only recovery записать
// известный эффект без повторной отправки или публикации runtime configuration.
func (repository *Repository) CompleteConfigurationWriteBackEffect(ctx context.Context, principal value.Principal, input platformrepo.ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, error) {
	current, tx, err := repository.beginWriteBackWorkTransaction(ctx, principal)
	if err != nil {
		return entity.ConfigurationWriteBack{}, err
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
	if err := repository.saveWriteBack(ctx, tx, current, &row, true); err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	return row.proposal, tx.Commit(ctx)
}
