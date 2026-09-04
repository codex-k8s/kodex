package boundary

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
)

func TestIdentityEnvironmentRoutesRequireSessionAndCSRF(t *testing.T) {
	for _, route := range []struct{ method, path string }{
		{"GET", "/api/v1/integration-connections/conn_fixture01/interaction-identities"},
		{"POST", "/api/v1/integration-connections/conn_fixture01/interaction-identities"},
		{"DELETE", "/api/v1/interaction-identities/iid_fixture01"},
		{"GET", "/api/v1/runtime-environments/env_fixture01/versions/ever_fixture02/impact"},
		{"POST", "/api/v1/runtime-environments/env_fixture01/versions/ever_fixture02/consumer-bindings"},
	} {
		for _, scenario := range []string{"valid", "no-session", "wrong-organization", "revoked", "no-csrf"} {
			t.Run(route.method+route.path+scenario, func(t *testing.T) {
				csrf := strings.Repeat("c", 43)
				digest := sha256.Sum256([]byte(csrf))
				claims := session.Claims{Subject: "actor-fixture", OrganizationID: "org-fixture", SessionID: "session-fixture", OIDCSessionID: "oidc-fixture", SessionRevision: 1,
					Bearer: "fixture-bearer", CSRFHash: hex.EncodeToString(digest[:]), ExpiresAt: time.Now().Add(time.Hour).Unix()}
				principal := oidcauth.Principal{Subject: claims.Subject, OrganizationID: claims.OrganizationID, SessionID: claims.OIDCSessionID, SessionRevision: claims.SessionRevision, ExpiresAt: time.Now().Add(time.Hour)}
				if scenario == "wrong-organization" {
					principal.OrganizationID = "org-other"
				}
				security := testBoundaryWithRevocations(t, &fakeOIDCVerifier{principal: principal}, &fakeSessionStore{claims: claims}, &fakeRevocationStore{revoked: scenario == "revoked"})
				r := authenticatedRequest(route.method, csrf)
				r.URL.Path = route.path
				if scenario == "no-session" {
					r.Header.Del("Cookie")
				}
				if scenario == "no-csrf" {
					r.Header.Del("X-CSRF-Token")
				}
				called := false
				w := httptest.NewRecorder()
				security.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					identity, ok := IdentityFromContext(r.Context())
					if !ok || identity.OrganizationID != claims.OrganizationID || identity.Subject != claims.Subject {
						t.Fatal("verified authority was lost")
					}
					if _, hasProject := ProjectReferenceFromContext(r.Context()); hasProject {
						t.Fatal("organization route acquired a project")
					}
					w.WriteHeader(http.StatusNoContent)
				})).ServeHTTP(w, r)
				want := http.StatusNoContent
				if scenario == "no-session" || scenario == "wrong-organization" || scenario == "revoked" {
					want = http.StatusUnauthorized
				}
				if scenario == "no-csrf" && route.method != http.MethodGet {
					want = http.StatusForbidden
				}
				if w.Code != want || called != (want == http.StatusNoContent) {
					t.Fatalf("status=%d want=%d called=%t", w.Code, want, called)
				}
			})
		}
	}
}
