package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// workflowExceptionEpisodeLimit bounds durable manager/recovery episodes for one published root.
// workflowExceptionEpisodeLimit ограничивает durable manager/recovery episodes опубликованного root.
const workflowExceptionEpisodeLimit = 48

// workflowExceptionHistory reads the private versioned episode array under the already-held root lock.
// workflowExceptionHistory читает приватный массив episodes под уже взятой блокировкой root.
//
//go:embed sql/workflow_exception__history.sql
var workflowExceptionHistory string

// workflowExceptionOutstanding selects the first unresolved final rejection under the root lock.
// workflowExceptionOutstanding выбирает первый неразобранный final rejection под root lock.
//
//go:embed sql/workflow_exception__outstanding.sql
var workflowExceptionOutstanding string

// resumeOutstandingWorkflowExceptionTx serializes sibling rejection episodes without replacing the
// active one or interpreting an older verdict as the result of an in-flight newer business attempt.
// resumeOutstandingWorkflowExceptionTx последовательно создаёт episodes отказов соседних ветвей,
// не заменяя активный episode и не принимая старый исход за результат новой выполняющейся попытки.
func (repository *Repository) resumeOutstandingWorkflowExceptionTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot) (bool, error) {
	var cause serverWorkflowException
	err := tx.QueryRow(ctx, workflowExceptionOutstanding, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID}).Scan(&cause.StepKey, &cause.Attempt, &cause.Reason, &cause.OriginNodeID, &cause.FinalNodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, errs.ErrUnavailable
	}
	return true, repository.ensureWorkflowExceptionTx(ctx, tx, current, root, cause)
}

// workflowExceptionSave preserves prior episodes and pins the active last version atomically.
// workflowExceptionSave сохраняет прежние episodes и атомарно закрепляет активную последнюю версию.
//
//go:embed sql/workflow_exception__save.sql
var workflowExceptionSave string

// workflowExceptionAgent resolves fresh owner launch eligibility without manufacturing a grant.
// workflowExceptionAgent проверяет свежую допустимость запуска владельцем без создания grant.
//
//go:embed sql/workflow_exception__agent.sql
var workflowExceptionAgent string

// workflowExceptionManager inserts only a root-owned exception analysis execution.
// workflowExceptionManager создаёт только принадлежащее root исполнение разбора исключения.
//
//go:embed sql/workflow_exception__manager.sql
var workflowExceptionManager string

// workflowExceptionManagerOrigin proves that a completed lease belongs to the active manager episode.
// workflowExceptionManagerOrigin доказывает принадлежность completed lease активному manager episode.
//
//go:embed sql/workflow_exception__manager_origin.sql
var workflowExceptionManagerOrigin string

// workflowExceptionRetry creates a fresh physical attempt and preserves the exact previous inputs.
// workflowExceptionRetry создаёт свежую physical attempt, сохраняя точные прежние inputs.
//
//go:embed sql/workflow_exception__retry.sql
var workflowExceptionRetry string

// workflowExceptionSource reads only the exact episode producer, including its sealed inputs.
// workflowExceptionSource читает только точного производителя episode вместе с sealed inputs.
//
//go:embed sql/workflow_exception__source.sql
var workflowExceptionSource string

// workflowExceptionEpisode is owner-issued durable recovery state; model analysis cannot change it.
// workflowExceptionEpisode — назначенное владельцем durable recovery state; разбор модели его не меняет.
type workflowExceptionEpisode struct {
	serverWorkflowException
	Episode        int64                  `json:"episode"`
	Version        int64                  `json:"version"`
	State          string                 `json:"state"`
	ManagerNodeID  string                 `json:"managerNodeId,omitempty"`
	ManagerNodeRef string                 `json:"managerNodeRef,omitempty"`
	GateID         string                 `json:"gateId,omitempty"`
	GateRef        string                 `json:"gateRef,omitempty"`
	RecoveryNodeID string                 `json:"recoveryNodeId,omitempty"`
	Inputs         []workflowInputReceipt `json:"inputs"`
}

// workflowConfigurationTx observes deferred row errors before legacy helpers map them to conflict.
// workflowConfigurationTx замечает deferred row errors до свёртки legacy helper в conflict.
type workflowConfigurationTx struct {
	pgx.Tx
	cause error
}

