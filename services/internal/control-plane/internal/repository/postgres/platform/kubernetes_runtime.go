package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"time"
)

// queryKubernetesRuntimeOrigin verifies the complete live lease and immutable invocation lineage.
// queryKubernetesRuntimeOrigin проверяет полную действующую lease и неизменяемое происхождение вызова.
//
//go:embed sql/kubernetes_runtime__origin.sql
var queryKubernetesRuntimeOrigin string

// queryKubernetesRuntimeResult exposes bounded original results only after exact owner authorization.
// queryKubernetesRuntimeResult выдаёт ограниченный исходный результат после точной авторизации владельца.
//
//go:embed sql/kubernetes_runtime__result.sql
var queryKubernetesRuntimeResult string

var _ port.KubernetesRuntimeReader = (*Repository)(nil)

// ReadKubernetesRuntimeInvocation uses one repeatable read for lease pins, fresh original rights and result disclosure.
// ReadKubernetesRuntimeInvocation использует одну repeatable read для lease pins, текущих прав инициатора и выдачи результата.
func (r *Repository) ReadKubernetesRuntimeInvocation(ctx context.Context, p value.Principal, q query.KubernetesRuntimeRead) (entity.KubernetesRuntimeResult, error) {
	return retryAssistantLockedRead(ctx, func(ctx context.Context) (entity.KubernetesRuntimeResult, error) {
		return r.readKubernetesRuntimeInvocation(ctx, p, q)
	})
}

// readKubernetesRuntimeInvocation derives the original actor from a live lease before reusing the common tool visibility rule.
// It grants no native execution and reads no credential, private input or uncommitted output.
// readKubernetesRuntimeInvocation выводит исходного инициатора из действующей lease перед общей проверкой видимости инструмента.
// Метод не разрешает native execution и не читает ключи, приватный input или неподтверждённый output.
func (r *Repository) readKubernetesRuntimeInvocation(ctx context.Context, p value.Principal, q query.KubernetesRuntimeRead) (result entity.KubernetesRuntimeResult, resultErr error) {
	if p.Validate() != nil || p.CallerWorkload != "runtime-controller" || p.Permission != "platform.runtime.kubernetes.invocation.read" {
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
	digest := sha256.Sum256([]byte(q.Fence))
	var projectRef string
	err = tx.QueryRow(ctx, queryKubernetesRuntimeOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "authority_project": current.authorityProjectID, "invocation_ref": q.InvocationRef, "lease_ref": q.LeaseRef, "fence_digest": hex.EncodeToString(digest[:]), "generation": q.Generation, "runtime_input_digest": q.RuntimeInputDigest}).Scan(&current.actorID, &current.actorRef, &current.authorityProjectID, &projectRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	}
	if err != nil {
		return result, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	// Only the owner-derived project is passed as a filter; the technical caller is never impersonated.
	// Фильтр проекта назначает владелец; технический caller не подменяется.
	origin := p
	origin.ProjectRef = projectRef
	id, err := r.lockPublicKubernetesTool(ctx, tx, current, origin, q.InvocationRef)
	if err != nil {
		return result, err
	}
	var expiresAt, observedAt time.Time
	var runID string
	readStarted := time.Now()
	err = tx.QueryRow(ctx, queryKubernetesRuntimeResult, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_id": id, "lease_ref": q.LeaseRef, "fence_digest": hex.EncodeToString(digest[:]), "generation": q.Generation, "runtime_input_digest": q.RuntimeInputDigest}).Scan(&expiresAt, &observedAt, &runID, &result.InvocationRef, &result.State, &result.ResultSummary, &result.SafeErrorCode, &result.EffectReceiptRef, &result.ResultDigest, &result.WaitingForGate)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.KubernetesRuntimeResult{}, errs.ErrNotFound
	}
	if err != nil {
		return entity.KubernetesRuntimeResult{}, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if result.InvocationRef != q.InvocationRef || !result.Valid() {
		return entity.KubernetesRuntimeResult{}, errs.ErrUnavailable
	}
	deadline, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, runID)
	if err != nil {
		return entity.KubernetesRuntimeResult{}, err
	}
	if expired {
		return entity.KubernetesRuntimeResult{}, errs.ErrNotFound
	}
	if deadline != nil && deadline.EffectiveDeadlineAt.Before(expiresAt) {
		expiresAt = deadline.EffectiveDeadlineAt
	}
	// Start before the database roundtrip: neither lock waiting nor response latency renews disclosure time.
	// Отсчёт начинается до обращения к БД: ожидание и доставка ответа не продлевают срок выдачи.
	disclosureDeadline := readStarted.Add(expiresAt.Sub(observedAt))
	if !time.Now().Before(disclosureDeadline) {
		return entity.KubernetesRuntimeResult{}, errs.ErrNotFound
	}
	if err = tx.Commit(ctx); err != nil {
		return entity.KubernetesRuntimeResult{}, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	if !time.Now().Before(disclosureDeadline) {
		return entity.KubernetesRuntimeResult{}, errs.ErrNotFound
	}
	return result, nil
}
