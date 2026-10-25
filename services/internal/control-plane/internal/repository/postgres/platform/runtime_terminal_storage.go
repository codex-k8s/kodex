package platform

import (
	"context"
	_ "embed"
	"log/slog"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

// queryRuntimeClaimTerminalStorage embeds bounded SKIP LOCKED queued execution candidates whose
// session storage is ERROR or PURGED, independent of full credential/configuration eligibility.
// Exact tenant/run/root/session/agent linkage prevents missing dependencies from hiding a terminal
// storage blocker.
// queryRuntimeClaimTerminalStorage встраивает ограниченный SKIP LOCKED выбор queued execution
// кандидатов со storage ERROR либо PURGED независимо от полной credential/configuration
// допустимости. Точные tenant/run/root/session/agent связи не позволяют отсутствующим зависимостям
// скрыть конечный storage blocker.
//
//go:embed sql/runtime_claim__select_terminal_storage.sql
var queryRuntimeClaimTerminalStorage string

// reconcileTerminalSessionStorage scans terminal-storage candidates before full eligibility,
// closes rows and deduplicates root IDs before logging and failing each owner graph. It returns
// one failed candidate per root without committing; partial domain changes on an error must roll
// back in the outer transaction.
// reconcileTerminalSessionStorage читает terminal-storage кандидатов до полной допустимости,
// закрывает rows и устраняет повторы root ID перед диагностикой и завершением каждого owner graph
// ошибкой. Возвращает одного неуспешного кандидата на root без Commit; частичные доменные
// изменения при ошибке откатываются внешней транзакцией.
func (repository *Repository) reconcileTerminalSessionStorage(ctx context.Context, tx pgx.Tx, current scope, input command.Command, limit int32) ([]claimableExecution, error) {
	rows, err := tx.Query(ctx, queryRuntimeClaimTerminalStorage, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "limit": limit,
	})
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	var candidates []claimableExecution
	for rows.Next() {
		var candidate claimableExecution
		if err := rows.Scan(&candidate.nodeID, &candidate.nodeRef, &candidate.runID, &candidate.runRef,
			&candidate.rootRunID, &candidate.projectID, &candidate.projectRef, &candidate.sessionID,
			&candidate.sessionRef, &candidate.stableKey); err != nil {
			rows.Close()
			return nil, errs.ErrUnavailable
		}
		candidates = append(candidates, candidate)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	seen := make(map[string]bool)
	var failed []claimableExecution
	for _, candidate := range candidates {
		if seen[candidate.rootRunID] {
			continue
		}
		logTerminalSessionStorageFailure(ctx, candidate)
		if err := repository.failRuntimeCandidateGraph(ctx, tx, current, input, candidate); err != nil {
			return nil, err
		}
		seen[candidate.rootRunID] = true
		failed = append(failed, candidate)
	}
	return failed, nil
}

// logTerminalSessionStorageFailure logs the fixed session_storage/TERMINAL_STORAGE diagnostic
// class and run/node/session refs. It emits no archive descriptor, source content, credential or
// domain state transition.
// logTerminalSessionStorageFailure пишет фиксированные диагностические
// session_storage/TERMINAL_STORAGE и ссылки run/node/session. Не выдаёт archive descriptor,
// содержимое источника и credentials и не выполняет доменный переход.
func logTerminalSessionStorageFailure(ctx context.Context, candidate claimableExecution) {
	slog.WarnContext(ctx, runtimeCandidateEligibilityDiagnosticMessage,
		"safe_stage", "session_storage", "error_class", "TERMINAL_STORAGE",
		"run_ref", candidate.runRef, "node_ref", candidate.nodeRef, "session_ref", candidate.sessionRef)
}
