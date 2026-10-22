package integrationfixture

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const (
	// maximumJournalBytes bounds the decoded nonempty journal identifier in bytes, not Unicode code points.
	// maximumJournalBytes ограничивает декодированный непустой идентификатор журнала в байтах, а не кодовых точках Unicode.
	maximumJournalBytes = 120
	// maximumValueBytes bounds create/update payload text after JSON decoding and before store mutation.
	// maximumValueBytes ограничивает текст create/update после разбора JSON и до изменения хранилища.
	maximumValueBytes = 4096
	// maximumRequestBodySize caps the encoded mutation body; the reader consumes one extra byte to detect overflow.
	// maximumRequestBodySize ограничивает закодированное тело изменения; читатель получает один лишний байт для обнаружения превышения.
	maximumRequestBodySize = 8 << 10
	// maximumBearerTokenSize caps the token bytes accepted by the optional OpenAPI fixture authentication path.
	// maximumBearerTokenSize ограничивает байты токена необязательного защищённого OpenAPI-пути оснастки.
	maximumBearerTokenSize = 512
	// minimumBearerTokenSize rejects short fixture tokens before hashing; it does not measure entropy or issue credentials.
	// minimumBearerTokenSize отклоняет короткие токены оснастки до хеширования, не измеряя энтропию и не выдавая реквизиты.
	minimumBearerTokenSize = 32
	// openAPIPathPrefix selects only the bearer-protected fixture namespace; ordinary local fixture routes remain separate.
	// openAPIPathPrefix выбирает только защищённое bearer-пространство оснастки; обычные локальные маршруты остаются отдельными.
	openAPIPathPrefix = "/openapi"
	// replayFaultValuePrefix enables a synthetic delay after a first committed effect to exercise lost-response replay.
	// replayFaultValuePrefix включает искусственную задержку после первого сохранённого эффекта для проверки повтора при потере ответа.
	replayFaultValuePrefix = "kodex-e2e-replay:"
	// replayFaultDelay is the default post-mutation delay for the explicit replay-fault fixture prefix.
	// replayFaultDelay задаёт обычную задержку после изменения для явного префикса проверки потери ответа.
	replayFaultDelay = 4 * time.Second
)

// idempotencyKeyPattern allows one 1..128-byte ASCII key beginning with an alphanumeric character.
// idempotencyKeyPattern допускает один ASCII-ключ длиной 1..128 байт, начинающийся буквой или цифрой.
var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

// errorResponse exposes only a closed fixture error code, not an internal error or credential detail.
// errorResponse выдаёт только закрытый код ошибки оснастки, а не внутреннюю ошибку или подробности реквизитов.
type errorResponse struct {
	Error string `json:"error"`
}

// Handler serves the closed disposable journal contract with atomic probe readiness and optional prefixed bearer authentication.
// Readiness changes probe output, not admission to every journal operation; state durability is limited to the supplied Store's process.
// Handler обслуживает закрытый контракт одноразового журнала с атомарной готовностью probe и необязательной bearer-защитой префикса.
// Готовность меняет ответ probe, а не допуск ко всем операциям журнала; устойчивость состояния ограничена процессом переданного Store.
type Handler struct {
	store               *Store
	ready               atomic.Bool
	replayDelay         time.Duration
	bearerDigest        [sha256.Size]byte
	bearerAuthAvailable bool
}

// NewHandler binds the caller's fixture store with the default replay delay and no OpenAPI bearer route enabled.
// NewHandler связывает хранилище оснастки вызывающего с обычной задержкой повтора без включения bearer-маршрута OpenAPI.
func NewHandler(store *Store) *Handler {
	return newHandler(store, replayFaultDelay)
}

// newHandler injects a replay delay for deterministic tests without starting a listener or allocating another store.
// newHandler подставляет задержку повтора для детерминированных тестов без запуска listener или создания другого хранилища.
func newHandler(store *Store, delay time.Duration) *Handler {
	return &Handler{store: store, replayDelay: delay}
}

// NewBearerHandler enables digest-checked bearer authentication only for /openapi paths, retaining the plain fixture routes.
// The digest and store are caller inputs; construction does not validate TLS, allocate secrets or confer platform permissions.
// NewBearerHandler включает проверку bearer по дайджесту только для путей /openapi, сохраняя обычные маршруты оснастки.
// Дайджест и хранилище задаёт вызывающий; конструктор не проверяет TLS, не создаёт секреты и не выдаёт права платформы.
func NewBearerHandler(store *Store, bearerDigest [sha256.Size]byte) *Handler {
	return &Handler{store: store, replayDelay: replayFaultDelay, bearerDigest: bearerDigest, bearerAuthAvailable: true}
}

