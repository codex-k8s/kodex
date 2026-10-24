package httptransport

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/controlplaneclient"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/boundary"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// bootstrapQueryStub supplies the bootstrap query fixture while embedding unsupported platform
// query methods for local session tests.
// bootstrapQueryStub предоставляет тестовый bootstrap query и встраивает остальные
// неподдерживаемые методы query платформы для локальных тестов сессии.
type bootstrapQueryStub struct {
	controlplanev1.PlatformQueryServiceClient
}

// GetBootstrapState returns an empty synthetic bootstrap state so the HTTP test can isolate the
// authenticated session revision.
// GetBootstrapState возвращает пустое синтетическое bootstrap-состояние для отдельной проверки
// ревизии аутентифицированной сессии на HTTP-границе.
func (*bootstrapQueryStub) GetBootstrapState(context.Context, *controlplanev1.GetBootstrapStateRequest, ...grpc.CallOption) (*controlplanev1.GetBootstrapStateResponse, error) {
	return &controlplanev1.GetBootstrapStateResponse{State: &controlplanev1.BootstrapState{}}, nil
}

// ownerSessionOIDCStub holds a synthetic principal for local owner-session issuance tests without
// verifying a real OIDC token.
// ownerSessionOIDCStub хранит синтетический principal для локальных тестов выдачи сессии владельца
// без проверки настоящего OIDC token.
type ownerSessionOIDCStub struct {
	principal oidcauth.Principal
}

// VerifyAuthorization returns the configured synthetic principal and bearer marker for the fixture
// authorization path.
// VerifyAuthorization возвращает заданный синтетический principal и маркер bearer для тестового
// пути авторизации.
func (stub ownerSessionOIDCStub) VerifyAuthorization(context.Context, string) (oidcauth.Principal, string, error) {
	return stub.principal, "fresh-bearer", nil
}

// VerifyToken returns an empty synthetic principal for the unused token-verification branch of
// this issuance fixture.
// VerifyToken возвращает пустой синтетический principal для неиспользуемой ветки проверки token в
// оснастке выдачи сессии.
func (ownerSessionOIDCStub) VerifyToken(context.Context, string) (oidcauth.Principal, error) {
	return oidcauth.Principal{}, nil
}

// ownerSessionStoreStub counts ordinary and elevated issuance separately and captures the
// elevation purpose using a controlled fixture clock.
// ownerSessionStoreStub раздельно подсчитывает обычную и повышенную выдачу сессий и сохраняет
// назначение повышения с управляемым тестовым временем.
type ownerSessionStoreStub struct {
	normalCalls   int
	elevatedCalls int
	elevation     *session.Elevation
	now           time.Time
}

// Issue counts ordinary issuance and returns synthetic claims and cookie markers with no elevation
// purpose.
// Issue подсчитывает обычную выдачу и возвращает синтетические claims и маркеры cookies без
// назначения повышения полномочий.
func (store *ownerSessionStoreStub) Issue(string, string, string, uint64, string, time.Time) (session.Claims, string, string, error) {
	store.normalCalls++
	return ownerSessionClaims(store.now, nil), "normal-session", "normal-csrf", nil
}

// IssueWithElevation counts elevated issuance, captures its exact purpose and returns synthetic
// claims and cookie markers bound to that purpose.
// IssueWithElevation подсчитывает повышенную выдачу, сохраняет точное назначение и возвращает
// связанные с ним синтетические claims и маркеры cookies.
func (store *ownerSessionStoreStub) IssueWithElevation(_ string, _ string, _ string, _ uint64, _ string, _ time.Time, elevation *session.Elevation) (session.Claims, string, string, error) {
	store.elevatedCalls++
	store.elevation = elevation
	return ownerSessionClaims(store.now, elevation), "elevated-session", "elevated-csrf", nil
}

// Open returns empty claims for the unused cookie-opening branch of the owner-session issuance
// fixture.
// Open возвращает пустые claims для неиспользуемой ветки открытия cookie в оснастке выдачи сессии
// владельца.
func (*ownerSessionStoreStub) Open(string) (session.Claims, error) {
	return session.Claims{}, nil
}

// Renew keeps the supplied fixture claims unchanged and reports no renewed session cookie.
// Renew сохраняет переданные тестовые claims без изменений и сообщает об отсутствии обновлённой
// cookie сессии.
func (*ownerSessionStoreStub) Renew(claims session.Claims, _ time.Time) (session.Claims, string, bool, error) {
	return claims, "", false, nil
}

