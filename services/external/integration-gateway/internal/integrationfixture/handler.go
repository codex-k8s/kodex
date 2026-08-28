package integrationfixture

import (
	"bytes"
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
	maximumJournalBytes    = 120
	maximumValueBytes      = 4096
	maximumRequestBodySize = 8 << 10
	replayFaultValuePrefix = "kodex-e2e-replay:"
	replayFaultDelay       = 4 * time.Second
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type errorResponse struct {
	Error string `json:"error"`
}

// Handler обслуживает только закрытый synthetic journal contract.
type Handler struct {
	store       *Store
	ready       atomic.Bool
	replayDelay time.Duration
}

func NewHandler(store *Store) *Handler {
	return newHandler(store, replayFaultDelay)
}

func newHandler(store *Store, delay time.Duration) *Handler {
	return &Handler{store: store, replayDelay: delay}
}

func (handler *Handler) SetReady(ready bool) {
	handler.ready.Store(ready)
}

func (handler *Handler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	setResponseHeaders(writer)
	if request.URL.RawQuery != "" {
		writeError(writer, http.StatusBadRequest, "query_not_allowed")
		return
	}
	switch request.URL.EscapedPath() {
	case "/healthz":
		handler.serveHealth(writer, request)
		return
	case "/readyz":
		handler.serveReadiness(writer, request)
		return
	}
	if journal, ok := diagnosticJournalPath(request.URL.EscapedPath()); ok {
		handler.readDiagnostic(writer, request, journal)
		return
	}

	journal, entries, ok := journalPath(request.URL.EscapedPath())
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

func (handler *Handler) readDiagnostic(writer http.ResponseWriter, request *http.Request, journal string) {
	if request.Method != http.MethodGet {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if !emptyBody(request.Body) {
		writeError(writer, http.StatusBadRequest, "request_body_invalid")
		return
	}
	writeJSON(writer, http.StatusOK, handler.store.ReadDiagnostic(journal))
}

func (handler *Handler) serveHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if !emptyBody(request.Body) {
		writeError(writer, http.StatusBadRequest, "request_body_invalid")
		return
	}
	writeJSON(writer, http.StatusOK, struct {
		Status string `json:"status"`
	}{Status: "ok"})
}

func (handler *Handler) serveReadiness(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if !emptyBody(request.Body) {
		writeError(writer, http.StatusBadRequest, "request_body_invalid")
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

func (handler *Handler) readJournal(writer http.ResponseWriter, request *http.Request, journal string) {
	if request.Method != http.MethodGet {
		writeError(writer, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	if !emptyBody(request.Body) {
		writeError(writer, http.StatusBadRequest, "request_body_invalid")
		return
	}
	effectKey, ok := optionalIdempotencyKey(request.Header)
	if !ok {
		writeError(writer, http.StatusBadRequest, "idempotency_key_invalid")
		return
	}
	writeJSON(writer, http.StatusOK, handler.store.Read(journal, effectKey))
}

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
	value, err := decodeEntryInput(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, "request_body_invalid")
		return
	}
	projection, replayed, err := handler.store.Append(journal, effectKey, value)
	if errors.Is(err, errIdempotencyConflict) {
		writeError(writer, http.StatusConflict, "idempotency_key_conflict")
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
	if !replayed && strings.HasPrefix(value, replayFaultValuePrefix) && handler.replayDelay > 0 {
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

func decodeEntryInput(request *http.Request) (string, error) {
	if request.Header.Get("Content-Encoding") != "" || len(request.Header.Values("Content-Type")) != 1 {
		return "", errors.New("content headers are invalid")
	}
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || len(parameters) != 0 {
		return "", errors.New("content type is invalid")
	}
	defer request.Body.Close()
	body, err := io.ReadAll(io.LimitReader(request.Body, maximumRequestBodySize+1))
	if err != nil || len(body) == 0 || len(body) > maximumRequestBodySize || !utf8.Valid(body) {
		return "", errors.New("request body is outside bounds")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') || !decoder.More() {
		return "", errors.New("request body is invalid")
	}
	key, err := decoder.Token()
	if err != nil || key != "value" {
		return "", errors.New("request field is invalid")
	}
	var value string
	if err := decoder.Decode(&value); err != nil || !validBoundedString(value, maximumValueBytes) || decoder.More() {
		return "", errors.New("request value is invalid")
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !errors.Is(decoder.Decode(&struct{}{}), io.EOF) {
		return "", errors.New("request body has trailing data")
	}
	return value, nil
}

func validBoundedString(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

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

func requiredIdempotencyKey(header http.Header) (string, bool) {
	key, ok := optionalIdempotencyKey(header)
	return key, ok && key != ""
}

func emptyBody(body io.ReadCloser) bool {
	if body == nil {
		return true
	}
	defer body.Close()
	content, err := io.ReadAll(io.LimitReader(body, 1))
	return err == nil && len(content) == 0
}

func setResponseHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeError(writer http.ResponseWriter, status int, code string) {
	writeJSON(writer, status, errorResponse{Error: code})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		status = http.StatusInternalServerError
		encoded = []byte(`{"error":"internal_error"}`)
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(append(encoded, '\n'))
}
