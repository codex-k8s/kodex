package boundary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
	"github.com/google/uuid"
)

type fakeOIDCVerifier struct {
	principal oidcauth.Principal
	err       error
}

func (verifier *fakeOIDCVerifier) VerifyAuthorization(context.Context, string) (oidcauth.Principal, string, error) {
	return verifier.principal, "bearer", verifier.err
}

func (verifier *fakeOIDCVerifier) VerifyToken(context.Context, string) (oidcauth.Principal, error) {
	return verifier.principal, verifier.err
}

type fakeSessionStore struct {
	claims      session.Claims
	openErr     error
	renewCalls  int
	renewed     session.Claims
	renewedCode string
}

func (store *fakeSessionStore) Issue(string, string, string, uint64, string, time.Time) (session.Claims, string, string, error) {
	return session.Claims{}, "", "", errors.New("unexpected issue")
}

func (store *fakeSessionStore) Open(string) (session.Claims, error) {
	return store.claims, store.openErr
}

func (store *fakeSessionStore) Renew(session.Claims, time.Time) (session.Claims, string, bool, error) {
	store.renewCalls++
	return store.renewed, store.renewedCode, true, nil
}

type fakeRevocationStore struct {
	revoked       bool
	revokedErr    error
	revokeErr     error
	checked       string
	revokedRecord string
}

func (store *fakeRevocationStore) Revoke(_ context.Context, sessionID string) error {
	store.revokedRecord = sessionID
	return store.revokeErr
}

func (store *fakeRevocationStore) Revoked(_ context.Context, sessionID string) (bool, error) {
	store.checked = sessionID
	return store.revoked, store.revokedErr
}

func TestAuthenticationProblemPreservesSigningKeyOutage(t *testing.T) {
	statusCode, code, retryable := authenticationProblem(oidcauth.ErrSigningKeysUnavailable)
	if statusCode != http.StatusServiceUnavailable || code != "UNAVAILABLE" || !retryable {
		t.Fatalf("JWKS outage mapping = %d/%s/%t", statusCode, code, retryable)
	}
	statusCode, code, retryable = authenticationProblem(errors.New("invalid bearer"))
	if statusCode != http.StatusUnauthorized || code != "UNAUTHENTICATED" || retryable {
		t.Fatalf("invalid bearer mapping = %d/%s/%t", statusCode, code, retryable)
	}
}

func TestCredentialedPreflightAllowsExactOwnerHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "https://control-api.kodex.local/api/v1/session", nil)
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match, X-CSRF-Token, X-Kodex-Project-ID")
	if !allowedPreflight(request) {
		t.Fatal("exact credentialed owner preflight was rejected")
	}
	request.Header.Set("Access-Control-Request-Headers", "Authorization, X-Caller-Owner")
	if allowedPreflight(request) {
		t.Fatal("caller-supplied authority header was accepted")
	}

	request.Header.Set("Origin", "https://control.example.test")
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type, Idempotency-Key, If-Match, X-CSRF-Token, X-Kodex-Project-ID")
	called := false
	boundary := &Boundary{origins: map[string]struct{}{"https://control.example.test": {}}}
	response := httptest.NewRecorder()
	boundary.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(response, request)
	if called || response.Code != http.StatusNoContent ||
		response.Header().Get("Access-Control-Allow-Origin") != "https://control.example.test" ||
		response.Header().Get("Access-Control-Allow-Credentials") != "true" ||
		response.Header().Get("Access-Control-Allow-Headers") != "Authorization, Content-Type, Idempotency-Key, If-Match, X-CSRF-Token, X-Kodex-Project-ID" {
		t.Fatalf("credentialed preflight response is incomplete: status=%d headers=%v", response.Code, response.Header())
	}
	vary := make([]string, 0)
	for _, header := range response.Header().Values("Vary") {
		for _, value := range strings.Split(header, ",") {
			vary = append(vary, strings.TrimSpace(value))
		}
	}
	for _, required := range []string{"Origin", "Access-Control-Request-Method", "Access-Control-Request-Headers"} {
		if !slices.Contains(vary, required) {
			t.Fatalf("preflight Vary misses %s: %v", required, vary)
		}
	}
}

