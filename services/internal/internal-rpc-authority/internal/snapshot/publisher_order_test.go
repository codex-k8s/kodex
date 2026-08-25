package snapshot

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
)

func TestVerifyPublisherSnapshotCompactUsesSnapshotLimit(t *testing.T) {
	t.Parallel()
	key := mustPublisherTestKey(t, "snapshot-limit")
	payload := struct {
		Data string `json:"data"`
	}{Data: strings.Repeat("x", internalrpcauth.MaxCompactJWSBytes)}
	compact, err := internalrpcauth.SignCanonicalJSONWithLimit(
		payload,
		key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: snapshotProtectedType, KeyID: key.KeyID,
		},
		maxSnapshotBytes,
	)
	if err != nil {
		t.Fatalf("sign large snapshot: %v", err)
	}
	if len(compact) <= internalrpcauth.MaxCompactJWSBytes {
		t.Fatal("test snapshot does not exceed ordinary authorization JWS limit")
	}
	if _, err := VerifyPublisherSnapshotCompact(compact, key.PublicOnly()); err != nil {
		t.Fatalf("verify large snapshot: %v", err)
	}
}

func TestPublisherKeyDocumentsIgnoreRegistryMapOrder(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_786_507_482, 0).UTC()
	alpha := mustPublisherTestKey(t, "alpha")
	beta := mustPublisherTestKey(t, "beta")
	authorization := []PublisherKey{
		publisherTestKey("spiffe://kodex.local/beta", "beta", "AUTHORIZATION_CONTEXT", 2, beta),
		publisherTestKey("spiffe://kodex.local/alpha", "alpha", "AUTHORIZATION_CONTEXT", 1, alpha),
	}
	left, err := publisherIssuerSets(authorization, now)
	if err != nil {
		t.Fatalf("build first issuer set: %v", err)
	}
	right, err := publisherIssuerSets([]PublisherKey{authorization[1], authorization[0]}, now)
	if err != nil {
		t.Fatalf("build reordered issuer set: %v", err)
	}
	leftJSON, err := internalrpcauth.CanonicalJSON(left)
	if err != nil {
		t.Fatalf("encode first issuer set: %v", err)
	}
	rightJSON, err := internalrpcauth.CanonicalJSON(right)
	if err != nil {
		t.Fatalf("encode reordered issuer set: %v", err)
	}
	if !bytes.Equal(leftJSON, rightJSON) {
		t.Fatal("authorization issuer set depends on registry iteration order")
	}

	proof := []PublisherKey{
		publisherTestKey("spiffe://kodex.local/beta", "beta", "AUTHORITY_PROOF", 2, beta),
		publisherTestKey("spiffe://kodex.local/alpha", "alpha", "AUTHORITY_PROOF", 1, alpha),
	}
	options := PublisherBuildOptions{SourceRevision: 1, AuthorityProofKeys: proof}
	firstProof, err := publisherProofTrust(options, now, string(make([]byte, 64)))
	if err != nil {
		t.Fatalf("build first proof trust: %v", err)
	}
	options.AuthorityProofKeys = []PublisherKey{proof[1], proof[0]}
	secondProof, err := publisherProofTrust(options, now, string(make([]byte, 64)))
	if err != nil {
		t.Fatalf("build reordered proof trust: %v", err)
	}
	if !bytes.Equal(firstProof, secondProof) {
		t.Fatal("proof trust depends on registry iteration order")
	}
}

func TestBindPublisherKeyAudiencesUsesSignedPolicy(t *testing.T) {
	t.Parallel()
	issuer := "spiffe://kodex.local/ns/kodex-system/sa/caller"
	proofIssuer := "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
	key := mustPublisherTestKey(t, "bound-audiences")
	authorization, proof, err := bindPublisherKeyAudiences(
		[]PublisherKey{{Issuer: issuer, Key: key}},
		[]PublisherKey{{Issuer: proofIssuer, Key: key}},
		policy{
			OperationBindings: []operationBinding{
				{CallerSPIFFEID: issuer, Issuer: issuer, Audience: "urn:kodex:beta"},
				{CallerSPIFFEID: issuer, Issuer: issuer, Audience: "urn:kodex:alpha"},
			},
			ProofProducers: []authorityProofProducer{
				{AuthorityProofIssuer: proofIssuer, AuthorityProofAudience: "urn:kodex:proof"},
			},
		},
	)
	if err != nil {
		t.Fatalf("bind publisher audiences: %v", err)
	}
	if strings.Join(authorization[0].Audiences, ",") != "urn:kodex:alpha,urn:kodex:beta" {
		t.Fatalf("unexpected authorization audiences: %v", authorization[0].Audiences)
	}
	if strings.Join(proof[0].Audiences, ",") != "urn:kodex:proof" {
		t.Fatalf("unexpected proof audiences: %v", proof[0].Audiences)
	}
}

func TestBindPublisherKeyAudiencesRejectsDelegatedIssuer(t *testing.T) {
	t.Parallel()
	_, _, err := bindPublisherKeyAudiences(nil, nil, policy{
		OperationBindings: []operationBinding{{
			CallerSPIFFEID: "spiffe://kodex.local/caller",
			Issuer:         "spiffe://kodex.local/other",
			Audience:       "urn:kodex:target",
		}},
	})
	if err == nil {
		t.Fatal("delegated issuer without matching caller key was accepted")
	}
}

func publisherTestKey(
	issuer string,
	workloadID string,
	purpose string,
	generation uint64,
	key internalrpcauth.ES256Key,
) PublisherKey {
	return PublisherKey{
		Issuer: issuer, WorkloadID: workloadID, Status: "CURRENT",
		Generation: generation, Purpose: purpose,
		Audiences: []string{"urn:kodex:test"}, Key: key,
	}
}

func mustPublisherTestKey(t *testing.T, id string) internalrpcauth.ES256Key {
	t.Helper()
	key, err := internalrpcauth.GenerateES256Key(id)
	if err != nil {
		t.Fatalf("generate ES256 key: %v", err)
	}
	return key
}
