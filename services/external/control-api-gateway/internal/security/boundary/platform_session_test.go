package boundary

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
)

// platformBoundaryVerifier models independent current owner state and a lost revoke response without weakening the boundary's real cookie/CSRF/family checks.
// It is a synthetic adapter fixture, not cryptographic OIDC or a live control-plane acceptance proof.
// platformBoundaryVerifier моделирует независимое current owner state и lost revoke response, сохраняя реальные cookie/CSRF/family checks.
// Это synthetic adapter fixture, а не доказательство cryptographic OIDC и live control-plane acceptance.
type platformBoundaryVerifier struct {
	principal                                                     oidcverifier.Principal
	ownerRevoked, lostResponse                                    bool
	normalCalls, closureCalls, revokeCalls, effects, refreshCalls int
}

// VerifyToken denies revoked or expired application identity while preserving the independently configured session tuple.
// VerifyToken отклоняет revoked и expired application identity, сохраняя независимо заданный session tuple.
func (fixture *platformBoundaryVerifier) VerifyToken(context.Context, string) (oidcverifier.Principal, error) {
	fixture.normalCalls++
	if fixture.ownerRevoked || !fixture.principal.ExpiresAt.After(time.Now()) {
		return oidcverifier.Principal{}, oidcverifier.ErrGrantRejected
	}
	return fixture.principal, nil
}

// VerifyAuthorization uses the ordinary owner path and never treats a closure tombstone as a new login.
// VerifyAuthorization использует ordinary owner path и не превращает closure tombstone в новый login.
func (fixture *platformBoundaryVerifier) VerifyAuthorization(ctx context.Context, header string) (oidcverifier.Principal, string, error) {
	if header != "Bearer synthetic-access" {
		return oidcverifier.Principal{}, "", oidcverifier.ErrGrantRejected
	}
	principal, err := fixture.VerifyToken(ctx, "synthetic-access")
	return principal, "synthetic-access", err
}

// VerifyClosureToken reads only a still-valid signed caller's existing session, including revoked state, without mutation.
// VerifyClosureToken читает лишь existing session действительного signed caller, включая revoked state, без mutation.
func (fixture *platformBoundaryVerifier) VerifyClosureToken(context.Context, string) (oidcverifier.Principal, error) {
	fixture.closureCalls++
	if !fixture.principal.ExpiresAt.After(time.Now()) {
		return oidcverifier.Principal{}, oidcverifier.ErrGrantRejected
	}
	return fixture.principal, nil
}

// RevokeIdentitySession commits one synthetic owner effect and optionally loses its first response; repeats return the same tombstone.
// RevokeIdentitySession фиксирует один synthetic owner effect и при настройке теряет первый ответ; повторы возвращают тот же tombstone.
func (fixture *platformBoundaryVerifier) RevokeIdentitySession(context.Context, string, oidcverifier.Principal) error {
	fixture.revokeCalls++
	if !fixture.ownerRevoked {
		fixture.effects++
		fixture.ownerRevoked = true
	}
	if fixture.lostResponse {
		fixture.lostResponse = false
		return oidcverifier.ErrIdentityStateUnavailable
	}
	return nil
}

// Refresh records an unexpected provider effect, allowing pending-revoke tests to detect accidental refresh admission.
// Refresh считает неожиданный provider effect, позволяя pending-revoke tests обнаружить ошибочный refresh admission.
func (fixture *platformBoundaryVerifier) Refresh(context.Context, string) (oidcverifier.BrowserTokens, error) {
	fixture.refreshCalls++
	return oidcverifier.BrowserTokens{}, oidcverifier.ErrGrantRejected
}

// platformHTTPFixture groups real protected family/cookie storage and HTTP middleware with the independently controlled synthetic owner.
// platformHTTPFixture объединяет реальное protected family/cookie storage и HTTP middleware с независимо управляемым synthetic owner.
type platformHTTPFixture struct {
	security                                *Boundary
	families                                *session.Families
	owner                                   *platformBoundaryVerifier
	family                                  session.Family
	cookie, csrf                            string
	revocations                             *fakeRevocationStore
	applicationCalls, completedClosureCalls int
}

