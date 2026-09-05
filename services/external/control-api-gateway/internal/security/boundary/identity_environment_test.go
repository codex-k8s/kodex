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
		{"POST", "/api/v1/projects/prj_fixture01/runtime-secret-drafts"},
		{"POST", "/api/v1/runtime-secrets/sec_fixture01/drafts"},
		{"GET", "/api/v1/runtime-secret-drafts/sdft_fixture01"},
		{"POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/validate"},
		{"POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/publish"},
		{"POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/discard"},
		{"POST", "/api/v1/runtime-secret-drafts/sdft_fixture01/impact-plans"},
		{"GET", "/api/v1/runtime-secret-draft-impact-plans/sdip_fixture01"},
		{"PUT", "/api/v1/integration-connections/conn_fixture01/email-mailbox/credential"},
		{"GET", "/api/v1/prompt-templates/catalog"},
		{"GET", "/api/v1/assistant-conversations"},
		{"POST", "/api/v1/assistant-conversations/conv_fixture01/archive"},
		{"GET", "/api/v1/managed-configurations/mcfg_fixture01/revisions/mrev_fixture01/impact"},
		{"GET", "/api/v1/model-capabilities"},
		{"GET", "/api/v1/search"},
		{"GET", "/api/v1/vfs/nodes"},
		{"GET", "/api/v1/vfs/search"},
		{"POST", "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/saves"},
		{"POST", "/api/v1/prompt-template-configurations/mcfg_fixture01/revisions/mrev_fixture01/discard"},
		{"POST", "/api/v1/role-image-configurations/mcfg_fixture01/revisions/mrev_fixture01/saves"},
		{"POST", "/api/v1/role-image-configurations/mcfg_fixture01/revisions/mrev_fixture01/discard"},
		{"POST", "/api/v1/integration-definition-configurations/mcfg_fixture01/revisions/mrev_fixture01/saves"},
		{"POST", "/api/v1/integration-definition-configurations/mcfg_fixture01/revisions/mrev_fixture01/discard"},
		{"POST", "/api/v1/system-stt-configurations/mcfg_fixture01/revisions/mrev_fixture01/saves"},
		{"POST", "/api/v1/system-stt-configurations/mcfg_fixture01/revisions/mrev_fixture01/discard"},
		{"GET", "/api/v1/integration-invocations/inv_fixture01/email-effect-receipt"},
		{"POST", "/api/v1/email-effect-receipts/erc_fixture01/reconciliation"},
		{"GET", "/api/v1/integration-connections/conn_fixture01/interaction-identities"},
		{"GET", "/api/v1/skill-bundles"},
		{"GET", "/api/v1/skill-bundles/skl_fixture01"},
		{"GET", "/api/v1/skill-bundles/skl_fixture01/revisions"},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/archive"},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/restoration"},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/purge"},
		{"PUT", "/api/v1/agents/agt_fixture01/skill-bundles/skl_fixture01"},
		{"DELETE", "/api/v1/agents/agt_fixture01/skill-bundles/skl_fixture01"},
		{"GET", "/api/v1/memory-records"},
		{"GET", "/api/v1/memory-records/mem_fixture01"},
		{"GET", "/api/v1/memory-records/mem_fixture01/revisions"},
		{"POST", "/api/v1/memory-records/mem_fixture01/archive"},
		{"POST", "/api/v1/memory-records/mem_fixture01/restoration"},
		{"POST", "/api/v1/memory-records/mem_fixture01/purge"},
		{"PUT", "/api/v1/agents/agt_fixture01/memory-records/mem_fixture01"},
		{"DELETE", "/api/v1/agents/agt_fixture01/memory-records/mem_fixture01"},
		{"PUT", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01"},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01/validation"},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01/review"},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01/publication"},
		{"POST", "/api/v1/skill-bundles/skl_fixture01/revisions/rev_fixture01/discard"},
		{"POST", "/api/v1/memory-records/mem_fixture01/revisions"},
		{"POST", "/api/v1/system-stt-configurations/typed-drafts"},
		{"POST", "/api/v1/integration-connections/conn_fixture01/interaction-identities"},
		{"DELETE", "/api/v1/interaction-identities/iid_fixture01"},
		{"GET", "/api/v1/runtime-environments/env_fixture01/versions/ever_fixture02/impact"},
		{"POST", "/api/v1/runtime-environments/env_fixture01/versions/ever_fixture02/consumer-bindings"},
		{"GET", "/api/v1/runtime-secrets/sec_fixture01/revisions/2/impact"},
		{"POST", "/api/v1/runtime-secrets/sec_fixture01/revisions/2/consumer-bindings"},
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
					if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > 5*time.Second {
						t.Fatal("bounded request deadline was lost")
					}
					identity, ok := IdentityFromContext(r.Context())
					if !ok || identity.OrganizationID != claims.OrganizationID || identity.Subject != claims.Subject {
						t.Fatal("verified authority was lost")
					}
					if project, hasProject := ProjectReferenceFromContext(r.Context()); hasProject != strings.HasPrefix(route.path, "/api/v1/projects/") || hasProject && project != "prj_fixture01" {
						t.Fatal("route project context differs from its explicit scope")
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
