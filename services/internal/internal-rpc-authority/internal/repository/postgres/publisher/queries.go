package publisher

import (
	_ "embed"
	"errors"
	"strings"
)

// loadDeliverySQL embeds retrieval of the persisted publisher delivery receipt by idempotency key, including directive and credential-generation bindings; it does not recreate or resend delivery.
// loadDeliverySQL встраивает получение сохранённой квитанции доставки publisher по ключу идемпотентности вместе с привязками директивы и поколений credential; доставка не пересоздаётся и не повторяется.
//
//go:embed sql/publisher__load_delivery.sql
var loadDeliverySQL string

// saveDeliverySQL embeds delivery-receipt insertion or conflict readback only when the stored directive digest and JTI match. The existing receipt fields are returned instead of silently replacing them on a conflicting request.
// saveDeliverySQL встраивает вставку квитанции доставки либо чтение конфликта только при совпадении сохранённых дайджеста директивы и JTI. Возвращаются прежние поля квитанции без их скрытой замены конфликтующим запросом.
//
//go:embed sql/publisher__save_delivery.sql
var saveDeliverySQL string

// readinessSQL embeds publisher role/session separation, restore-fence, receipt-table privilege and active runtime-identity checks. Declaring or header-validating this query does not prove live database readiness.
// readinessSQL встраивает проверки разделения роли publisher и сессии, ограждения восстановления, привилегий таблицы квитанций и активной runtime-идентичности. Объявление или проверка заголовка не доказывают живую готовность БД.
//
//go:embed sql/publisher__readiness.sql
var readinessSQL string

// pinReadbackIntentSQL embeds insertion of a PINNED intent or exact existing-intent retrieval by ID, digest and workload SPIFFE identity. The database statement returns material bindings; the variable does not sign or authorize them.
// pinReadbackIntentSQL встраивает вставку намерения PINNED либо получение точного существующего намерения по ID, дайджесту и SPIFFE-идентичности workload. Statement БД возвращает привязки материала; переменная их не подписывает и не авторизует.
//
//go:embed sql/publisher__pin_readback_intent.sql
var pinReadbackIntentSQL string

// loadPinnedReadbackIntentSQL embeds readback restricted to the pinned intent's ID, digest, workload SPIFFE identity and PINNED status, returning the persisted key and generation bindings.
// loadPinnedReadbackIntentSQL встраивает чтение по ID закреплённого намерения, дайджесту, SPIFFE-идентичности workload и статусу PINNED с возвратом сохранённых привязок ключа и поколений.
//
//go:embed sql/publisher__load_pinned_readback_intent.sql
var loadPinnedReadbackIntentSQL string

// loadSnapshotHistorySQL embeds newest-first retrieval of thirty-three revision/digest pairs, allowing the current revision to be removed while preserving a thirty-two-predecessor window after restart.
// loadSnapshotHistorySQL встраивает чтение тридцати трёх пар ревизии и дайджеста от новых к старым, позволяя убрать текущую ревизию и сохранить окно тридцати двух предшественников после перезапуска.
//
//go:embed sql/publisher__load_snapshot_history.sql
var loadSnapshotHistorySQL string

// loadSnapshotPublicationSQL embeds publication readback by exact source revision and publication-input digest, returning the persisted intent, predecessor and signed snapshot instead of generating a new publication.
// loadSnapshotPublicationSQL встраивает чтение публикации по точной исходной ревизии и дайджесту входа публикации с сохранёнными намерением, предшественником и подписанным снимком вместо новой генерации публикации.
//
//go:embed sql/publisher__load_snapshot_publication.sql
var loadSnapshotPublicationSQL string

// loadSnapshotPredecessorSQL embeds the database-owned predecessor lookup bound to source revision/digest and registry digest; cross-domain provenance is resolved by that function, not inferred from a local counter.
// loadSnapshotPredecessorSQL встраивает принадлежащий БД поиск предшественника с привязкой к исходной ревизии, дайджесту и дайджесту реестра; междоменное происхождение разрешает функция, а не локальный счётчик.
//
//go:embed sql/publisher__load_snapshot_predecessor.sql
var loadSnapshotPredecessorSQL string

// prepareRotationSQL embeds the function call that prepares snapshot rotation using exact intent, source and predecessor bindings plus expected readback count. Declaration itself performs no transition.
// prepareRotationSQL встраивает вызов подготовки ротации снимка с точными привязками намерения, источника и предшественника и ожидаемым числом readback. Объявление само не выполняет переход.
//
//go:embed sql/publisher__prepare_rotation.sql
var prepareRotationSQL string

// loadOrPrepareRotationOperationSQL embeds the typed database function for loading or preparing a registry-bound rotation operation and its persisted timing/status fields; arguments are supplied by the adapter.
// loadOrPrepareRotationOperationSQL встраивает типизированную функцию БД чтения или подготовки операции ротации, связанной с реестром, и её сохранённых полей времени и статуса; аргументы передаёт адаптер.
//
//go:embed sql/publisher__load_or_prepare_rotation_operation.sql
var loadOrPrepareRotationOperationSQL string

