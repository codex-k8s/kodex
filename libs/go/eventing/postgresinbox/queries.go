package postgresinbox

import (
	_ "embed"
	"fmt"
	"strings"
)

// rawSchemaSetSearchPath pins every transaction to the configured schema before any inbox query runs.
// rawSchemaSetSearchPath закрепляет каждую транзакцию за настроенной схемой до выполнения запросов inbox.
//
//go:embed sql/schema__set_search_path.sql
var rawSchemaSetSearchPath string

// rawCursorEnsure creates the durable ordering cursor if its tenant-scoped row does not exist yet.
// rawCursorEnsure создаёт устойчивый курсор порядка, если его строка в области арендатора ещё отсутствует.
//
//go:embed sql/cursor__ensure.sql
var rawCursorEnsure string

// rawCursorGetForUpdate locks the cursor row before deciding how an incoming sequence may advance.
// rawCursorGetForUpdate блокирует строку курсора перед решением о продвижении входящей последовательности.
//
//go:embed sql/cursor__get_for_update.sql
var rawCursorGetForUpdate string

// rawCursorTakeFence allocates a monotonic ownership fence and rejects counter overflow.
// rawCursorTakeFence выделяет монотонный барьер владения и отклоняет переполнение счётчика.
//
//go:embed sql/cursor__take_fence.sql
var rawCursorTakeFence string

// rawCursorAdvance persists the completed sequence under the cursor's transaction lock.
// rawCursorAdvance сохраняет завершённую последовательность под транзакционной блокировкой курсора.
//
//go:embed sql/cursor__advance.sql
var rawCursorAdvance string

// rawInboxGetByEventForUpdate locks an event delivery row before a state-changing retry or completion.
// rawInboxGetByEventForUpdate блокирует доставку события перед изменяющим состояние повтором или завершением.
//
//go:embed sql/inbox__get_by_event_for_update.sql
var rawInboxGetByEventForUpdate string

// rawInboxGetByEvent reads the persisted event delivery without taking a write lock.
// rawInboxGetByEvent читает сохранённую доставку события без блокировки на запись.
//
//go:embed sql/inbox__get_by_event.sql
var rawInboxGetByEvent string

// rawInboxGetBySequenceForUpdate locks the delivery at an ordering position before a claim transition.
// rawInboxGetBySequenceForUpdate блокирует доставку в позиции порядка перед переходом claim.
//
//go:embed sql/inbox__get_by_sequence_for_update.sql
var rawInboxGetBySequenceForUpdate string

// rawInboxInsertReceived stores a new delivery with its original sequence and immutable event identity.
// rawInboxInsertReceived сохраняет новую доставку с исходной последовательностью и неизменным идентификатором события.
//
//go:embed sql/inbox__insert_received.sql
var rawInboxInsertReceived string

// rawInboxInsertStale records an obsolete delivery without making it eligible for execution.
// rawInboxInsertStale фиксирует устаревшую доставку, не допуская её к исполнению.
//
//go:embed sql/inbox__insert_stale.sql
var rawInboxInsertStale string

// rawInboxClaim atomically assigns an eligible delivery to one fenced worker attempt.
// rawInboxClaim атомарно назначает допущенную доставку одной ограждённой попытке исполнителя.
//
//go:embed sql/inbox__claim.sql
var rawInboxClaim string

// rawInboxComplete marks a claimed delivery terminal within the same transaction as its effect.
// rawInboxComplete помечает назначенную доставку завершённой в одной транзакции с её эффектом.
//
//go:embed sql/inbox__complete.sql
var rawInboxComplete string

// rawInboxMarkRetry persists a retryable failure and its next attempt time instead of losing the delivery.
// rawInboxMarkRetry сохраняет повторяемую ошибку и время следующей попытки, не теряя доставку.
//
//go:embed sql/inbox__mark_retry.sql
var rawInboxMarkRetry string

// rawInboxMarkDeadLetter makes a non-retryable delivery terminal while retaining its diagnostic state.
// rawInboxMarkDeadLetter завершает невосстановимую доставку, сохраняя её диагностическое состояние.
//
//go:embed sql/inbox__mark_dead_letter.sql
var rawInboxMarkDeadLetter string

// rawInboxExpireToDeadLetter closes an exhausted delivery only after any processing lease has expired.
// rawInboxExpireToDeadLetter закрывает исчерпавшую попытки доставку только после истечения действующей аренды обработки.
//
//go:embed sql/inbox__expire_to_dead_letter.sql
var rawInboxExpireToDeadLetter string

// rawInboxRenew extends only the matching live claim so a stale worker cannot recover ownership.
// rawInboxRenew продлевает только совпадающий действующий claim, исключая возврат владения устаревшим исполнителем.
//
//go:embed sql/inbox__renew.sql
var rawInboxRenew string

// rawInboxCleanup removes only deliveries that satisfy the repository's retention predicate.
// rawInboxCleanup удаляет только доставки, соответствующие условию хранения репозитория.
//
//go:embed sql/inbox__cleanup.sql
var rawInboxCleanup string

