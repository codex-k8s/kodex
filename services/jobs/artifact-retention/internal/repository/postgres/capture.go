package postgres

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/managedkubernetes"
	"github.com/codex-k8s/kodex/services/jobs/artifact-retention/internal/capture"
	"github.com/jackc/pgx/v5"
)

// queryCaptureClaim invokes only the independently fenced owner function, never direct table reads.
// queryCaptureClaim вызывает independently fenced owner function без direct table reads.
//
//go:embed sql/capture_claim.sql
var queryCaptureClaim string

// queryCaptureHeartbeat records compatibility under immutable PostgreSQL session_user identity.
// queryCaptureHeartbeat фиксирует compatibility под immutable PostgreSQL session_user identity.
//
//go:embed sql/capture_heartbeat.sql
var queryCaptureHeartbeat string

// ClaimCapture returns at most one task because the caller already reserved one real worker slot.
// Database authority assigns tenant/source; an instance name alone cannot mint a task grant.
// ClaimCapture возвращает одну task: caller уже зарезервировал actual worker slot.
// Database authority назначает tenant/source; instance name не создаёт task grant.
func (repository *Repository) ClaimCapture(ctx context.Context, owner string, lease int64) (*capture.Claim, error) {
	if lease < 30 || lease > 300 || !managedkubernetes.ValidLocator(owner) || len(owner) > 128 {
		return nil, capture.ErrMalformed
	}
	var result capture.Claim
	binding := &result.Binding
	binding.WorkloadInstance = owner
	err := repository.pool.QueryRow(ctx, queryCaptureClaim, pgx.StrictNamedArgs{"owner": owner, "lease": lease}).Scan(&binding.CaptureRef, &binding.LeaseRef, &binding.Fence, &binding.Generation, &binding.SnapshotDigest, &result.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, capture.ErrUncertain
	}
	if binding.Validate() != nil {
		return nil, capture.ErrMalformed
	}
	return &result, nil
}

// HeartbeatCapture proves only the compatible live worker; it cannot renew an invocation or source.
// HeartbeatCapture подтверждает compatible live worker, не продлевая invocation либо source.
func (repository *Repository) HeartbeatCapture(ctx context.Context, owner string, revision uint32) error {
	if revision != managedkubernetes.CaptureContractRevision || !managedkubernetes.ValidLocator(owner) || len(owner) > 128 {
		return capture.ErrMalformed
	}
	var accepted bool
	if err := repository.pool.QueryRow(ctx, queryCaptureHeartbeat, pgx.StrictNamedArgs{"owner": owner, "revision": revision}).Scan(&accepted); err != nil {
		return capture.ErrUncertain
	}
	if !accepted {
		return capture.ErrLostClaim
	}
	return nil
}