// ownerSessionClaims builds revision-five synthetic session claims with a fresh UUID,
// fifteen-minute lifetime and optional purpose-bound elevation.
// ownerSessionClaims создаёт синтетические claims сессии пятой ревизии с новым UUID, сроком
// пятнадцать минут и необязательным целевым повышением полномочий.
func ownerSessionClaims(now time.Time, elevation *session.Elevation) session.Claims {
	return session.Claims{SessionRevision: 5, SessionID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(15 * time.Minute).Unix(), Elevation: elevation}
}

// TestCreateOwnerSessionAcceptsOnlyTypedFreshPurpose checks normal login, exact secret and email
// purposes, their distinct authentication freshness limits and malformed-purpose rejection before
// issuance.
// TestCreateOwnerSessionAcceptsOnlyTypedFreshPurpose проверяет обычный вход, точные назначения
// секретов и почты, разные пределы свежести аутентификации и отказ неверным назначениям до выдачи
// сессии.
func TestCreateOwnerSessionAcceptsOnlyTypedFreshPurpose(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	emailPurpose := `{"purpose":{"kind":"EMAIL_EFFECT_RECONCILIATION","receiptRef":"erc_fixture01","receiptVersion":3,"receiptDigest":"` + strings.Repeat("a", 64) + `"}}`
	for _, test := range []struct {
		name            string
		body            string
		authenticatedAt time.Time
		wantStatus      int
		wantNormal      int
		wantElevated    int
	}{
		{name: "normal login", wantStatus: http.StatusNoContent, wantNormal: 1},
		{name: "fresh reveal", body: `{"purpose":{"kind":"RUNTIME_SECRET_REVEAL","scopeKind":"PROJECT","organizationRef":"org_fixture01","projectRef":"project_sales","secretRef":"secret_main"}}`, authenticatedAt: now.Add(-30 * time.Second), wantStatus: http.StatusNoContent, wantElevated: 1},
		{name: "fresh email", body: emailPurpose, authenticatedAt: now.Add(-4 * time.Minute), wantStatus: http.StatusNoContent, wantElevated: 1},
		{name: "stale email", body: emailPurpose, authenticatedAt: now.Add(-5 * time.Minute), wantStatus: http.StatusForbidden},
		{name: "mixed email project", body: strings.Replace(emailPurpose, `"receiptRef":`, `"projectRef":"prj_fixture01","receiptRef":`, 1), authenticatedAt: now, wantStatus: http.StatusBadRequest},
		{name: "mixed email secret", body: strings.Replace(emailPurpose, `"receiptRef":`, `"secretRef":"sec_fixture01","receiptRef":`, 1), authenticatedAt: now, wantStatus: http.StatusBadRequest},
		{name: "missing email version", body: strings.Replace(emailPurpose, `"receiptVersion":3,`, "", 1), authenticatedAt: now, wantStatus: http.StatusBadRequest},
		{name: "null email version", body: strings.Replace(emailPurpose, `"receiptVersion":3`, `"receiptVersion":null`, 1), authenticatedAt: now, wantStatus: http.StatusBadRequest},
		{name: "unsafe email version", body: strings.Replace(emailPurpose, `"receiptVersion":3`, `"receiptVersion":9007199254740992`, 1), authenticatedAt: now, wantStatus: http.StatusBadRequest},
		{name: "stale auth_time", body: `{"purpose":{"kind":"RUNTIME_SECRET_REVEAL","scopeKind":"PROJECT","organizationRef":"org_fixture01","projectRef":"project_sales","secretRef":"secret_main"}}`, authenticatedAt: now.Add(-3 * time.Minute), wantStatus: http.StatusForbidden},
		{name: "unknown purpose field", body: `{"purpose":{"kind":"RUNTIME_SECRET_REVEAL","scopeKind":"PROJECT","organizationRef":"org_fixture01","projectRef":"project_sales","secretRef":"secret_main","extra":true}}`, authenticatedAt: now, wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &ownerSessionStoreStub{now: now}
			principal := oidcauth.Principal{
				Subject: uuid.NewString(), OrganizationID: uuid.NewString(), SessionID: uuid.NewString(), SessionRevision: 5,
				AuthenticatedAt: test.authenticatedAt, ExpiresAt: now.Add(time.Hour), ACR: "1", AMR: []string{"pwd"},
			}
			security, err := boundary.New(boundary.Config{
				Origins: []string{"https://control.example.test"}, Verifier: ownerSessionOIDCStub{principal: principal},
				Sessions: store, Revocations: &runtimeSecretRevocationStoreStub{},
				Limiter: ratelimit.New(ratelimit.Config{Window: time.Minute, Limit: 100, MaximumKeys: 10, PreAuthConcurrency: 2, GlobalHTTPConcurrency: 4, PerSubjectHTTPConcurrency: 2, GlobalWebSocketConcurrency: 4, PerSubjectWebSocketConcurrency: 2}),
				Timeout: 5 * time.Second,
			})
			if err != nil {
				t.Fatalf("new boundary: %v", err)
			}
			server := &Server{boundary: security}
			request := httptest.NewRequest(http.MethodPost, "https://control.example.test/api/v1/session", bytes.NewBufferString(test.body))
			request.Header.Set("Origin", "https://control.example.test")
			request.Header.Set("Authorization", "Bearer fresh")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			security.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				server.CreateOwnerSession(writer, request, generated.CreateOwnerSessionParams{IdempotencyKey: "idem-session-123"})
			})).ServeHTTP(response, request)

			if response.Code != test.wantStatus || store.normalCalls != test.wantNormal || store.elevatedCalls != test.wantElevated {
				t.Fatalf("session result = status %d normal %d elevated %d", response.Code, store.normalCalls, store.elevatedCalls)
			}
			if test.wantElevated == 1 {
				e := store.elevation
				if strings.Contains(test.body, session.ElevationKindEmailReconciliation) {
					if e == nil || e.Kind != session.ElevationKindEmailReconciliation || e.ReceiptRef != "erc_fixture01" || e.ReceiptVersion != 3 || e.ReceiptDigest != strings.Repeat("a", 64) || e.ProjectRef != "" || e.SecretRef != "" {
						t.Fatalf("email elevation is not exact: %#v", e)
					}
				} else if e == nil || e.Kind != session.ElevationKindRuntimeSecretReveal || e.ProjectRef != "project_sales" || e.SecretRef != "secret_main" || e.ReceiptRef != "" || e.ReceiptVersion != 0 || e.ReceiptDigest != "" {
					t.Fatalf("secret elevation is not exact: %#v", e)
				}
			}
		})
	}
}

