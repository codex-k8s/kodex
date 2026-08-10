package oidc

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"
)

func TestFileVerifierRejectsChangedTrustAndClaims(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	issuer, audience, keyID := "https://sso.mattercodex.local", "mattercodex-integration-gateway", "provider-key-1"
	path, digest := writeProviderSnapshot(t, issuer, audience, keyID, &privateKey.PublicKey)
	config := FileConfig{Issuer: issuer, Audience: audience, File: path, ExpectedSHA256: digest, ExpectedGeneration: 7}
	verifier, err := NewFile(config)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	base := map[string]any{
		"iss": issuer, "aud": audience, "sub": uuid.NewString(), "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"organization_id": uuid.NewString(), "project_id": uuid.NewString(), "permissions": []string{"integration.approval.decide"},
	}
	valid := signToken(t, privateKey, jose.RS256, keyID, base)
	if _, err := verifier.Verify(t.Context(), "Bearer "+valid); err != nil {
		t.Fatalf("valid provider snapshot token rejected: %v", err)
	}
	mutations := []struct {
		name string
		alg  jose.SignatureAlgorithm
		kid  string
		set  func(map[string]any)
	}{
		{name: "issuer", alg: jose.RS256, kid: keyID, set: func(claims map[string]any) { claims["iss"] = "https://other.example" }},
		{name: "audience", alg: jose.RS256, kid: keyID, set: func(claims map[string]any) { claims["aud"] = "other-audience" }},
		{name: "algorithm", alg: jose.RS512, kid: keyID, set: func(map[string]any) {}},
		{name: "kid", alg: jose.RS256, kid: "unknown-key", set: func(map[string]any) {}},
		{name: "expiry", alg: jose.RS256, kid: keyID, set: func(claims map[string]any) { claims["exp"] = now.Add(-time.Minute).Unix() }},
	}
	for _, mutation := range mutations {
		claims := make(map[string]any, len(base))
		for key, value := range base {
			claims[key] = value
		}
		mutation.set(claims)
		raw := signToken(t, privateKey, mutation.alg, mutation.kid, claims)
		if _, err := verifier.Verify(t.Context(), "Bearer "+raw); err == nil {
			t.Fatalf("%s mismatch was accepted", mutation.name)
		}
	}
	rollback := config
	rollback.ExpectedSHA256 = strings.Repeat("0", 64)
	if _, err := NewFile(rollback); err == nil {
		t.Fatal("JWKS digest rollback was accepted")
	}
	wrongIssuer := config
	wrongIssuer.Issuer = "https://other.example"
	if _, err := NewFile(wrongIssuer); err == nil {
		t.Fatal("snapshot issuer mismatch was accepted")
	}
	wrongAudience := config
	wrongAudience.Audience = "other-audience"
	if _, err := NewFile(wrongAudience); err == nil {
		t.Fatal("snapshot audience mismatch was accepted")
	}
}

func writeProviderSnapshot(t *testing.T, issuer, audience, keyID string, key *rsa.PublicKey) (string, string) {
	t.Helper()
	keys := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: key, KeyID: keyID, Algorithm: string(jose.RS256), Use: "sig",
	}}}
	canonicalJWKS, err := json.Marshal(keys)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := providerSnapshot{
		SchemaVersion: 1, Generation: 7, Issuer: issuer, Audience: audience,
		Algorithms: []string{string(jose.RS256)}, JWKS: canonicalJWKS,
	}
	digestRaw, err := json.Marshal(snapshotDigestInput{
		SchemaVersion: snapshot.SchemaVersion, Generation: snapshot.Generation, Issuer: snapshot.Issuer,
		Audience: snapshot.Audience, Algorithms: snapshot.Algorithms, JWKS: canonicalJWKS,
	})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(digestRaw)
	snapshot.DigestSHA256 = hex.EncodeToString(sum[:])
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "provider-snapshot.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, snapshot.DigestSHA256
}

func signToken(t *testing.T, key *rsa.PrivateKey, algorithm jose.SignatureAlgorithm, keyID string, claims map[string]any) string {
	t.Helper()
	options := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", keyID)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: algorithm, Key: key}, options)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := signed.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
