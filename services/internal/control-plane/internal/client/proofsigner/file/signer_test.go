package file

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

func TestNewAcceptsRotatedCurrentProofSigner(t *testing.T) {
	now := time.Now().UTC()
	previous := mustProofKey(t, "proof-g1")
	current := mustProofKey(t, "proof-g2")
	next := mustProofKey(t, "proof-g3")
	audience := "urn:mattercodex:internal-rpc-authority-issuer:test"
	issuer := "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane"

	directory := t.TempDir()
	privatePath := filepath.Join(directory, "private.jwk")
	privateRaw, err := internalrpcauth.MarshalPrivateJWK(current)
	if err != nil {
		t.Fatalf("marshal current private key: %v", err)
	}
	if err := os.WriteFile(privatePath, privateRaw, 0o600); err != nil {
		t.Fatalf("write current private key: %v", err)
	}

	trust := trustDocument{
		Version:        1,
		Purpose:        "AUTHORITY_PROOF_VERIFICATION",
		SourceRevision: 2,
		SourceDigest:   strings.Repeat("a", 64),
		Predecessor: revisionDigest{
			Revision: 1, Digest: strings.Repeat("b", 64),
		},
		Keys: []trustKey{
			proofTrustKey(t, previous, issuer, audience, 1, "PREVIOUS", now),
			proofTrustKey(t, current, issuer, audience, 2, "CURRENT", now),
			proofTrustKey(t, next, issuer, audience, 3, "NEXT", now),
		},
	}
	trustRaw, err := json.Marshal(trust)
	if err != nil {
		t.Fatalf("marshal proof trust: %v", err)
	}
	trustPath := filepath.Join(directory, "jwks.json")
	if err := os.WriteFile(trustPath, trustRaw, 0o600); err != nil {
		t.Fatalf("write proof trust: %v", err)
	}

	signer, err := New(Config{
		PrivateJWKFile: privatePath,
		TrustFile:      trustPath,
		Issuer:         issuer,
		Audience:       audience,
	})
	if err != nil {
		t.Fatalf("load rotated current proof signer: %v", err)
	}
	state, err := signer.Check(t.Context())
	if err != nil {
		t.Fatalf("check rotated current proof signer: %v", err)
	}
	if state.SignerGeneration != 2 {
		t.Fatalf("signer generation = %d, want 2", state.SignerGeneration)
	}
}

func proofTrustKey(
	t *testing.T,
	key internalrpcauth.ES256Key,
	issuer string,
	audience string,
	generation uint64,
	status string,
	now time.Time,
) trustKey {
	t.Helper()
	publicRaw, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatalf("marshal proof public key: %v", err)
	}
	return trustKey{
		Issuer: issuer, Generation: generation, Status: status,
		Purpose: "AUTHORITY_PROOF", Audiences: []string{audience},
		NotBefore: now.Add(-time.Minute).Unix(),
		NotAfter:  now.Add(time.Hour).Unix(),
		JWK:       json.RawMessage(publicRaw),
	}
}

func mustProofKey(t *testing.T, keyID string) internalrpcauth.ES256Key {
	t.Helper()
	key, err := internalrpcauth.GenerateES256Key(keyID)
	if err != nil {
		t.Fatalf("generate proof key: %v", err)
	}
	return key
}
