package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestMatchesObservedCaller(t *testing.T) {
	const (
		callerSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
		targetSPIFFEID = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	)
	claims := model.AuthorizationClaims{
		Caller: model.Workload{SPIFFEID: callerSPIFFEID},
		Target: model.Workload{SPIFFEID: targetSPIFFEID},
	}

	if !matchesObservedCaller(claims, callerSPIFFEID) {
		t.Fatal("observed caller SPIFFE ID was rejected")
	}
	if matchesObservedCaller(claims, targetSPIFFEID) {
		t.Fatal("target SPIFFE ID was accepted as the observed caller")
	}
}

func TestVerifyAcceptsCallerKeyGenerationIndependentFromVerifierGeneration(t *testing.T) {
	t.Parallel()

	callerKey, err := internalrpcauth.GenerateES256Key("caller-g1")
	if err != nil {
		t.Fatalf("generate caller key: %v", err)
	}
	verifierKey, err := internalrpcauth.GenerateES256Key("verifier-g7")
	if err != nil {
		t.Fatalf("generate verifier key: %v", err)
	}
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	const (
		callerIssuer   = "spiffe://kodex.local/ns/kodex-system/sa/automation-scheduler"
		verifierIssuer = "spiffe://kodex.local/ns/kodex-system/sa/control-api-gateway"
		audience       = "urn:kodex:internal-rpc:control-plane"
		method         = "/controlplane.v1.RuntimeWorkService/ClaimDueSchedules"
		operation      = "platform.runtime.schedules.claim"
		callerSPIFFE   = "spiffe://kodex.local/ns/kodex-system/sa/automation-scheduler"
		targetSPIFFE   = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	)
	policy := model.PolicySnapshot{
		Version: 1, DefaultDecision: "DENY", TokenTTLSeconds: 30,
		AllowedClockSkewSeconds: 2, SourceRevision: 7,
		SourceDigestSHA256: strings.Repeat("a", 64), PredecessorRevision: 6,
		PredecessorDigestSHA256: strings.Repeat("b", 64), KeySetRevision: 7,
		PolicyRevision: 31, SignerGeneration: 7, Issuer: verifierIssuer,
		SignerKeyID: verifierKey.KeyID,
		OperationBindings: []model.OperationBinding{{
			OperationID: operation, CallerWorkloadID: "automation-scheduler",
			CallerSPIFFEID: callerSPIFFE, Issuer: callerIssuer,
			TargetWorkloadID: "control-plane", TargetSPIFFEID: targetSPIFFE,
			Audience: audience, FullMethod: method, Permission: operation,
			AuthorityProofIssuer: targetSPIFFE, AuthorityProofAudience: "urn:kodex:proof",
			AuthoritySources: []string{"DOMAIN_STATE"}, TokenTTLSeconds: 30,
		}},
	}
	keyRecord := func(
		key internalrpcauth.ES256Key,
		issuer string,
		generation uint64,
	) VerificationKeyRecord {
		return VerificationKeyRecord{
			Key: key.PublicOnly(), Issuer: issuer, Generation: generation,
			Status: keyStatusCurrent, Purpose: contextKeyPurpose,
			Audiences: map[string]struct{}{audience: {}},
			NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		}
	}
	authority, err := NewAuthority(policy, KeyMaterial{
		VerificationKeys: map[string]VerificationKeyRecord{
			callerKey.KeyID:   keyRecord(callerKey, callerIssuer, 1),
			verifierKey.KeyID: keyRecord(verifierKey, verifierIssuer, 7),
		},
	}, testAuthorityStore{})
	if err != nil {
		t.Fatalf("create verifier authority: %v", err)
	}
	authority.now = func() time.Time { return now }
	if err := authority.ActivateSnapshot(
		context.Background(),
		"82d62007-3a12-4c43-8a0b-1dbf694f55b4",
	); err != nil {
		t.Fatalf("activate verifier authority: %v", err)
	}
	claims := model.AuthorizationClaims{
		Version: model.ContractVersion, Issuer: callerIssuer, Audience: audience,
		Subject:    callerSPIFFE,
		Caller:     model.Workload{WorkloadID: "automation-scheduler", SPIFFEID: callerSPIFFE},
		Target:     model.Workload{WorkloadID: "control-plane", SPIFFEID: targetSPIFFE},
		FullMethod: method, OperationID: operation, Permission: operation,
		JTI: "1d30e336-18e7-4b3c-a939-d6af0ac198ef", IssuedAt: now.Unix(),
		NotBefore: now.Unix(), ExpiresAt: now.Add(30 * time.Second).Unix(),
		ReplayMode: model.ReplayModeOneTime, SourceRevision: 7,
		SourceDigestSHA256: strings.Repeat("a", 64), KeySetRevision: 7,
		PolicyRevision: 31, SignerGeneration: 1,
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		claims,
		callerKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: internalrpcauth.AuthorizationContextProtectedType, KeyID: callerKey.KeyID,
		},
	)
	if err != nil {
		t.Fatalf("sign caller authorization context: %v", err)
	}
	if _, err := authority.Verify(context.Background(), compact, method, callerSPIFFE); err != nil {
		t.Fatalf("verify caller generation independent from verifier: %v", err)
	}
	claims.SignerGeneration = 2
	claims.JTI = "5c9fd75d-eaf7-447d-b9cf-36a4e56d0c17"
	compact, err = internalrpcauth.SignCanonicalJSON(
		claims,
		callerKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: internalrpcauth.AuthorizationContextProtectedType, KeyID: callerKey.KeyID,
		},
	)
	if err != nil {
		t.Fatalf("sign mismatched caller authorization context: %v", err)
	}
	if _, err := authority.Verify(context.Background(), compact, method, callerSPIFFE); err == nil {
		t.Fatal("caller generation not matching its trusted key was accepted")
	}
}

