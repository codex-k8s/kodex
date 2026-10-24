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

// queryRuntimeActivityExecution Reads exact organization/root/node execution, session/turn
// counters and persisted actor/input, using run task for the first USER turn and stored turn
// content otherwise. The caller validates positive counters and derives callback source
// separately.
// queryRuntimeActivityExecution Читает выполнение точных организации, корня и узла, сессию,
// счётчики turn, сохранённых автора и вход. Для первого USER turn берёт task запуска, иначе
// содержимое turn. Вызывающий проверяет положительные счётчики и отдельно выводит
// callback-источник.
//
//go:embed sql/runtime_activity__execution.sql
var queryRuntimeActivityExecution string

// queryRuntimeMessageSource Derives ORDINARY/CALLBACK_CONTINUATION from exact persisted
// organization/run/turn/node actor and canonical CONTINUES or completed required-workflow callback
// lineage, including pinned TURN_QUEUED evidence. Message text, locale and caller-supplied actor
// do not classify the source.
// queryRuntimeMessageSource Выводит ORDINARY/CALLBACK_CONTINUATION из точных сохранённых
// организации, запуска, turn, узла и автора, канонической CONTINUES либо завершённой
// callback-цепочки обязательного workflow с привязанным TURN_QUEUED. Текст, локаль и автор из
// запроса источник не назначают.
//
//go:embed sql/runtime_message__source.sql
var queryRuntimeMessageSource string

// callbackContinuationPublicText Names the fixed public i18n marker replacing internal
// callback-continuation input in runtime activity projection. It reveals no callback payload or
// new authority.
// callbackContinuationPublicText Задаёт фиксированный публичный i18n-маркер вместо внутреннего
// входа callback-продолжения в проекции активности. Callback payload и новые полномочия не
// раскрывает.
const callbackContinuationPublicText = "i18n:CALLBACK_CONTINUATION_PUBLIC"

// readRuntimeMessageSource Reads the persisted canonical source and accepts only
// ORDINARY/CALLBACK_CONTINUATION. Any scan failure or unknown origin returns unavailable with zero
// output; it does not infer source from message text.
// readRuntimeMessageSource Читает сохранённый канонический источник и допускает только
// ORDINARY/CALLBACK_CONTINUATION. Любая ошибка чтения или иной origin даёт Unavailable с нулевым
// результатом; текст сообщения источник не определяет.
func readRuntimeMessageSource(ctx context.Context, tx pgx.Tx, organizationID, runRef, turnRef, nodeRef string) (entity.MessageSource, error) {
	var source entity.MessageSource
	if err := tx.QueryRow(ctx, queryRuntimeMessageSource, pgx.StrictNamedArgs{
		"organization_id": organizationID, "run_ref": runRef, "turn_ref": turnRef, "node_ref": nodeRef,
	}).Scan(&source.Origin); err != nil || (source.Origin != "ORDINARY" && source.Origin != "CALLBACK_CONTINUATION") {
		return entity.MessageSource{}, errs.ErrUnavailable
	}
	return source, nil
}

// queryRuntimeActivityLockRoot Locks the exact tenant/root run and returns its public ref,
// serializing activity receipt lookup with later event append across nodes of the same root. The
// caller supplies owner-resolved root identity.
// queryRuntimeActivityLockRoot Блокирует точный корневой запуск организации и возвращает публичную
// ссылку, сериализуя поиск квитанции активности с последующим добавлением события между узлами
// корня. Идентичность корня разрешает вызывающий.
//
//go:embed sql/runtime_activity__lock_root.sql
var queryRuntimeActivityLockRoot string

// queryRuntimeActivityLatest Finds the latest exact organization/root/node/turn/attempt activity
// event by message or tool ref and returns sequence/safe delta/tool JSON. Missing rows mean no
// receipt; the caller reloads authoritative event layout and verifies execution identity.
// queryRuntimeActivityLatest Находит последнее событие точных организации, корня, узла, turn и
// attempt по ссылке сообщения или инструмента, возвращая sequence, safe delta и JSON вызова.
// Отсутствие строки означает отсутствие квитанции; вызывающий перечитывает событие и сверяет
// выполнение.
//
//go:embed sql/runtime_activity__latest.sql
var queryRuntimeActivityLatest string

// queryRuntimeActivityTerminalTools Selects the latest RUNNING tool projection per
// node/turn/attempt/ref only when that current node is terminal and execution pins match. It
// identifies observations to close, not proof that an external tool effect was cancelled.
// queryRuntimeActivityTerminalTools Выбирает последнюю RUNNING проекцию инструмента по узлу, turn,
// attempt и ref только для текущего terminal-узла с совпавшими привязками выполнения. Выбирает
// наблюдения для закрытия, а не доказывает отмену внешнего эффекта.
//
//go:embed sql/runtime_activity__terminal_tools.sql
var queryRuntimeActivityTerminalTools string

// runtimeActivityRefPattern Accepts 8..96 ASCII letters/digits/underscore/hyphen as published
// activity refs, with no normalization or ownership verification.
// runtimeActivityRefPattern Допускает 8..96 ASCII-букв, цифр, подчёркиваний и дефисов в ссылке
// опубликованной активности без нормализации или проверки владения.
var runtimeActivityRefPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// validPublishedMessage Requires a nonnil message with valid ref, first revision, COMMENTARY/FINAL
// phase and nonblank valid UTF-8 text within the runtime byte limit. It performs no
// redaction/source authorization and adds no independent NUL-content predicate.
// validPublishedMessage Требует непустое сообщение с допустимой ссылкой, первой ревизией, фазой
// COMMENTARY/FINAL и непустым UTF-8 текстом в пределах runtime-лимита. Не скрывает содержимое, не
// авторизует источник и не добавляет отдельной проверки NUL.
func validPublishedMessage(message *entity.RunMessage) bool {
	return message != nil && runtimeActivityRefPattern.MatchString(message.Ref) && message.Revision == 1 &&
		(message.Phase == "COMMENTARY" || message.Phase == "FINAL") &&
		strings.TrimSpace(message.Text) != "" && utf8.ValidString(message.Text) && len(message.Text) <= runtimecontract.MaximumRuntimeMessageBytes
}

