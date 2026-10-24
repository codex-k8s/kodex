package httptransport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// emailOnceStore models shared, mutex-protected one-time session consumption and an injected
// storage failure for local elevation tests.
// emailOnceStore моделирует общее для тестовых обработчиков однократное потребление сессии под
// mutex и позволяет задать ошибку хранилища.
type emailOnceStore struct {
	mu         sync.Mutex
	consumed   map[string]bool
	consumeErr error
}

// Revoke marks the synthetic session as consumed under the fixture mutex without calling a
// persistent revocation backend.
// Revoke отмечает синтетическую сессию потреблённой под mutex тестового хранилища без обращения к
// постоянному хранилищу отзыва.
func (s *emailOnceStore) Revoke(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consumed[id] = true
	return nil
}

// Revoked reads the synthetic session consumption flag while holding the fixture mutex.
// Revoked читает признак потребления синтетической сессии под mutex тестового хранилища.
func (s *emailOnceStore) Revoked(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.consumed[id], nil
}

// ConsumeOnce atomically accepts one fixture session consumption, rejecting repeats or returning
// the configured storage error.
// ConsumeOnce атомарно разрешает одно потребление тестовой сессии, отклоняет повторы либо
// возвращает заданную ошибку хранилища.
func (s *emailOnceStore) ConsumeOnce(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consumeErr != nil {
		return false, s.consumeErr
	}
	if s.consumed[id] {
		return false, nil
	}
	s.consumed[id] = true
	return true, nil
}

// emailSessionStore adds an injectable replacement-session issuance failure to the existing
// runtime-secret session stub.
// emailSessionStore добавляет управляемый сбой выдачи замещающей сессии к существующей заглушке
// сессий runtime secrets.
type emailSessionStore struct {
	runtimeSecretSessionStoreStub
	issueErr error
}

// Issue returns the configured issuance error or delegates synthetic replacement-session creation
// to the embedded stub.
// Issue возвращает заданную ошибку выдачи либо передаёт создание синтетической замещающей сессии
// встроенной заглушке.
func (s *emailSessionStore) Issue(a, b, c string, d uint64, e string, f time.Time) (session.Claims, string, string, error) {
	if s.issueErr != nil {
		return session.Claims{}, "", "", s.issueErr
	}
	return s.runtimeSecretSessionStoreStub.Issue(a, b, c, d, e, f)
}

// emailHTTPFixture groups synthetic identity, CSRF, session and shared consumption state with the
// email reconciliation HTTP handler.
// emailHTTPFixture объединяет синтетические identity, CSRF, сессию и общее состояние её
// потребления с HTTP-обработчиком сверки почтового эффекта.
type emailHTTPFixture struct {
	handler   http.Handler
	client    *emailEffectRecorder
	store     *emailSessionStore
	once      *emailOnceStore
	principal oidcauth.Principal
	csrf      string
}

// newEmailHTTPFixture builds a local authenticated fixture with receipt-, version- and
// digest-bound email elevation and a replacement session.
// newEmailHTTPFixture создаёт локальную аутентифицированную оснастку с почтовым повышением
// полномочий, привязанным к квитанции, версии и дайджесту, и замещающей сессией.
func newEmailHTTPFixture(client *emailEffectRecorder) *emailHTTPFixture {
	now := time.Now().UTC()
	csrf := strings.Repeat("c", 43)
	digest := sha256.Sum256([]byte(csrf))
	claims := session.Claims{Subject: uuid.NewString(), OrganizationID: uuid.NewString(), OIDCSessionID: uuid.NewString(), SessionID: uuid.NewString(), SessionRevision: 2,
		Bearer: "fixture-bearer", CSRFHash: hex.EncodeToString(digest[:]), IssuedAt: now.Unix(), ExpiresAt: now.Add(15 * time.Minute).Unix(),
		Elevation: &session.Elevation{Kind: session.ElevationKindEmailReconciliation, ReceiptRef: "erc_fixture01", ReceiptVersion: 3, ReceiptDigest: strings.Repeat("a", 64), ExpiresAt: now.Add(time.Minute).Unix()},
	}
	replacement := claims
	replacement.SessionID = uuid.NewString()
	replacement.Elevation = nil
	f := &emailHTTPFixture{client: client, csrf: csrf, store: &emailSessionStore{runtimeSecretSessionStoreStub: runtimeSecretSessionStoreStub{claims: claims, replacement: replacement}},
		once:      &emailOnceStore{consumed: map[string]bool{}},
		principal: oidcauth.Principal{Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID, SessionRevision: claims.SessionRevision, ExpiresAt: now.Add(time.Hour), AuthenticatedAt: now, ACR: "1", AMR: []string{"pwd"}},
	}
	f.handler = f.replica()
	return f
}

