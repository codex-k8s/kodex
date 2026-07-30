package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestIssueVerifyAndReplay(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_800_000_000, 0).UTC()
	signingKey := testKey(t, "issuer-signing-g1")
	proofKey := testKey(t, "proof-signing-g1")
	store := newMemoryStore()
	authority, err := NewAuthority(
		testPolicy(signingKey.KeyID),
		testKeyMaterial(signingKey, proofKey),
		store,
	)
	if err != nil {
		t.Fatalf("construct authority: %v", err)
	}
	authority.now = func() time.Time { return now }
	if err := authority.ActivateSnapshot(
		context.Background(),
		"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	); err != nil {
		t.Fatalf("activate snapshot: %v", err)
	}
	proof := testProof(now)
	proofCompact, err := internalrpcauth.SignCanonicalJSON(
		proof,
		proofKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  proofProtectedType,
			KeyID: proofKey.KeyID,
		},
	)
	if err != nil {
		t.Fatalf("sign authority proof: %v", err)
	}
	compact, issued, err := authority.Issue(
		context.Background(),
		"control.project.get",
		proofCompact,
	)
	if err != nil {
		t.Fatalf("issue authorization context: %v", err)
	}
	if issued.Authority.Project == nil || !uuidPattern.MatchString(issued.JTI) {
		t.Fatalf("issued claims lost project authority or valid jti: %#v", issued)
	}
	verified, err := authority.Verify(
		context.Background(),
		compact,
		"/controlplane.v1.ProjectService/GetProject",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
	)
	if err != nil {
		t.Fatalf("verify authorization context: %v", err)
	}
	if verified.JTI != issued.JTI || verified.Permission != "control.project.get" {
		t.Fatalf("verified claims mismatch: %#v", verified)
	}
	if _, err := authority.Verify(
		context.Background(),
		compact,
		"/controlplane.v1.ProjectService/GetProject",
		"spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
	); !failure.IsKind(err, failure.ReplayDetected) {
		t.Fatalf("expected replay rejection, got %v", err)
	}
	if err := authority.Ready(context.Background()); err != nil {
		t.Fatalf("authority readiness: %v", err)
	}
}

func TestIssueRejectsAuthorityOutsidePolicy(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_800_000_000, 0).UTC()
	signingKey := testKey(t, "issuer-signing-g1")
	proofKey := testKey(t, "proof-signing-g1")
	authority, err := NewAuthority(
		testPolicy(signingKey.KeyID),
		testKeyMaterial(signingKey, proofKey),
		newMemoryStore(),
	)
	if err != nil {
		t.Fatalf("construct authority: %v", err)
	}
	authority.now = func() time.Time { return now }
	proof := testProof(now)
	proof.Authority.Project.Provenance.Source = "AUTOMATION_OCCURRENCE"
	compact, err := internalrpcauth.SignCanonicalJSON(
		proof,
		proofKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  proofProtectedType,
			KeyID: proofKey.KeyID,
		},
	)
	if err != nil {
		t.Fatalf("sign authority proof: %v", err)
	}
	if _, _, err := authority.Issue(
		context.Background(),
		"control.project.get",
		compact,
	); !failure.IsKind(err, failure.AuthorityRejected) {
		t.Fatalf("expected authority source rejection, got %v", err)
	}
}

func testPolicy(signingKeyID string) model.PolicySnapshot {
	return model.PolicySnapshot{
		Version:                 model.ContractVersion,
		TrustDomain:             "mattercodex.local",
		DefaultDecision:         "DENY",
		TokenTTLSeconds:         30,
		AllowedClockSkewSeconds: 2,
		MaxCompactJWSBytes:      internalrpcauth.MaxCompactJWSBytes,
		Issuer:                  "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
		SignerKeyID:             signingKeyID,
		SourceRevision:          1,
		SourceDigestSHA256:      strings.Repeat("a", 64),
		PredecessorRevision:     0,
		PredecessorDigestSHA256: strings.Repeat("0", 64),
		KeySetRevision:          4,
		PolicyRevision:          5,
		SignerGeneration:        6,
		OperationBindings: []model.OperationBinding{{
			OperationID:            "control.project.get",
			CallerWorkloadID:       "control-api-gateway",
			CallerSPIFFEID:         "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
			Issuer:                 "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
			TargetWorkloadID:       "control-plane",
			TargetSPIFFEID:         "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
			Audience:               "urn:mattercodex:internal-rpc:control-plane",
			FullMethod:             "/controlplane.v1.ProjectService/GetProject",
			Permission:             "control.project.get",
			AuthorityProofIssuer:   "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
			AuthorityProofAudience: "urn:mattercodex:internal-rpc-authority-issuer:control-api-gateway",
			AuthoritySources:       []string{"OIDC_SESSION", "DOMAIN_STATE"},
			ProjectRequired:        true,
			TokenTTLSeconds:        20,
		}},
	}
}

