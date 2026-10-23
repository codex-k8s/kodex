package integrationfixture

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBearerProtectedOpenAPIPath requires bearer authentication for prefixed reads/writes and checks the same store through the plain fixture path.
// It uses a synthetic token and loopback HTTP, not provider OAuth, production TLS or platform authorization.
// TestBearerProtectedOpenAPIPath требует bearer для чтения и записи с префиксом и проверяет то же хранилище через обычный путь оснастки.
// Используются искусственный токен и loopback HTTP, а не OAuth провайдера, production TLS или полномочия платформы.
func TestBearerProtectedOpenAPIPath(t *testing.T) {
	t.Parallel()
	const token = "fixture-token-with-at-least-thirty-two-bytes"
	handler := NewBearerHandler(NewStore(), sha256.Sum256([]byte(token)))
	handler.SetReady(true)
	server := httptest.NewServer(handler)
	defer server.Close()

	endpoint := server.URL + "/openapi/v1/journals/openapi"
	assertStatus(t, server.Client(), http.MethodGet, endpoint, "", nil, http.StatusUnauthorized)
	assertStatus(t, server.Client(), http.MethodGet, endpoint, "", http.Header{
		"Authorization": {"Bearer invalid-token-with-at-least-thirty-two-bytes"},
	}, http.StatusUnauthorized)
	projection := requestProjection(t, server.Client(), http.MethodGet, endpoint, "", http.Header{
		"Authorization": {"Bearer " + token},
	})
	if projection.Count != 0 || projection.Sequence != 0 {
		t.Fatalf("protected projection = %#v", projection)
	}

	writeHeaders := mutationHeaders("eff-openapi")
	writeHeaders.Set("Authorization", "Bearer "+token)
	written := requestProjection(t, server.Client(), http.MethodPost,
		server.URL+"/openapi/v1/journals/openapi/entries", `{"value":"protected"}`, writeHeaders)
	if written.Count != 1 || written.Value != "protected" {
		t.Fatalf("protected write projection = %#v", written)
	}
	legacy := requestProjection(t, server.Client(), http.MethodGet, server.URL+"/v1/journals/openapi", "", nil)
	if legacy.Count != 1 || legacy.Value != "protected" {
		t.Fatalf("legacy readback projection = %#v", legacy)
	}
}

// TestOpenAPIPathIsClosedWithoutBearerConfiguration requires a prefixed route to remain absent when its authentication mode is disabled.
// TestOpenAPIPathIsClosedWithoutBearerConfiguration требует отсутствия маршрута с префиксом, когда его режим аутентификации не включён.
func TestOpenAPIPathIsClosedWithoutBearerConfiguration(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewStore())
	server := httptest.NewServer(handler)
	defer server.Close()

	assertStatus(t, server.Client(), http.MethodGet, server.URL+"/openapi/v1/journals/main", "", http.Header{
		"Authorization": {"Bearer fixture-token-with-at-least-thirty-two-bytes"},
	}, http.StatusNotFound)
}