// workflowConfigurationRow keeps a query's actual Scan outcome private to its savepoint owner.
// workflowConfigurationRow сохраняет фактический Scan outcome приватно для владельца savepoint.
type workflowConfigurationRow struct {
	pgx.Row
	owner *workflowConfigurationTx
}

// QueryRow preserves the native pgx row while observing its deferred error exactly once.
// QueryRow сохраняет native pgx row, наблюдая отложенную ошибку ровно один раз.
func (tx *workflowConfigurationTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return workflowConfigurationRow{Row: tx.Tx.QueryRow(ctx, query, args...), owner: tx}
}

// Scan distinguishes a legitimate missing candidate from a database failure hidden by a mapper.
// Scan отличает законно отсутствующего кандидата от ошибки БД, скрытой mapper.
func (row workflowConfigurationRow) Scan(dest ...any) error {
	err := row.Row.Scan(dest...)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) && row.owner.cause == nil {
		row.owner.cause = err
	}
	return err
}

// readWorkflowExceptionHistoryTx never derives episode identity from an arbitrary JSON request.
// readWorkflowExceptionHistoryTx не выводит identity episode из произвольного JSON запроса.
func readWorkflowExceptionHistoryTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot) ([]workflowExceptionEpisode, error) {
	var history []workflowExceptionEpisode
	if err := tx.QueryRow(ctx, workflowExceptionHistory, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID}).Scan(&history); err != nil {
		return nil, errs.ErrUnavailable
	}
	return history, nil
}

// saveWorkflowExceptionEpisodeTx changes only the last episode after an owner transition.
// saveWorkflowExceptionEpisodeTx изменяет только последний episode после owner transition.
func saveWorkflowExceptionEpisodeTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, history []workflowExceptionEpisode) error {
	var active any
	last := history[len(history)-1]
	if contains([]string{"PENDING_MANAGER", "MANAGER_QUEUED", "WAITING_OWNER"}, last.State) {
		active = last
	}
	tag, err := tx.Exec(ctx, workflowExceptionSave, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "history": asJSON(history), "exception": asJSON(active)})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrUnavailable
	}
	root.Exception = asJSON(active)
	return nil
}

// ensureWorkflowExceptionTx records the first outstanding cause without replacing an older episode.
// A concurrent sibling may append its own result journal, but cannot overwrite the recovery cause.
// ensureWorkflowExceptionTx сохраняет первую незавершённую причину, не заменяя прежний episode.
// Параллельный sibling может дополнить свой журнал, но не перезаписать причину восстановления.
func (repository *Repository) ensureWorkflowExceptionTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, cause serverWorkflowException) error {
	history, err := readWorkflowExceptionHistoryTx(ctx, tx, current, root)
	if err != nil {
		return err
	}
	if string(root.Exception) == "null" {
		if len(history) >= workflowExceptionEpisodeLimit {
			return errs.ErrConflict
		}
		inputs := []workflowInputReceipt{}
		if cause.Attempt == 0 {
			anchors, err := readServerWorkflowAnchorsTx(ctx, tx, current, root)
			if err != nil {
				return err
			}
			steps := map[string]entity.WorkflowStep{}
			for _, step := range root.Version.Steps {
				steps[step.Key] = step
			}
			for _, key := range steps[cause.StepKey].DependsOn {
				receipt, accepted := acceptedServerWorkflowAttempt(anchors[key], steps[key])
				if !accepted {
					return errs.ErrConflict
				}
				inputs = append(inputs, workflowInputReceipt{StepKey: key, SourceNodeID: anchors[key].ID, Receipt: receipt})
			}
		} else {
			var ignored string
			if tx.QueryRow(ctx, workflowExceptionSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "node_id": cause.FinalNodeID}).Scan(&ignored, &inputs) != nil {
				return errs.ErrUnavailable
			}
		}
		history = append(history, workflowExceptionEpisode{serverWorkflowException: cause, Episode: int64(len(history) + 1), Version: 1, State: "PENDING_MANAGER", Inputs: inputs})
		if err := saveWorkflowExceptionEpisodeTx(ctx, tx, current, root, history); err != nil {
			return err
		}
	}
	return repository.reconcileWorkflowExceptionTx(ctx, tx, current, root)
}