func TestProjectReferenceBinding(t *testing.T) {
	t.Parallel()

	const projectID = "prj_AQIDBAUGBwgJCgsMDQ4PEBES"
	tests := []struct {
		name      string
		method    string
		path      string
		header    string
		wantBound bool
		wantErr   bool
	}{
		{name: "HTTP header", method: http.MethodGet, path: "/api/v1/runs", header: projectID, wantBound: true},
		{name: "run stream uses run eligibility", method: http.MethodGet, path: "/api/v1/runs/run_12345678/stream"},
		{name: "exact project update path", method: http.MethodPut, path: "/api/v1/projects/" + projectID, wantBound: true},
		{name: "exact project delete path", method: http.MethodDelete, path: "/api/v1/projects/" + projectID, wantBound: true},
		{name: "matching exact project header", method: http.MethodDelete, path: "/api/v1/projects/" + projectID, header: projectID, wantBound: true},
		{name: "project collection is unbound", method: http.MethodPost, path: "/api/v1/projects"},
		{name: "invalid header", method: http.MethodGet, path: "/api/v1/runs", header: "invalid", wantErr: true},
		{name: "invalid exact project path", method: http.MethodDelete, path: "/api/v1/projects/invalid", wantErr: true},
		{name: "mismatched exact project scope", method: http.MethodDelete, path: "/api/v1/projects/" + projectID, header: "prj_EhEQDw4NDAsKCQgHBgUEAwIB", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(test.method, "https://control.example.test"+test.path, nil)
			if test.header != "" {
				request.Header.Set(ProjectReferenceHeader, test.header)
			}
			base := context.Background()
			result, err := withProjectReference(base, request)
			if (err != nil) != test.wantErr {
				t.Fatalf("withProjectReference() error = %v, wantErr %v", err, test.wantErr)
			}
			if err == nil && (result != base) != test.wantBound {
				t.Fatalf("withProjectReference() bound = %v, want %v", result != base, test.wantBound)
			}
		})
	}
}

func TestRealtimePathClassification(t *testing.T) {
	for _, path := range []string{
		"/api/v1/runs/run_12345678/stream",
		"/api/v1/platform/stream",
	} {
		if !isRealtimePath(path) {
			t.Fatalf("realtime path was not classified: %s", path)
		}
	}
	for _, path := range []string{
		"/api/v1/runs",
		"/api/v1/platform/stream/extra",
		"/api/v1/platform",
	} {
		if isRealtimePath(path) {
			t.Fatalf("ordinary HTTP path was classified as realtime: %s", path)
		}
	}
}