// newPlatformHTTPFixture creates an explicit synthetic platform family and the unchanged browser protections without network or production credentials.
// newPlatformHTTPFixture создаёт explicit synthetic platform family и прежние browser protections без сети и production credentials.
func newPlatformHTTPFixture(t *testing.T) *platformHTTPFixture {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	owner := &platformBoundaryVerifier{principal: oidcverifier.Principal{Issuer: "https://identity.example.invalid/realm/synthetic", Subject: "20000000-0000-4000-8000-000000000001",
		OrganizationID: "50000000-0000-4000-8000-000000000001", SessionID: "30000000-0000-4000-8000-000000000001", SessionRevision: 27,
		AuthenticatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(5 * time.Minute), PlatformManagedOIDC: true, PlatformSessionExpiresAt: now.Add(time.Hour)}}
	keys := testSessionKeys(t)
	storage := &browserMemory{records: map[string]browserstate.Record{}}
	families, err := session.NewFamilies(t.Context(), storage, owner, keys)
	if err != nil {
		t.Fatal(err)
	}
	family, csrf, err := families.CreateWithCSRF(t.Context(), oidcverifier.BrowserTokens{Principal: owner.principal, AccessToken: "synthetic-access", RefreshToken: "synthetic-refresh", RefreshExpiresAt: now.Add(2 * time.Hour)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, cookie, err := families.Cookie(family)
	if err != nil {
		t.Fatal(err)
	}
	revocations := &fakeRevocationStore{}
	security, err := New(Config{Families: families, Origins: []string{"https://app.example.invalid"}, Verifier: owner, Sessions: keys, Revocations: revocations,
		Limiter: ratelimit.New(ratelimit.Config{Window: time.Minute, Limit: 100, MaximumKeys: 100, PreAuthConcurrency: 4, GlobalHTTPConcurrency: 8, PerSubjectHTTPConcurrency: 2, GlobalWebSocketConcurrency: 4, PerSubjectWebSocketConcurrency: 1}), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return &platformHTTPFixture{security: security, families: families, owner: owner, family: family, cookie: cookie, csrf: csrf, revocations: revocations}
}

// request runs the real middleware and a small exact logout consumer; its synthetic handler exposes admission only, not platform business behavior.
// request запускает реальный middleware и небольшой exact logout consumer; synthetic handler показывает admission, а не platform business behavior.
func (fixture *platformHTTPFixture) request(method, path, origin, csrf string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://app.example.invalid"+path, nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("If-Match", fmt.Sprintf("\"%d\"", fixture.family.Principal.SessionRevision))
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: fixture.cookie})
	r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: fixture.csrf})
	w := httptest.NewRecorder()
	fixture.security.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPlatformLogout(r) {
			identity, ok := IdentityFromContext(r.Context())
			if !ok {
				w.WriteHeader(500)
				return
			}
			if identity.closureComplete {
				fixture.completedClosureCalls++
			}
			if err := fixture.security.RevokeSession(r.Context(), identity); err != nil {
				code, _, _ := authenticationProblem(err)
				w.WriteHeader(code)
				return
			}
		} else {
			fixture.applicationCalls++
		}
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(w, r)
	return w
}

