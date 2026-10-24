package platform

import (
	"context"
	_ "embed"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// queryConfigurationSourceRetry requeues only CLAIMED work below attempt three and before
// deadline, increments attempt, persists receipts/failure and revokes claimant/fence/expiry for
// the next claim.
// queryConfigurationSourceRetry возвращает в очередь только CLAIMED work с attempt ниже трёх до
// deadline, увеличивает attempt, сохраняет receipts/failure и отзывает claimant/fence/expiry до
// следующего claim.
//
//go:embed sql/configuration_source__retry.sql
var queryConfigurationSourceRetry string

// sourceActionDigest hashes the exact typed Action/Lease/Content JSON envelope, preserving field
// order, all lease coordinates, content encoding and the existing ignored-marshal-error hash of
// nil bytes; it does not validate action or grant caller authority.
// sourceActionDigest хеширует точный typed Action/Lease/Content JSON envelope, сохраняя field
// order, все lease координаты, content encoding и прежний hash nil bytes при игнорируемой marshal
// error; action не проверяет и caller authority не выдаёт.
func sourceActionDigest(action string, lease entity.ManagedConfigurationSourceLease, content any) string {
	return jsonValueDigest(struct {
		Action  string
		Lease   entity.ManagedConfigurationSourceLease
		Content any
	}{action, lease, content})
}

// sourceReplay looks up the durable attempt/claim-generation receipt, returning not-found
// separately from an action/digest conflict or exact saved safe result. It does not check fresh
// lease liveness or root eligibility; callers decide when replay precedes those checks.
// sourceReplay ищет устойчивую attempt/claim-generation receipt, различая отсутствие,
// action/digest conflict и точный сохранённый безопасный result. Не проверяет свежие lease
// liveness либо root eligibility; порядок replay относительно этих проверок определяет caller.
func sourceReplay(row sourceWorkRow, lease entity.ManagedConfigurationSourceLease, action, digest string) (entity.ManagedConfigurationGitSource, bool, error) {
	receipt, ok := row.receipts[sourceReceiptKey(lease)]
	if !ok {
		return entity.ManagedConfigurationGitSource{}, false, nil
	}
	if receipt.Action != action || receipt.Digest != digest {
		return entity.ManagedConfigurationGitSource{}, true, errs.ErrConflict
	}
	return receipt.Result, true, nil
}

// validSourceFailure accepts only the seven fixed
// unavailable/credential/access/not-found/diverged/content/response failure codes for the
// source-work protocol, rejecting arbitrary diagnostics.
// validSourceFailure принимает только семь фиксированных
// unavailable/credential/access/not-found/diverged/content/response failure codes source-work
// protocol, отклоняя произвольную диагностику.
func validSourceFailure(failure string) bool {
	switch failure {
	case entity.ConfigurationSourceUnavailable, entity.ConfigurationSourceCredential, entity.ConfigurationSourceAccess, entity.ConfigurationSourceNotFound,
		entity.ConfigurationSourceDiverged, entity.ConfigurationSourceContent, entity.ConfigurationSourceResponse:
		return true
	default:
		return false
	}
}

// FailConfigurationSourceWork validates the closed failure code, locks exact work in a
// Serializable transaction and replays an existing matching receipt before fresh lease/root
// checks. New failure retries only eligible UNAVAILABLE work below attempt three with more than
// one minute remaining, otherwise blocks source; eligibility loss records ACCESS. It stores source
// audit/event and exact work receipt plus retry or terminal state together, clearing returned
// public failure for QUEUED and committing before response.
// FailConfigurationSourceWork проверяет закрытый failure code, блокирует точную work в
// Serializable transaction и повторяет существующую совпавшую receipt до fresh lease/root checks.
// Новая ошибка повторяется только для допустимой UNAVAILABLE work с attempt ниже трёх и остатком
// больше минуты, иначе source блокируется; потеря eligibility записывает ACCESS. Сохраняет source
// audit/event и точную work receipt вместе с retry либо terminal state, очищает возвращаемый
// публичный failure для QUEUED и делает commit до ответа.
func (repository *Repository) FailConfigurationSourceWork(ctx context.Context, p value.Principal, lease entity.ManagedConfigurationSourceLease, failure string) (entity.ManagedConfigurationGitSource, error) {
	if !validSourceFailure(failure) {
		return entity.ManagedConfigurationGitSource{}, errs.ErrInvalid
	}
	current, tx, err := repository.beginSourceWorkTransaction(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	row, err := lockSourceWork(ctx, tx, current, lease.WorkRef)
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	digest := sourceActionDigest("FAIL", lease, failure)
	if result, found, err := sourceReplay(row, lease, "FAIL", digest); found {
		return result, err
	}
	if !sourceLeaseMatches(row, lease, time.Now().UTC()) {
		return entity.ManagedConfigurationGitSource{}, errs.ErrForbidden
	}
	source, actor, eligibilityErr := repository.sourceWorkEligibility(ctx, tx, row, current)
	if source.id == "" || source.Generation != lease.SourceGeneration {
		return entity.ManagedConfigurationGitSource{}, errs.ErrForbidden
	}
	if actor.actorID == "" {
		actor = current
	}
	next := entity.ConfigurationSourceBlocked
	retry := failure == entity.ConfigurationSourceUnavailable && eligibilityErr == nil && row.lease.Attempt < 3 && time.Now().Add(time.Minute).Before(row.deadline)
	if retry {
		next = entity.ConfigurationSourceQueued
	}
	storedFailure := failure
	if eligibilityErr != nil {
		storedFailure = entity.ConfigurationSourceAccess
	}
	result, err := repository.sourceState(ctx, tx, actor, source, next, storedFailure)
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	row.receipts[sourceReceiptKey(lease)] = sourceWorkReceipt{Action: "FAIL", Digest: digest, Result: result}
	if retry {
		if _, err := tx.Exec(ctx, queryConfigurationSourceRetry, row.id, asJSON(row.receipts), storedFailure); err != nil {
			return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
		}
	} else if _, err := tx.Exec(ctx, queryConfigurationSourceFinish, row.id, "FAILED", asJSON(row.receipts), storedFailure, digest); err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrConflict
	}
	return result, nil
}

// CompleteConfigurationSourceWork delegates the public repository port to the canonical source
// completion transaction; it neither rebuilds the work snapshot nor invents a separate completion
// receipt.
// CompleteConfigurationSourceWork передаёт публичный repository port канонической source
// completion transaction; work snapshot не пересобирает и отдельную completion receipt не создаёт.
func (repository *Repository) CompleteConfigurationSourceWork(ctx context.Context, p value.Principal, input platformrepo.ConfigurationSourceCompletion) (entity.ManagedConfigurationGitSource, error) {
	return repository.completeConfigurationSource(ctx, p, input)
}
