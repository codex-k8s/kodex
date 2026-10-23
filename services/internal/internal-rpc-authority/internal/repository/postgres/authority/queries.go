package authority

import (
	_ "embed"
	"fmt"
	"strings"
)

// proofReserveSQL embeds the proof reservation CTE with a restore fence and nonregressing caller/operation/issuer watermark. Its string is executed by the store; merely loading it neither consumes a proof nor establishes authority.
// proofReserveSQL встраивает CTE резервирования proof с ограждением восстановления и неоткатываемой отметкой caller, operation и issuer. Строку выполняет store; её загрузка не потребляет proof и не устанавливает полномочия.
//
//go:embed sql/proof__reserve.sql
var proofReserveSQL string

// contextReserveSQL embeds the fenced insert of one target/JTI replay reservation, returning acceptance only for a newly inserted row; caller bindings and execution belong to the store.
// contextReserveSQL встраивает защищённую ограждением вставку replay reservation по target и JTI, возвращая принятие только для новой строки; привязками вызывающего и исполнением владеет store.
//
//go:embed sql/context__reserve.sql
var contextReserveSQL string

// continuationReserveSQL embeds one statement that requires an unexpired accepted parent binding before inserting the child's proof reservation. The selected booleans distinguish parent acceptance from successful child insertion.
// continuationReserveSQL встраивает один statement, требующий непросроченную принятую привязку родителя до вставки reservation дочернего proof. Возвращённые bool отличают принятие родителя от успешной вставки дочернего.
//
//go:embed sql/continuation__reserve.sql
var continuationReserveSQL string

// verifierActivateSnapshotSQL embeds snapshot-watermark activation with fresh attestation, monotonic revisions and signed predecessor/history constraints. Loading the bytes does not execute activation or verify a signature in Go.
// verifierActivateSnapshotSQL встраивает активацию отметки снимка со свежей аттестацией, монотонными ревизиями и ограничениями подписанной истории и предшественника. Загрузка байтов не активирует снимок и не проверяет подпись в Go.
//
//go:embed sql/verifier__activate_snapshot.sql
var verifierActivateSnapshotSQL string

// verifierAcceptContextSQL embeds issued-binding validation, exact-or-advanced snapshot acceptance and one-use context reservation. The exact-snapshot path avoids an unnecessary watermark upsert while retaining receipt freshness checks.
// verifierAcceptContextSQL встраивает проверку issued binding, принятие точного либо продвинутого снимка и одноразовое резервирование контекста. Путь точного снимка избегает лишнего upsert отметки, сохраняя проверки свежести квитанции.
//
//go:embed sql/verifier__accept_context.sql
var verifierAcceptContextSQL string

// verifierFreshnessSQL embeds exact workload/snapshot/generation readback returning the shared receipt, its computed freshness deadline and database time under the restore fence; it is not a local pod clock check.
// verifierFreshnessSQL встраивает чтение точных workload, снимка и поколений с общей квитанцией, вычисленным сроком свежести и временем БД под ограждением восстановления; это не проверка локальных часов пода.
//
//go:embed sql/verifier__freshness.sql
var verifierFreshnessSQL string

// verifierReadinessSQL embeds existence/freshness checks for the workload's stored exact snapshot and receipt, plus the restore fence. It does not require the caller replica's own receipt identifier.
// verifierReadinessSQL встраивает проверку существования и свежести точного сохранённого снимка workload и квитанции вместе с ограждением восстановления. Собственный идентификатор квитанции реплики вызывающего не требуется.
//
//go:embed sql/verifier__readiness.sql
var verifierReadinessSQL string

// contextReservationsDeleteExpiredSQL embeds cleanup of context replay rows only when both expiry and acceptance time precede the supplied cutoff; the store owns choosing that cutoff and executing deletion.
// contextReservationsDeleteExpiredSQL встраивает очистку context replay rows, только когда срок и время принятия раньше переданного порога; выбором порога и удалением владеет store.
//
//go:embed sql/context_reservations__delete_expired.sql
var contextReservationsDeleteExpiredSQL string

// proofReservationsDeleteExpiredSQL embeds proof-reservation cleanup with both expiry and acceptance cutoffs, leaving the separate proof watermark untouched by this statement.
// proofReservationsDeleteExpiredSQL встраивает очистку proof reservations по порогам срока и времени принятия, не затрагивая этим statement отдельную отметку proof.
//
//go:embed sql/proof_reservations__delete_expired.sql
var proofReservationsDeleteExpiredSQL string