// replica builds another local handler over the same session and consumption stubs to test
// competing reconciliation requests.
// replica создаёт ещё один локальный обработчик над общими заглушками сессий и потребления для
// проверки конкурирующих запросов сверки.
func (f *emailHTTPFixture) replica() http.Handler {
	security, err := boundary.New(boundary.Config{Origins: []string{"https://control.example.test"}, Verifier: runtimeSecretOIDCStub{principal: f.principal}, Sessions: f.store, Revocations: f.once,
		Limiter: ratelimit.New(ratelimit.Config{Window: time.Minute, Limit: 100, MaximumKeys: 10, PreAuthConcurrency: 16, GlobalHTTPConcurrency: 32, PerSubjectHTTPConcurrency: 16, GlobalWebSocketConcurrency: 8, PerSubjectWebSocketConcurrency: 4}), Timeout: 5 * time.Second})
	if err != nil {
		panic(err)
	}
	server := &Server{boundary: security, control: &controlplaneclient.Client{Query: cp.NewPlatformQueryServiceClient(f.client), Command: cp.NewPlatformCommandServiceClient(f.client)}}
	return security.Middleware(generated.Handler(server))
}

// authorizeRequest attaches the fixture origin, CSRF proof and synthetic session cookies to a
// request without contacting an identity provider.
// authorizeRequest добавляет к запросу тестовый origin, подтверждение CSRF и синтетические cookies
// сессии без обращения к провайдеру identity.
func (f *emailHTTPFixture) authorizeRequest(r *http.Request) {
	r.Header.Set("Origin", "https://control.example.test")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: boundary.SessionCookieName, Value: "fixture-session"})
	r.AddCookie(&http.Cookie{Name: boundary.CSRFCookieName, Value: f.csrf})
}

// request creates an authorized reconciliation POST with the fixture receipt digest and an
// EFFECT_CONFIRMED decision.
// request создаёт авторизованный POST сверки с дайджестом тестовой квитанции и решением
// EFFECT_CONFIRMED.
func (f *emailHTTPFixture) request() *http.Request {
	r := managedTestRequest("POST", emailDecisionPath, emailDecisionBody("EFFECT_CONFIRMED"))
	f.authorizeRequest(r)
	return r
}

// TestEmailReconciliationRequiresExactElevation checks that absent, expired or mismatched
// elevation fails before RPC, session consumption and replacement cookies.
// TestEmailReconciliationRequiresExactElevation проверяет отказ при отсутствии, истечении либо
// несовпадении повышения полномочий до RPC, потребления сессии и выдачи замещающих cookies.
func TestEmailReconciliationRequiresExactElevation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*emailHTTPFixture)
	}{
		{"missing", func(f *emailHTTPFixture) { f.store.claims.Elevation = nil }},
		{"expired", func(f *emailHTTPFixture) { f.store.claims.Elevation.ExpiresAt = time.Now().Add(-time.Second).Unix() }},
		{"wrong-ref", func(f *emailHTTPFixture) { f.store.claims.Elevation.ReceiptRef = "erc_other01" }},
		{"wrong-version", func(f *emailHTTPFixture) { f.store.claims.Elevation.ReceiptVersion = 2 }},
		{"wrong-digest", func(f *emailHTTPFixture) { f.store.claims.Elevation.ReceiptDigest = strings.Repeat("b", 64) }},
		{"secret-purpose", func(f *emailHTTPFixture) {
			f.store.claims.Elevation = &session.Elevation{Kind: session.ElevationKindRuntimeSecretReveal, ScopeKind: "PROJECT", OrganizationRef: "org_fixture01", ProjectRef: "prj_fixture01", SecretRef: "sec_fixture01", ExpiresAt: time.Now().Add(time.Minute).Unix()}
		}},
		{"mixed-project", func(f *emailHTTPFixture) { f.store.claims.Elevation.ProjectRef = "prj_fixture01" }},
		{"mixed-secret", func(f *emailHTTPFixture) { f.store.claims.Elevation.SecretRef = "sec_fixture01" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newEmailHTTPFixture(&emailEffectRecorder{})
			tc.change(f)
			w := httptest.NewRecorder()
			f.handler.ServeHTTP(w, f.request())
			if w.Code != 403 || f.client.request != nil || len(f.once.consumed) != 0 || len(w.Result().Cookies()) != 0 {
				t.Fatalf("status=%d called=%t consumed=%d", w.Code, f.client.request != nil, len(f.once.consumed))
			}
		})
	}
}

