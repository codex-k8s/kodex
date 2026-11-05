package oidcverifier

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

// standardIdentityFixture supplies an isolated RS256/JWKS verifier and signer with no provider credentials or application mapping.
// Its loopback HTTP endpoint belongs only to the unit fixture and does not change the production HTTPS transport.
// standardIdentityFixture создаёт изолированные RS256/JWKS verifier и signer без provider credentials и прикладной mapping.
// Loopback HTTP endpoint принадлежит только unit fixture и не меняет production HTTPS transport.
func standardIdentityFixture(t *testing.T) (*Verifier, func(map[string]any) string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "synthetic-standard"))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "synthetic-standard", Use: "sig", Algorithm: "RS256"}}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(keys)
	}))
	t.Cleanup(server.Close)
	set := newBoundedKeySet(server.Client(), server.URL)
	if err := set.Refresh(t.Context()); err != nil {
		t.Fatal(err)
	}
	const issuer = "https://identity.example.invalid/realm/synthetic"
	verifier := &Verifier{client: server.Client(), keys: set, issuer: issuer,
		verifier: coreoidc.NewVerifier(issuer, set, &coreoidc.Config{ClientID: "synthetic-api", SupportedSigningAlgs: []string{"RS256"}})}
	sign := func(values map[string]any) string {
		t.Helper()
		raw, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		object, err := signer.Sign(raw)
		if err != nil {
			t.Fatal(err)
		}
		value, err := object.CompactSerialize()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	return verifier, sign
}

// standardIdentityClaims supplies only synthetic standard identity/session evidence, with no tenant, revision or owner fields.
// standardIdentityClaims задаёт лишь synthetic standard identity/session evidence без tenant, revision и owner fields.
func standardIdentityClaims() map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": "https://identity.example.invalid/realm/synthetic", "sub": "synthetic-standard-user", "aud": "synthetic-api",
		"sid": "synthetic-standard-session", "jti": "synthetic-standard-token", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"auth_time": now.Add(-time.Minute).Unix(), "name": "Synthetic User", "groups": []string{"synthetic-readers"},
	}
}

// TestVerifyIdentityAcceptsStandardClaimsWithoutGrantingAuthority verifies genuine signed evidence while the legacy Principal path stays closed.
// TestVerifyIdentityAcceptsStandardClaimsWithoutGrantingAuthority проверяет подписанные standard evidence при закрытом legacy Principal path.
func TestVerifyIdentityAcceptsStandardClaimsWithoutGrantingAuthority(t *testing.T) {
	verifier, sign := standardIdentityFixture(t)
	raw := sign(standardIdentityClaims())
	identity, err := verifier.VerifyIdentity(t.Context(), raw)
	if err != nil || identity.Subject == "" || identity.SessionID == "" || identity.TokenID == "" ||
		identity.Issuer != verifier.issuer || identity.IssuedAt.IsZero() || len(identity.Groups) != 1 {
		t.Fatalf("standard evidence was not verified: %v", err)
	}
	if principal, err := verifier.VerifyToken(t.Context(), raw); err == nil || principal.OrganizationID != "" || principal.SessionRevision != 0 || principal.OwnerClaim {
		t.Fatal("standard identity silently became a legacy application principal")
	}
}

// TestVerifyIdentityIgnoresInjectedApplicationAuthority proves custom tenant/revision/owner shapes cannot influence standard identity output.
// TestVerifyIdentityIgnoresInjectedApplicationAuthority доказывает независимость standard identity от внедрённых tenant/revision/owner shapes.
func TestVerifyIdentityIgnoresInjectedApplicationAuthority(t *testing.T) {
	verifier, sign := standardIdentityFixture(t)
	values := standardIdentityClaims()
	values["organization_id"], values["session_revision"] = map[string]any{"untrusted": true}, "untrusted"
	values["scope"], values["realm_access"] = []string{"kodex.owner"}, map[string]any{"roles": "kodex-owner"}
	identity, err := verifier.VerifyIdentity(t.Context(), sign(values))
	if err != nil || identity.Subject == "" || identity.Issuer != verifier.issuer {
		t.Fatalf("custom authority contaminated standard identity verification: %v", err)
	}
	if _, err := verifier.VerifyToken(t.Context(), sign(values)); err == nil {
		t.Fatal("malformed legacy authority was accepted")
	}
}

