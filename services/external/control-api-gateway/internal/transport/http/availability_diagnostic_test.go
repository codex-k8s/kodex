package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	brokeravailability "github.com/codex-k8s/kodex/libs/go/eventing/availability"
	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	sharedobservability "github.com/codex-k8s/kodex/libs/go/observability"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/availability"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/problemresponse"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// availabilityMemory stores encrypted family records and injects failures only after normal creation; it is not a durable broker.
// availabilityMemory хранит зашифрованные записи семьи и вводит отказы только после обычного создания; это не устойчивый брокер.
type availabilityMemory struct {
	records         map[string]browserstate.Record
	familyID, fault string
	writes          int
}

// Read returns fixture records or a typed outage at the selected family/ledger boundary, without exposing ciphertext in errors.
// Read возвращает тестовые записи либо типизированный отказ выбранного family/ledger пути без шифротекста в ошибке.
func (store *availabilityMemory) Read(_ context.Context, id string) (browserstate.Record, error) {
	if store.fault == "family" || store.fault == "ledger" && id != store.familyID {
		return browserstate.Record{}, brokeravailability.Wrap(browserstate.ErrUnavailable, nats.ErrNoResponders)
	}
	r, ok := store.records[id]
	if !ok {
		return r, browserstate.ErrNotFound
	}
	r.Ciphertext = bytes.Clone(r.Ciphertext)
	return r, nil
}

// CompareAndSwap performs one local CAS or injects contention; the counter detects diagnostic code adding a write or retry.
// CompareAndSwap выполняет один локальный CAS либо вводит конфликт; счётчик выявляет добавленные диагностикой записи или повторы.
func (store *availabilityMemory) CompareAndSwap(_ context.Context, id string, expected uint64, data []byte) (browserstate.Record, error) {
	store.writes++
	if store.fault == "cas" || store.records[id].Sequence != expected {
		return browserstate.Record{}, browserstate.ErrConflict
	}
	r := browserstate.Record{Sequence: expected + 1, Ciphertext: bytes.Clone(data)}
	store.records[id] = r
	return r, nil
}

// availabilityVerifier reuses the existing fixed-principal verifier and forbids a provider refresh during these routes.
// availabilityVerifier использует прежний verifier фиксированной личности и запрещает обновление провайдера на этих маршрутах.
type availabilityVerifier struct{ runtimeSecretOIDCStub }

// Refresh rejects any unexpected refresh rather than fabricating an external provider result.
// Refresh отклоняет неожиданное обновление вместо имитации внешнего результата провайдера.
func (availabilityVerifier) Refresh(context.Context, string) (oidcauth.BrowserTokens, error) {
	return oidcauth.BrowserTokens{}, errors.New("unexpected refresh")
}

// availabilityRevocations overrides only the read path of the existing isolated fixture.
// availabilityRevocations заменяет только чтение прежней изолированной оснастки.
type availabilityRevocations struct {
	runtimeSecretRevocationStoreStub
	err error
}

// Revoked returns the injected failure without mutating session state.
// Revoked возвращает заданный отказ без изменения состояния сеанса.
func (store *availabilityRevocations) Revoked(context.Context, string) (bool, error) {
	return false, store.err
}

// unavailableRuns counts actual generated owner calls and returns an unavailable CP response with a leak sentinel.
// unavailableRuns считает настоящие generated owner-вызовы и возвращает недоступность CP с контрольной строкой утечки.
type unavailableRuns struct {
	cp.PlatformQueryServiceClient
	calls int
}

// ListRuns returns the injected CP refusal; ticket success must never invoke it.
// ListRuns возвращает заданный отказ CP; успешная выдача ticket не должна его вызывать.
func (query *unavailableRuns) ListRuns(context.Context, *cp.ListRunsRequest, ...grpc.CallOption) (*cp.ListRunsResponse, error) {
	query.calls++
	return nil, status.Error(codes.Unavailable, "private-credential")
}

