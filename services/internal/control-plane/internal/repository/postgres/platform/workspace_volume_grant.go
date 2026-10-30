package platform

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

// authorizeWorkspaceVolumeGrantTx separates physical preparation eligibility from model/DAG concurrency and caps it at the exact ancestor deadline.
// It never fabricates a model lease or expires a root by an unverified payload; the existing deadline owner reconciles the complete graph.
// authorizeWorkspaceVolumeGrantTx отделяет physical preparation eligibility от model/DAG concurrency и ограничивает её exact ancestor deadline.
// Проверка не создаёт model lease и не завершает root по непроверенному payload; existing deadline owner согласует полный graph.
func (repository *Repository) authorizeWorkspaceVolumeGrantTx(ctx context.Context, tx pgx.Tx, current scope, task workspaceVolumeTask) (*time.Time, bool, error) {
	if task.volumeGeneration != task.currentVolumeGeneration || task.workspaceState != "EMPTY" || !task.noWriter {
		return nil, false, nil
	}
	actorID, err := repository.authorizeWorkspaceVolumeOrigin(ctx, tx, current, task.nodeID, task.rootRunID, task.input.Scope)
	if errors.Is(err, errs.ErrForbidden) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if actorID != task.actorID {
		return nil, false, nil
	}
	deadline, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, task.runID)
	if err != nil {
		return nil, false, err
	}
	if expired {
		return nil, false, nil
	}
	if deadline == nil {
		return nil, true, nil
	}
	value := deadline.EffectiveDeadlineAt
	return &value, true, nil
}
