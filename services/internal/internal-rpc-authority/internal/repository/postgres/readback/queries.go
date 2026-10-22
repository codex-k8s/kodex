package readback

import (
	_ "embed"
	"errors"
	"strings"
)

// resolveIntentSQL embeds lookup of an unexpired PINNED intent bound to the peer SPIFFE identity under the restore fence, returning persisted credential/key/snapshot bindings rather than trusting an ID alone.
// resolveIntentSQL встраивает поиск непросроченного намерения PINNED с привязкой к SPIFFE-идентичности peer под ограждением восстановления, возвращая сохранённые привязки credential, ключа и снимка вместо доверия одному ID.
//
//go:embed sql/readback__resolve_intent.sql
var resolveIntentSQL string

// issueChallengeSQL embeds the database-owned challenge issuance call with exact intent, nonce/digests, credential binding and semantic idempotency digest; loading the string does not mint or store a challenge.
// issueChallengeSQL встраивает принадлежащий БД вызов выдачи challenge с точными намерением, nonce и дайджестами, привязкой credential и семантическим дайджестом идемпотентности; загрузка строки не создаёт и не сохраняет challenge.
//
//go:embed sql/readback__issue_challenge.sql
var issueChallengeSQL string

// loadChallengeSQL embeds challenge/intent join readback requiring both peer SPIFFE bindings and an open restore fence. It returns consumed/expiry fields for later validation rather than filtering consumed challenges here.
// loadChallengeSQL встраивает чтение объединённых challenge и intent с обеими SPIFFE-привязками peer и открытым ограждением восстановления. Поля потребления и срока возвращаются для последующей проверки, а потреблённые challenge здесь не отфильтровываются.
//
//go:embed sql/readback__load_challenge.sql
var loadChallengeSQL string

// loadReceiptSQL embeds receipt lookup scoped to the peer SPIFFE identity and idempotency key, returning the stored evidence, verifier generation and timestamps without issuing another receipt.
// loadReceiptSQL встраивает поиск квитанции в области SPIFFE-идентичности peer и ключа идемпотентности с сохранёнными доказательством, поколением verifier и временными метками без выдачи новой квитанции.
//
//go:embed sql/readback__load_receipt.sql
var loadReceiptSQL string

// consumeChallengeSQL embeds the database function for challenge consumption tied to receipt/evidence/verifier and semantic idempotency bindings; one-use state enforcement remains in the function, not the embedded variable.
// consumeChallengeSQL встраивает функцию БД потребления challenge с привязками квитанции, доказательства, verifier и семантической идемпотентности; одноразовость обеспечивает функция, а не встроенная переменная.
//
//go:embed sql/readback__consume_challenge.sql
var consumeChallengeSQL string

// readinessSQL embeds readback-attestor role/session checks, required reads/function execution, forbidden direct challenge/receipt DML and the restore fence. Header validation alone does not exercise these privileges.
// readinessSQL встраивает проверки роли и сессии readback-attestor, необходимых чтений и исполнения функций, запрета прямого DML challenge и квитанций и ограждения восстановления. Проверка заголовка сама не проверяет эти привилегии исполнением.
//
//go:embed sql/readback__readiness.sql
var readinessSQL string

// activateTrustSQL embeds the server-owned trust activation call bound to root, bundle, trust revisions/digests, predecessor and served-state digest/time; it does not load or verify a mounted JWK itself.
// activateTrustSQL встраивает серверный вызов активации доверия с привязками корня, bundle, ревизий и дайджестов доверия, предшественника и обслуживаемого состояния и времени; смонтированный JWK сама строка не читает и не проверяет.
//
//go:embed sql/readback__activate_trust.sql
var activateTrustSQL string

// trustReadinessSQL embeds exact trust-watermark existence and activation-function privilege checks for the fixed attestor identity. It compares persisted bindings, not signature bytes or local wall-clock freshness.
// trustReadinessSQL встраивает проверку существования точной отметки доверия и права исполнения функции активации для фиксированной идентичности attestor. Сравниваются сохранённые привязки, а не байты подписи или свежесть по локальным часам.
//
//go:embed sql/readback__trust_readiness.sql
var trustReadinessSQL string

// validateQueries checks eight fixed readback bodies for nonempty trimmed text and matching name followed by a space, returning a fixed error. It is not SQL/cardinality validation, live readiness or directory discovery.
// validateQueries проверяет восемь фиксированных тел readback на непустой обрезанный текст и совпадающий префикс имени с пробелом, возвращая фиксированную ошибку. Это не проверка синтаксиса SQL и cardinality, живой готовности или каталога.
func validateQueries() error {
	for name, query := range map[string]string{
		"readback__resolve_intent":    resolveIntentSQL,
		"readback__issue_challenge":   issueChallengeSQL,
		"readback__load_challenge":    loadChallengeSQL,
		"readback__load_receipt":      loadReceiptSQL,
		"readback__consume_challenge": consumeChallengeSQL,
		"readback__readiness":         readinessSQL,
		"readback__activate_trust":    activateTrustSQL,
		"readback__trust_readiness":   trustReadinessSQL,
	} {
		if strings.TrimSpace(query) == "" ||
			!strings.HasPrefix(strings.TrimSpace(query), "-- name: "+name+" ") {
			return errors.New("invalid embedded readback query")
		}
	}
	return nil
}
