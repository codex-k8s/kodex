package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// managedMCPStartupRecovery carries private server-resolved startup recovery scope/agent/project
// and grant candidates. It signals maintenance intent only, not readiness, probe execution, a
// lease or provider authority.
// managedMCPStartupRecovery хранит внутренние scope/agent/project и grants кандидата startup
// recovery, разрешённые сервером. Обозначает только намерение обслуживания, а не готовность, право
// probe, lease и полномочия провайдера.
type managedMCPStartupRecovery struct {
	scopeKind, scopeRef, agentRef, projectRef string
	grants                                    []runtimecontract.RunnerIntegrationGrant
}

// Error returns a fixed English marker for stale startup health recovery. It exposes no connection
// configuration, credential or provider response.
// Error возвращает фиксированный английский маркер восстановления устаревшего startup health. Не
// раскрывает конфигурацию подключения, credentials и ответ провайдера.
func (*managedMCPStartupRecovery) Error() string {
	return "managed MCP startup health refresh is required"
}

// queryRuntimeManagedMCPStartupLock embeds a row lock on the exact tenant/ref/version Context7
// connection before startup recovery dependencies are rechecked. The lock alone does not make the
// connection ready or claim a probe.
// queryRuntimeManagedMCPStartupLock встраивает блокировку точных организации, ссылки и версии
// Context7 подключения перед повторной проверкой startup-зависимостей. Блокировка сама не делает
// подключение готовым и не захватывает probe.
//
//go:embed sql/runtime_managed_mcp__startup_lock.sql
var queryRuntimeManagedMCPStartupLock string

// enqueueManagedMCPStartupRecovery requires a valid exact grant pair, locks its connection and
// rechecks startup dependencies plus stale-success eligibility. A conflict from the profile reread
// returns false,nil without resetting another task; otherwise it creates one DUE refresh with
// predecessor and attempt1. Integration-gateway remains the probe executor, and the caller owns
// rollback/materialization recovery and atomic audit/receipt commit.
// enqueueManagedMCPStartupRecovery требует корректную точную пару grants, блокирует подключение и
// перепроверяет startup-зависимости и допустимость устаревшего успеха. Conflict повторного чтения
// профиля возвращает false,nil без сброса чужой задачи; иначе создаёт одну DUE refresh с
// предшественником и попыткой1. Исполнителем probe остаётся integration-gateway, вызывающий код
// отвечает за rollback материализации и атомарный Commit audit/receipt.
func enqueueManagedMCPStartupRecovery(ctx context.Context, tx pgx.Tx, current scope, recovery *managedMCPStartupRecovery) (bool, error) {
	if recovery == nil {
		return false, errs.ErrInvalid
	}
	resolve, _, valid := managedMCPPendingGrantPair(recovery.grants)
	if !valid {
		return false, errs.ErrConflict
	}
	var connectionID string
	err := tx.QueryRow(ctx, queryRuntimeManagedMCPStartupLock, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "connection_ref": resolve.ConnectionRef, "connection_version": resolve.ConnectionVersion,
	}).Scan(&connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, errs.ErrConflict
	}
	if err != nil {
		return false, serializableTransactionError(err, errs.ErrUnavailable)
	}
	if err := requireManagedMCPStartupDependencies(ctx, tx, current.organizationID, recovery.agentRef, recovery.grants); err != nil {
		return false, err
	}
	profiles, err := runtimeManagedMCPProfilesWithHealthQuery(ctx, tx, current.organizationID, recovery.scopeKind, recovery.scopeRef, recovery.agentRef, recovery.projectRef, recovery.grants, queryRuntimeManagedMCPStale, false)
	// Другой owner уже поставил probe или сохранил fresh receipt: никакого reset.
	if errors.Is(err, errs.ErrConflict) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(profiles) != 1 {
		return false, errs.ErrConflict
	}
	ref, err := newRef("tst")
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, queryIntegrationHealthRefreshCreate, pgx.StrictNamedArgs{
		"ref": ref, "organization_id": current.organizationID, "connection_id": connectionID,
		"actor_id": current.actorID, "attempt": 1, "predecessor_ref": profiles[0].Health.TestRef,
	}); err != nil {
		return false, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return true, nil
}