func TestAuthorizationMetadataLastKnownGoodWindowIsNonSliding(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	authority := &Authority{store: testAuthorityStore{}}
	authority.now = func() time.Time { return now }

	if err := authority.ActivateSnapshot(
		context.Background(),
		"82d62007-3a12-4c43-8a0b-1dbf694f55b4",
	); err != nil {
		t.Fatalf("activate authority metadata: %v", err)
	}
	now = now.Add(authorizationMetadataLKGWindow - time.Second)
	if err := authority.Ready(context.Background()); err != nil {
		t.Fatalf("fresh last-known-good metadata was rejected: %v", err)
	}
	now = now.Add(time.Second)
	if err := authority.Ready(context.Background()); err == nil {
		t.Fatal("expired last-known-good metadata was accepted")
	}
}

func TestAuthorizationExpiryDoesNotExceedMetadataWindow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	validUntil := now.Add(7 * time.Second)

	if got := authorizationExpiry(now, 30*time.Second, validUntil); !got.Equal(validUntil) {
		t.Fatalf("authorization expiry exceeded metadata window: got=%s want=%s", got, validUntil)
	}
	if got := authorizationExpiry(now, 5*time.Second, validUntil); !got.Equal(now.Add(5 * time.Second)) {
		t.Fatalf("authorization expiry ignored shorter binding TTL: got=%s", got)
	}
}

type testAuthorityStore struct{}

func (testAuthorityStore) Reserve(context.Context, repository.Reservation) error { return nil }
func (testAuthorityStore) ActivateSnapshot(context.Context, repository.SnapshotState) error {
	return nil
}
func (testAuthorityStore) AcceptVerification(
	context.Context,
	repository.SnapshotState,
	repository.Reservation,
) error {
	return nil
}
func (testAuthorityStore) Ready(context.Context, repository.SnapshotState) error { return nil }
func (testAuthorityStore) Close()                                                {}