// TestReadinessAndStrictJournalContract exercises probe transitions and rejects query/path escapes, malformed JSON, bad keys and payload/content bounds.
// All requests target a disposable loopback handler; no platform readiness or external provider state is inferred.
// TestReadinessAndStrictJournalContract проверяет переходы probe и отклоняет query и обход пути, неверный JSON, ключи и пределы содержимого.
// Все запросы идут к одноразовому loopback-обработчику; готовность платформы или состояние внешнего провайдера не выводятся.
func TestReadinessAndStrictJournalContract(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewStore())
	server := httptest.NewServer(handler)
	defer server.Close()

	assertStatus(t, server.Client(), http.MethodGet, server.URL+"/readyz", "", nil, http.StatusServiceUnavailable)
	handler.SetReady(true)
	assertStatus(t, server.Client(), http.MethodGet, server.URL+"/healthz", "", nil, http.StatusOK)
	assertStatus(t, server.Client(), http.MethodGet, server.URL+"/readyz", "", nil, http.StatusOK)
	assertStatus(t, server.Client(), http.MethodPost, server.URL+"/readyz", "", nil, http.StatusMethodNotAllowed)
	assertStatus(t, server.Client(), http.MethodGet, server.URL+"/v1/journals/main?unsafe=true", "", nil, http.StatusBadRequest)
	assertStatus(t, server.Client(), http.MethodGet, server.URL+"/v1/journals/main%2Fhidden", "", nil, http.StatusNotFound)

	invalidBodies := []string{
		`{}`,
		`{"value":""}`,
		`{"value":"ok","unknown":true}`,
		`{"value":"first","value":"second"}`,
		`{"value":1}`,
		`{"value":"line\nfeed"}`,
		`{"value":"ok"} {}`,
	}
	for index, body := range invalidBodies {
		headers := http.Header{"Content-Type": {"application/json"}, "Idempotency-Key": {fmt.Sprintf("eff-invalid-%d", index)}}
		assertStatus(t, server.Client(), http.MethodPost, server.URL+"/v1/journals/main/entries", body, headers, http.StatusBadRequest)
	}
	oversized := `{"value":"` + strings.Repeat("a", maximumValueBytes+1) + `"}`
	assertStatus(t, server.Client(), http.MethodPost, server.URL+"/v1/journals/main/entries", oversized, http.Header{
		"Content-Type": {"application/json"}, "Idempotency-Key": {"eff-oversized"},
	}, http.StatusBadRequest)
	oversizedBody := `{"value":"ok"}` + strings.Repeat(" ", maximumRequestBodySize)
	assertStatus(t, server.Client(), http.MethodPost, server.URL+"/v1/journals/main/entries", oversizedBody, http.Header{
		"Content-Type": {"application/json"}, "Idempotency-Key": {"eff-oversized-body"},
	}, http.StatusBadRequest)
	assertStatus(t, server.Client(), http.MethodPost, server.URL+"/v1/journals/main/entries", `{"value":"ok"}`, http.Header{
		"Content-Type": {"application/json"},
	}, http.StatusBadRequest)
	assertStatus(t, server.Client(), http.MethodPost, server.URL+"/v1/journals/main/entries", `{"value":"ok"}`, http.Header{
		"Content-Type": {"application/json; charset=utf-8"}, "Idempotency-Key": {"eff-content-type"},
	}, http.StatusBadRequest)
}

// TestWriteRetryReturnsExactReadbackAndConflictDoesNotMutate requires exact same-key replay and rejects a changed value without another effect.
// It compares both stored receipts and current journal state within one process, not durable exactly-once delivery.
// TestWriteRetryReturnsExactReadbackAndConflictDoesNotMutate требует точного повтора того же ключа и отклоняет изменённое значение без нового эффекта.
// Сравниваются квитанции и текущее состояние журнала одного процесса, а не устойчивая доставка exactly-once.
func TestWriteRetryReturnsExactReadbackAndConflictDoesNotMutate(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewStore())
	handler.SetReady(true)
	server := httptest.NewServer(handler)
	defer server.Close()

	endpoint := server.URL + "/v1/journals/main/entries"
	headers := http.Header{"Content-Type": {"application/json"}, "Idempotency-Key": {"eff-stable"}}
	first := requestProjection(t, server.Client(), http.MethodPost, endpoint, `{"value":"first"}`, headers)
	second := requestProjection(t, server.Client(), http.MethodPost, endpoint, `{"value":"first"}`, headers)
	if first != second || first.Sequence != 1 || first.Count != 1 || first.Value != "first" {
		t.Fatalf("idempotent readback differs: first=%#v second=%#v", first, second)
	}
	assertStatus(t, server.Client(), http.MethodPost, endpoint, `{"value":"different"}`, headers, http.StatusConflict)
	current := requestProjection(t, server.Client(), http.MethodGet, server.URL+"/v1/journals/main", "", nil)
	if current.Sequence != 1 || current.Count != 1 || current.Value != "first" {
		t.Fatalf("conflicting retry mutated journal: %#v", current)
	}
}