// TestVerifyIdentityRetainsCryptographicAndSessionChecks rejects wrong trust, expiry, missing issuance/session evidence and damaged signatures.
// TestVerifyIdentityRetainsCryptographicAndSessionChecks отклоняет неверные trust/expiry, пропавшие issuance/session evidence и повреждённую подпись.
func TestVerifyIdentityRetainsCryptographicAndSessionChecks(t *testing.T) {
	verifier, sign := standardIdentityFixture(t)
	for _, scenario := range []string{"issuer", "audience", "expiry", "issued-at", "future-issued-at", "session", "token-id", "groups", "signature"} {
		t.Run(scenario, func(t *testing.T) {
			values := standardIdentityClaims()
			switch scenario {
			case "issuer":
				values["iss"] = "https://other.example.invalid"
			case "audience":
				values["aud"] = "other-api"
			case "expiry":
				values["exp"] = time.Now().Add(-time.Hour).Unix()
			case "issued-at":
				delete(values, "iat")
			case "future-issued-at":
				values["iat"] = time.Now().Add(5 * time.Minute).Unix()
			case "session":
				delete(values, "sid")
			case "token-id":
				delete(values, "jti")
			case "groups":
				values["groups"] = []string{"invalid\ngroup"}
			}
			raw := sign(values)
			if scenario == "signature" {
				parts := strings.Split(raw, ".")
				replacement := "A"
				if parts[2][0] == 'A' {
					replacement = "B"
				}
				parts[2] = replacement + parts[2][1:]
				raw = strings.Join(parts, ".")
			}
			if identity, err := verifier.VerifyIdentity(t.Context(), raw); err == nil || identity.Subject != "" {
				t.Fatal("invalid standard evidence escaped as verified identity")
			}
		})
	}
}

// TestVerifyTokenPreservesExplicitLegacyAuthority keeps the existing tenant/revision and combined owner scope/role behavior unchanged.
// TestVerifyTokenPreservesExplicitLegacyAuthority сохраняет прежние tenant/revision и совместную проверку owner scope/role.
func TestVerifyTokenPreservesExplicitLegacyAuthority(t *testing.T) {
	verifier, sign := standardIdentityFixture(t)
	values := standardIdentityClaims()
	values["organization_id"], values["session_revision"] = "20000000-0000-4000-8000-000000000002", 7
	values["scope"], values["realm_access"] = "openid kodex.owner", map[string]any{"roles": []string{"kodex-owner"}}
	principal, err := verifier.VerifyToken(t.Context(), sign(values))
	if err != nil || principal.OrganizationID != values["organization_id"] || principal.SessionRevision != 7 || !principal.OwnerClaim {
		t.Fatalf("legacy authority regression: %v", err)
	}
}

// TestVerifyIdentitySupportsShortCaseSensitiveStandardSubjects accepts normal bounded OIDC identifiers without applying legacy UUID semantics.
// TestVerifyIdentitySupportsShortCaseSensitiveStandardSubjects допускает обычные bounded OIDC identifiers без legacy UUID semantics.
func TestVerifyIdentitySupportsShortCaseSensitiveStandardSubjects(t *testing.T) {
	verifier, sign := standardIdentityFixture(t)
	var prior string
	for _, subject := range []string{"alice", "Alice"} {
		values := standardIdentityClaims()
		values["sub"] = subject
		identity, err := verifier.VerifyIdentity(t.Context(), sign(values))
		if err != nil || identity.Subject == "" || identity.Subject == prior {
			t.Fatalf("standard subject lost its exact identity: %v", err)
		}
		prior = identity.Subject
	}
}