// expectedWorkflowConfigurationFailure admits only closed domain eligibility failures, never a SQL
// or cleanup error. Callers still require a successful savepoint rollback before preserving capture.
// expectedWorkflowConfigurationFailure допускает только закрытые domain eligibility failures, не SQL
// или cleanup error. Для сохранения capture вызывающий требует успешный rollback savepoint.
func expectedWorkflowConfigurationFailure(err error) bool {
	return errors.Is(err, errs.ErrConflict) || errors.Is(err, errs.ErrNotFound) || errors.Is(err, errs.ErrForbidden) || errors.Is(err, errs.ErrInvalid) || errors.Is(err, errs.ErrCapabilityRequired)
}

// workflowConfigurationAttemptTx isolates SQL-only materialization and proves rollback independently
// from cancellation. Unexpected errors remain fatal; no external effect is retried here.
// workflowConfigurationAttemptTx изолирует SQL-only materialization и подтверждает rollback независимо
// от отмены. Неожиданные ошибки terminal; внешние действия здесь не повторяются.
func workflowConfigurationAttemptTx(ctx context.Context, tx pgx.Tx, operation func(pgx.Tx) error) (bool, error) {
	child, err := tx.Begin(ctx)
	if err != nil {
		return false, errs.ErrUnavailable
	}
	observed := &workflowConfigurationTx{Tx: child}
	err = operation(observed)
	if err == nil {
		if child.Commit(ctx) != nil {
			return false, errs.ErrUnavailable
		}
		return false, nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	if child.Rollback(cleanup) != nil {
		return false, errs.ErrUnavailable
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if observed.cause != nil {
		return false, errs.ErrUnavailable
	}
	if ctx.Err() != nil || !expectedWorkflowConfigurationFailure(err) {
		return false, err
	}
	return true, nil
}

// reconcileWorkflowExceptionTx admits one manager per episode within the shared model budget.
// Missing manager configuration produces an owner-visible gate without an invented execution.
// reconcileWorkflowExceptionTx допускает одного manager на episode в общем model budget.
// Отсутствующая конфигурация manager создаёт owner-visible gate без выдуманного исполнения.
func (repository *Repository) reconcileWorkflowExceptionTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot) error {
	history, err := readWorkflowExceptionHistoryTx(ctx, tx, current, root)
	if err != nil || len(history) == 0 {
		return err
	}
	episode := &history[len(history)-1]
	if _, expired, err := readRuntimeExecutionDeadline(ctx, tx, current.organizationID, root.ID); err != nil {
		return err
	} else if expired {
		return nil
	}
	if episode.State != "PENDING_MANAGER" {
		return nil
	}
	var active, running int64
	if tx.QueryRow(ctx, queryWorkflowDispatchActive, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID}).Scan(&active, &running) != nil {
		return errs.ErrUnavailable
	}
	if active >= int64(root.Version.Concurrency) {
		return nil
	}
	var managerID, managerRef string
	missing, err := workflowConfigurationAttemptTx(ctx, tx, func(child pgx.Tx) error {
		context, err := readWorkflowExceptionContextTx(ctx, child, current, root, *episode)
		if err != nil {
			return err
		}
		task, err := workflowExceptionTask(root.Task, context)
		if err != nil {
			return err
		}
		stage, err := repository.createWorkflowAgentRunTx(ctx, child, current, root, root.Version.CoordinatorAgentRef, "Workflow exception analysis", task)
		if err != nil {
			return err
		}
		managerRef, err = newRef("nod")
		if err != nil {
			return err
		}
		edgeRef, err := newRef("edg")
		if err != nil {
			return err
		}
		return child.QueryRow(ctx, workflowExceptionManager, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "run_id": stage.RunID, "turn_id": stage.TurnID, "agent_id": stage.AgentID, "node_ref": managerRef, "step_key": fmt.Sprintf("workflow.coordinator.exception.%d", episode.Episode), "display_name": stage.Name, "role": stage.Role, "edge_ref": edgeRef}).Scan(&managerID)
	})
	if err != nil {
		return err
	}
	episode.Version++
	if missing {
		return repository.openWorkflowRecoveryGateTx(ctx, tx, current, root, history, "Manager analysis could not be admitted; no manager execution was created.")
	}
	episode.State, episode.ManagerNodeID, episode.ManagerNodeRef = "MANAGER_QUEUED", managerID, managerRef
	if err := saveWorkflowExceptionEpisodeTx(ctx, tx, current, root, history); err != nil {
		return err
	}
	_, err = repository.emitRunEvent(ctx, tx, current, root.ProjectID, root.ID, managerRef, "TURN_QUEUED", managerRef, "", "", "", "i18n:OWNER_DECISION_REQUIRED", "", "")
	return err
}