// TestCRUDWithVersionPreconditionsAndFaultClassification covers synthetic create/update/delete, stale versions and immutable fault/retry keys.
// It checks deleted historical value versus empty current state and diagnostic counts without relying on external APIs.
// TestCRUDWithVersionPreconditionsAndFaultClassification проверяет искусственные create, update и delete, старые версии и неизменяемые ключи ошибок и повторов.
// Проверяются удалённое историческое значение, пустое текущее состояние и диагностические счётчики без внешних API.
func TestCRUDWithVersionPreconditionsAndFaultClassification(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewStore())
	handler.SetReady(true)
	server := httptest.NewServer(handler)
	defer server.Close()

	endpoint := server.URL + "/v1/journals/crud/entries"
	create := requestProjection(t, server.Client(), http.MethodPost, endpoint,
		`{"action":"CREATE","value":"first"}`, mutationHeaders("eff-create"))
	if create.Sequence != 1 || create.Count != 1 || create.Value != "first" {
		t.Fatalf("create projection = %#v", create)
	}
	assertStatus(t, server.Client(), http.MethodPost, endpoint,
		`{"action":"CREATE","value":"duplicate"}`, mutationHeaders("eff-duplicate"), http.StatusConflict)
	assertStatus(t, server.Client(), http.MethodPost, endpoint,
		`{"action":"UPDATE","value":"stale","expected_sequence":9}`, mutationHeaders("eff-stale"), http.StatusPreconditionFailed)
	assertStatus(t, server.Client(), http.MethodPost, endpoint,
		`{"action":"UPDATE","value":"retry","expected_sequence":1,"fault":"RETRYABLE_ONCE"}`,
		mutationHeaders("eff-retry"), http.StatusServiceUnavailable)
	current := requestProjection(t, server.Client(), http.MethodGet, server.URL+"/v1/journals/crud", "", nil)
	if current.Sequence != 1 || current.Value != "first" || current.Count != 1 {
		t.Fatalf("retryable failure mutated resource: %#v", current)
	}
	updated := requestProjection(t, server.Client(), http.MethodPost, endpoint,
		`{"action":"UPDATE","value":"retry","expected_sequence":1,"fault":"RETRYABLE_ONCE"}`,
		mutationHeaders("eff-retry"))
	if updated.Sequence != 2 || updated.Value != "retry" || updated.Count != 1 {
		t.Fatalf("update projection = %#v", updated)
	}
	assertStatus(t, server.Client(), http.MethodPost, endpoint,
		`{"action":"DELETE","expected_sequence":2,"fault":"TERMINAL"}`,
		mutationHeaders("eff-terminal"), http.StatusUnprocessableEntity)
	assertStatus(t, server.Client(), http.MethodPost, endpoint,
		`{"action":"DELETE","expected_sequence":1,"fault":"TERMINAL"}`,
		mutationHeaders("eff-terminal"), http.StatusConflict)
	current = requestProjection(t, server.Client(), http.MethodGet, server.URL+"/v1/journals/crud", "", nil)
	if current.Sequence != 2 || current.Value != "retry" || current.Count != 1 {
		t.Fatalf("terminal failure mutated resource: %#v", current)
	}
	deleted := requestProjection(t, server.Client(), http.MethodPost, endpoint,
		`{"action":"DELETE","expected_sequence":2}`, mutationHeaders("eff-delete"))
	if deleted.Sequence != 3 || deleted.Value != "retry" || deleted.Count != 0 {
		t.Fatalf("delete projection = %#v", deleted)
	}
	current = requestProjection(t, server.Client(), http.MethodGet, server.URL+"/v1/journals/crud", "", nil)
	if current.Sequence != 3 || current.Value != "" || current.Count != 0 {
		t.Fatalf("deleted readback = %#v", current)
	}
	diagnostic := requestDiagnostic(t, server.Client(), server.URL+"/v1/diagnostics/journals/crud")
	if diagnostic.Exists || diagnostic.Sequence != 3 || diagnostic.Count != 0 ||
		diagnostic.RetryableFailureCount != 1 || diagnostic.TerminalFailureCount != 1 ||
		diagnostic.LastEffectKey != "eff-delete" {
		t.Fatalf("diagnostic projection = %#v", diagnostic)
	}
}

