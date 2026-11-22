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

// queryBrokerHealthLocator resolves one unambiguous existing queue address without selecting credentials or admitting effects.
// queryBrokerHealthLocator разрешает один однозначный existing queue address без выбора credentials либо допуска effects.
//
//go:embed sql/broker_health__locator.sql
var queryBrokerHealthLocator string

// queryBrokerHealthSharedLock reads typed immutable shared work under its exact Connection lock; material remains purpose-private.
// queryBrokerHealthSharedLock читает typed immutable shared work под exact Connection lock; material остаётся purpose-private.
//
//go:embed sql/broker_health__shared_lock.sql
var queryBrokerHealthSharedLock string

// brokerSharedHealthWork retains stored shared queue coordinates only; neither metadata nor a purpose argument replaces the authoritative credential selector.
// brokerSharedHealthWork сохраняет лишь stored shared queue coordinates; metadata и purpose argument не заменяют authoritative credential selector.
type brokerSharedHealthWork struct {
	id, ref, connectionRef, state, leaseRef, fenceDigest, instance, inputDigest, identityID string
	generation                                                                              int64
	expiresAt                                                                               *time.Time
	identity                                                                                entity.IntegrationBrokerIdentityRevision
	connection                                                                              lockedIntegrationConnection
}

// locateBrokerIdentityHealthTestTx rejects collisions between the existing personal and shared queues before any caller can choose a different ownership path.
// locateBrokerIdentityHealthTestTx отклоняет collisions existing personal/shared queues до выбора другого ownership path вызывающим.
func locateBrokerIdentityHealthTestTx(ctx context.Context, tx pgx.Tx, organizationID, ref string) (string, bool, error) {
	var connectionRef string
	var shared bool
	err := tx.QueryRow(ctx, queryBrokerHealthLocator, pgx.StrictNamedArgs{"organization_id": organizationID, "ref": ref}).Scan(&connectionRef, &shared)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, errs.ErrNotFound
	}
	if err != nil {
		return "", false, errs.ErrUnavailable
	}
	return connectionRef, shared, nil
}

// lockSharedBrokerIdentityHealthTx uses Connection→test ordering and exact stored kind/purpose; each purpose owner still rechecks its own package, current source and material fact.
// lockSharedBrokerIdentityHealthTx соблюдает Connection→test ordering и exact stored kind/purpose; purpose owner отдельно проверяет package, current source и material fact.
func (repository *Repository) lockSharedBrokerIdentityHealthTx(ctx context.Context, tx pgx.Tx, organizationID, ref, connectionRef string, purpose entity.IntegrationBrokerIdentityPurpose) (brokerSharedHealthWork, error) {
	job := brokerSharedHealthWork{ref: ref, connectionRef: connectionRef}
	if !purpose.Valid() {
		return job, errs.ErrForbidden
	}
	connection, err := repository.lockIntegrationConnection(ctx, tx, organizationID, connectionRef)
	if err != nil {
		return job, err
	}
	job.connection = connection
	var connectionID, kind, storedPurpose string
	err = tx.QueryRow(ctx, queryBrokerHealthSharedLock, pgx.StrictNamedArgs{"organization_id": organizationID, "ref": ref}).Scan(&job.id, &connectionID, &job.state, &job.leaseRef, &job.fenceDigest, &job.instance, &job.generation, &job.expiresAt, &job.inputDigest, &kind, &storedPurpose, &job.identityID, &job.identity.Ref, &job.identity.Revision, &job.identity.TargetRef)
	if err != nil {
		return job, errs.ErrUnavailable
	}
	if connectionID != connection.id || kind != "BROKER_IDENTITY" || storedPurpose != string(purpose) {
		return job, errs.ErrForbidden
	}
	job.identity.Purpose = purpose
	return job, nil
}

// queryBrokerHealthSharedFinish keeps the existing bounded shared queue/Connection transition under its caller-owned source lock.
// queryBrokerHealthSharedFinish сохраняет existing bounded shared queue/Connection transition под caller-owned source lock.
//
//go:embed sql/broker_health__shared_finish.sql
var queryBrokerHealthSharedFinish string

// queryBrokerHealthSharedConnection keeps the existing bounded shared queue/Connection transition under its caller-owned source lock.
// queryBrokerHealthSharedConnection сохраняет existing bounded shared queue/Connection transition под caller-owned source lock.
//
//go:embed sql/broker_health__shared_connection.sql
var queryBrokerHealthSharedConnection string

// queryBrokerHealthSharedReset keeps the existing bounded shared queue/Connection transition under its caller-owned source lock.
// queryBrokerHealthSharedReset сохраняет existing bounded shared queue/Connection transition под caller-owned source lock.
//
//go:embed sql/broker_health__shared_reset.sql
var queryBrokerHealthSharedReset string
