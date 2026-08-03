package integrationgatewayauth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
)

func TestResultGrantRoundTripAndTamperRejection(t *testing.T) {
	privateFile, publicFile := testJWKFiles(t)
	config := Config{
		Issuer:   "https://control-plane.test/authority/integration-continuation",
		Audience: "urn:mattercodex:integration-result-access", WorkloadID: "agent-runner",
		CallerSPIFFEID: "spiffe://mattercodex.test/ns/system/sa/agent-runner",
		Generation:     3, MaximumTTL: 8 * 24 * time.Hour,
	}
	signer, err := NewSigner(config, privateFile, publicFile)
	if err != nil {
		t.Fatalf("NewSigner() error = %v", err)
	}
	verifier, err := NewVerifier(config, publicFile)
	if err != nil {
		t.Fatalf("NewVerifier() error = %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	verifier.now = func() time.Time { return now }
	claims := Claims{
		Version: 1, Issuer: config.Issuer, Audience: config.Audience,
		Purpose: PurposeResultAccess, Subject: "actor-1", OrganizationID: "tenant-1", ProjectID: "project-1",
		WorkloadID: config.WorkloadID, CallerSPIFFEID: config.CallerSPIFFEID,
		SessionID: "session-1", TurnID: "turn-2", Attempt: 2, InputSHA256: digest64("a"),
		RuntimeRevisionID: "revision-2", RuntimeRevisionVersion: 2, RuntimeRevisionSHA256: digest64("b"),
		GrantGeneration: 7, ContinuationID: "continuation-1", ContinuationVersion: 4,
		ContinuationFence: 4, InvocationID: "invocation-1", ResultAttemptID: "attempt-1",
		ResultSHA256: digest64("c"), AllowedOperationIDs: []string{"integration.result.resolve"},
		JTI: "grant-1", IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: now.Add(time.Hour).Unix(),
	}
	compact, err := signer.Sign(context.Background(), claims)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	verified, err := verifier.Verify(context.Background(), compact)
	if err != nil || verified.InvocationID != claims.InvocationID || verified.ResultSHA256 != claims.ResultSHA256 {
		t.Fatalf("Verify() = %#v, %v", verified, err)
	}
	tampered := compact[:len(compact)-1] + "A"
	if _, err := verifier.Verify(context.Background(), tampered); err == nil {
		t.Fatal("tampered grant was accepted")
	}
	verifier.now = func() time.Time { return now.Add(2 * time.Hour) }
	if _, err := verifier.Verify(context.Background(), compact); err == nil {
		t.Fatal("expired grant was accepted")
	}
}

func testJWKFiles(t *testing.T) (string, string) {
	t.Helper()
	key, err := internalrpcauth.GenerateES256Key("integration-continuation-g3")
	if err != nil {
		t.Fatal(err)
	}
	privateRaw, err := internalrpcauth.MarshalPrivateJWK(key)
	if err != nil {
		t.Fatal(err)
	}
	publicRaw, err := internalrpcauth.MarshalPublicJWK(key.PublicOnly())
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	privateFile, publicFile := filepath.Join(directory, "private.jwk"), filepath.Join(directory, "public.jwk")
	if os.WriteFile(privateFile, privateRaw, 0o600) != nil || os.WriteFile(publicFile, publicRaw, 0o600) != nil {
		t.Fatal("write test JWK")
	}
	return privateFile, publicFile
}

func digest64(symbol string) string {
	value := ""
	for range 64 {
		value += symbol
	}
	return value
}