// contextRegisterIssuedSQL embeds the database-owned issued-context registration call with exact identity, digest, snapshot, timing and parent bindings; registration policy is not implemented by this string.
// contextRegisterIssuedSQL встраивает принадлежащий БД вызов регистрации выданного контекста с точными идентичностью, дайджестом, снимком, временем и родителем; сама строка не реализует политику регистрации.
//
//go:embed sql/context__register_issued.sql
var contextRegisterIssuedSQL string

// contextBindingsCleanupSQL embeds the caller-scoped database function for issued-binding cleanup; it does not expose a general table-delete operation or run automatically on declaration.
// contextBindingsCleanupSQL встраивает функцию БД очистки issued bindings в области caller; общий delete таблицы не предоставляется, объявление не запускает очистку.
//
//go:embed sql/context_bindings__cleanup.sql
var contextBindingsCleanupSQL string

// contextIssuedReadbackSQL embeds the exact issued-binding readback function with identity, digest, snapshot, timing and parent arguments rather than trusting a JTI lookup alone.
// contextIssuedReadbackSQL встраивает функцию чтения точной issued binding с аргументами идентичности, дайджеста, снимка, времени и родителя вместо доверия одному поиску JTI.
//
//go:embed sql/context__issued_readback.sql
var contextIssuedReadbackSQL string

// querySet holds the authority adapter's fixed embedded SQL bodies after header validation, keeping semantic query identities explicit rather than accepting arbitrary runtime query names.
// querySet хранит фиксированные встроенные тела SQL адаптера authority после проверки заголовков, явно сохраняя семантические идентичности запросов вместо произвольных runtime-имён.
type querySet struct {
	contextIssuedReadback            string
	contextRegisterIssued            string
	contextBindingsCleanup           string
	proofReserve                     string
	contextReserve                   string
	continuationReserve              string
	verifierActivateSnapshot         string
	verifierAcceptContext            string
	verifierReadiness                string
	verifierFreshness                string
	contextReservationsDeleteExpired string
	proofReservationsDeleteExpired   string
}

// loadQueries binds twelve embedded bodies to named fields and checks nonempty trimmed text plus the expected name/cardinality prefix. It returns original bytes, performs no SQL parsing, directory enumeration or database execution, and rejects a failed prefix with the query name.
// loadQueries связывает двенадцать встроенных тел с именованными полями и проверяет непустой обрезанный текст и ожидаемый префикс имени и cardinality. Возвращаются исходные байты без разбора SQL, обхода каталога или исполнения в БД; отказ префикса содержит имя запроса.
func loadQueries() (querySet, error) {
	queries := querySet{
		contextIssuedReadback:            contextIssuedReadbackSQL,
		contextRegisterIssued:            contextRegisterIssuedSQL,
		contextBindingsCleanup:           contextBindingsCleanupSQL,
		proofReserve:                     proofReserveSQL,
		contextReserve:                   contextReserveSQL,
		continuationReserve:              continuationReserveSQL,
		verifierActivateSnapshot:         verifierActivateSnapshotSQL,
		verifierAcceptContext:            verifierAcceptContextSQL,
		verifierReadiness:                verifierReadinessSQL,
		verifierFreshness:                verifierFreshnessSQL,
		contextReservationsDeleteExpired: contextReservationsDeleteExpiredSQL,
		proofReservationsDeleteExpired:   proofReservationsDeleteExpiredSQL,
	}
	for _, definition := range []struct {
		name        string
		cardinality string
		body        string
	}{
		{"context__issued_readback", "one", queries.contextIssuedReadback},
		{"context__register_issued", "one", queries.contextRegisterIssued},
		{"context_bindings__cleanup", "one", queries.contextBindingsCleanup},
		{"proof__reserve", "one", queries.proofReserve},
		{"context__reserve", "one", queries.contextReserve},
		{"continuation__reserve", "one", queries.continuationReserve},
		{"verifier__activate_snapshot", "one", queries.verifierActivateSnapshot},
		{"verifier__accept_context", "one", queries.verifierAcceptContext},
		{"verifier__readiness", "one", queries.verifierReadiness},
		{"verifier__freshness", "one", queries.verifierFreshness},
		{
			"context_reservations__delete_expired",
			"exec",
			queries.contextReservationsDeleteExpired,
		},
		{
			"proof_reservations__delete_expired",
			"exec",
			queries.proofReservationsDeleteExpired,
		},
	} {
		header := fmt.Sprintf("-- name: %s :%s", definition.name, definition.cardinality)
		if strings.TrimSpace(definition.body) == "" ||
			!strings.HasPrefix(strings.TrimSpace(definition.body), header) {
			return querySet{}, fmt.Errorf("invalid embedded query %s", definition.name)
		}
	}
	return queries, nil
}
