package integrationgatewayauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

func TestContinuationGrantRoundTripAndDeterministicTamperRejection(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key := testKey(t, 3)
	privateFile := writePrivate(t, key)
	keysetFile := writeKeyset(t, PublicKeySet{
		Version: 1, Revision: 3, HighWatermark: 3, ServedGeneration: 3,
		Keys: []PublicKeyRef{{Generation: 3, Status: keyStatusCurrent, JWK: publicRaw(t, key)}},
	})
	config := testConfig(3)
	signer, err := NewSigner(config, privateFile, keysetFile)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	verifier, err := NewVerifier(config, keysetFile)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	verifier.now = func() time.Time { return now }
	claims := testClaims(config, now)
	compact, err := signer.Sign(context.Background(), claims)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	verified, err := verifier.Verify(context.Background(), compact)
	if err != nil || verified.InvocationID != claims.InvocationID || verified.ContinuationID != claims.ContinuationID {
		t.Fatalf("Verify() = %#v, %v", verified, err)
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		t.Fatal("compact JWS has invalid shape")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) == 0 {
		t.Fatal("compact JWS signature is invalid")
	}
	signature[0] ^= 0x01
	tampered := parts[0] + "." + parts[1] + "." + base64.RawURLEncoding.EncodeToString(signature)
	if tampered == compact {
		t.Fatal("tamper helper did not change signed bytes")
	}
	if _, err := verifier.Verify(context.Background(), tampered); err == nil {
		t.Fatal("tampered grant was accepted")
	}
	verifier.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := verifier.Verify(context.Background(), compact); err == nil {
		t.Fatal("expired grant was accepted")
	}
}

func TestVerifierRotationClosedStates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	previous, current, next, retired := testKey(t, 2), testKey(t, 3), testKey(t, 4), testKey(t, 1)
	keyset := writeKeyset(t, PublicKeySet{
		Version: 1, Revision: 3, HighWatermark: 3, ServedGeneration: 3,
		Keys: []PublicKeyRef{
			{Generation: 1, Status: keyStatusRetired, JWK: publicRaw(t, retired)},
			{Generation: 2, Status: keyStatusPrevious, AcceptUntil: now.Add(time.Hour).Unix(), JWK: publicRaw(t, previous)},
			{Generation: 3, Status: keyStatusCurrent, JWK: publicRaw(t, current)},
			{Generation: 4, Status: keyStatusNext, JWK: publicRaw(t, next)},
		},
	})
	verifier, err := NewVerifier(testConfig(3), keyset)
	if err != nil {
		t.Fatal(err)
	}
	verifier.now = func() time.Time { return now }
	previousToken := signForGeneration(t, previous, 2, now)
	if _, err := verifier.Verify(context.Background(), previousToken); err != nil {
		t.Fatalf("PREVIOUS overlap token rejected: %v", err)
	}
	for name, token := range map[string]string{
		"retired": signForGeneration(t, retired, 1, now),
		"next":    signForGeneration(t, next, 4, now),
		"unknown": signForGeneration(t, testKey(t, 7), 7, now),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifier.Verify(context.Background(), token); err == nil {
				t.Fatal("closed rotation state was accepted")
			}
		})
	}
	verifier.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := verifier.Verify(context.Background(), previousToken); err == nil {
		t.Fatal("expired PREVIOUS overlap was accepted")
	}
}

func TestVerifierRejectsRollbackSnapshot(t *testing.T) {
	key := testKey(t, 2)
	rollback := writeKeyset(t, PublicKeySet{
		Version: 1, Revision: 2, HighWatermark: 2, ServedGeneration: 2,
		Keys: []PublicKeyRef{{Generation: 2, Status: keyStatusCurrent, JWK: publicRaw(t, key)}},
	})
	if _, err := NewVerifier(testConfig(3), rollback); err == nil {
		t.Fatal("rollback keyset was accepted for configured generation")
	}
}

func signForGeneration(t *testing.T, key internalrpcauth.ES256Key, generation uint64, now time.Time) string {
	t.Helper()
	keyset := writeKeyset(t, PublicKeySet{
		Version: 1, Revision: generation, HighWatermark: generation, ServedGeneration: generation,
		Keys: []PublicKeyRef{{Generation: generation, Status: keyStatusCurrent, JWK: publicRaw(t, key)}},
	})
	signer, err := NewSigner(testConfig(generation), writePrivate(t, key), keyset)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(context.Background(), testClaims(testConfig(generation), now))
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func testConfig(generation uint64) Config {
	return Config{
		Issuer:   "https://control-plane.test/authority/integration-continuation",
		Audience: "urn:mattercodex:integration-continuation", WorkloadID: "integration-gateway",
		CallerSPIFFEID: "spiffe://mattercodex.test/ns/system/sa/integration-gateway",
		Generation:     generation, MaximumTTL: 8 * 24 * time.Hour,
	}
}

func testClaims(config Config, now time.Time) Claims {
	return Claims{
		Version: 1, Issuer: config.Issuer, Audience: config.Audience,
		Purpose: PurposeTransition, Subject: "actor-1", OrganizationID: "tenant-1", ProjectID: "project-1",
		WorkloadID: config.WorkloadID, CallerSPIFFEID: config.CallerSPIFFEID,
		SessionID: "session-1", TurnID: "turn-2", Attempt: 2, InputSHA256: digest64("a"),
		RuntimeRevisionID: "revision-2", RuntimeRevisionVersion: 2, RuntimeRevisionSHA256: digest64("b"),
		GrantGeneration: 7, ContinuationID: "continuation-1", ContinuationVersion: 4,
		ContinuationFence: 4, InvocationID: "invocation-1",
		AllowedOperationIDs: []string{"control.integration-execution.complete"},
		JTI:                 "grant-1", IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
	}
}

func testKey(t *testing.T, generation uint64) internalrpcauth.ES256Key {
	t.Helper()
	key, err := internalrpcauth.GenerateES256Key("integration-continuation-g" + string(rune('0'+generation)))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func publicRaw(t *testing.T, key internalrpcauth.ES256Key) json.RawMessage {
	t.Helper()
	raw, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writePrivate(t *testing.T, key internalrpcauth.ES256Key) string {
	t.Helper()
	raw, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		t.Fatal(err)
	}
	return writeTestFile(t, "private.jwk", raw)
}

func writeKeyset(t *testing.T, keyset PublicKeySet) string {
	t.Helper()
	raw, err := internalrpcauth.CanonicalJSON(keyset)
	if err != nil {
		t.Fatal(err)
	}
	return writeTestFile(t, "public-keyset.json", raw)
}

func writeTestFile(t *testing.T, name string, raw []byte) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	if os.WriteFile(file, raw, 0o600) != nil {
		t.Fatal("write test file")
	}
	return file
}

func digest64(symbol string) string {
	value := ""
	for range 64 {
		value += symbol
	}
	return value
}