// TestReplayDiagnosticProvesOneEffect uses a short injected replay delay and confirms one mutation plus one exact retry in diagnostics.
// It also rejects writes and query parameters on the read-only diagnostic endpoint.
// TestReplayDiagnosticProvesOneEffect использует короткую подставленную задержку повтора и подтверждает одно изменение и один точный повтор диагностикой.
// Также отклоняются запись и query-параметры диагностического endpoint только для чтения.
func TestReplayDiagnosticProvesOneEffect(t *testing.T) {
	t.Parallel()
	handler := newHandler(NewStore(), time.Millisecond)
	handler.SetReady(true)
	server := httptest.NewServer(handler)
	defer server.Close()

	endpoint := server.URL + "/v1/journals/replay/entries"
	headers := http.Header{"Content-Type": {"application/json"}, "Idempotency-Key": {"eff-replay"}}
	body := `{"value":"kodex-e2e-replay:once"}`
	requestProjection(t, server.Client(), http.MethodPost, endpoint, body, headers)
	requestProjection(t, server.Client(), http.MethodPost, endpoint, body, headers)

	diagnostic := requestDiagnostic(t, server.Client(), server.URL+"/v1/diagnostics/journals/replay")
	if diagnostic.Count != 1 || diagnostic.Value != "kodex-e2e-replay:once" ||
		diagnostic.LastEffectKey != "eff-replay" || diagnostic.ReplayCount != 1 ||
		diagnostic.LastReplayEffectKey != "eff-replay" {
		t.Fatalf("replay diagnostic = %#v", diagnostic)
	}
	assertStatus(t, server.Client(), http.MethodPost, server.URL+"/v1/diagnostics/journals/replay", "", nil, http.StatusMethodNotAllowed)
	assertStatus(t, server.Client(), http.MethodGet, server.URL+"/v1/diagnostics/journals/replay?unsafe=true", "", nil, http.StatusBadRequest)
}

// TestConcurrentRetryCreatesOneEffect releases 64 local HTTP mutations with one effect key and checks a single resulting sequence/resource.
// Responses and errors are joined before final readback; this proves the in-process mutex path, not cross-restart or distributed idempotency.
// TestConcurrentRetryCreatesOneEffect одновременно выпускает 64 локальных HTTP-изменения с одним ключом и проверяет одну итоговую последовательность и ресурс.
// Ответы и ошибки собираются до итогового чтения; доказывается путь mutex одного процесса, не идемпотентность между перезапусками или узлами.
func TestConcurrentRetryCreatesOneEffect(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewStore())
	handler.SetReady(true)
	server := httptest.NewServer(handler)
	defer server.Close()

	const workers = 64
	responses := make(chan Projection, workers)
	errorsChannel := make(chan error, workers)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			request, err := http.NewRequest(http.MethodPost, server.URL+"/v1/journals/concurrent/entries", strings.NewReader(`{"value":"once"}`))
			if err != nil {
				errorsChannel <- err
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Idempotency-Key", "eff-concurrent")
			response, err := server.Client().Do(request)
			if err != nil {
				errorsChannel <- err
				return
			}
			defer response.Body.Close()
			var projection Projection
			if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&projection) != nil {
				errorsChannel <- fmt.Errorf("unexpected response status %d", response.StatusCode)
				return
			}
			responses <- projection
		}()
	}
	close(start)
	group.Wait()
	close(responses)
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatal(err)
	}
	for projection := range responses {
		if projection.Sequence != 1 || projection.Count != 1 || projection.EffectKey != "eff-concurrent" {
			t.Fatalf("concurrent retry created a second effect: %#v", projection)
		}
	}
	current := requestProjection(t, server.Client(), http.MethodGet, server.URL+"/v1/journals/concurrent", "", nil)
	if current.Count != 1 || current.Sequence != 1 {
		t.Fatalf("journal count after concurrent retry = %#v", current)
	}
}

