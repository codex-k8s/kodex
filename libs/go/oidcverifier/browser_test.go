package oidcverifier

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func TestBrowserCodeExchangeVerifiesBothTokensAndNonce(t *testing.T) {
	t.Parallel()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const issuer = "https://identity.example.test/realms/kodex"
	const clientID = "kodex-control-center"
	nonce := strings.Repeat("n", 43)
	authenticatedAt := time.Now().Add(-time.Minute).Unix()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "test"))
	if err != nil {
		t.Fatal(err)
	}
	sign := func(audience, nonce string) string {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"iss": issuer, "sub": "test-user-opaque-identifier", "aud": audience,
			"sid": "test-session-opaque-identifier", "organization_id": "6446ef93-3448-4e44-8da0-e6e1a18f858d", "session_revision": 1,
			"jti": "test-token-opaque-identifier", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"auth_time": authenticatedAt, "nonce": nonce})
		if err != nil {
			t.Fatal(err)
		}
		object, err := signer.Sign(raw)
		if err != nil {
			t.Fatal(err)
		}
		token, err := object.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	access, id := sign("kodex-control-api", ""), sign(clientID, nonce)
	keys, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test", Use: "sig", Algorithm: "RS256"}}})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/keys" {
			_, _ = w.Write(keys)
			return
		}
		calls.Add(1)
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("client_id") != clientID {
			t.Error("unexpected token request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") == "authorization_code" && (r.Form.Get("code_verifier") != strings.Repeat("v", 43) || r.Form.Get("redirect_uri") != "https://app.example.test/auth/callback") {
			t.Error("code exchange lost PKCE or redirect binding")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "id_token": id, "refresh_token": "rotated-refresh", "token_type": "Bearer", "expires_in": 3600, "refresh_expires_in": 7200})
	}))
	defer server.Close()
	set := newBoundedKeySet(server.Client(), server.URL+"/keys")
	if err := set.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	verifier := &Verifier{client: server.Client(), keys: set, issuer: issuer,
		verifier: coreoidc.NewVerifier(issuer, set, &coreoidc.Config{ClientID: "kodex-control-api", SupportedSigningAlgs: []string{"RS256"}})}
	client, err := verifier.BrowserClient(clientID, "https://app.example.test/auth/callback")
	if err != nil {
		t.Fatal(err)
	}
	client.tokenURL = server.URL + "/token"
	tokens, err := client.ExchangeCode(t.Context(), "one-time-code", strings.Repeat("v", 43), nonce)
	if err != nil || tokens.AccessToken != access || tokens.RefreshToken != "rotated-refresh" || tokens.Principal.SessionRevision != 1 {
		t.Fatal("valid code exchange failed")
	}
	if _, err := client.ExchangeCode(t.Context(), "other-code", strings.Repeat("v", 43), strings.Repeat("x", 43)); !errors.Is(err, ErrExchangeUncertain) {
		t.Fatal("wrong nonce accepted")
	}
	refreshed, err := client.Refresh(t.Context(), "previous-refresh")
	if err != nil || refreshed.Principal.SessionID != tokens.Principal.SessionID {
		t.Fatal("refresh lost verified identity")
	}
	if calls.Load() != 3 {
		t.Fatal("token request was repeated")
	}
	authorization, err := client.AuthorizationURL(strings.Repeat("s", 43), nonce, strings.Repeat("c", 43), false)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(authorization)
	if err != nil || parsed.Query().Get("scope") != "openid kodex.owner" || parsed.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("authorization changed existing scope or PKCE")
	}
}

func TestBrowserExchangeNeverRetriesProviderFailure(t *testing.T) {
	t.Parallel()
	for _, status := range []int{400, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			}))
			defer server.Close()
			client := &BrowserClient{verifier: &Verifier{client: server.Client()}, clientID: "test", tokenURL: server.URL}
			_, err := client.Refresh(t.Context(), "refresh")
			want := ErrExchangeUncertain
			if status == 400 {
				want = ErrGrantRejected
			}
			if !errors.Is(err, want) || calls.Load() != 1 {
				t.Fatal("provider failure was retried or misclassified")
			}
		})
	}
}
