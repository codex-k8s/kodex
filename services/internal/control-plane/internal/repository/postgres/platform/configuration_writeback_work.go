package platform

import (
	"context"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// writeBackWork projects pinned provider inputs and effect metadata for a worker, selecting
// RECOVER_READ_ONLY only for UNKNOWN_OUTCOME. EXECUTE deadline is capped by the proposal deadline;
// recovery uses lease expiry and preserves candidate identities and deterministic commit metadata.
// writeBackWork проецирует закреплённый ввод провайдера и метаданные эффекта для worker, выбирая
// RECOVER_READ_ONLY только для UNKNOWN_OUTCOME. EXECUTE deadline ограничен deadline предложения;
// recovery использует lease expiry и сохраняет candidate identities и детерминированные commit
// metadata.
func writeBackWork(row writeBackRow) entity.ConfigurationWriteBackWork {
	s := row.snapshot.Source
	mode := "EXECUTE"
	if row.proposal.State == entity.WriteBackUnknown {
		mode = "RECOVER_READ_ONLY"
	}
	deadline := row.lease.ExpiresAt
	if mode == "EXECUTE" && row.deadline != nil && row.deadline.Before(deadline) {
		deadline = *row.deadline
	}
	return entity.ConfigurationWriteBackWork{Lease: row.lease, Proposal: row.proposal, Mode: mode, Effect: row.effect,
		DefinitionKey: s.DefinitionKey, DefinitionVersion: s.DefinitionVersion, DefinitionDigest: s.DefinitionDigest, DefinitionPackage: s.DefinitionPackage,
		PublicConfiguration: s.PublicConfiguration, CredentialRevision: s.CredentialRevision, ProposedContent: []byte(row.snapshot.ProposedContent),
		EffectMarker: writeBackMarker(row.proposal.Ref), CommitMessage: writeBackCommitMessage, CommitAuthorName: "Kodex", CommitAuthorEmail: "configuration@kodex.invalid",
		CommitTime: row.proposal.CreatedAt, CandidateTreeSHA: row.tree, CandidateBlobSHA: row.blob, EffectStartedAt: row.started, Deadline: deadline}
}

// ClaimConfigurationWriteBackWork selects candidate refs under Serializable and locks/rechecks
// each using one database time. It expires unapproved work, fails exhausted/ineligible unstarted
// work, and sends any started effect to UNKNOWN_OUTCOME recovery before those checks. Fresh
// fences/generations are always issued, attempts increment only for execution, and
// state/audit/events commit even for an empty returned batch.
// ClaimConfigurationWriteBackWork выбирает ссылки candidates в Serializable и
// блокирует/перепроверяет каждую по одному времени БД. Завершает истёкшую неодобренную работу,
// отклоняет исчерпанную/недопустимую неначатую работу, а любой начатый эффект направляет в
// UNKNOWN_OUTCOME recovery до этих проверок. Всегда выдаёт свежие fences/generations, attempts
// увеличивает лишь для execution; state/audit/events фиксирует даже при пустом возвращаемом
// пакете.
func (repository *Repository) ClaimConfigurationWriteBackWork(ctx context.Context, p value.Principal, claimant string, limit int32) ([]entity.ConfigurationWriteBackWork, error) {
	current, tx, err := repository.beginWriteBackWorkTransaction(ctx, p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return nil, errs.ErrUnavailable
	}
	rows, err := tx.Query(ctx, queryWriteBackCandidates, current.organizationID, limit)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	refs := []string{}
	for rows.Next() {
		var ref string
		if rows.Scan(&ref) != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		refs = append(refs, ref)
	}
	rows.Close()
	if rows.Err() != nil {
		return nil, errs.ErrUnavailable
	}
	result := make([]entity.ConfigurationWriteBackWork, 0, len(refs))
	for _, ref := range refs {
		row, err := lockWriteBack(ctx, tx, current, ref)
		if err != nil {
			return nil, err
		}
		if writeBackTerminal(row.proposal.State) || now.Before(row.lease.ExpiresAt) {
			continue
		}
		if row.started != nil {
			row.proposal.State, row.proposal.FailureCode = entity.WriteBackUnknown, "OUTCOME_UNCONFIRMED"
		} else if row.proposal.State == entity.WriteBackWaiting {
			if now.Before(row.proposal.ExpiresAt) {
				continue
			}
			row.proposal.State, row.proposal.FailureCode, row.proposal.CompletedAt = entity.WriteBackExpired, "DEADLINE_EXCEEDED", &now
		} else if row.deadline == nil || !now.Before(*row.deadline) || row.lease.Attempt >= 3 {
			row.proposal.State, row.proposal.FailureCode, row.proposal.CompletedAt = entity.WriteBackFailed, "DEADLINE_EXCEEDED", &now
		} else if repository.writeBackEligibility(ctx, tx, current, row, false) != nil {
			row.proposal.State, row.proposal.FailureCode, row.proposal.CompletedAt = entity.WriteBackFailed, "AUTHORITY_CHANGED", &now
		}
		if writeBackTerminal(row.proposal.State) {
			row.lease.Fence, row.lease.Claimant, row.lease.ExpiresAt = "", "", time.Time{}
			if err := repository.saveWriteBack(ctx, tx, current, &row, true); err != nil {
				return nil, err
			}
			continue
		}
		fence, err := newRef("mcwbf")
		if err != nil {
			return nil, errs.ErrUnavailable
		}
		row.lease.ClaimGeneration++
		if row.proposal.State != entity.WriteBackUnknown {
			row.lease.Attempt++
			row.proposal.State, row.proposal.FailureCode = entity.WriteBackClaimed, ""
		}
		row.lease.Fence, row.lease.Claimant, row.lease.ExpiresAt = fence, claimant, now.Add(time.Minute)
		if row.proposal.State != entity.WriteBackUnknown && row.deadline.Before(row.lease.ExpiresAt) {
			row.lease.ExpiresAt = *row.deadline
		}
		if err := repository.saveWriteBack(ctx, tx, current, &row, true); err != nil {
			return nil, err
		}
		result = append(result, writeBackWork(row))
	}
	return result, tx.Commit(ctx)
}

// RenewConfigurationWriteBackWork verifies the fenced live lease, requiring fresh eligibility and
// deadline only outside UNKNOWN_OUTCOME, then grants one minute capped by the execution deadline.
// Saving increments proposal version and emits audit/event; a raw commit error may accompany the
// populated lease.
// RenewConfigurationWriteBackWork проверяет fenced живой lease, требуя свежие eligibility и
// deadline только вне UNKNOWN_OUTCOME, затем выдаёт минуту с ограничением execution deadline.
// Сохранение увеличивает версию proposal и создаёт audit/event; исходная ошибка commit может
// сопровождать заполненный lease.
func (repository *Repository) RenewConfigurationWriteBackWork(ctx context.Context, p value.Principal, lease entity.ConfigurationWriteBackLease) (entity.ConfigurationWriteBackLease, error) {
	current, tx, err := repository.beginWriteBackWorkTransaction(ctx, p)
	if err != nil {
		return entity.ConfigurationWriteBackLease{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := lockWriteBack(ctx, tx, current, lease.ProposalRef)
	if err != nil {
		return entity.ConfigurationWriteBackLease{}, err
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.ConfigurationWriteBackLease{}, errs.ErrUnavailable
	}
	if !writeBackLeaseMatches(row, lease, now) {
		return entity.ConfigurationWriteBackLease{}, errs.ErrForbidden
	}
	if row.proposal.State != entity.WriteBackUnknown && (row.deadline == nil || !now.Before(*row.deadline) || repository.writeBackEligibility(ctx, tx, current, row, false) != nil) {
		return entity.ConfigurationWriteBackLease{}, errs.ErrForbidden
	}
	row.lease.ExpiresAt = now.Add(time.Minute)
	if row.proposal.State != entity.WriteBackUnknown && row.deadline.Before(row.lease.ExpiresAt) {
		row.lease.ExpiresAt = *row.deadline
	}
	if err := repository.saveWriteBack(ctx, tx, current, &row, true); err != nil {
		return entity.ConfigurationWriteBackLease{}, err
	}
	return row.lease, tx.Commit(ctx)
}

// validWriteBackFailure accepts only the ten declared worker failure strings; it neither selects
// retry policy nor validates provider error text.
// validWriteBackFailure принимает только десять объявленных строк ошибок worker; не выбирает retry
// policy и не проверяет текст ошибки провайдера.
func validWriteBackFailure(failure string) bool {
	switch failure {
	case "UNAVAILABLE", "CREDENTIAL_REJECTED", "ACCESS_DENIED", "SOURCE_CHANGED", "CONTENT_INVALID", "RESPONSE_INVALID", "AUTHORITY_CHANGED", "DEADLINE_EXCEEDED", "BRANCH_CONFLICT", "OUTCOME_UNCONFIRMED":
		return true
	default:
		return false
	}
}

// FailConfigurationWriteBackWork validates the failure code and replays a matching LAST_FAIL
// receipt before checking the live lease. A digest mismatch continues through lease validation;
// started effects become UNKNOWN_OUTCOME, while only bounded pre-effect UNAVAILABLE may requeue.
// It clears claimant/fence, applies a one-minute retry pause and saves response
// receipt/audit/event without external actions.
// FailConfigurationWriteBackWork проверяет код ошибки и воспроизводит совпадающую LAST_FAIL
// receipt до проверки живого lease. Несовпадение digest продолжает проверку lease; начатый эффект
// становится UNKNOWN_OUTCOME, а только ограниченный UNAVAILABLE до эффекта допускает очередь.
// Очищает claimant/fence, назначает минутную паузу и сохраняет response receipt/audit/event без
// внешних действий.
func (repository *Repository) FailConfigurationWriteBackWork(ctx context.Context, p value.Principal, lease entity.ConfigurationWriteBackLease, failure string) (entity.ConfigurationWriteBack, error) {
	if !validWriteBackFailure(failure) {
		return entity.ConfigurationWriteBack{}, errs.ErrInvalid
	}
	current, tx, err := repository.beginWriteBackWorkTransaction(ctx, p)
	if err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := lockWriteBack(ctx, tx, current, lease.ProposalRef)
	if err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	digest := writeBackDigest(asJSON(struct {
		Lease   entity.ConfigurationWriteBackLease
		Failure string
	}{lease, failure}))
	key := "LAST_FAIL"
	if result, found, err := writeBackReplay(row, key, digest); found && err == nil {
		return result, nil
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.ConfigurationWriteBack{}, errs.ErrUnavailable
	}
	if !writeBackLeaseMatches(row, lease, now) {
		return entity.ConfigurationWriteBack{}, errs.ErrForbidden
	}
	row.proposal.FailureCode = failure
	if row.started != nil {
		row.proposal.State = entity.WriteBackUnknown
	} else if failure == "UNAVAILABLE" && row.lease.Attempt < 3 && row.deadline != nil && now.Add(time.Minute).Before(*row.deadline) {
		row.proposal.State = entity.WriteBackQueued
	} else {
		row.proposal.State, row.proposal.CompletedAt = entity.WriteBackFailed, &now
	}
	// Ограниченная пауза препятствует бесконечному busy-loop recovery.
	row.lease.ExpiresAt = now.Add(time.Minute)
	row.lease.Fence, row.lease.Claimant = "", ""
	receipt := row.proposal
	receipt.Version++
	row.receipts[key] = writeBackReceipt{Digest: digest, Proposal: receipt}
	if err := repository.saveWriteBack(ctx, tx, current, &row, true); err != nil {
		return entity.ConfigurationWriteBack{}, err
	}
	return row.proposal, tx.Commit(ctx)
}

// beginWriteBackWorkTransaction resolves the existing principal scope before opening the
// Serializable transaction shared by five write-back worker actions. It preserves resolve errors
// and maps only begin failure to Unavailable; callers retain locks, replay/authority ordering,
// rollback and commit. It neither starts effects nor grants authority or a new context budget.
// beginWriteBackWorkTransaction разрешает прежний principal scope до открытия общей Serializable
// transaction пяти write-back worker actions. Сохраняет resolve errors и преобразует только begin
// failure в Unavailable; callers сохраняют блокировки, порядок replay/authority, rollback и commit.
// Не запускает эффекты, не выдаёт полномочия и не назначает новый context budget.
func (repository *Repository) beginWriteBackWorkTransaction(ctx context.Context, p value.Principal) (scope, pgx.Tx, error) {
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return scope{}, nil, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return scope{}, nil, errs.ErrUnavailable
	}
	return current, tx, nil
}
