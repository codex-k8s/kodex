package boundary

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
)

func TestConfigurationLifecycleRequiresFreshSessionAndCSRF(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("44", 32)), 0400); err != nil {
		t.Fatal(err)
	}
	keys, err := session.New(session.Config{CurrentKeyFile: keyPath, TTL: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	verifier := &browserVerifier{original: oidcauth.Principal{Issuer: "https://identity.example.test", Subject: "710025af-02cf-4a7b-9f29-8479f757e42a", OrganizationID: "6832a593-1d97-4c50-a3d5-6f5b27bbf2c4", SessionID: "b2c2d1e9-cb78-43a0-b1df-299ce8a49d50", SessionRevision: 1, AuthenticatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}}
	families, err := session.NewFamilies(t.Context(), &browserMemory{records: map[string]browserstate.Record{}}, verifier, keys)
	if err != nil {
		t.Fatal(err)
	}
	family, csrf, err := families.CreateWithCSRF(t.Context(), oidcauth.BrowserTokens{AccessToken: "initial", RefreshToken: "refresh", Principal: verifier.original, RefreshExpiresAt: now.Add(8 * time.Hour)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, cookie, err := families.Cookie(family)
	if err != nil {
		t.Fatal(err)
	}
	limiter := ratelimit.New(ratelimit.Config{Window: time.Minute, Limit: 100, MaximumKeys: 100, PreAuthConcurrency: 4, GlobalHTTPConcurrency: 8, PerSubjectHTTPConcurrency: 2, GlobalWebSocketConcurrency: 4, PerSubjectWebSocketConcurrency: 1})
	security, err := New(Config{Families: families, Origins: []string{"https://app.example.test"}, Verifier: verifier, Sessions: keys, Revocations: &fakeRevocationStore{}, Limiter: limiter, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	handler := security.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(204) }))
	paths := []string{"/api/v1/role-image-configurations/copies", "/api/v1/integration-definition-configurations/copies", "/api/v1/role-image-configurations/mcfg_fixture01/archive", "/api/v1/integration-definition-configurations/mcfg_fixture01/archive"}
	request := func(path, token, origin, encoded string) int {
		r := httptest.NewRequest("POST", "https://app.example.test"+path, nil)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", token)
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: encoded})
		r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrf})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	for _, path := range paths {
		before := calls
		if request(path, "wrong", "https://app.example.test", cookie) != 403 || request(path, csrf, "https://foreign.example.test", cookie) != 403 || request(path, csrf, "https://app.example.test", "") != 401 || calls != before {
			t.Fatal("untrusted mutation passed boundary")
		}
		if request(path, csrf, "https://app.example.test", cookie) != 204 || calls != before+1 {
			t.Fatal("fresh session rejected")
		}
	}
	if err := families.Revoke(t.Context(), family.ID); err != nil {
		t.Fatal(err)
	}
	before := calls
	for _, path := range paths {
		if request(path, csrf, "https://app.example.test", cookie) != 401 {
			t.Fatal("revoked session accepted")
		}
	}
	if calls != before || verifier.refreshCalls != 0 {
		t.Fatal("mutation refreshed or crossed revoked boundary")
	}
}