// TestBootstrapReturnsAuthenticatedSessionRevision checks that bootstrap exposes revision five
// from the authenticated fixture session as its HTTP ETag.
// TestBootstrapReturnsAuthenticatedSessionRevision проверяет выдачу пятой ревизии
// аутентифицированной тестовой сессии в HTTP ETag ответа bootstrap.
func TestBootstrapReturnsAuthenticatedSessionRevision(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	claims := ownerSessionClaims(now, nil)
	claims.Subject = uuid.NewString()
	claims.OrganizationID = uuid.NewString()
	claims.OIDCSessionID = uuid.NewString()
	claims.Bearer = "bound-bearer"
	principal := oidcauth.Principal{
		Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID,
		SessionRevision: claims.SessionRevision, ExpiresAt: now.Add(time.Hour),
	}
	security, err := boundary.New(boundary.Config{
		Origins: []string{"https://control.example.test"}, Verifier: runtimeSecretOIDCStub{principal: principal},
		Sessions: runtimeSecretSessionStoreStub{claims: claims}, Revocations: &runtimeSecretRevocationStoreStub{},
		Limiter: ratelimit.New(ratelimit.Config{Window: time.Minute, Limit: 100, MaximumKeys: 10, PreAuthConcurrency: 2, GlobalHTTPConcurrency: 4, PerSubjectHTTPConcurrency: 2, GlobalWebSocketConcurrency: 4, PerSubjectWebSocketConcurrency: 2}),
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("new boundary: %v", err)
	}
	server := &Server{control: &controlplaneclient.Client{Query: &bootstrapQueryStub{}}}
	request := httptest.NewRequest(http.MethodGet, "https://control.example.test/api/v1/bootstrap", nil)
	request.AddCookie(&http.Cookie{Name: boundary.SessionCookieName, Value: "encoded-session"})
	response := httptest.NewRecorder()

	security.Middleware(http.HandlerFunc(server.GetBootstrapState)).ServeHTTP(response, request)

	if response.Code != http.StatusOK || response.Header().Get("ETag") != `"5"` {
		t.Fatalf("bootstrap result = status %d ETag %q", response.Code, response.Header().Get("ETag"))
	}
}
