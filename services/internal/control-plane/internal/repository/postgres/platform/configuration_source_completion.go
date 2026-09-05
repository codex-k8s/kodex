package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/configuration_source__retry.sql
var queryConfigurationSourceRetry string

func sourceActionDigest(action string, lease entity.ManagedConfigurationSourceLease, content any) string {
	digest := sha256.Sum256(asJSON(struct {
		Action  string
		Lease   entity.ManagedConfigurationSourceLease
		Content any
	}{action, lease, content}))
	return hex.EncodeToString(digest[:])
}

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

func validSourceFailure(failure string) bool {
	switch failure {
	case entity.ConfigurationSourceUnavailable, entity.ConfigurationSourceCredential, entity.ConfigurationSourceAccess, entity.ConfigurationSourceNotFound,
		entity.ConfigurationSourceDiverged, entity.ConfigurationSourceContent, entity.ConfigurationSourceResponse:
		return true
	default:
		return false
	}
}

func (repository *Repository) FailConfigurationSourceWork(ctx context.Context, p value.Principal, lease entity.ManagedConfigurationSourceLease, failure string) (entity.ManagedConfigurationGitSource, error) {
	if !validSourceFailure(failure) {
		return entity.ManagedConfigurationGitSource{}, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.ManagedConfigurationGitSource{}, errs.ErrUnavailable
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

func (repository *Repository) CompleteConfigurationSourceWork(ctx context.Context, p value.Principal, input platformrepo.ConfigurationSourceCompletion) (entity.ManagedConfigurationGitSource, error) {
	return repository.completeConfigurationSource(ctx, p, input)
}
