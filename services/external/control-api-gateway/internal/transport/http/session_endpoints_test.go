package httptransport

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
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

type bootstrapQueryStub struct {
	controlplanev1.PlatformQueryServiceClient
}

func (*bootstrapQueryStub) GetBootstrapState(context.Context, *controlplanev1.GetBootstrapStateRequest, ...grpc.CallOption) (*controlplanev1.GetBootstrapStateResponse, error) {
	return &controlplanev1.GetBootstrapStateResponse{State: &controlplanev1.BootstrapState{}}, nil
}

type ownerSessionOIDCStub struct {
	principal oidcauth.Principal
}

func (stub ownerSessionOIDCStub) VerifyAuthorization(context.Context, string) (oidcauth.Principal, string, error) {
	return stub.principal, "fresh-bearer", nil
}

func (ownerSessionOIDCStub) VerifyToken(context.Context, string) (oidcauth.Principal, error) {
	return oidcauth.Principal{}, nil
}

type ownerSessionStoreStub struct {
	normalCalls   int
	elevatedCalls int
	elevation     *session.Elevation
	now           time.Time
}

func (store *ownerSessionStoreStub) Issue(string, string, string, uint64, string, time.Time) (session.Claims, string, string, error) {
	store.normalCalls++
	return ownerSessionClaims(store.now, nil), "normal-session", "normal-csrf", nil
}

func (store *ownerSessionStoreStub) IssueWithElevation(_ string, _ string, _ string, _ uint64, _ string, _ time.Time, elevation *session.Elevation) (session.Claims, string, string, error) {
	store.elevatedCalls++
	store.elevation = elevation
	return ownerSessionClaims(store.now, elevation), "elevated-session", "elevated-csrf", nil
}

func (*ownerSessionStoreStub) Open(string) (session.Claims, error) {
	return session.Claims{}, nil
}

func (*ownerSessionStoreStub) Renew(claims session.Claims, _ time.Time) (session.Claims, string, bool, error) {
	return claims, "", false, nil
}

func ownerSessionClaims(now time.Time, elevation *session.Elevation) session.Claims {
	return session.Claims{SessionRevision: 5, SessionID: uuid.NewString(), IssuedAt: now.Unix(), ExpiresAt: now.Add(15 * time.Minute).Unix(), Elevation: elevation}
}

func TestCreateOwnerSessionAcceptsOnlyTypedFreshPurpose(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, test := range []struct {
		name            string
		body            string
		authenticatedAt time.Time
		wantStatus      int
		wantNormal      int
		wantElevated    int
	}{
		{name: "normal login", wantStatus: http.StatusNoContent, wantNormal: 1},
		{name: "fresh reveal", body: `{"purpose":{"kind":"RUNTIME_SECRET_REVEAL","projectRef":"project_sales","secretRef":"secret_main"}}`, authenticatedAt: now.Add(-30 * time.Second), wantStatus: http.StatusNoContent, wantElevated: 1},
		{name: "stale auth_time", body: `{"purpose":{"kind":"RUNTIME_SECRET_REVEAL","projectRef":"project_sales","secretRef":"secret_main"}}`, authenticatedAt: now.Add(-3 * time.Minute), wantStatus: http.StatusForbidden},
		{name: "unknown purpose field", body: `{"purpose":{"kind":"RUNTIME_SECRET_REVEAL","projectRef":"project_sales","secretRef":"secret_main","extra":true}}`, authenticatedAt: now, wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &ownerSessionStoreStub{now: now}
			principal := oidcauth.Principal{
				Subject: uuid.NewString(), OrganizationID: uuid.NewString(), SessionID: uuid.NewString(), SessionRevision: 5,
				AuthenticatedAt: test.authenticatedAt, ExpiresAt: now.Add(time.Hour),
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
			if test.wantElevated == 1 && (store.elevation == nil || store.elevation.ProjectRef != "project_sales" || store.elevation.SecretRef != "secret_main") {
				t.Fatalf("elevation is not exact: %#v", store.elevation)
			}
		})
	}
}

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