// rawOperatorGetReceipt reads an operator command receipt to keep repeated requests idempotent.
// rawOperatorGetReceipt читает квитанцию операторской команды для идемпотентности повторных запросов.
//
//go:embed sql/operator__get_receipt.sql
var rawOperatorGetReceipt string

// rawOperatorInsertReceipt stores the outcome of an operator command in the same transaction as its effect.
// rawOperatorInsertReceipt сохраняет результат операторской команды в одной транзакции с её эффектом.
//
//go:embed sql/operator__insert_receipt.sql
var rawOperatorInsertReceipt string

// rawInboxRequeue returns a selected terminal delivery to the retry path under an authorized operator command.
// rawInboxRequeue возвращает выбранную завершённую доставку к повторам по разрешённой операторской команде.
//
//go:embed sql/inbox__requeue.sql
var rawInboxRequeue string

// rawInboxRecoverRejoin restores a delivery after reconciliation without inventing a new event identity.
// rawInboxRecoverRejoin восстанавливает доставку после сверки без создания нового идентификатора события.
//
//go:embed sql/inbox__recover_rejoin.sql
var rawInboxRecoverRejoin string

// rawBlockageGet reads the exact ordering blockage for one delivery stream.
// rawBlockageGet читает точную блокировку порядка для одного потока доставки.
//
//go:embed sql/blockage__get.sql
var rawBlockageGet string

// rawBlockageList enumerates ordering blockages for bounded operator inspection.
// rawBlockageList перечисляет блокировки порядка для ограниченного операторского просмотра.
//
//go:embed sql/blockage__list.sql
var rawBlockageList string

// rawDeliveryReadOutcome loads the persisted delivery outcome used by protected read paths.
// rawDeliveryReadOutcome загружает сохранённый исход доставки для защищённых путей чтения.
//
//go:embed sql/delivery__read_outcome.sql
var rawDeliveryReadOutcome string

// rawEffectInspect checks whether a named effect function is present before the processor calls it.
// rawEffectInspect проверяет наличие именованной функции эффекта до её вызова обработчиком.
//
//go:embed sql/effect__inspect.sql
var rawEffectInspect string

// rawEffectCall is the validated SQL template for invoking one configured effect function.
// rawEffectCall содержит проверяемый шаблон SQL для вызова одной настроенной функции эффекта.
//
//go:embed sql/effect__call.sql
var rawEffectCall string

// rawSchemaInspect enumerates schema objects required by inbox readiness checks.
// rawSchemaInspect перечисляет объекты схемы, необходимые проверкам готовности inbox.
//
//go:embed sql/schema__inspect.sql
var rawSchemaInspect string

// rawSchemaProbe verifies that the configured schema can be queried with the active connection.
// rawSchemaProbe проверяет доступность настроенной схемы через действующее соединение.
//
//go:embed sql/schema__probe.sql
var rawSchemaProbe string

// querySet holds validated SQL text for each repository operation instead of accepting runtime SQL fragments.
// querySet хранит проверенный SQL каждой операции репозитория вместо принятия фрагментов SQL во время исполнения.
type querySet struct {
	schemaSetSearchPath         string
	cursorEnsure                string
	cursorGetForUpdate          string
	cursorTakeFence             string
	cursorAdvance               string
	inboxGetByEventForUpdate    string
	inboxGetByEvent             string
	inboxGetBySequenceForUpdate string
	inboxInsertReceived         string
	inboxInsertStale            string
	inboxClaim                  string
	inboxComplete               string
	inboxMarkRetry              string
	inboxMarkDeadLetter         string
	inboxExpireToDeadLetter     string
	inboxRenew                  string
	inboxCleanup                string
	operatorGetReceipt          string
	operatorInsertReceipt       string
	inboxRequeue                string
	inboxRecoverRejoin          string
	blockageGet                 string
	blockageList                string
	deliveryReadOutcome         string
	effectInspect               string
	schemaInspect               string
	schemaProbe                 string
}

