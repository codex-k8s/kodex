package platform

import (
	"context"
	_ "embed"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// queryIntegrationHealthRefreshCancelStale embeds a bounded SKIP LOCKED cancellation of
// DUE/CLAIMED managed-MCP refreshes whose eligibility or exact
// connection/configuration/package/credential pins changed. It clears lease/fence fields and
// records terminal unavailability in the caller transaction.
// queryIntegrationHealthRefreshCancelStale встраивает ограниченную отмену SKIP LOCKED задач
// DUE/CLAIMED managed-MCP refresh при изменении допустимости либо точных версий подключения,
// конфигурации, пакета или credentials. Очищает аренду и fence и записывает конечную недоступность
// в переданной транзакции.
//
//go:embed sql/integration_health_refresh__cancel_stale.sql
var queryIntegrationHealthRefreshCancelStale string

// queryIntegrationHealthRefreshExpire embeds bounded expiration of claimed integration-gateway
// refresh leases, marking FAILED and clearing claim metadata with a 30-second retry delay. It
// changes the ledger, not gateway health or immutable runtime pins.
// queryIntegrationHealthRefreshExpire встраивает ограниченное истечение захваченных
// integration-gateway refresh leases, переводя в FAILED и очищая захват с задержкой повтора 30
// секунд. Меняет реестр, а не здоровье gateway и не неизменяемые runtime-привязки.
//
//go:embed sql/integration_health_refresh__expire.sql
var queryIntegrationHealthRefreshExpire string

// queryIntegrationHealthRefreshCandidates embeds locked eligible connections without active tests
// for periodic refresh, bounded failed-attempt retry, cancellation recovery or initial probing. It
// returns predecessor and next attempt metadata, applying the four-minute success interval and
// retry eligibility in SQL.
// queryIntegrationHealthRefreshCandidates встраивает заблокированные допустимые подключения без
// активного теста для периодического refresh, ограниченного повтора ошибки, восстановления после
// отмены или первой проверки. Возвращает предшественника и следующую попытку, применяя интервал
// четыре минуты после успеха и условия повтора в SQL.
//
//go:embed sql/integration_health_refresh__candidates.sql
var queryIntegrationHealthRefreshCandidates string

// queryIntegrationHealthRefreshCreate embeds creation of a DUE MANAGED_MCP_REFRESH task with exact
// connection, actor, attempt and optional predecessor. Claiming, execution and terminal health
// updates occur through other lifecycle commands.
// queryIntegrationHealthRefreshCreate встраивает создание DUE задачи MANAGED_MCP_REFRESH с точными
// подключением, субъектом, попыткой и необязательным предшественником. Захват, исполнение и
// конечное обновление здоровья выполняют другие команды жизненного цикла.
//
//go:embed sql/integration_health_refresh__create.sql
var queryIntegrationHealthRefreshCreate string

// enqueueManagedMCPHealthRefresh requires a batch limit from 1 to 32, cancels stale tasks, expires
// old claims, then scans locked candidates before creating typed refresh tasks in the caller
// transaction. Integration-gateway remains claimant/executor; it performs no probe, changes no
// immutable grant pins and does not commit.
// enqueueManagedMCPHealthRefresh требует размер пакета от 1 до 32, отменяет устаревшие задачи,
// закрывает просроченные захваты, затем читает заблокированных кандидатов и создаёт типизированные
// refresh-задачи в переданной транзакции. Захват и исполнение остаются у integration-gateway;
// сетевой probe не выполняется, неизменяемые версии grants не меняются, Commit отсутствует.
func enqueueManagedMCPHealthRefresh(ctx context.Context, tx pgx.Tx, current scope, limit int32) error {
	if limit < 1 || limit > 32 {
		return errs.ErrInvalid
	}
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "limit": limit}
	for _, query := range []string{queryIntegrationHealthRefreshCancelStale, queryIntegrationHealthRefreshExpire} {
		if _, err := tx.Exec(ctx, query, args); err != nil {
			return errs.ErrUnavailable
		}
	}
	rows, err := tx.Query(ctx, queryIntegrationHealthRefreshCandidates,
		pgx.StrictNamedArgs{"organization_id": current.organizationID, "limit": limit})
	if err != nil {
		return errs.ErrUnavailable
	}
	// candidate stores one locked refresh candidate connection, predecessor ref and next attempt until
	// rows are closed. It is local scheduling metadata and does not itself claim or execute work.
	// candidate хранит подключение заблокированного кандидата refresh, ссылку предшественника и
	// следующую попытку до закрытия rows. Это локальные данные планирования; сами не захватывают и не
	// исполняют работу.
	type candidate struct {
		connectionID, predecessor string
		attempt                   int
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.connectionID, &item.predecessor, &item.attempt); err != nil {
			rows.Close()
			return errs.ErrUnavailable
		}
		candidates = append(candidates, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	for _, item := range candidates {
		ref, err := newRef("tst")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, queryIntegrationHealthRefreshCreate, pgx.StrictNamedArgs{
			"ref": ref, "organization_id": current.organizationID, "connection_id": item.connectionID,
			"actor_id": current.actorID, "attempt": item.attempt, "predecessor_ref": item.predecessor,
		}); err != nil {
			return errs.ErrUnavailable
		}
	}
	return nil
}