// validToolActivityLifecycle Requires RUNNING at revision 1 with empty result/zero duration, or
// SUCCEEDED/FAILED/CANCELLED at revision 2. Terminal result and duration are not checked by this
// narrow predicate; other caller validations remain required.
// validToolActivityLifecycle Требует RUNNING в ревизии 1 с пустым результатом и нулевой
// длительностью либо SUCCEEDED/FAILED/CANCELLED в ревизии 2. Итоговый результат и длительность
// этой узкой проверкой не ограничены; прочие проверки вызывающего сохраняются.
func validToolActivityLifecycle(state string, revision int64, result string, duration int64) bool {
	if state == "RUNNING" {
		return revision == 1 && result == "" && duration == 0
	}
	return (state == "SUCCEEDED" || state == "FAILED" || state == "CANCELLED") && revision == 2
}

// closeTerminalToolActivity Reads and closes pending tool projections before writes, then records
// CANCELLED revision 2 observations and enriches both event/outbox in the same owner transaction.
// Retains the original actor and execution lineage; CANCELLED closes observation without asserting
// the external effect outcome.
// closeTerminalToolActivity Читает и закрывает rows незавершённых проекций до записей, затем
// фиксирует наблюдения CANCELLED ревизии 2 и дополняет событие с outbox в одной транзакции
// владельца. Сохраняет автора и цепочку выполнения; CANCELLED закрывает наблюдение без утверждения
// об исходе внешнего эффекта.
func (repository *Repository) closeTerminalToolActivity(ctx context.Context, tx pgx.Tx, current scope, projectID, rootRunID string) error {
	rows, err := tx.Query(ctx, queryRuntimeActivityTerminalTools, pgx.StrictNamedArgs{
		"organization_id": current.organizationID, "root_run_id": rootRunID,
	})
	if err != nil {
		return errs.ErrUnavailable
	}
	// pending Buffers exact node ref, decoded running tool call and persisted actor after the query
	// closes, so event/outbox writes do not compete with open rows on the transaction connection.
	// pending Буферизует точную ссылку узла, декодированный RUNNING вызов и сохранённого автора после
	// закрытия запроса, чтобы записи event/outbox не конфликтовали с открытыми rows соединения
	// транзакции.
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

// attachToolCallActivity Updates exact organization/event actor/tool-call projection and its
// linked durable outbox before mutating the returned event in memory. Any SQL failure is
// unavailable; it owns no commit, external publication or authorization and does not check
// affected-row counts.
// attachToolCallActivity Обновляет автора и проекцию вызова точного события организации, затем
// связанный outbox до изменения возвращаемого события в памяти. SQL-сбой даёт Unavailable; commit,
// внешняя публикация и авторизация сюда не входят, число строк не проверяется.
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

// runtimeActivityTurnInput Carries persisted turn content, actor and canonical message source for
// safe event projection. Callback input is replaced by a fixed public marker before return; these
// fields do not establish caller authority.
// runtimeActivityTurnInput Хранит сохранённые содержимое turn, автора и канонический источник для
// безопасной проекции события. Callback-вход заменяется фиксированным публичным маркером до
// возврата; поля не назначают полномочия вызывающего.
type runtimeActivityTurnInput struct {
	Content string
	Actor   entity.RunEventActor
	Source  entity.MessageSource
}

// readRuntimeActivityExecution Scans exact execution/turn input and requires positive
// turn/attempt, resolves canonical source and replaces callback continuation text with the public
// marker. Any read/counter failure returns zero execution/input as unavailable; no execution state
// is changed.
// readRuntimeActivityExecution Читает точные выполнение и вход turn, требует положительные
// turn/attempt, разрешает канонический источник и заменяет callback-текст публичным маркером.
// Ошибка чтения или счётчиков даёт нулевой результат с Unavailable; состояние выполнения не
// меняется.
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
	input.Source, err = readRuntimeMessageSource(ctx, tx, organizationID, execution.RunRef, execution.TurnRef, execution.NodeRef)
	if err != nil {
		return nil, runtimeActivityTurnInput{}, err
	}
	if input.Source.Origin == "CALLBACK_CONTINUATION" {
		input.Content = callbackContinuationPublicText
	}
	return &execution, input, nil
}

// latestRuntimeActivity Locks the owner root, derives current execution from trusted lease
// metadata, finds its latest exact activity receipt and reloads the historical authoritative
// event. Requires sequence, delta run and full execution equality, then returns stored graph
// revision; no receipt returns nil,nil. It never reconstructs/rewrites a historical graph or
// grants new authority.
// latestRuntimeActivity Блокирует корень владельца, выводит текущее выполнение из доверенных
// данных аренды, находит точную квитанцию активности и перечитывает историческое событие. Требует
// совпадения sequence, наличия run в delta и полного выполнения, возвращает сохранённую graph
// revision. Отсутствие квитанции даёт nil,nil; граф не восстанавливает и не переписывает,
// полномочия не выдаёт.
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
