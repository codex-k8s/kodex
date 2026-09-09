package snapshot

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
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
	left, err := publisherIssuerSets(authorization, now, nil)
	if err != nil {
		t.Fatalf("build first issuer set: %v", err)
	}
	right, err := publisherIssuerSets([]PublisherKey{authorization[1], authorization[0]}, now, nil)
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
	firstProof, err := publisherProofTrust(options, now, strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("build first proof trust: %v", err)
	}
	options.AuthorityProofKeys = []PublisherKey{proof[1], proof[0]}
	secondProof, err := publisherProofTrust(options, now, strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("build reordered proof trust: %v", err)
	}
	if !bytes.Equal(firstProof, secondProof) {
		t.Fatal("proof trust depends on registry iteration order")
	}
}

func TestPublisherKeyValidityAllowsPlannedForwardOnlyRotation(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_786_507_482, 0).UTC()
	key := mustPublisherTestKey(t, "planned-rotation")
	sets, err := publisherIssuerSets([]PublisherKey{
		publisherTestKey(
			"spiffe://kodex.local/issuer",
			"issuer",
			"AUTHORIZATION_CONTEXT",
			1,
			key,
		),
	}, now, nil)
	if err != nil {
		t.Fatalf("build issuer set: %v", err)
	}
	if len(sets) != 1 || len(sets[0].Keys) != 1 {
		t.Fatal("unexpected issuer set shape")
	}
	if got, want := sets[0].Keys[0].NotAfter, now.Add(PublisherSnapshotValidity).Unix(); got != want {
		t.Fatalf("unexpected key validity: got %d, want %d", got, want)
	}
	if PublisherSnapshotValidity < 90*24*time.Hour {
		t.Fatal("snapshot validity leaves no operational rotation window")
	}
}

func TestPublisherPreviousKeyUsesAuthoritativeDeadline(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_786_507_482, 0).UTC()
	deadline := now.Add(40*time.Second + 371*time.Millisecond)
	key := mustPublisherTestKey(t, "previous-deadline")
	value := publisherTestKey(
		"spiffe://kodex.local/issuer",
		"issuer",
		"AUTHORIZATION_CONTEXT",
		1,
		key,
	)
	value.Status = "PREVIOUS"
	sets, err := publisherIssuerSets([]PublisherKey{value}, now, &deadline)
	if err != nil {
		t.Fatalf("build PREVIOUS issuer set: %v", err)
	}
	if got, want := sets[0].Keys[0].NotAfter, deadline.Unix(); got != want {
		t.Fatalf("PREVIOUS NotAfter = %d, want %d", got, want)
	}
	value.Purpose = "AUTHORITY_PROOF"
	currentProof := publisherTestKey(
		value.Issuer,
		value.WorkloadID,
		"AUTHORITY_PROOF",
		2,
		mustPublisherTestKey(t, "current-proof"),
	)
	proofRaw, err := publisherProofTrust(PublisherBuildOptions{
		SourceRevision:      1,
		AuthorityProofKeys:  []PublisherKey{value, currentProof},
		PreviousKeyNotAfter: &deadline,
	}, now, strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("build PREVIOUS proof trust: %v", err)
	}
	var proof proofTrustDocument
	if err := json.Unmarshal(proofRaw, &proof); err != nil {
		t.Fatalf("decode PREVIOUS proof trust: %v", err)
	}
	if got, want := proof.Keys[0].NotAfter, deadline.Unix(); got != want {
		t.Fatalf("proof PREVIOUS NotAfter = %d, want %d", got, want)
	}
}

func TestPublisherPreviousKeyCanBeRetiredAfterDeadline(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_786_507_482, 0).UTC()
	deadline := now.Add(-2 * time.Minute)
	key := mustPublisherTestKey(t, "expired-previous-deadline")
	value := publisherTestKey(
		"spiffe://kodex.local/issuer",
		"issuer",
		"AUTHORIZATION_CONTEXT",
		1,
		key,
	)
	value.Status = "PREVIOUS"
	currentAuthorization := publisherTestKey(
		value.Issuer,
		value.WorkloadID,
		"AUTHORIZATION_CONTEXT",
		2,
		mustPublisherTestKey(t, "expired-current-authorization"),
	)
	sets, err := publisherIssuerSets([]PublisherKey{value, currentAuthorization}, now, &deadline)
	if err != nil {
		t.Fatalf("build expired PREVIOUS issuer set: %v", err)
	}
	if got, want := sets[0].Keys[0].NotAfter, deadline.Unix(); got != want {
		t.Fatalf("expired PREVIOUS NotAfter = %d, want %d", got, want)
	}
	if sets[0].Keys[0].NotBefore >= sets[0].Keys[0].NotAfter {
		t.Fatal("expired PREVIOUS issuer interval is invalid")
	}
	loadedIssuers, _, err := loadIssuerKeys(
		sets,
		[]operationBinding{{Issuer: value.Issuer, Audience: value.Audiences[0]}},
		"other-workload",
		false,
		now,
	)
	if err != nil || len(loadedIssuers) != 2 {
		t.Fatalf("load issuer set with expired PREVIOUS: keys=%d err=%v", len(loadedIssuers), err)
	}

	value.Purpose = "AUTHORITY_PROOF"
	currentProof := publisherTestKey(
		value.Issuer,
		value.WorkloadID,
		"AUTHORITY_PROOF",
		2,
		mustPublisherTestKey(t, "expired-current-proof"),
	)
	proofRaw, err := publisherProofTrust(PublisherBuildOptions{
		SourceRevision:      1,
		AuthorityProofKeys:  []PublisherKey{value, currentProof},
		PreviousKeyNotAfter: &deadline,
	}, now, strings.Repeat("0", 64))
	if err != nil {
		t.Fatalf("build expired PREVIOUS proof trust: %v", err)
	}
	var proof proofTrustDocument
	if err := json.Unmarshal(proofRaw, &proof); err != nil {
		t.Fatalf("decode expired PREVIOUS proof trust: %v", err)
	}
	if proof.Keys[0].NotBefore >= proof.Keys[0].NotAfter {
		t.Fatal("expired PREVIOUS proof interval is invalid")
	}
	proofPath := filepath.Join(t.TempDir(), "proof-trust.json")
	if err := os.WriteFile(proofPath, proofRaw, 0o444); err != nil {
		t.Fatalf("write expired PREVIOUS proof trust: %v", err)
	}
	loadedProof, err := loadProofTrust(proofPath, now, 1, strings.Repeat("0", 64))
	if err != nil || len(loadedProof) != 2 {
		t.Fatalf("load proof trust with expired PREVIOUS: keys=%d err=%v", len(loadedProof), err)
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