// TestConcurrentDistinctJournalsHaveOneSequenceEach creates 128 distinct resources concurrently and requires per-journal sequence one.
// It checks independent journal versions against the shared mutex-backed store without network traffic.
// TestConcurrentDistinctJournalsHaveOneSequenceEach конкурентно создаёт 128 разных ресурсов и требует последовательность один для каждого журнала.
// Независимые версии проверяются на общем хранилище с mutex без сетевого трафика.
func TestConcurrentDistinctJournalsHaveOneSequenceEach(t *testing.T) {
	t.Parallel()
	store := NewStore()
	const effects = 128
	sequences := make(chan int64, effects)
	var group sync.WaitGroup
	for index := 0; index < effects; index++ {
		index := index
		group.Add(1)
		go func() {
			defer group.Done()
			journal := fmt.Sprintf("journal-%03d", index)
			projection, replayed, err := store.Mutate(journal, fmt.Sprintf("eff-%03d", index), mutationInput{
				Action: mutationCreate, Value: fmt.Sprintf("value-%03d", index), Fault: faultNone,
			})
			if err != nil || replayed {
				t.Errorf("Mutate() error = %v, replayed = %v", err, replayed)
				return
			}
			sequences <- projection.Sequence
		}()
	}
	group.Wait()
	close(sequences)
	actual := make([]int, 0, effects)
	for sequence := range sequences {
		actual = append(actual, int(sequence))
	}
	sort.Ints(actual)
	for index, sequence := range actual {
		if sequence != 1 {
			t.Fatalf("sequence[%d] = %d", index, sequence)
		}
	}
	if current := store.Read("journal-000", ""); current.Count != 1 || current.Sequence != 1 {
		t.Fatalf("journal projection = %#v", current)
	}
}

// mutationHeaders allocates fresh exact JSON and idempotency headers so callers may add authentication without sharing mutations.
// mutationHeaders создаёт новые точные JSON-заголовки и ключ идемпотентности, позволяя добавлять аутентификацию без общих изменений.
func mutationHeaders(effectKey string) http.Header {
	return http.Header{"Content-Type": {"application/json"}, "Idempotency-Key": {effectKey}}
}

// requestProjection sends a fixture request with cloned headers, closes its response and requires one exact 200 projection JSON document.
// Error-body reads are only test diagnostics and are not used as a production bounded-provider transport.
// requestProjection отправляет запрос оснастки с копией заголовков, закрывает ответ и требует один точный JSON-документ проекции со статусом 200.
// Чтение тела ошибки служит лишь тестовой диагностикой и не используется как production-транспорт с ограничением ответа провайдера.
func requestProjection(t *testing.T, client *http.Client, method, endpoint, body string, headers http.Header) Projection {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header = headers.Clone()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		content, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, body = %s", response.StatusCode, content)
	}
	var projection Projection
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&projection); err != nil || !errorsIsEOF(decoder.Decode(&struct{}{})) {
		t.Fatalf("decode projection: %#v, %v", projection, err)
	}
	return projection
}

// requestDiagnostic reads one 200 diagnostic document, rejects unknown fields/trailing JSON and closes the local HTTP response.
// requestDiagnostic читает один диагностический документ со статусом 200, отклоняет неизвестные поля и лишний JSON и закрывает локальный HTTP-ответ.
func requestDiagnostic(t *testing.T, client *http.Client, endpoint string) DiagnosticProjection {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("diagnostic status = %d", response.StatusCode)
	}
	var projection DiagnosticProjection
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&projection); err != nil || !errorsIsEOF(decoder.Decode(&struct{}{})) {
		t.Fatalf("decode diagnostic: %#v, %v", projection, err)
	}
	return projection
}