// TestEmailReconciliationConsumesBeforeRPCAndKeepsDenial checks consumption before successful or
// failed owner RPC, replacement cookies and replay rejection by another local handler.
// TestEmailReconciliationConsumesBeforeRPCAndKeepsDenial проверяет потребление до успешного либо
// ошибочного RPC владельца, замещающие cookies и отказ повторному запросу через другой локальный
// обработчик.
func TestEmailReconciliationConsumesBeforeRPCAndKeepsDenial(t *testing.T) {
	for _, failure := range []error{nil, status.Error(codes.PermissionDenied, "private denial"), status.Error(codes.DeadlineExceeded, "private timeout")} {
		f := newEmailHTTPFixture(&emailEffectRecorder{failure: failure})
		f.client.beforeInvoke = func() {
			consumed, err := f.once.Revoked(context.Background(), f.store.claims.SessionID)
			if err != nil || !consumed {
				t.Error("RPC preceded durable consumption")
			}
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, f.request())
		want := 200
		if status.Code(failure) == codes.PermissionDenied {
			want = 403
		}
		if status.Code(failure) == codes.DeadlineExceeded {
			want = 504
		}
		if w.Code != want || f.client.request == nil || !f.once.consumed[f.store.claims.SessionID] || len(w.Result().Cookies()) != 2 {
			t.Fatalf("status=%d called=%t cookies=%d", w.Code, f.client.request != nil, len(w.Result().Cookies()))
		}
		f.client.request = nil
		replay := httptest.NewRecorder()
		f.replica().ServeHTTP(replay, f.request())
		if replay.Code != 401 || f.client.request != nil {
			t.Fatalf("replica replay status=%d called=%t", replay.Code, f.client.request != nil)
		}
	}
}

// TestEmailReconciliationStoreFailureDoesNotCallRPC checks that consumption or session-issuance
// errors return unavailable without RPC or cookies, preserving any prior consumption.
// TestEmailReconciliationStoreFailureDoesNotCallRPC проверяет отказ недоступности без RPC и
// cookies при ошибках потребления либо выдачи сессии с сохранением уже совершённого потребления.
func TestEmailReconciliationStoreFailureDoesNotCallRPC(t *testing.T) {
	for _, afterConsumption := range []bool{false, true} {
		f := newEmailHTTPFixture(&emailEffectRecorder{})
		if afterConsumption {
			f.store.issueErr = errors.New("synthetic issuer unavailable")
		} else {
			f.once.consumeErr = errors.New("synthetic store unavailable")
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, f.request())
		if w.Code != 503 || f.client.request != nil || len(w.Result().Cookies()) != 0 || f.once.consumed[f.store.claims.SessionID] != afterConsumption {
			t.Fatalf("status=%d consumed=%t called=%t", w.Code, f.once.consumed[f.store.claims.SessionID], f.client.request != nil)
		}
	}
}

// TestEmailReconciliationHasOneWinnerAcrossReplicas checks that eight concurrent requests through
// two local handlers sharing an in-memory store produce one owner RPC and one success.
// TestEmailReconciliationHasOneWinnerAcrossReplicas проверяет единственный RPC владельца и
// единственный успех среди восьми конкурентных запросов к двум локальным обработчикам с общим
// хранилищем в памяти.
func TestEmailReconciliationHasOneWinnerAcrossReplicas(t *testing.T) {
	f := newEmailHTTPFixture(&emailEffectRecorder{})
	replicas := []http.Handler{f.handler, f.replica()}
	outcomes := make(chan int, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			w := httptest.NewRecorder()
			replicas[i%len(replicas)].ServeHTTP(w, f.request())
			outcomes <- w.Code
		}(i)
	}
	workers.Wait()
	close(outcomes)
	winners := 0
	for code := range outcomes {
		if code == 200 {
			winners++
		} else if code != 401 && code != 403 {
			t.Fatalf("unexpected loser status=%d", code)
		}
	}
	if winners != 1 || f.client.calls.Load() != 1 || len(f.once.consumed) != 1 {
		t.Fatalf("winners=%d calls=%d consumed=%d", winners, f.client.calls.Load(), len(f.once.consumed))
	}
}

// TestEmailReceiptReadDoesNotRequireOrConsumeElevation checks that receipt reading succeeds
// without email elevation and leaves one-time session consumption untouched.
// TestEmailReceiptReadDoesNotRequireOrConsumeElevation проверяет успешное чтение квитанции без
// почтового повышения полномочий и без однократного потребления сессии.
func TestEmailReceiptReadDoesNotRequireOrConsumeElevation(t *testing.T) {
	f := newEmailHTTPFixture(&emailEffectRecorder{})
	f.store.claims.Elevation = nil
	r := managedTestRequest("GET", emailReceiptPath, "")
	f.authorizeRequest(r)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 200 || len(f.once.consumed) != 0 {
		t.Fatalf("read status=%d consumed=%d", w.Code, len(f.once.consumed))
	}
}
