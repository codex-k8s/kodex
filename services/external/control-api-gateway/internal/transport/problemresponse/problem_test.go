package problemresponse

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestWritePreservesEnvelopeAndCallerHeaders checks exact body fields, status, newline framing and fresh UUIDs while leaving ordinary/STT retry hints and cache/content headers entirely to the caller.
// TestWritePreservesEnvelopeAndCallerHeaders проверяет точные поля, статус, перевод строки и новые UUID, оставляя обычные и STT-подсказки повтора, cache/content-заголовки полностью вызывающей стороне.
func TestWritePreservesEnvelopeAndCallerHeaders(t *testing.T) {
	t.Parallel()
	seen := make(map[string]struct{})
	for _, test := range []struct {
		name       string
		status     int
		code       string
		retryable  bool
		retryAfter string
	}{
		{name: "ordinary denial", status: http.StatusForbidden, code: "PERMISSION_DENIED"},
		{name: "ordinary rate limit", status: http.StatusTooManyRequests, code: "RATE_LIMITED", retryable: true, retryAfter: "1"},
		{name: "speech hint", status: http.StatusTooManyRequests, code: "TRANSCRIPTION_RATE_LIMITED", retryable: true, retryAfter: "17"},
		{name: "speech without hint", status: http.StatusTooManyRequests, code: "TRANSCRIPTION_RATE_LIMITED"},
		{name: "unknown status", status: 599, code: "INTERNAL"},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			recorder.Header().Set("Content-Type", "application/problem+json")
			recorder.Header().Set("Cache-Control", "no-store")
			if test.retryAfter != "" {
				recorder.Header().Set("Retry-After", test.retryAfter)
			}
			Write(recorder, test.status, test.code, test.retryable)
			if recorder.Code != test.status || recorder.Header().Get("Retry-After") != test.retryAfter || recorder.Header().Get("Content-Type") != "application/problem+json" || recorder.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("status or caller-owned header changed")
			}
			if !strings.HasSuffix(recorder.Body.String(), "\n") {
				t.Fatal("JSON encoder omitted newline framing")
			}
			decoder := json.NewDecoder(recorder.Body)
			var body map[string]any
			if err := decoder.Decode(&body); err != nil {
				t.Fatal(err)
			}
			var extra any
			if err := decoder.Decode(&extra); err != io.EOF || len(body) != 6 {
				t.Fatal("problem has trailing data or unexpected fields")
			}
			id, ok := body["correlationId"].(string)
			parsed, err := uuid.Parse(id)
			if !ok || err != nil || parsed.Version() != 4 || parsed.String() != id {
				t.Fatal("correlation ID is not a canonical random UUID")
			}
			if _, duplicate := seen[id]; duplicate {
				t.Fatal("problem reused a correlation ID")
			}
			seen[id] = struct{}{}
			if body["type"] != "urn:kodex:problem:"+strings.ToLower(test.code) || body["title"] != http.StatusText(test.status) || body["status"] != float64(test.status) || body["code"] != test.code || body["retryable"] != test.retryable {
				t.Fatal("problem fields differ from supplied classification")
			}
		})
	}
	plain := httptest.NewRecorder()
	Write(plain, http.StatusTooManyRequests, "RATE_LIMITED", true)
	if len(plain.Header()) != 0 {
		t.Fatal("body writer unexpectedly assigned header policy")
	}
}

// localizingWriter records localization calls and the already-committed status, without implementing any real catalog or HTTP middleware.
// localizingWriter запоминает вызовы локализации и уже отправленный статус без настоящего каталога или HTTP middleware.
type localizingWriter struct {
	*httptest.ResponseRecorder
	calls      []string
	statusSeen int
}

// Localize records the exact code and status visible at invocation, returning a synthetic localized title.
// Localize запоминает точный код и видимый при вызове статус, возвращая тестовый локализованный заголовок.
func (writer *localizingWriter) Localize(code string) string {
	writer.calls = append(writer.calls, code)
	writer.statusSeen = writer.Code
	return "localized:" + code
}

// TestWriteUsesLocalizationAfterStatus ensures optional localization runs once after WriteHeader and affects only the displayed title, not the classification code.
// TestWriteUsesLocalizationAfterStatus проверяет единственный вызов необязательной локализации после WriteHeader и замену только заголовка, а не кода классификации.
func TestWriteUsesLocalizationAfterStatus(t *testing.T) {
	t.Parallel()
	writer := &localizingWriter{ResponseRecorder: httptest.NewRecorder()}
	Write(writer, http.StatusServiceUnavailable, "UNAVAILABLE", true)
	var body map[string]any
	if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(writer.calls) != 1 || writer.calls[0] != "UNAVAILABLE" || writer.statusSeen != http.StatusServiceUnavailable || body["title"] != "localized:UNAVAILABLE" || body["code"] != "UNAVAILABLE" {
		t.Fatal("localization changed status order or classification")
	}
}

// failedWriter counts attempted status/body writes and rejects every body write, exposing retry behavior without opening a socket.
// failedWriter считает попытки отправки статуса и тела и отклоняет каждую запись тела, показывая повторы без открытия сокета.
type failedWriter struct {
	header     http.Header
	statuses   []int
	bodyWrites int
}

// Header returns the fixture header map, allowing normal ResponseWriter use without adding policy.
// Header возвращает тестовую таблицу заголовков для обычного использования ResponseWriter без добавления политики.
func (writer *failedWriter) Header() http.Header { return writer.header }

// WriteHeader records each attempted status commit so the test detects duplicate error responses.
// WriteHeader запоминает каждую попытку отправить статус, позволяя обнаружить повторный ответ об ошибке.
func (writer *failedWriter) WriteHeader(status int) {
	writer.statuses = append(writer.statuses, status)
}

// Write rejects the encoded bytes with a synthetic I/O error after incrementing the attempt counter.
// Write отклоняет закодированные байты тестовой ошибкой ввода-вывода после увеличения счётчика попыток.
func (writer *failedWriter) Write([]byte) (int, error) {
	writer.bodyWrites++
	return 0, errors.New("synthetic response write failure")
}

// TestWriteDoesNotRetryFailedEncode proves the original best-effort failure policy: one status and one body attempt, with no recovery response or hidden retry.
// TestWriteDoesNotRetryFailedEncode доказывает исходную политику best-effort: один статус и одна попытка тела без восстановительного ответа и скрытого повтора.
func TestWriteDoesNotRetryFailedEncode(t *testing.T) {
	t.Parallel()
	writer := &failedWriter{header: make(http.Header)}
	Write(writer, http.StatusBadRequest, "INVALID_REQUEST", false)
	if len(writer.statuses) != 1 || writer.statuses[0] != http.StatusBadRequest || writer.bodyWrites != 1 {
		t.Fatal("failed body encode was retried or response status changed")
	}
}