// TestAvailabilityRealRoutesSeparateAuthTicketAndRPC exercises the real route/boundary/crypto/CAS path and proves a CP outage cannot be misattributed to ticket issuance.
// TestAvailabilityRealRoutesSeparateAuthTicketAndRPC проверяет настоящий route/boundary/crypto/CAS путь и исключает приписывание отказа CP выдаче ticket.
func TestAvailabilityRealRoutesSeparateAuthTicketAndRPC(t *testing.T) {
	for _, item := range []struct {
		name, method, path, fault, stage string
		want, calls, writes              int
	}{
		{"runs-revocation", "GET", "/api/v1/runs", "revocation", "auth_revocation_read", 503, 0, 0},
		{"ticket-revocation", "POST", "/api/v1/session/ticket", "revocation", "auth_revocation_read", 503, 0, 0},
		{"runs-family", "GET", "/api/v1/runs", "family", "auth_family_read", 503, 0, 0},
		{"ticket-family", "POST", "/api/v1/session/ticket", "family", "auth_family_read", 503, 0, 0},
		{"ticket-ledger", "POST", "/api/v1/session/ticket", "ledger", "ticket_ledger_read", 503, 0, 0},
		{"ticket-cas", "POST", "/api/v1/session/ticket", "cas", "ticket_cas_exhausted", 503, 0, 3},
		{"runs-cp", "GET", "/api/v1/runs", "", "upstream_rpc", 503, 1, 0},
		{"ticket-with-cp-down", "POST", "/api/v1/session/ticket", "", "", 200, 0, 1},
	} {
		t.Run(item.name, func(t *testing.T) {
			keyPath := filepath.Join(t.TempDir(), "key")
			if err := os.WriteFile(keyPath, []byte(strings.Repeat("44", 32)), 0400); err != nil {
				t.Fatal(err)
			}
			keys, err := session.New(session.Config{CurrentKeyFile: keyPath, TTL: 15 * time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now()
			principal := oidcauth.Principal{Issuer: "https://identity.example.test", Subject: "710025af-02cf-4a7b-9f29-8479f757e42a", OrganizationID: "6832a593-1d97-4c50-a3d5-6f5b27bbf2c4", SessionID: "b2c2d1e9-cb78-43a0-b1df-299ce8a49d50", SessionRevision: 1, AuthenticatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
			verifier := availabilityVerifier{runtimeSecretOIDCStub{principal: principal}}
			memory := &availabilityMemory{records: map[string]browserstate.Record{}}
			families, err := session.NewFamilies(t.Context(), memory, verifier, keys)
			if err != nil {
				t.Fatal(err)
			}
			family, csrf, err := families.CreateWithCSRF(t.Context(), oidcauth.BrowserTokens{AccessToken: "private-access", RefreshToken: "private-refresh", Principal: principal, RefreshExpiresAt: now.Add(8 * time.Hour)}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, cookie, err := families.Cookie(family)
			if err != nil {
				t.Fatal(err)
			}
			memory.familyID, memory.fault, memory.writes = family.ID, item.fault, 0
			revocations := &availabilityRevocations{}
			if item.fault == "revocation" {
				revocations.err = brokeravailability.Wrap(errors.New("safe refusal"), nats.ErrNoResponders)
			}
			limiter := ratelimit.New(ratelimit.Config{Window: time.Minute, Limit: 100, MaximumKeys: 100, PreAuthConcurrency: 4, GlobalHTTPConcurrency: 8, PerSubjectHTTPConcurrency: 2, GlobalWebSocketConcurrency: 4, PerSubjectWebSocketConcurrency: 1})
			security, err := boundary.New(boundary.Config{Families: families, Origins: []string{"https://app.example.test"}, Verifier: verifier, Sessions: keys, Revocations: revocations, Limiter: limiter, Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			query := &unavailableRuns{}
			server := &Server{boundary: security, control: &controlplaneclient.Client{Query: query}, logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			r := httptest.NewRequest(item.method, "https://app.example.test"+item.path, nil)
			r.Header.Set("Origin", "https://app.example.test")
			r.Header.Set("X-CSRF-Token", csrf)
			r.AddCookie(&http.Cookie{Name: boundary.SessionCookieName, Value: cookie})
			r.AddCookie(&http.Cookie{Name: boundary.CSRFCookieName, Value: csrf})
			w := httptest.NewRecorder()
			server.Handler().ServeHTTP(w, r)
			if w.Code != item.want || query.calls != item.calls || memory.writes != item.writes {
				t.Fatalf("route outcome changed: status=%d calls=%d writes=%d", w.Code, query.calls, memory.writes)
			}
			if item.want == 200 {
				if logs.Len() != 0 {
					t.Fatal("success emitted failure diagnostic")
				}
				return
			}
			var body, entry map[string]any
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || json.Unmarshal(logs.Bytes(), &entry) != nil {
				t.Fatal("expected exactly one problem and diagnostic")
			}
			wantRetryAfter := ""
			if item.fault == "ledger" || item.fault == "cas" {
				wantRetryAfter = "1"
			}
			if entry["error_stage"] != item.stage || entry["correlation_id"] != body["correlationId"] || body["code"] != "UNAVAILABLE" || body["retryable"] != true || w.Header().Get("Retry-After") != wantRetryAfter {
				t.Fatalf("diagnostic mismatch: stage=%s code=%s retryable=%v retryAfter=%q correlationMatches=%v", entry["error_stage"], body["code"], body["retryable"], w.Header().Get("Retry-After"), entry["correlation_id"] == body["correlationId"])
			}
			for _, secret := range []string{cookie, csrf, "private-access", "private-refresh", "private-credential", family.ID} {
				if strings.Contains(logs.String(), secret) || strings.Contains(w.Body.String(), secret) {
					t.Fatal("credential or state identifier leaked")
				}
			}
		})
	}
}

// TestAvailabilityDiagnosticOnceAndIsolation checks concurrent marks produce one final record and cannot taint another request or a previously successful response.
// TestAvailabilityDiagnosticOnceAndIsolation проверяет одну запись при конкурентных отметках и отсутствие влияния на другой запрос либо уже успешный ответ.
func TestAvailabilityDiagnosticOnceAndIsolation(t *testing.T) {
	var logs bytes.Buffer
	server := &Server{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	h := server.localizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var group sync.WaitGroup
		for range 8 {
			group.Go(func() {
				problemresponse.MarkFailure(w, availability.Fixed("auth_family_read", "no_responders", errors.New("private-credential")))
			})
		}
		group.Wait()
		writeLocalProblem(w, 503, "UNAVAILABLE", true)
		writeLocalProblem(w, 503, "UNAVAILABLE", true)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/runs", nil))
	if strings.Count(logs.String(), unavailableRequestMessage) != 1 {
		t.Fatal("duplicate availability record")
	}
	logs.Reset()
	server.localizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		writeLocalProblem(w, 503, "UNAVAILABLE", true)
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/runs", nil))
	if logs.Len() != 0 {
		t.Fatal("already successful response was logged as unavailable")
	}
}

// TestAvailabilityUsesExistingTraceAndNormalizesMetadata proves the production logger retains the request's trace and rejects arbitrary diagnostic fields without a new exporter or exception observer.
// TestAvailabilityUsesExistingTraceAndNormalizesMetadata доказывает сохранение trace запроса штатным логгером и отказ произвольным полям без нового exporter либо exception observer.
func TestAvailabilityUsesExistingTraceAndNormalizesMetadata(t *testing.T) {
	var logs bytes.Buffer
	telemetry := &sharedobservability.Runtime{}
	server := &Server{logger: telemetry.Logger(&logs)}
	traceID, _ := trace.TraceIDFromHex("1234567890abcdef1234567890abcdef")
	spanID, _ := trace.SpanIDFromHex("1234567890abcdef")
	ctx := trace.ContextWithSpanContext(t.Context(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID}))
	h := server.localizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(interface{ ObserveFailure(string, string) }).ObserveFailure("private-credential", "private-credential")
		writeLocalProblem(w, 503, "UNAVAILABLE", true)
	}))
	w := httptest.NewRecorder()
	r := httptest.NewRequest("UNREGISTERED", "/private-credential?token=private-credential", nil).WithContext(ctx)
	h.ServeHTTP(w, r)
	var entry map[string]any
	if json.Unmarshal(logs.Bytes(), &entry) != nil {
		t.Fatal("missing diagnostic")
	}
	if entry["trace_id"] != traceID.String() || entry["span_id"] != spanID.String() || entry["error_stage"] != "unknown" || entry["error_class"] != "unknown" || entry["method"] != "OTHER" || entry["route"] != "unknown" || strings.Contains(logs.String(), "private-credential") {
		t.Fatal("trace or normalized metadata boundary failed")
	}
}

// TestAvailabilityStreamingWireStatus exercises real TCP flush and hijack, proving that a later problem cannot relabel an already successful or detached response.
// TestAvailabilityStreamingWireStatus проверяет flush и hijack через настоящий TCP: поздняя проблема не меняет диагностику успешного либо переданного ответа.
func TestAvailabilityStreamingWireStatus(t *testing.T) {
	for _, mode := range []string{"flush", "hijack"} {
		t.Run(mode, func(t *testing.T) {
			var logs bytes.Buffer
			done := make(chan error, 1)
			server := &Server{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			h := server.localizationMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				controller := http.NewResponseController(w)
				if mode == "flush" {
					if err := controller.Flush(); err != nil {
						done <- err
						return
					}
				} else {
					conn, buffer, err := controller.Hijack()
					if err != nil {
						done <- err
						return
					}
					defer conn.Close()
					if _, err = buffer.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"); err == nil {
						err = buffer.Flush()
					}
					if err != nil {
						done <- err
						return
					}
				}
				writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", true)
				done <- nil
			}))
			network := httptest.NewUnstartedServer(h)
			network.Config.ErrorLog = log.New(io.Discard, "", 0)
			network.Start()
			defer network.Close()
			client := network.Client()
			client.Timeout = 5 * time.Second
			response, err := client.Get(network.URL + "/api/v1/runs")
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if _, err := io.Copy(io.Discard, response.Body); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusOK || logs.Len() != 0 {
				t.Fatal("streamed or hijacked successful response was logged as unavailable")
			}
		})
	}
}

// diagnosticPlainWriter deliberately exposes no streaming capabilities while retaining an observable response recorder.
// diagnosticPlainWriter намеренно не предоставляет потоковые возможности, сохраняя наблюдаемый recorder ответа.
type diagnosticPlainWriter struct{ http.ResponseWriter }

// TestAvailabilityUnsupportedStreamingLeavesHeadersUncommitted preserves unsupported controller calls and the subsequent real HTTP503 diagnostic.
// TestAvailabilityUnsupportedStreamingLeavesHeadersUncommitted сохраняет отказ неподдержанных вызовов и диагностику последующего настоящего HTTP503.
func TestAvailabilityUnsupportedStreamingLeavesHeadersUncommitted(t *testing.T) {
	var logs bytes.Buffer
	recorder := httptest.NewRecorder()
	w := &localizedResponseWriter{ResponseWriter: diagnosticPlainWriter{recorder}, diagnostic: requestDiagnostic{ctx: t.Context(), logger: slog.New(slog.NewJSONHandler(&logs, nil))}}
	if err := http.NewResponseController(w).Flush(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatal("unsupported flush changed")
	}
	if _, _, err := http.NewResponseController(w).Hijack(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatal("unsupported hijack changed")
	}
	if w.diagnostic.status != 0 || w.diagnostic.detached || w.diagnostic.flushUncertain {
		t.Fatal("unsupported capability committed response")
	}
	writeLocalProblem(w, http.StatusServiceUnavailable, "UNAVAILABLE", true)
	if recorder.Code != http.StatusServiceUnavailable || strings.Count(logs.String(), unavailableRequestMessage) != 1 {
		t.Fatal("real unavailable response lost after unsupported capability")
	}
}