func TestSessionRenewalRunsOnlyAfterCompleteSecurityBoundary(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	csrf := strings.Repeat("c", 43)
	csrfDigest := sha256.Sum256([]byte(csrf))
	claims := session.Claims{
		Subject: uuid.NewString(), OrganizationID: uuid.NewString(), OIDCSessionID: uuid.NewString(),
		SessionRevision: 7, SessionID: uuid.NewString(), Bearer: "bearer",
		CSRFHash: hex.EncodeToString(csrfDigest[:]), IssuedAt: now.Add(-11 * time.Minute).Unix(), ExpiresAt: now.Add(4 * time.Minute).Unix(),
	}
	renewed := claims
	renewed.IssuedAt = now.Unix()
	renewed.ExpiresAt = now.Add(15 * time.Minute).Unix()
	principal := oidcauth.Principal{
		Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID,
		SessionRevision: claims.SessionRevision, ExpiresAt: now.Add(time.Hour),
	}
	store := &fakeSessionStore{claims: claims, renewed: renewed, renewedCode: "renewed-session"}
	verifier := &fakeOIDCVerifier{principal: principal}
	security := testBoundary(t, verifier, store)

	request := authenticatedRequest(http.MethodPut, csrf)
	request.URL.Path = "/api/v1/session"
	request.Header.Set("X-CSRF-Token", csrf)
	response := httptest.NewRecorder()
	security.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		identity, ok := IdentityFromContext(request.Context())
		if !ok || !identity.ExpiresAt.Equal(time.Unix(renewed.ExpiresAt, 0).UTC()) {
			t.Fatalf("renewed identity is absent: %#v/%t", identity, ok)
		}
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || store.renewCalls != 1 || len(response.Header().Values("Set-Cookie")) != 2 {
		t.Fatalf("successful renewal = status %d, calls %d, cookies %v", response.Code, store.renewCalls, response.Header().Values("Set-Cookie"))
	}
	for _, name := range []string{SessionCookieName, CSRFCookieName} {
		if !strings.Contains(strings.Join(response.Header().Values("Set-Cookie"), "\n"), name+"=") {
			t.Fatalf("renewal did not set %s", name)
		}
	}

	ordinaryMutation := authenticatedRequest(http.MethodPut, csrf)
	beforeMutation := store.renewCalls
	mutationResponse := httptest.NewRecorder()
	security.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(mutationResponse, ordinaryMutation)
	if mutationResponse.Code != http.StatusNoContent || store.renewCalls != beforeMutation ||
		len(mutationResponse.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("ordinary mutation renewed session: status=%d calls=%d cookies=%v", mutationResponse.Code,
			store.renewCalls-beforeMutation, mutationResponse.Header().Values("Set-Cookie"))
	}

	readWithoutProof := authenticatedRequest(http.MethodGet, csrf)
	readWithoutProof.Header.Del("Origin")
	beforeRead := store.renewCalls
	readResponse := httptest.NewRecorder()
	security.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(readResponse, readWithoutProof)
	if readResponse.Code != http.StatusNoContent || store.renewCalls != beforeRead ||
		len(readResponse.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("unproven read renewed session: status=%d calls=%d cookies=%v", readResponse.Code,
			store.renewCalls-beforeRead, readResponse.Header().Values("Set-Cookie"))
	}

	realtimeRequest := authenticatedRequest(http.MethodGet, csrf)
	realtimeRequest.SetPathValue("runRef", "run_12345678")
	realtimeRequest.URL.Path = "/api/v1/runs/run_12345678/stream"
	beforeRealtime := store.renewCalls
	realtimeResponse := httptest.NewRecorder()
	security.Middleware(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(realtimeResponse, realtimeRequest)
	if realtimeResponse.Code != http.StatusNoContent || store.renewCalls != beforeRealtime ||
		len(realtimeResponse.Header().Values("Set-Cookie")) != 0 {
		t.Fatalf("realtime handshake renewed session: status=%d calls=%d cookies=%v", realtimeResponse.Code,
			store.renewCalls-beforeRealtime, realtimeResponse.Header().Values("Set-Cookie"))
	}

	tests := []struct {
		name      string
		request   *http.Request
		configure func()
	}{
		{name: "rejected Origin", request: authenticatedRequest(http.MethodGet, csrf), configure: func() {
		}},
		{name: "invalid CSRF", request: authenticatedRequest(http.MethodPost, "wrong"), configure: func() {
		}},
		{name: "invalid session", request: authenticatedRequest(http.MethodGet, csrf), configure: func() {
			store.openErr = errors.New("expired session")
		}},
		{name: "invalid OIDC binding", request: authenticatedRequest(http.MethodGet, csrf), configure: func() {
			verifier.principal.SessionID = uuid.NewString()
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store.openErr = nil
			verifier.principal = principal
			test.configure()
			if test.name == "rejected Origin" {
				test.request.Header.Set("Origin", "https://rejected.example.test")
			}
			before := store.renewCalls
			response := httptest.NewRecorder()
			security.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("request with invalid security context reached handler")
			})).ServeHTTP(response, test.request)
			if store.renewCalls != before || len(response.Header().Values("Set-Cookie")) != 0 {
				t.Fatalf("rejected request renewed session: calls=%d cookies=%v", store.renewCalls-before, response.Header().Values("Set-Cookie"))
			}
		})
	}
}