// workflowAgentRun holds private coordinates created only by an owner-side execution materializer.
// workflowAgentRun хранит приватные координаты, созданные только owner-side materializer исполнения.
type workflowAgentRun struct{ RunID, TurnID, AgentID, Name, Role string }

// createWorkflowAgentRunTx reuses native session, provider and turn primitives with fresh launch ACL.
// It does not insert an execution node or mint a delegation grant.
// createWorkflowAgentRunTx использует native session/provider/turn primitives со свежим launch ACL.
// Функция не создаёт execution node и не выдаёт delegation grant.
func (repository *Repository) createWorkflowAgentRunTx(ctx context.Context, tx pgx.Tx, current scope, root *serverWorkflowRoot, agentRef, title, task string) (workflowAgentRun, error) {
	var stage workflowAgentRun
	err := tx.QueryRow(ctx, workflowExceptionAgent, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": root.ID, "agent_ref": agentRef}).Scan(&stage.AgentID, &stage.Name, &stage.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return stage, errs.ErrConflict
	}
	if err != nil {
		return stage, errs.ErrUnavailable
	}
	accountID, err := repository.selectProviderAccountForAgent(ctx, tx, current.organizationID, agentRef)
	if err != nil {
		return stage, err
	}
	sessionRef, err := newRef("ses")
	if err != nil {
		return stage, err
	}
	var sessionID string
	if tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertChildSession, pgx.StrictNamedArgs{"session_ref": sessionRef, "organization_id": current.organizationID, "project_id": root.ProjectID, "target_agent_ref": agentRef, "provider_account_id": accountID, "created_by": root.InitiatorID}).Scan(&sessionID) != nil {
		return stage, errs.ErrUnavailable
	}
	if err := bindSessionModelCatalog(ctx, tx, current.organizationID, sessionID, agentRef); err != nil {
		return stage, err
	}
	runRef, err := newRef("run")
	if err != nil {
		return stage, err
	}
	if tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertRunsRefProjectIdRootRunId, pgx.StrictNamedArgs{"run_ref": runRef, "organization_id": current.organizationID, "project_id": root.ProjectID, "session_id": sessionID, "root_run_id": root.ID, "parent_run_id": root.ID, "target_agent_ref": agentRef, "title": title, "task": task, "input": root.Input, "initiated_by": root.InitiatorID}).Scan(&stage.RunID) != nil {
		return stage, errs.ErrUnavailable
	}
	turnRef, err := newRef("trn")
	if err != nil {
		return stage, err
	}
	var number int64
	if tx.QueryRow(ctx, queryRuntimeDelegateexecutionSelectSessionsId, sessionID).Scan(&number) != nil ||
		tx.QueryRow(ctx, queryRuntimeDelegateexecutionInsertSessionTurnsRefSessionIdTurnNumber, turnRef, current.organizationID, sessionID, stage.RunID, number, root.Ref, task).Scan(&stage.TurnID) != nil {
		return stage, errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeDelegateexecutionUpdateSessionsNextTurnNumberVersionUpdatedAt, sessionID); err != nil {
		return stage, errs.ErrUnavailable
	}
	return stage, nil
}

// decodeWorkflowEpisode accepts only the stored owner shape, never a caller's recovery proposal.
// decodeWorkflowEpisode принимает только сохранённую owner shape, не recovery proposal вызывающего.
func decodeWorkflowEpisode(raw json.RawMessage) (workflowExceptionEpisode, error) {
	var episode workflowExceptionEpisode
	if json.Unmarshal(raw, &episode) != nil || episode.Episode < 1 || episode.Episode > workflowExceptionEpisodeLimit || episode.Version < 1 {
		return episode, errs.ErrConflict
	}
	return episode, nil
}
