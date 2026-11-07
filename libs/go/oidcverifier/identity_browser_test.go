package oidcverifier

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestIdentityBrowserExchangeKeepsNonceAndTokenBindings checks the real signed standard-token browser path without application claims or fabricated Principal.
// It observes one request per exchange, disabled HTTP body replay, refresh evidence and mismatching ID-token rejection.
// TestIdentityBrowserExchangeKeepsNonceAndTokenBindings проверяет подписанный standard-token browser path без application claims и фиктивного Principal.
// Проверяет один request на exchange, отключённый body replay, refresh evidence и отказ при несовпадении ID token.
func TestIdentityBrowserExchangeKeepsNonceAndTokenBindings(t *testing.T) {
	verifier, sign := standardIdentityFixture(t)
	base := standardIdentityClaims()
	access := sign(base)
	nonce := strings.Repeat("n", 43)
	var responseID atomic.Value
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.Form.Get("client_id") != "synthetic-console" {
			t.Error("token endpoint lost its bounded POST/client binding")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") == "authorization_code" && (r.Form.Get("code_verifier") != strings.Repeat("v", 43) || r.Form.Get("redirect_uri") != "https://app.example.invalid/auth/callback") {
			t.Error("standard exchange lost PKCE or redirect binding")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": access, "id_token": responseID.Load().(string),
			"refresh_token": "synthetic-rotated-refresh", "token_type": "Bearer", "expires_in": 3600, "refresh_expires_in": 7200})
	}))
	t.Cleanup(server.Close)
	client, err := verifier.BrowserClientConfigured(BrowserClientConfig{ClientID: "synthetic-console", RedirectURI: "https://app.example.invalid/auth/callback", Scope: "openid profile"})
	if err != nil {
		t.Fatal(err)
	}
	client.tokenURL = server.URL
	transport := server.Client().Transport
	verifier.client = &http.Client{Transport: refreshTransport(func(r *http.Request) (*http.Response, error) {
		if r.GetBody != nil {
			t.Error("code/refresh HTTP body replay is enabled")
		}
		return transport.RoundTrip(r)
	})}
	idClaims := make(map[string]any)
	for name, value := range base {
		idClaims[name] = value
	}
	idClaims["aud"], idClaims["nonce"] = "synthetic-console", nonce
	responseID.Store(sign(idClaims))
	tokens, err := client.ExchangeIdentityCode(t.Context(), "synthetic-once-code", strings.Repeat("v", 43), nonce)
	if err != nil || tokens.Identity.Subject == "" || tokens.AccessToken != access || calls.Load() != 1 {
		t.Fatalf("signed standard exchange failed: %v", err)
	}
	refreshed, err := client.RefreshIdentity(t.Context(), "synthetic-previous-refresh")
	if err != nil || refreshed.Identity.Subject != tokens.Identity.Subject || refreshed.Identity.SessionID != tokens.Identity.SessionID || calls.Load() != 2 {
		t.Fatalf("standard refresh evidence changed or request repeated: %v", err)
	}
	for _, field := range []string{"nonce", "sub", "sid", "auth_time", "aud", "iss"} {
		t.Run(field, func(t *testing.T) {
			changed := make(map[string]any)
			for name, value := range idClaims {
				changed[name] = value
			}
			switch field {
			case "nonce":
				changed[field] = strings.Repeat("x", 43)
			case "sub", "sid":
				changed[field] = "other-synthetic-identity"
			case "auth_time":
				changed[field] = base[field].(int64) - 1
			case "aud":
				changed[field] = "other-console"
			case "iss":
				changed[field] = "https://other.example.invalid"
			}
			responseID.Store(sign(changed))
			before := calls.Load()
			result, err := client.ExchangeIdentityCode(t.Context(), "synthetic-next-code", strings.Repeat("v", 43), nonce)
			if !errors.Is(err, ErrExchangeUncertain) || result.Identity.Subject != "" || calls.Load() != before+1 {
				t.Fatal("mismatching token identity escaped or exchange was repeated")
			}
		})
	}
}