func TestRevokedBrowserSessionIsRejected(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	csrf := strings.Repeat("c", 43)
	csrfDigest := sha256.Sum256([]byte(csrf))
	claims := session.Claims{
		Subject: uuid.NewString(), OrganizationID: uuid.NewString(), OIDCSessionID: uuid.NewString(),
		SessionRevision: 7, SessionID: uuid.NewString(), Bearer: "bearer",
		CSRFHash: hex.EncodeToString(csrfDigest[:]), IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(14 * time.Minute).Unix(),
	}
	principal := oidcauth.Principal{
		Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID,
		SessionRevision: claims.SessionRevision, ExpiresAt: now.Add(time.Hour),
	}
	store := &fakeSessionStore{claims: claims}
	revocations := &fakeRevocationStore{revoked: true}
	security := testBoundaryWithRevocations(t, &fakeOIDCVerifier{principal: principal}, store, revocations)
	request := authenticatedRequest(http.MethodGet, csrf)
	response := httptest.NewRecorder()
	called := false
	security.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	})).ServeHTTP(response, request)
	if called || response.Code != http.StatusUnauthorized || revocations.checked != claims.SessionID {
		t.Fatalf("revoked browser session reached handler: called=%t status=%d", called, response.Code)
	}
}

func TestRevocationStoreFailureRejectsBrowserSession(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	csrf := strings.Repeat("c", 43)
	csrfDigest := sha256.Sum256([]byte(csrf))
	claims := session.Claims{
		Subject: uuid.NewString(), OrganizationID: uuid.NewString(), OIDCSessionID: uuid.NewString(),
		SessionRevision: 7, SessionID: uuid.NewString(), Bearer: "bearer",
		CSRFHash: hex.EncodeToString(csrfDigest[:]), IssuedAt: now.Add(-time.Minute).Unix(), ExpiresAt: now.Add(14 * time.Minute).Unix(),
	}
	principal := oidcauth.Principal{
		Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID,
		SessionRevision: claims.SessionRevision, ExpiresAt: now.Add(time.Hour),
	}
	revocations := &fakeRevocationStore{revokedErr: errors.New("NATS unavailable")}
	security := testBoundaryWithRevocations(t, &fakeOIDCVerifier{principal: principal}, &fakeSessionStore{claims: claims}, revocations)
	request := authenticatedRequest(http.MethodGet, csrf)
	response := httptest.NewRecorder()
	called := false
	security.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })).ServeHTTP(response, request)
	if called || response.Code != http.StatusUnauthorized || revocations.checked != claims.SessionID {
		t.Fatalf("request passed unavailable revocation store: called=%t status=%d", called, response.Code)
	}
}

func TestRevokeSessionPersistsBrowserSessionID(t *testing.T) {
	revocations := &fakeRevocationStore{}
	security := testBoundaryWithRevocations(t, &fakeOIDCVerifier{}, &fakeSessionStore{}, revocations)
	sessionID := uuid.NewString()
	if err := security.RevokeSession(context.Background(), Identity{BrowserSessionID: sessionID}); err != nil {
		t.Fatalf("revoke browser session: %v", err)
	}
	if revocations.revokedRecord != sessionID {
		t.Fatalf("revoked session = %q, want %q", revocations.revokedRecord, sessionID)
	}
}

func testBoundary(t *testing.T, verifier OIDCVerifier, sessions SessionStore) *Boundary {
	t.Helper()
	return testBoundaryWithRevocations(t, verifier, sessions, &fakeRevocationStore{})
}

func testBoundaryWithRevocations(t *testing.T, verifier OIDCVerifier, sessions SessionStore, revocations RevocationStore) *Boundary {
	t.Helper()
	security, err := New(Config{
		Origins: []string{"https://control.example.test"}, Verifier: verifier, Sessions: sessions, Revocations: revocations,
		Limiter: ratelimit.New(ratelimit.Config{
			Window: time.Minute, Limit: 100, MaximumKeys: 10, PreAuthConcurrency: 2,
			GlobalHTTPConcurrency: 4, PerSubjectHTTPConcurrency: 2,
			GlobalWebSocketConcurrency: 4, PerSubjectWebSocketConcurrency: 2,
		}), Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("new boundary: %v", err)
	}
	return security
}

func authenticatedRequest(method, csrf string) *http.Request {
	request := httptest.NewRequest(method, "https://control.example.test/api/v1/projects", nil)
	request.Header.Set("Origin", "https://control.example.test")
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "encoded-session"})
	request.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrf})
	if method != http.MethodGet {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	return request
}