// loadQueries validates every embedded query's name and cardinality and binds
// it to exactly one repository field in the same explicit manifest.
// RU: loadQueries проверяет имя и кардинальность каждого встроенного запроса
// и связывает его ровно с одним полем репозитория в едином явном реестре.
func loadQueries() (querySet, error) {
	queries := querySet{}
	definitions := []struct {
		filename    string
		name        string
		cardinality string
		raw         string
		target      *string
	}{
		{"schema__set_search_path.sql", "schema__set_search_path", ":one", rawSchemaSetSearchPath, &queries.schemaSetSearchPath},
		{"cursor__ensure.sql", "cursor__ensure", ":exec", rawCursorEnsure, &queries.cursorEnsure},
		{"cursor__get_for_update.sql", "cursor__get_for_update", ":one", rawCursorGetForUpdate, &queries.cursorGetForUpdate},
		{"cursor__take_fence.sql", "cursor__take_fence", ":one", rawCursorTakeFence, &queries.cursorTakeFence},
		{"cursor__advance.sql", "cursor__advance", ":exec", rawCursorAdvance, &queries.cursorAdvance},
		{"inbox__get_by_event_for_update.sql", "inbox__get_by_event_for_update", ":one", rawInboxGetByEventForUpdate, &queries.inboxGetByEventForUpdate},
		{"inbox__get_by_event.sql", "inbox__get_by_event", ":one", rawInboxGetByEvent, &queries.inboxGetByEvent},
		{"inbox__get_by_sequence_for_update.sql", "inbox__get_by_sequence_for_update", ":one", rawInboxGetBySequenceForUpdate, &queries.inboxGetBySequenceForUpdate},
		{"inbox__insert_received.sql", "inbox__insert_received", ":one", rawInboxInsertReceived, &queries.inboxInsertReceived},
		{"inbox__insert_stale.sql", "inbox__insert_stale", ":one", rawInboxInsertStale, &queries.inboxInsertStale},
		{"inbox__claim.sql", "inbox__claim", ":one", rawInboxClaim, &queries.inboxClaim},
		{"inbox__complete.sql", "inbox__complete", ":exec", rawInboxComplete, &queries.inboxComplete},
		{"inbox__mark_retry.sql", "inbox__mark_retry", ":exec", rawInboxMarkRetry, &queries.inboxMarkRetry},
		{"inbox__mark_dead_letter.sql", "inbox__mark_dead_letter", ":exec", rawInboxMarkDeadLetter, &queries.inboxMarkDeadLetter},
		{"inbox__expire_to_dead_letter.sql", "inbox__expire_to_dead_letter", ":exec", rawInboxExpireToDeadLetter, &queries.inboxExpireToDeadLetter},
		{"inbox__renew.sql", "inbox__renew", ":one", rawInboxRenew, &queries.inboxRenew},
		{"inbox__cleanup.sql", "inbox__cleanup", ":one", rawInboxCleanup, &queries.inboxCleanup},
		{"operator__get_receipt.sql", "operator__get_receipt", ":one", rawOperatorGetReceipt, &queries.operatorGetReceipt},
		{"operator__insert_receipt.sql", "operator__insert_receipt", ":one", rawOperatorInsertReceipt, &queries.operatorInsertReceipt},
		{"inbox__requeue.sql", "inbox__requeue", ":exec", rawInboxRequeue, &queries.inboxRequeue},
		{"inbox__recover_rejoin.sql", "inbox__recover_rejoin", ":exec", rawInboxRecoverRejoin, &queries.inboxRecoverRejoin},
		{"blockage__get.sql", "blockage__get", ":one", rawBlockageGet, &queries.blockageGet},
		{"blockage__list.sql", "blockage__list", ":many", rawBlockageList, &queries.blockageList},
		{"delivery__read_outcome.sql", "delivery__read_outcome", ":one", rawDeliveryReadOutcome, &queries.deliveryReadOutcome},
		{"effect__inspect.sql", "effect__inspect", ":one", rawEffectInspect, &queries.effectInspect},
		{"schema__inspect.sql", "schema__inspect", ":many", rawSchemaInspect, &queries.schemaInspect},
		{"schema__probe.sql", "schema__probe", ":one", rawSchemaProbe, &queries.schemaProbe},
	}
	for index := range definitions {
		loaded, err := loadQuery(
			definitions[index].filename,
			definitions[index].name,
			definitions[index].cardinality,
			definitions[index].raw,
		)
		if err != nil {
			return querySet{}, err
		}
		*definitions[index].target = loaded
	}
	return queries, nil
}

// loadQuery rejects a mismatched filename, header, cardinality, or empty SQL body and returns trimmed text.
// loadQuery отклоняет несоответствующие имя файла, заголовок, кардинальность или пустое тело SQL и возвращает очищенный текст.
func loadQuery(filename, name, cardinality, raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	lines := strings.SplitN(trimmed, "\n", 2)
	expectedHeader := fmt.Sprintf("-- name: %s %s", name, cardinality)
	if len(lines) != 2 || strings.TrimSpace(lines[0]) != expectedHeader ||
		strings.TrimSpace(lines[1]) == "" || filename != name+".sql" {
		return "", ErrInvalidConfiguration
	}
	return trimmed, nil
}

// buildEffectCallQuery substitutes exactly one function placeholder after validating the embedded template.
// buildEffectCallQuery подставляет ровно одно имя функции после проверки встроенного шаблона.
func buildEffectCallQuery(identifier string) (string, error) {
	template, err := loadQuery(
		"effect__call.sql",
		"effect__call",
		":one",
		rawEffectCall,
	)
	if err != nil || strings.Count(template, "__postgresinbox_effect_function__") != 1 {
		return "", ErrInvalidConfiguration
	}
	return strings.Replace(
		template,
		"__postgresinbox_effect_function__",
		identifier,
		1,
	), nil
}