// loadRotationOperationSQL embeds one rotation-operation lookup by UUID with explicit result columns, leaving decoding and caller eligibility to the owning adapter and database boundary.
// loadRotationOperationSQL встраивает поиск одной операции ротации по UUID с явными колонками результата; декодированием и допустимостью caller владеют адаптер и граница БД.
//
//go:embed sql/publisher__load_rotation_operation.sql
var loadRotationOperationSQL string

// prepareRotationPhaseSQL embeds the typed database phase-preparation call, preserving the positional argument contract instead of constructing SQL from runtime phase text.
// prepareRotationPhaseSQL встраивает типизированный вызов БД подготовки фазы с сохранением контракта позиционных аргументов вместо построения SQL из runtime-текста фазы.
//
//go:embed sql/publisher__prepare_rotation_phase.sql
var prepareRotationPhaseSQL string

// advanceRotationOperationSQL embeds the typed operation-advance function and explicit persisted state columns; the server-owned function, not loading this string, determines the permitted transition.
// advanceRotationOperationSQL встраивает типизированную функцию продвижения операции и явные колонки сохранённого состояния; допустимый переход определяет серверная функция, а не загрузка строки.
//
//go:embed sql/publisher__advance_rotation_operation.sql
var advanceRotationOperationSQL string

// beginRotationDeliverySQL embeds the database call that begins delivery for an exact intent/source revision/digest; it does not itself deliver Kubernetes material or open a network client.
// beginRotationDeliverySQL встраивает вызов БД начала доставки для точных намерения, исходной ревизии и дайджеста; материал Kubernetes не доставляется и сетевой клиент не открывается самой строкой.
//
//go:embed sql/publisher__begin_rotation_delivery.sql
var beginRotationDeliverySQL string

// markRotationDeliveredSQL embeds marking exact intent/source material as delivered through the database-owned transition; it is separate from beginning delivery and from snapshot promotion.
// markRotationDeliveredSQL встраивает отметку доставки точного материала намерения и источника через принадлежащий БД переход; это отдельно от начала доставки и продвижения снимка.
//
//go:embed sql/publisher__mark_rotation_delivered.sql
var markRotationDeliveredSQL string

// appendSnapshotSQL embeds durable signed snapshot-history append with exact publication intent/input digest, revisions, predecessor and expected readbacks. Loading it does not publish a broker event or write database state.
// appendSnapshotSQL встраивает устойчивое добавление подписанной истории снимка с точными намерением публикации, дайджестом входа, ревизиями, предшественником и ожидаемыми readback. Загрузка не публикует событие брокера и не пишет состояние БД.
//
//go:embed sql/publisher__append_snapshot.sql
var appendSnapshotSQL string

// promoteSnapshotSQL embeds promotion with exact expected workload IDs, roles and generations alongside the count and publication binding; a global count of arbitrary receipts is not its input contract.
// promoteSnapshotSQL встраивает продвижение с точными ожидаемыми ID workload, ролями и поколениями вместе с числом и привязкой публикации; глобальный счётчик произвольных квитанций не является контрактом входа.
//
//go:embed sql/publisher__promote_snapshot.sql
var promoteSnapshotSQL string

// validateQueries checks seventeen fixed publisher bodies for nonempty text and their exact name prefix followed by a space. It does not validate cardinality, parse SQL, execute a query or enumerate extra files; map iteration can choose which invalid body is reported first.
// validateQueries проверяет семнадцать фиксированных тел publisher на непустой текст и точный префикс имени с последующим пробелом. Cardinality, синтаксис SQL, исполнение и лишние файлы не проверяются; порядок map может менять первый обнаруженный неверный запрос.
func validateQueries() error {
	for name, query := range map[string]string{
		"publisher__load_delivery":                      loadDeliverySQL,
		"publisher__save_delivery":                      saveDeliverySQL,
		"publisher__readiness":                          readinessSQL,
		"publisher__pin_readback_intent":                pinReadbackIntentSQL,
		"publisher__load_pinned_readback_intent":        loadPinnedReadbackIntentSQL,
		"publisher__load_snapshot_history":              loadSnapshotHistorySQL,
		"publisher__load_snapshot_publication":          loadSnapshotPublicationSQL,
		"publisher__load_snapshot_predecessor":          loadSnapshotPredecessorSQL,
		"publisher__prepare_rotation":                   prepareRotationSQL,
		"publisher__load_rotation_operation":            loadRotationOperationSQL,
		"publisher__load_or_prepare_rotation_operation": loadOrPrepareRotationOperationSQL,
		"publisher__prepare_rotation_phase":             prepareRotationPhaseSQL,
		"publisher__advance_rotation_operation":         advanceRotationOperationSQL,
		"publisher__begin_rotation_delivery":            beginRotationDeliverySQL,
		"publisher__mark_rotation_delivered":            markRotationDeliveredSQL,
		"publisher__append_snapshot":                    appendSnapshotSQL,
		"publisher__promote_snapshot":                   promoteSnapshotSQL,
	} {
		if strings.TrimSpace(query) == "" ||
			!strings.HasPrefix(strings.TrimSpace(query), "-- name: "+name+" ") {
			return errors.New("invalid embedded publisher query")
		}
	}
	return nil
}