// assertStatus sends an independently constructed fixture request and compares its exact status without changing or retrying the input.
// The response is closed on success and failure; mismatch details are local test diagnostics only.
// assertStatus отправляет независимо созданный запрос оснастки и сравнивает точный статус без изменения или повтора входа.
// Ответ закрывается и при успехе, и при ошибке; подробности несовпадения служат только локальной диагностикой теста.
func assertStatus(t *testing.T, client *http.Client, method, endpoint, body string, headers http.Header, expected int) {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header = headers.Clone()
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != expected {
		content, _ := io.ReadAll(response.Body)
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, endpoint, response.StatusCode, expected, content)
	}
}

// errorsIsEOF accepts only the exact io.EOF sentinel from the fixture decoder's trailing-document probe.
// errorsIsEOF принимает только точный sentinel io.EOF проверки лишнего документа декодером оснастки.
func errorsIsEOF(err error) bool {
	return err == io.EOF
}

// trackedReadBody counts explicit handler closes while exposing the supplied reader unchanged for guard-order assertions.
// trackedReadBody считает явные закрытия тела обработчиком, сохраняя переданный читатель для проверки порядка отказов.
type trackedReadBody struct {
	io.Reader
	closes int
}

// Close records a close without consuming more bytes or invoking external cleanup.
// Close фиксирует закрытие без потребления дополнительных байтов или внешней очистки.
func (body *trackedReadBody) Close() error { body.closes++; return nil }

// TestReadGuardsPreserveOrderClosureAndNoEffects checks all four GET routes for method-first rejection and one-byte body validation.
// It requires the same safe error JSON, header policy, exact body-close count and no journal/effect allocation without opening a listener.
// TestReadGuardsPreserveOrderClosureAndNoEffects проверяет все четыре GET-маршрута на приоритет метода и однобайтовую проверку тела.
// Требуются прежние безопасные JSON-ошибки, заголовки, точное число закрытий тела и отсутствие создания журналов и эффектов без открытия listener.
func TestReadGuardsPreserveOrderClosureAndNoEffects(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/healthz", "/readyz", "/v1/journals/guard", "/v1/diagnostics/journals/guard"} {
		for _, scenario := range []struct {
			name, method, value, errorCode string
			status, closes, remaining      int
		}{
			{name: "method before body", method: http.MethodPost, value: "bad", errorCode: "method_not_allowed", status: http.StatusMethodNotAllowed, remaining: 3},
			{name: "body rejected", method: http.MethodGet, value: "bad", errorCode: "request_body_invalid", status: http.StatusBadRequest, closes: 1, remaining: 2},
			{name: "empty body accepted", method: http.MethodGet, status: http.StatusOK, closes: 1},
		} {
			t.Run(path+"/"+scenario.name, func(t *testing.T) {
				store := NewStore()
				handler := NewHandler(store)
				handler.SetReady(true)
				request := httptest.NewRequest(scenario.method, "http://fixture.invalid"+path, nil)
				reader := strings.NewReader(scenario.value)
				body := &trackedReadBody{Reader: reader}
				request.Body = body
				writer := httptest.NewRecorder()
				handler.ServeHTTP(writer, request)
				if writer.Code != scenario.status || body.closes != scenario.closes || reader.Len() != scenario.remaining {
					t.Fatalf("read guard changed: status=%d closes=%d remaining=%d", writer.Code, body.closes, reader.Len())
				}
				if scenario.errorCode != "" && writer.Body.String() != `{"error":"`+scenario.errorCode+`"}`+"\n" {
					t.Fatal("read guard error bytes changed")
				}
				if writer.Header().Get("Cache-Control") != "no-store" || writer.Header().Get("Content-Type") != "application/json" || writer.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("read guard response headers changed")
				}
				if len(store.journals) != 0 || len(store.receipts) != 0 || len(store.faultAttempts) != 0 {
					t.Fatal("read guard allocated a synthetic effect")
				}
			})
		}
	}
}