// TestPlatformLogoutLostResponseRemainsClosed proves a lost owner response fences HTTP/WS/refresh until exact reconciliation, then permits only repeated cookie cleanup.
// CSRF and Origin remain mandatory before any owner mutation, including after a confirmed receipt.
// TestPlatformLogoutLostResponseRemainsClosed проверяет закрытие HTTP/WS/refresh при потерянном owner response до exact reconciliation, затем лишь repeated cookie cleanup.
// CSRF и Origin обязательны до owner mutation, включая запрос после confirmed receipt.
func TestPlatformLogoutLostResponseRemainsClosed(t *testing.T) {
	f := newPlatformHTTPFixture(t)
	if got := f.request(http.MethodGet, "/api/v1/projects", "https://app.example.invalid", ""); got.Code != 204 {
		t.Fatal("active platform family could not enter ordinary middleware")
	}
	if got := f.request(http.MethodDelete, "/api/v1/session", "https://app.example.invalid", "wrong"); got.Code != 403 || f.owner.revokeCalls != 0 {
		t.Fatal("invalid CSRF reached the owner mutation")
	}
	if got := f.request(http.MethodDelete, "/api/v1/session", "https://other.example.invalid", f.csrf); got.Code != 403 || f.owner.revokeCalls != 0 {
		t.Fatal("foreign Origin reached the owner mutation")
	}
	f.owner.lostResponse = true
	if got := f.request(http.MethodDelete, "/api/v1/session", "https://app.example.invalid", f.csrf); got.Code != 503 || f.owner.effects != 1 || f.owner.revokeCalls != 1 {
		t.Fatal("lost owner response was not retained as an unknown closed intent")
	}
	if _, err := f.families.Read(t.Context(), f.family.ID); err == nil {
		t.Fatal("pending revoke left application family active")
	}
	for _, target := range []struct{ method, path string }{{http.MethodGet, "/api/v1/projects"}, {http.MethodGet, "/api/v1/realtime"}, {http.MethodPut, "/api/v1/session"}, {http.MethodPost, "/api/v1/session/ticket"}} {
		if got := f.request(target.method, target.path, "https://app.example.invalid", f.csrf); got.Code != 401 {
			t.Fatalf("pending revoke admitted %s %s", target.method, target.path)
		}
	}
	if f.owner.refreshCalls != 0 || f.applicationCalls != 1 {
		t.Fatal("pending revoke reached provider or application consumer")
	}
	if got := f.request(http.MethodDelete, "/api/v1/session", "https://app.example.invalid", f.csrf); got.Code != 204 || f.owner.effects != 1 || f.owner.revokeCalls != 2 {
		t.Fatal("same tombstone did not reconcile the lost owner response")
	}
	terminal, err := f.families.ReadForClosure(t.Context(), f.family.ID)
	if err != nil || !terminal.OwnerRevokeConfirmed || terminal.AccessToken != "" || terminal.RefreshToken != "" || f.revocations.revokedRecord != f.family.BrowserSessionID {
		t.Fatal("confirmed owner receipt failed to erase credentials or revoke the browser record")
	}
	if got := f.request(http.MethodDelete, "/api/v1/session", "https://app.example.invalid", f.csrf); got.Code != 204 || f.owner.revokeCalls != 2 || f.completedClosureCalls != 1 {
		t.Fatal("confirmed replay did more than local logout completion")
	}
	if got := f.request(http.MethodDelete, "/api/v1/session", "https://app.example.invalid", "wrong"); got.Code != 403 || f.owner.revokeCalls != 2 {
		t.Fatal("confirmed receipt bypassed CSRF")
	}
	if got := f.request(http.MethodGet, "/api/v1/projects", "https://app.example.invalid", ""); got.Code != 401 {
		t.Fatal("confirmed receipt reopened ordinary application access")
	}
}

// TestPlatformClosureRejectsExpiredCredentialAndModeConfusion proves neither expired identity nor a valid legacy-mode cookie enables the special logout path.
// TestPlatformClosureRejectsExpiredCredentialAndModeConfusion проверяет, что expired identity и действительная legacy-mode cookie не включают special logout path.
func TestPlatformClosureRejectsExpiredCredentialAndModeConfusion(t *testing.T) {
	f := newPlatformHTTPFixture(t)
	f.owner.principal.ExpiresAt = time.Now().Add(-time.Second)
	if got := f.request(http.MethodDelete, "/api/v1/session", "https://app.example.invalid", f.csrf); got.Code != 401 || f.owner.revokeCalls != 0 {
		t.Fatal("expired provider credential obtained self-revoke authority")
	}
	f = newPlatformHTTPFixture(t)
	legacy := f.owner.principal
	legacy.PlatformManagedOIDC = false
	legacy.PlatformSessionExpiresAt = time.Time{}
	_, cookie, csrf, err := f.security.IssueSession(legacy, "synthetic-access", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = cookie, csrf
	if got := f.request(http.MethodGet, "/api/v1/projects", "https://app.example.invalid", ""); got.Code != 401 || f.owner.normalCalls != 0 {
		t.Fatal("valid legacy cookie crossed authority modes before owner resolution")
	}
}
