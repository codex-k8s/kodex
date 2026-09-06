package boundary

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/eventing/browserstate"
	oidcauth "github.com/codex-k8s/kodex/libs/go/oidcverifier"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/ratelimit"
	"github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/security/session"
)

type browserMemory struct {
	mu      sync.Mutex
	records map[string]browserstate.Record
}

func (s *browserMemory) Read(_ context.Context, id string) (browserstate.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.records[id]
	if !ok {
		return browserstate.Record{}, browserstate.ErrNotFound
	}
	v.Ciphertext = bytes.Clone(v.Ciphertext)
	return v, nil
}
func (s *browserMemory) CompareAndSwap(_ context.Context, id string, expected uint64, data []byte) (browserstate.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records[id].Sequence != expected {
		return browserstate.Record{}, browserstate.ErrConflict
	}
	v := browserstate.Record{Sequence: expected + 1, Ciphertext: bytes.Clone(data)}
	s.records[id] = v
	return v, nil
}

type browserVerifier struct {
	original, current oidcauth.Principal
	refreshCalls      int
	verified          string
}

func (v *browserVerifier) VerifyAuthorization(context.Context, string) (oidcauth.Principal, string, error) {
	return oidcauth.Principal{}, "", ErrUnauthenticated
}
func (v *browserVerifier) VerifyToken(_ context.Context, token string) (oidcauth.Principal, error) {
	v.verified = token
	p := v.original
	if token == "rotated" {
		p = v.current
	} else if token != "initial" {
		return oidcauth.Principal{}, ErrUnauthenticated
	}
	if !p.ExpiresAt.After(time.Now()) {
		return oidcauth.Principal{}, ErrUnauthenticated
	}
	return p, nil
}
func (v *browserVerifier) Refresh(context.Context, string) (oidcauth.BrowserTokens, error) {
	v.refreshCalls++
	v.current = v.original
	v.current.ExpiresAt = time.Now().Add(time.Hour)
	return oidcauth.BrowserTokens{AccessToken: "rotated", RefreshToken: "rotated-refresh", RefreshExpiresAt: time.Now().Add(8 * time.Hour), Principal: v.current}, nil
}

func TestBackendRenewAfterAccessExpiryRequiresCurrentCookieAndCSRF(t *testing.T) {
	t.Parallel()
	keyPath := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("44", 32)), 0400); err != nil {
		t.Fatal(err)
	}
	keys, err := session.New(session.Config{CurrentKeyFile: keyPath, TTL: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	verifier := &browserVerifier{original: oidcauth.Principal{Issuer: "https://identity.example.test", Subject: "710025af-02cf-4a7b-9f29-8479f757e42a",
		OrganizationID: "6832a593-1d97-4c50-a3d5-6f5b27bbf2c4", SessionID: "b2c2d1e9-cb78-43a0-b1df-299ce8a49d50", SessionRevision: 1,
		AuthenticatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Second)}}
	storage := &browserMemory{records: map[string]browserstate.Record{}}
	families, err := session.NewFamilies(t.Context(), storage, verifier, keys)
	if err != nil {
		t.Fatal(err)
	}
	family, csrf, err := families.CreateWithCSRF(t.Context(), oidcauth.BrowserTokens{AccessToken: "initial", RefreshToken: "refresh", Principal: verifier.original, RefreshExpiresAt: now.Add(8 * time.Hour)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	claims, encoded, err := families.Cookie(family)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := keys.Open(encoded)
	if err != nil || opened.Bearer != "" || opened.FamilyID != family.ID {
		t.Fatal("family cookie carries OIDC credentials")
	}
	limiter := ratelimit.New(ratelimit.Config{Window: time.Minute, Limit: 100, MaximumKeys: 100, PreAuthConcurrency: 4, GlobalHTTPConcurrency: 8, PerSubjectHTTPConcurrency: 2, GlobalWebSocketConcurrency: 4, PerSubjectWebSocketConcurrency: 1})
	security, err := New(Config{Families: families, Origins: []string{"https://app.example.test"}, Verifier: verifier, Sessions: keys, Revocations: &fakeRevocationStore{}, Limiter: limiter, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	handler := security.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		metadata, err := security.SessionMetadata(r.Context())
		if err != nil || !metadata.BackendRefresh || metadata.Version < 3 {
			t.Error("renewal metadata was not current")
		}
		w.WriteHeader(http.StatusOK)
	}))
	request := func(method, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://app.example.test/api/v1/session", nil)
		r.Header.Set("Origin", "https://app.example.test")
		r.Header.Set("X-CSRF-Token", token)
		r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: encoded})
		r.AddCookie(&http.Cookie{Name: CSRFCookieName, Value: csrf})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	<-time.After(time.Until(verifier.original.ExpiresAt) + time.Millisecond)
	if got := request(http.MethodGet, ""); got.Code != http.StatusUnauthorized || len(got.Result().Cookies()) != 0 {
		t.Fatal("GET renewed expired bearer")
	}
	if got := request(http.MethodPut, "wrong"); got.Code != http.StatusForbidden || verifier.refreshCalls != 0 {
		t.Fatal("CSRF failure invoked refresh")
	}
	got := request(http.MethodPut, csrf)
	if got.Code != http.StatusOK || verifier.refreshCalls != 1 || verifier.verified != "rotated" || len(got.Result().Cookies()) != 2 {
		t.Fatal("backend renewal did not verify and deliver fresh session")
	}
	if err := families.Revoke(t.Context(), family.ID); err != nil {
		t.Fatal(err)
	}
	if got := request(http.MethodPut, csrf); got.Code != http.StatusUnauthorized || verifier.refreshCalls != 1 {
		t.Fatal("logout permitted another refresh")
	}
	if !session.VerifyCSRF(claims, csrf) {
		t.Fatal(errors.New("fixture CSRF mismatch"))
	}
}
