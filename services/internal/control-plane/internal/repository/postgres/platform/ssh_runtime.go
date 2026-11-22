package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// querySSHRuntimeOrigin resolves the original actor and package only from the exact current runtime lease and immutable SSH pin.
// querySSHRuntimeOrigin разрешает original actor и package лишь из exact current runtime lease и immutable SSH pin.
//
//go:embed sql/ssh_runtime__origin.sql
var querySSHRuntimeOrigin string

// querySSHRuntimeResult rechecks physical lease/deadline at the final bounded output read after all owner locks.
// querySSHRuntimeResult повторяет physical lease/deadline при final bounded output read после всех owner locks.
//
//go:embed sql/ssh_runtime__result.sql
var querySSHRuntimeResult string

// querySSHRuntimeLock serializes only the exact tenant SSH invocation after its Connection lock; it grants no effect admission.
// querySSHRuntimeLock сериализует лишь exact tenant SSH invocation после Connection lock, не выдавая effect admission.
//
//go:embed sql/ssh_runtime__lock.sql
var querySSHRuntimeLock string

var _ port.SSHRuntimeReader = (*Repository)(nil)

// ReadSSHRuntimeInvocation reuses the bounded owner read retry contract, never generic organization-wide invocation disclosure.
// ReadSSHRuntimeInvocation использует bounded owner read retry contract без generic organization-wide invocation disclosure.
func (r *Repository) ReadSSHRuntimeInvocation(ctx context.Context, p value.Principal, q query.SSHRuntimeRead) (entity.SSHRuntimeResult, error) {
	return retryAssistantLockedRead(ctx, func(ctx context.Context) (entity.SSHRuntimeResult, error) {
		return r.readSSHRuntimeInvocation(ctx, p, q)
	})
}

// readSSHRuntimeInvocation derives the original actor/project from the live runtime proof, validates the exact SSH package and fresh run/Connection visibility, then reads only the original result.
// No live credential or new effect is issued; a credential revoke may still be observed as safe closed owner metadata.
// readSSHRuntimeInvocation выводит original actor/project из live runtime proof, проверяет exact SSH package и fresh run/Connection visibility, затем читает лишь original result.
// Live credential либо новый effect не выдаются; credential revoke остаётся доступен как safe closed owner metadata.
func (r *Repository) readSSHRuntimeInvocation(ctx context.Context, p value.Principal, q query.SSHRuntimeRead) (result entity.SSHRuntimeResult, resultErr error) {
	if p.Validate() != nil || p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.ssh.invocation.read" {
		return result, errs.ErrForbidden
	}
	current, err := r.resolveScope(ctx, p)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultErr)
	fence := sha256.Sum256([]byte(q.Fence))
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "authority_project": current.authorityProjectID,
		"invocation_ref": q.InvocationRef, "lease_ref": q.LeaseRef, "fence_digest": hex.EncodeToString(fence[:]),
		"generation": q.Generation, "runtime_input_digest": q.RuntimeInputDigest}
	var runRef, connection, key, version, digest, operation string
	err = tx.QueryRow(ctx, querySSHRuntimeOrigin, args).Scan(&current.actorID, &current.actorRef, &current.authorityProjectID, &runRef, &connection, &key, &version, &digest, &operation)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	definition, err := r.integrationPackage(ctx, tx, current.organizationID, connection, key, version, digest)
	if err != nil {
		return result, err
	}
	if _, err := sshHealthMode(definition); err != nil {
		return result, err
	}
	if _, known := definition.CapabilityByOperation(operation); !known {
		return result, errs.ErrNotFound
	}
	var accessDeadline time.Time
	for _, target := range []struct{ permission, kind, ref string }{{"run.view", "RUN", runRef}, {"integration.view", "INTEGRATION", connection}} {
		until, err := r.requireAccessUntil(ctx, tx, current, target.permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: target.kind, ResourceRef: target.ref})
		if err != nil {
			return result, concealAccessDenial(err, errs.ErrNotFound)
		}
		if !until.IsZero() && (accessDeadline.IsZero() || until.Before(accessDeadline)) {
			accessDeadline = until
		}
	}
	if _, err := r.lockIntegrationConnection(ctx, tx, current.organizationID, connection); err != nil {
		return result, err
	}
	var id string
	err = tx.QueryRow(ctx, querySSHRuntimeLock, pgx.StrictNamedArgs{"organization_id": current.organizationID, "actor_id": current.actorID, "invocation_ref": q.InvocationRef}).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	var expires, observed time.Time
	var runID string
	started := time.Now()
	delete(args, "authority_project")
	delete(args, "invocation_ref")
	args["invocation_id"] = id
	err = tx.QueryRow(ctx, querySSHRuntimeResult, args).Scan(&expires, &observed, &runID, &result.InvocationRef, &result.State,
		&result.ResultSummary, &result.SafeErrorCode, &result.EffectReceiptRef, &result.ResultDigest, &result.WaitingForGate)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.SSHRuntimeResult{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.SSHRuntimeResult{}, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if result.InvocationRef != q.InvocationRef || !result.Valid() {
		return entity.SSHRuntimeResult{}, errs.ErrUnavailable
	}
	deadline, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, runID)
	if err != nil {
		return entity.SSHRuntimeResult{}, err
	}
	if expired {
		return entity.SSHRuntimeResult{}, errs.ErrNotFound
	}
	if deadline != nil && deadline.EffectiveDeadlineAt.Before(expires) {
		expires = deadline.EffectiveDeadlineAt
	}
	disclosureDeadline := started.Add(expires.Sub(observed))
	if !accessDeadline.IsZero() && accessDeadline.Before(disclosureDeadline) {
		disclosureDeadline = accessDeadline
	}
	if !time.Now().Before(disclosureDeadline) {
		return entity.SSHRuntimeResult{}, errs.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.SSHRuntimeResult{}, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if !time.Now().Before(disclosureDeadline) {
		return entity.SSHRuntimeResult{}, errs.ErrNotFound
	}
	return result, nil
}