func testProof(now time.Time) model.AuthorityProof {
	provenance := func(source, id, digest string) model.Identity {
		return model.Identity{
			ID: id,
			Provenance: model.Provenance{
				Source:       source,
				Reference:    id,
				Revision:     1,
				DigestSHA256: strings.Repeat(digest, 64),
			},
		}
	}
	project := provenance(
		"DOMAIN_STATE",
		"22222222-2222-4222-8222-222222222222",
		"d",
	)
	return model.AuthorityProof{
		Version:  model.ContractVersion,
		Issuer:   "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		Audience: "urn:mattercodex:internal-rpc-authority-issuer:control-api-gateway",
		Caller: model.Workload{
			WorkloadID: "control-api-gateway",
			SPIFFEID:   "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
		},
		OperationID:                  "control.project.get",
		AuthorizationContextAudience: "urn:mattercodex:internal-rpc:control-plane",
		Authority: model.Authority{
			ActorKind: "HUMAN",
			Actor: provenance(
				"OIDC_SESSION",
				"00000000-0000-4000-8000-000000000000",
				"a",
			),
			Tenant: provenance(
				"DOMAIN_STATE",
				"11111111-1111-4111-8111-111111111111",
				"b",
			),
			Project: &project,
		},
		ProofRevision:    1,
		SignerGeneration: 1,
		JTI:              "33333333-3333-4333-8333-333333333333",
		IssuedAt:         now.Unix(),
		NotBefore:        now.Unix(),
		ExpiresAt:        now.Add(maxProofTTL).Unix(),
	}
}

func testKey(t *testing.T, keyID string) internalrpcauth.ES256Key {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test key: %v", err)
	}
	return internalrpcauth.ES256Key{
		KeyID:   keyID,
		Public:  &privateKey.PublicKey,
		Private: privateKey,
	}
}

func testKeyMaterial(
	signingKey internalrpcauth.ES256Key,
	proofKey internalrpcauth.ES256Key,
) KeyMaterial {
	return KeyMaterial{
		SigningKey: signingKey,
		VerificationKeys: map[string]VerificationKeyRecord{
			signingKey.KeyID: {
				Key:        signingKey.PublicOnly(),
				Issuer:     "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
				Generation: 6,
				Status:     keyStatusCurrent,
				Purpose:    contextKeyPurpose,
				Audiences: map[string]struct{}{
					"urn:mattercodex:internal-rpc:control-plane": {},
				},
				NotBefore: time.Unix(0, 0),
				NotAfter:  time.Unix(math.MaxInt32, 0),
			},
		},
		ProofKeys: map[string]VerificationKeyRecord{
			proofKey.KeyID: {
				Key:        proofKey.PublicOnly(),
				Issuer:     "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
				Generation: 1,
				Status:     keyStatusCurrent,
				Purpose:    proofKeyPurpose,
				Audiences: map[string]struct{}{
					"urn:mattercodex:internal-rpc-authority-issuer:control-api-gateway": {},
				},
				NotBefore: time.Unix(0, 0),
				NotAfter:  time.Unix(math.MaxInt32, 0),
			},
		},
	}
}

type memoryStore struct {
	mu           sync.Mutex
	reservations map[string]string
	snapshot     repository.SnapshotState
	active       bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{reservations: make(map[string]string)}
}

func (store *memoryStore) Reserve(
	_ context.Context,
	reservation repository.Reservation,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	key := string(reservation.Kind) + ":" + reservation.JTI
	if _, exists := store.reservations[key]; exists {
		return repository.ErrReplay
	}
	store.reservations[key] = reservation.Digest
	return nil
}

func (store *memoryStore) ActivateSnapshot(
	_ context.Context,
	state repository.SnapshotState,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.active && snapshotRollback(store.snapshot, state) {
		return repository.ErrSnapshotRollback
	}
	store.snapshot = state
	store.active = true
	return nil
}

func (store *memoryStore) AcceptVerification(
	ctx context.Context,
	state repository.SnapshotState,
	reservation repository.Reservation,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.active && snapshotRollback(store.snapshot, state) {
		return repository.ErrSnapshotRollback
	}
	key := string(reservation.Kind) + ":" + reservation.JTI
	if _, exists := store.reservations[key]; exists {
		return repository.ErrReplay
	}
	store.snapshot = state
	store.active = true
	store.reservations[key] = reservation.Digest
	return nil
}

func (store *memoryStore) Ready(
	_ context.Context,
	expected repository.SnapshotState,
) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.active || !reflect.DeepEqual(store.snapshot, expected) {
		return repository.ErrNotReady
	}
	return nil
}

func (*memoryStore) Close() {}

func snapshotRollback(
	current repository.SnapshotState,
	next repository.SnapshotState,
) bool {
	return next.SourceRevision < current.SourceRevision ||
		next.SourceRevision > current.SourceRevision+1 ||
		next.SourceRevision == current.SourceRevision+1 &&
			(next.PredecessorRevision != current.SourceRevision ||
				next.PredecessorDigestSHA256 != current.SourceDigestSHA256) ||
		next.KeySetRevision < current.KeySetRevision ||
		next.PolicyRevision < current.PolicyRevision ||
		next.SignerGeneration < current.SignerGeneration ||
		next.SourceRevision == current.SourceRevision &&
			next.SourceDigestSHA256 != current.SourceDigestSHA256
}

var _ repository.Store = (*memoryStore)(nil)
