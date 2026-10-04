package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

//go:embed sql/runtime_activity__execution.sql
var queryRuntimeActivityExecution string

//go:embed sql/runtime_activity__lock_root.sql
var queryRuntimeActivityLockRoot string

//go:embed sql/runtime_activity__latest.sql
var queryRuntimeActivityLatest string

//go:embed sql/runtime_activity__terminal_tools.sql
var queryRuntimeActivityTerminalTools string

var runtimeActivityRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

func validPublishedMessage(message *entity.RunMessage) bool {
	return message != nil && runtimeActivityRefPattern.MatchString(message.Ref) && message.Revision == 1 &&
		(message.Phase == "COMMENTARY" || message.Phase == "FINAL") &&
		strings.TrimSpace(message.Text) != "" && utf8.ValidString(message.Text) && len(message.Text) <= runtimecontract.MaximumRuntimeMessageBytes
}

func validToolActivityLifecycle(state string, revision int64, result string, duration int64) bool {
	if state == "RUNNING" {
		return revision == 1 && result == "" && duration == 0
	}
	return (state == "SUCCEEDED" || state == "FAILED" || state == "CANCELLED") && revision == 2
}

// Terminal node закрывает незавершённые проекции в той же owner transaction.
// CANCELLED означает закрытие наблюдения, а не утверждение об исходе внешнего effect.
func (repository *Repository) closeTerminalToolActivity(ctx context.Context, tx pgx.Tx, current scope, projectID, rootRunID string) error {
	rows, err := tx.Query(ctx, queryRuntimeActivityTerminalTools, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": rootRunID,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	type pending struct {
		nodeRef string
		call    entity.RunToolCall
		actor   entity.RunEventActor
	}
	var calls []pending
	for rows.Next() {
		var item pending
		var body []byte
		if rows.Scan(&item.nodeRef, &body, &item.actor.Kind, &item.actor.Ref, &item.actor.Name) != nil || json.Unmarshal(body, &item.call) != nil || item.call.Revision != 1 {
			rows.Close()
			return errs.ErrUnavailable
		}
		calls = append(calls, item)
	}
	rows.Close()
	if rows.Err() != nil {
		return errs.ErrUnavailable
	}
	for _, item := range calls {
		item.call.State, item.call.Revision, item.call.SafeResult = "CANCELLED", 2, "CANCELLED"
		event, err := repository.emitRunEvent(ctx, tx, current, projectID, rootRunID, item.call.Ref,
			"TOOL_CALL_RECORDED", item.nodeRef, "", "", "", "i18n:RUNTIME_TOOL_CALL_RECORDED", "", "")
		if err != nil {
			return err
		}
		if err := attachToolCallActivity(ctx, tx, current, &event, item.actor, &item.call); err != nil {
			return err
		}
	}
	return nil
}

func attachToolCallActivity(ctx context.Context, tx pgx.Tx, current scope, event *entity.RunEvent, actor entity.RunEventActor, call *entity.RunToolCall) error {
	if _, err := tx.Exec(ctx, queryRuntimeRecordtoolcallUpdateEvent, current.organizationID, actor.Kind, actor.Ref,
		actor.Name, asJSON(call), event.Ref); err != nil {
		return errs.ErrUnavailable
	}
	if _, err := tx.Exec(ctx, queryRuntimeRecordtoolcallUpdateOutbox, current.organizationID, asJSON(call), event.Ref); err != nil {
		return errs.ErrUnavailable
	}
	event.Actor, event.MessageKind, event.ToolCall = actor, "TOOL_CALL", call
	return nil
}

type runtimeActivityTurnInput struct {
	Content string
	Actor   entity.RunEventActor
}

func readRuntimeActivityExecution(ctx context.Context, tx pgx.Tx, organizationID, rootRunID, nodeRef string) (*entity.RunEventExecution, runtimeActivityTurnInput, error) {
	var execution entity.RunEventExecution
	var input runtimeActivityTurnInput
	err := tx.QueryRow(ctx, queryRuntimeActivityExecution, pgx.StrictNamedArgs{
		"organization_id": organizationID, "root_run_id": rootRunID, "node_ref": nodeRef,
	}).Scan(&execution.RunRef, &execution.NodeRef, &execution.SessionRef, &execution.TurnRef, &execution.TurnNumber, &execution.Attempt,
		&input.Content, &input.Actor.Kind, &input.Actor.Ref, &input.Actor.Name)
	if err != nil || execution.TurnNumber < 1 || execution.Attempt < 1 {
		return nil, runtimeActivityTurnInput{}, errs.ErrUnavailable
	}
	return &execution, input, nil
}

// Блокировка root сериализует проверку receipt с append события, включая разные
// узлы одной сессии. Авторитетное происхождение берётся только из текущего lease.
func latestRuntimeActivity(ctx context.Context, tx pgx.Tx, current scope, lease map[string]any, kind, ref string) (*entity.RunEvent, error) {
	var locked string
	if err := tx.QueryRow(ctx, queryRuntimeActivityLockRoot, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": lease["rootRunID"],
	}).Scan(&locked); err != nil {
		return nil, errs.ErrUnavailable
	}
	execution, _, err := readRuntimeActivityExecution(ctx, tx, current.organizationID, stringMap(lease, "rootRunID"), stringMap(lease, "nodeRef"))
	if err != nil {
		return nil, err
	}
	var sequence int64
	var delta, tool []byte
	err = tx.QueryRow(ctx, queryRuntimeActivityLatest, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": lease["rootRunID"], "node_ref": execution.NodeRef,
		"turn_ref": execution.TurnRef, "attempt": execution.Attempt, "activity_kind": kind, "activity_ref": ref,
	}).Scan(&sequence, &delta, &tool)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	// Точный receipt читается через тот же authoritative event layout, а не из
	// реконструированного текущего графа. Историческое событие не переписывается.
	rows, err := tx.Query(ctx, queryQueriesListruneventsSelectRunEventsOrganizationIdRef, current.organizationID, locked, sequence-1, 1)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, errs.ErrUnavailable
	}
	var event entity.RunEvent
	if err := rows.Scan(&event.Ref, &event.RunRef, &event.Sequence, &event.Type, &event.NodeRef, &event.EdgeRef,
		&event.GateRef, &event.ArtifactRef, &event.Summary, &event.Progress, &event.RunState, &event.NodeState,
		&delta, &event.Actor.Kind, &event.Actor.Ref, &event.Actor.Name, &event.MessageKind, &tool, &event.OccurredAt); err != nil {
		return nil, errs.ErrUnavailable
	}
	if event.Sequence != sequence || json.Unmarshal(delta, &event.Delta) != nil || event.Delta.Run == nil ||
		!reflect.DeepEqual(event.Delta.Execution, execution) || len(tool) > 0 && json.Unmarshal(tool, &event.ToolCall) != nil {
		return nil, errs.ErrUnavailable
	}
	event.GraphRevision = event.Delta.Run.GraphRevision
	return &event, nil
}