// SetReady atomically changes the readiness probe flag without modifying stored effects or rejecting other routes.
// SetReady атомарно меняет флаг probe готовности без изменения сохранённых эффектов или запрета других маршрутов.
func (handler *Handler) SetReady(ready bool) {
	handler.ready.Store(ready)
}

// ServeHTTP sets safe response headers, rejects any query, dispatches probes and authenticates only the OpenAPI prefix before routing.
// Journal path/method/body validation remains in the selected handlers; unknown paths do not allocate or mutate store state.
// ServeHTTP задаёт защитные заголовки, отклоняет любой query, направляет probes и проверяет только OpenAPI-префикс до маршрутизации.
// Проверка пути, метода и тела журнала остаётся выбранным обработчикам; неизвестные пути не создают и не меняют состояние хранилища.
func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	setResponseHeaders(writer)
	if request.URL.RawQuery != "" {
		writeError(writer, http.StatusBadRequest, "query_not_allowed")
		return
	}
	escapedPath := request.URL.EscapedPath()
	switch escapedPath {
	case "/healthz":
		handler.serveHealth(writer, request)
		return
	case "/readyz":
		handler.serveReadiness(writer, request)
		return
	}
	if strings.HasPrefix(escapedPath, openAPIPathPrefix+"/") {
		if !handler.bearerAuthAvailable {
			writeError(writer, http.StatusNotFound, "not_found")
			return
		}
		if !handler.authorizeBearer(request.Header) {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="kodex-local-integration-fixture"`)
			writeError(writer, http.StatusUnauthorized, "authentication_required")
			return
		}
		escapedPath = strings.TrimPrefix(escapedPath, openAPIPathPrefix)
	}
	if journal, ok := diagnosticJournalPath(escapedPath); ok {
		handler.readDiagnostic(writer, request, journal)
		return
	}

	journal, entries, ok := journalPath(escapedPath)
	if !ok {
		writeError(writer, http.StatusNotFound, "not_found")
		return
	}
	if entries {
		handler.appendEntry(writer, request, journal)
		return
	}
	handler.readJournal(writer, request, journal)
}

// authorizeBearer requires one bounded Bearer header without ASCII controls, space or DEL and constant-time compares its SHA-256 digest.
// It authenticates this local fixture token only, not a tenant, role or platform authorization context.
// authorizeBearer требует один ограниченный заголовок Bearer без управляющих ASCII-символов, пробела или DEL и сравнивает SHA-256 дайджест за постоянное время.
// Аутентифицируется только токен локальной оснастки, а не tenant, роль или контекст полномочий платформы.
func (handler *Handler) authorizeBearer(header http.Header) bool {
	values := header.Values("Authorization")
	if len(values) != 1 {
		return false
	}
	scheme, token, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || len(token) < minimumBearerTokenSize || len(token) > maximumBearerTokenSize {
		return false
	}
	for _, character := range token {
		if character <= ' ' || character == '\u007f' {
			return false
		}
	}
	digest := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(digest[:], handler.bearerDigest[:]) == 1
}

// readDiagnostic returns fixture-only counters/state for one validated journal after the common GET/empty-body guard.
// readDiagnostic возвращает счётчики и состояние оснастки для одного проверенного журнала после общей проверки GET и пустого тела.
func (handler *Handler) readDiagnostic(writer http.ResponseWriter, request *http.Request, journal string) {
	if !validReadRequest(writer, request) {
		return
	}
	writeJSON(writer, http.StatusOK, handler.store.ReadDiagnostic(journal))
}

// serveHealth reports process health for a valid GET even when the independent readiness flag is false.
// serveHealth сообщает здоровье процесса при допустимом GET, даже если независимый флаг готовности выключен.
func (handler *Handler) serveHealth(writer http.ResponseWriter, request *http.Request) {
	if !validReadRequest(writer, request) {
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

// serveReadiness checks the read-only request shape before returning the atomic ready flag as 200 or 503.
// serveReadiness проверяет форму запроса чтения до выдачи атомарного флага готовности статусом 200 либо 503.
func (handler *Handler) serveReadiness(writer http.ResponseWriter, request *http.Request) {
	if !validReadRequest(writer, request) {
		return
	}
	if !handler.ready.Load() {
		writeError(writer, http.StatusServiceUnavailable, "not_ready")
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "ready"})
}

// readJournal returns current state or one exact historical effect receipt selected by a validated optional idempotency key.
// It performs no mutation; a key belonging to another journal is not exposed through this journal's route.
// readJournal возвращает текущее состояние либо точную историческую квитанцию эффекта по проверенному необязательному ключу идемпотентности.
// Изменение не выполняется; ключ другого журнала не раскрывается через маршрут этого журнала.
func (handler *Handler) readJournal(writer http.ResponseWriter, request *http.Request, journal string) {
	if !validReadRequest(writer, request) {
		return
	}
	effectKey, ok := optionalIdempotencyKey(request.Header)
	if !ok {
		writeError(writer, http.StatusBadRequest, "idempotency_key_invalid")
		return
	}
	if effectKey != "" {
		projection, exists := handler.store.ReadEffect(journal, effectKey)
		if !exists {
			writeError(writer, http.StatusNotFound, "effect_not_found")
			return
		}
		writeJSON(writer, http.StatusOK, projection)
		return
	}
	writeJSON(writer, http.StatusOK, handler.store.Read(journal, effectKey))
}

// appendEntry validates POST/key/input, executes one mutex-protected fixture mutation and maps closed store outcomes to HTTP.
// Replay-fault delay occurs after the effect is stored; canceled delivery may return no body while a later exact retry reads its receipt.
// appendEntry проверяет POST, ключ и вход, выполняет одно изменение оснастки под mutex и сопоставляет закрытые исходы HTTP-статусам.
// Задержка потери ответа начинается после сохранения эффекта; отменённая доставка может не вернуть тело, а последующий точный повтор прочитает квитанцию.
func (handler *Handler) appendEntry(writer http.ResponseWriter, request *http.Request, journal string) {
	if request.Method != http.MethodPost {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	effectKey, ok := requiredIdempotencyKey(request.Header)
	if !ok {
		writeError(writer, http.StatusBadRequest, "idempotency_key_invalid")
		return
	}
	input, err := decodeMutationInput(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "request_body_invalid")
		return
	}
	projection, replayed, err := handler.store.Mutate(journal, effectKey, input)
	if errors.Is(err, errIdempotencyConflict) {
		writeError(writer, http.StatusConflict, "idempotency_key_conflict")
		return
	}
	if errors.Is(err, errResourceExists) {
		writeError(writer, http.StatusConflict, "resource_exists")
		return
	}
	if errors.Is(err, errResourceNotFound) {
		writeError(writer, http.StatusNotFound, "resource_not_found")
		return
	}
	if errors.Is(err, errVersionConflict) {
		writeError(writer, http.StatusPreconditionFailed, "version_conflict")
		return
	}
	if errors.Is(err, errRetryableFault) {
		writer.Header().Set("Retry-After", "0")
		writeError(writer, http.StatusServiceUnavailable, "synthetic_retryable_error")
		return
	}
	if errors.Is(err, errTerminalFault) {
		writeError(writer, http.StatusUnprocessableEntity, "synthetic_terminal_error")
		return
	}
	if errors.Is(err, errStoreCapacity) {
		writeError(writer, http.StatusInsufficientStorage, "fixture_capacity_exhausted")
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "internal_error")
		return
	}
	if !replayed && strings.HasPrefix(input.Value, replayFaultValuePrefix) && handler.replayDelay > 0 {
		timer := time.NewTimer(handler.replayDelay)
		defer timer.Stop()
		select {
		case <-request.Context().Done():
			return
		case <-timer.C:
		}
	}
	if replayed {
		writer.Header().Set("Idempotency-Replayed", "true")
	}
	writeJSON(writer, http.StatusOK, projection)
}

// diagnosticJournalPath parses one escaped diagnostic journal segment and rejects decoded slashes or out-of-bound text.
// diagnosticJournalPath разбирает один экранированный сегмент диагностического журнала и отклоняет декодированные слеши и текст вне пределов.
func diagnosticJournalPath(escapedPath string) (string, bool) {
	const prefix = "/v1/diagnostics/journals/"
	if !strings.HasPrefix(escapedPath, prefix) {
		return "", false
	}
	remainder := strings.TrimPrefix(escapedPath, prefix)
	if remainder == "" || strings.Contains(remainder, "/") {
		return "", false
	}
	journal, err := url.PathUnescape(remainder)
	if err != nil || strings.Contains(journal, "/") || !validBoundedString(journal, maximumJournalBytes) {
		return "", false
	}
	return journal, true
}

// journalPath parses one bounded journal segment and optional /entries suffix without accepting nested or escaped-slash names.
// The booleans distinguish mutation routing and successful path parsing, not authorization or resource existence.
// journalPath разбирает один ограниченный сегмент журнала и необязательный суффикс /entries без вложенных имён или экранированных слешей.
// Логические значения различают маршрут изменения и успешность разбора пути, а не полномочия или существование ресурса.
func journalPath(escapedPath string) (string, bool, bool) {
	const prefix = "/v1/journals/"
	if !strings.HasPrefix(escapedPath, prefix) {
		return "", false, false
	}
	remainder := strings.TrimPrefix(escapedPath, prefix)
	entries := strings.HasSuffix(remainder, "/entries")
	if entries {
		remainder = strings.TrimSuffix(remainder, "/entries")
	}
	if remainder == "" || strings.Contains(remainder, "/") {
		return "", false, false
	}
	journal, err := url.PathUnescape(remainder)
	if err != nil || strings.Contains(journal, "/") || !validBoundedString(journal, maximumJournalBytes) {
		return "", false, false
	}
	return journal, entries, true
}

// decodeMutationInput requires exact JSON content headers and a bounded UTF-8 object with no duplicate/unknown fields or trailing data.
// It defaults action/fault, validates the action-specific shape and closes the body after content-header checks have passed.
// decodeMutationInput требует точные JSON-заголовки и ограниченный UTF-8 объект без повторных или неизвестных полей и лишних данных.
// Он задаёт умолчания action и fault, проверяет форму действия и закрывает тело после успешной проверки заголовков содержимого.
func decodeMutationInput(request *http.Request) (mutationInput, error) {
	if request.Header.Get("Content-Encoding") != "" || len(request.Header.Values("Content-Type")) != 1 {
		return mutationInput{}, errors.New("content headers are invalid")
	}
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || len(parameters) != 0 {
		return mutationInput{}, errors.New("content type is invalid")
	}
	defer request.Body.Close()
	body, err := io.ReadAll(io.LimitReader(request.Body, maximumRequestBodySize+1))
	if err != nil || len(body) == 0 || len(body) > maximumRequestBodySize || !utf8.Valid(body) {
		return mutationInput{}, errors.New("request body is outside bounds")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return mutationInput{}, errors.New("request body is invalid")
	}
	input := mutationInput{Action: mutationCreate, Fault: faultNone}
	seen := make(map[string]struct{}, 4)
	for decoder.More() {
		token, tokenErr := decoder.Token()
		key, keyOK := token.(string)
		if tokenErr != nil || !keyOK {
			return mutationInput{}, errors.New("request field is invalid")
		}
		if _, exists := seen[key]; exists {
			return mutationInput{}, errors.New("request field is duplicated")
		}
		seen[key] = struct{}{}
		switch key {
		case "action":
			var action string
			if decoder.Decode(&action) != nil {
				return mutationInput{}, errors.New("request action is invalid")
			}
			input.Action = mutationAction(action)
		case "value":
			if decoder.Decode(&input.Value) != nil {
				return mutationInput{}, errors.New("request value is invalid")
			}
		case "expected_sequence":
			if decoder.Decode(&input.ExpectedSequence) != nil {
				return mutationInput{}, errors.New("request sequence is invalid")
			}
		case "fault":
			var fault string
			if decoder.Decode(&fault) != nil {
				return mutationInput{}, errors.New("request fault is invalid")
			}
			input.Fault = faultMode(fault)
		default:
			return mutationInput{}, errors.New("request field is unknown")
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return mutationInput{}, errors.New("request body has trailing data")
	}
	if !validMutationInput(input) {
		return mutationInput{}, errors.New("request mutation is invalid")
	}
	return input, nil
}

// validMutationInput restricts fault/action to closed sets and requires appropriate text and expected sequence for create/update/delete.
// It validates a fixture command's shape without reading current sequence or changing the store.
// validMutationInput ограничивает fault и action закрытыми множествами и требует подходящие текст и ожидаемую последовательность для create, update и delete.
// Проверяется форма команды оснастки без чтения текущей последовательности или изменения хранилища.
func validMutationInput(input mutationInput) bool {
	if input.Fault != faultNone && input.Fault != faultRetryableOnce && input.Fault != faultTerminal {
		return false
	}
	switch input.Action {
	case mutationCreate:
		return input.ExpectedSequence == 0 && validBoundedString(input.Value, maximumValueBytes)
	case mutationUpdate:
		return input.ExpectedSequence > 0 && validBoundedString(input.Value, maximumValueBytes)
	case mutationDelete:
		return input.ExpectedSequence > 0 && input.Value == ""
	default:
		return false
	}
}

// validBoundedString requires nonempty valid UTF-8 within the byte limit and rejects NUL, CR and LF, but not all control characters.
// validBoundedString требует непустой корректный UTF-8 в пределах байтового лимита и запрещает NUL, CR и LF, но не все управляющие символы.
func validBoundedString(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

// optionalIdempotencyKey accepts an absent header or exactly one grammar-valid key, rejecting repeated or malformed values.
// optionalIdempotencyKey принимает отсутствие заголовка либо ровно один ключ допустимой формы, отклоняя повторные или неверные значения.
func optionalIdempotencyKey(header http.Header) (string, bool) {
	values := header.Values("Idempotency-Key")
	if len(values) == 0 {
		return "", true
	}
	if len(values) != 1 || !idempotencyKeyPattern.MatchString(values[0]) {
		return "", false
	}
	return values[0], true
}

// requiredIdempotencyKey applies the optional key parser and additionally rejects absence for mutation requests.
// requiredIdempotencyKey применяет разбор необязательного ключа и дополнительно запрещает его отсутствие у запросов изменения.
func requiredIdempotencyKey(header http.Header) (string, bool) {
	key, ok := optionalIdempotencyKey(header)
	return key, ok && key != ""
}

// validReadRequest enforces GET before inspecting/closing at most one body byte and writes the previous closed errors on rejection.
// The wrong-method path leaves body handling to the caller/server exactly as before; no journal or auth decision is made here.
// validReadRequest проверяет GET до чтения и закрытия не более одного байта тела и выдаёт прежние закрытые ошибки при отказе.
// При неверном методе тело остаётся вызывающему или серверу, как прежде; решений о журнале или полномочиях здесь нет.
func validReadRequest(writer http.ResponseWriter, request *http.Request) bool {
	if request.Method != http.MethodGet {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return false
	}
	if !emptyBody(request.Body) {
		writeError(writer, http.StatusBadRequest, "request_body_invalid")
		return false
	}
	return true
}

// emptyBody accepts nil or an immediate clean EOF, reading at most one byte and closing any nonnil body.
// emptyBody принимает nil либо немедленный EOF без ошибки, читая не более одного байта и закрывая любое ненулевое тело.
func emptyBody(body io.ReadCloser) bool {
	if body == nil {
		return true
	}
	defer body.Close()
	content, err := io.ReadAll(io.LimitReader(body, 1))
	return err == nil && len(content) == 0
}

// setResponseHeaders applies the fixture's fixed no-store, JSON and nosniff response headers before routing.
// setResponseHeaders задаёт фиксированные заголовки no-store, JSON и nosniff оснастки до маршрутизации.
func setResponseHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
}

// writeError serializes one caller-selected closed error code using the common JSON response writer.
// writeError сериализует один выбранный вызывающим закрытый код ошибки общим JSON-писателем ответа.
func writeError(writer http.ResponseWriter, status int, code string) {
	writeJSON(writer, status, errorResponse{Error: code})
}

// writeJSON marshals before committing status, falls back to a safe 500 document on encoding failure and appends one newline.
// It ignores final writer errors and does not add retries or change headers; callers install response headers first.
// writeJSON сериализует до отправки статуса, заменяет сбой кодирования безопасным документом 500 и добавляет один перевод строки.
// Итоговые ошибки записи игнорируются, повторы и изменение заголовков не добавляются; заголовки заранее задаёт вызывающий.
func writeJSON(writer http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		encoded = []byte(`{"error":"internal_error"}`)
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(append(encoded, '\n'))
}
