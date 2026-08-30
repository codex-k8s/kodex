package integrationfixture

import (
	"bytes"
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

func mutationHeaders(effectKey string) http.Header {
	return http.Header{"Content-Type": {"application/json"}, "Idempotency-Key": {effectKey}}
}

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

func errorsIsEOF(err error) bool {
	return err == io.EOF
}
