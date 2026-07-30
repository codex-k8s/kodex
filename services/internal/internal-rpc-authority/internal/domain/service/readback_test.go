package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/types"
)

func TestReadbackChallengeConsumeRetryAndReplay(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	signer := testKey(t, "readback-credential-g1")
	possession := testKey(t, "readback-possession-g7")
	intent := testReadbackIntent(t, possession, now)
	store := newMemoryReadbackStore(intent)
	attestor, err := NewReadbackAttestor(
		testReadbackTrust(signer),
		store,
		9,
	)
	if err != nil {
		t.Fatalf("construct readback attestor: %v", err)
	}
	attestor.now = func() time.Time { return now }
	credentialCompact, credential := signReadbackCredential(
		t,
		signer,
		intent,
		now,
	)
	first, err := attestor.IssueChallenge(
		context.Background(),
		intent.WorkloadSPIFFEID,
		intent.IntentID,
		credentialCompact,
		"22222222-2222-4222-8222-222222222222",
	)
	if err != nil {
		t.Fatalf("issue challenge: %v", err)
	}
	retried, err := attestor.IssueChallenge(
		context.Background(),
		intent.WorkloadSPIFFEID,
		intent.IntentID,
		credentialCompact,
		"22222222-2222-4222-8222-222222222222",
	)
	if err != nil {
		t.Fatalf("retry challenge: %v", err)
	}
	if first.Challenge.ChallengeID != retried.Challenge.ChallengeID ||
		first.Challenge.Nonce != retried.Challenge.Nonce {
		t.Fatal("challenge retry did not return persisted result")
	}
	evidenceCompact := signReadbackEvidence(
		t,
		possession,
		credential,
		credentialCompact,
		first.Challenge,
		now,
	)
	accepted, err := attestor.Attest(
		context.Background(),
		intent.WorkloadSPIFFEID,
		intent.IntentID,
		first.Challenge.ChallengeID,
		credentialCompact,
		evidenceCompact,
		"33333333-3333-4333-8333-333333333333",
	)
	if err != nil {
		t.Fatalf("attest served state: %v", err)
	}
	retriedReceipt, err := attestor.Attest(
		context.Background(),
		intent.WorkloadSPIFFEID,
		intent.IntentID,
		first.Challenge.ChallengeID,
		credentialCompact,
		evidenceCompact,
		"33333333-3333-4333-8333-333333333333",
	)
	if err != nil {
		t.Fatalf("retry attest served state: %v", err)
	}
	if accepted.Receipt.ReceiptID != retriedReceipt.Receipt.ReceiptID {
		t.Fatal("attestation retry did not return persisted receipt")
	}
	if _, err := attestor.Attest(
		context.Background(),
		intent.WorkloadSPIFFEID,
		intent.IntentID,
		first.Challenge.ChallengeID,
		credentialCompact,
		evidenceCompact,
		"44444444-4444-4444-8444-444444444444",
	); !failure.IsKind(err, failure.ReplayDetected) {
		t.Fatalf("expected one-time replay rejection, got %v", err)
	}
}

func TestReadbackRejectsCrossAudienceAndHiddenIntent(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0).UTC()
	signer := testKey(t, "readback-credential-g1")
	possession := testKey(t, "readback-possession-g7")
	intent := testReadbackIntent(t, possession, now)
	store := newMemoryReadbackStore(intent)
	attestor, err := NewReadbackAttestor(testReadbackTrust(signer), store, 9)
	if err != nil {
		t.Fatalf("construct readback attestor: %v", err)
	}
	attestor.now = func() time.Time { return now }
	credentialCompact, _ := signReadbackCredential(t, signer, intent, now)
	if _, err := attestor.IssueChallenge(
		context.Background(),
		"spiffe://mattercodex.local/ns/other/sa/hidden",
		intent.IntentID,
		credentialCompact,
		"22222222-2222-4222-8222-222222222222",
	); !failure.IsKind(err, failure.NotFound) {
		t.Fatalf("expected tenant-hidden NotFound, got %v", err)
	}
	claims := readbackCredentialClaims(t, intent, possession, now)
	claims.Audience = "urn:mattercodex:internal-rpc-authority-restore-controller"
	crossAudience, err := internalrpcauth.SignCanonicalJSON(
		claims,
		signer,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: readbackCredentialType, KeyID: signer.KeyID,
		},
	)
	if err != nil {
		t.Fatalf("sign cross-audience credential: %v", err)
	}
	if _, err := attestor.IssueChallenge(
		context.Background(),
		intent.WorkloadSPIFFEID,
		intent.IntentID,
		crossAudience,
		"22222222-2222-4222-8222-222222222222",
	); !failure.IsKind(err, failure.BindingMismatch) {
		t.Fatalf("expected cross-audience rejection, got %v", err)
	}
}

func testReadbackTrust(key internalrpcauth.ES256Key) map[string]VerificationKeyRecord {
	return map[string]VerificationKeyRecord{
		key.KeyID: {
			Key:        key.PublicOnly(),
			Issuer:     readbackPublisherIssuer,
			Generation: 1,
			Status:     keyStatusCurrent,
			Purpose:    readbackCredentialPurpose,
			Audiences:  map[string]struct{}{readbackAudience: {}},
			NotBefore:  time.Unix(0, 0),
			NotAfter:   time.Unix(math.MaxInt32, 0),
		},
	}
}

func testReadbackIntent(
	t *testing.T,
	possession internalrpcauth.ES256Key,
	now time.Time,
) model.ReadbackIntent {
	t.Helper()
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(possession.PublicOnly())
	if err != nil {
		t.Fatalf("compute possession thumbprint: %v", err)
	}
	return model.ReadbackIntent{
		IntentID:                "11111111-1111-4111-8111-111111111111",
		Kind:                    "SNAPSHOT",
		IntentRevision:          11,
		IntentDigestSHA256:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkloadID:              "control-plane",
		WorkloadSPIFFEID:        "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-plane",
		Role:                    "AUTHORIZATION_VERIFIER",
		WorkloadGeneration:      3,
		CredentialGeneration:    4,
		MaterialGeneration:      5,
		PossessionKeyID:         possession.KeyID,
		PossessionKeyGeneration: 7,
		PossessionPublicJWK:     testPublicJWK(t, possession),
		PossessionKeyThumbprint: thumbprint,
		SourceRevision:          12,
		ServedStateDigestSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Status:                  "PINNED",
		ExpiresAt:               now.Add(5 * time.Minute),
	}
}

func readbackCredentialClaims(
	t *testing.T,
	intent model.ReadbackIntent,
	possession internalrpcauth.ES256Key,
	now time.Time,
) model.ReadbackCredentialClaims {
	t.Helper()
	return model.ReadbackCredentialClaims{
		Version: model.ContractVersion, Issuer: readbackPublisherIssuer,
		Audience: readbackAudience, Subject: intent.WorkloadID,
		JTI:     "55555555-5555-4555-8555-555555555555",
		Purpose: "SNAPSHOT_READBACK", IntentID: intent.IntentID,
		IntentKind: intent.Kind, IntentRevision: intent.IntentRevision,
		IntentDigestSHA256: intent.IntentDigestSHA256,
		WorkloadID:         intent.WorkloadID, WorkloadSPIFFEID: intent.WorkloadSPIFFEID,
		Role: intent.Role, WorkloadGeneration: intent.WorkloadGeneration,
		CredentialGeneration:     intent.CredentialGeneration,
		MaterialGeneration:       intent.MaterialGeneration,
		PossessionKeyID:          intent.PossessionKeyID,
		PossessionKeyGeneration:  intent.PossessionKeyGeneration,
		PossessionPublicJWK:      testPublicJWK(t, possession),
		PossessionKeyThumbprint:  intent.PossessionKeyThumbprint,
		SignerSourceRevision:     1,
		SignerSourceDigestSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		SignerKeySetRevision:     1, SignerGeneration: 1,
		IssuedAt: now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(readbackCredentialTTL).Unix(),
	}
}

func signReadbackCredential(
	t *testing.T,
	signer internalrpcauth.ES256Key,
	intent model.ReadbackIntent,
	now time.Time,
) (string, model.ReadbackCredentialClaims) {
	t.Helper()
	possession, err := internalrpcauth.ParsePublicJWK(intent.PossessionPublicJWK)
	if err != nil {
		t.Fatalf("parse possession key: %v", err)
	}
	claims := readbackCredentialClaims(t, intent, possession, now)
	compact, err := internalrpcauth.SignCanonicalJSON(
		claims,
		signer,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: readbackCredentialType, KeyID: signer.KeyID,
		},
	)
	if err != nil {
		t.Fatalf("sign readback credential: %v", err)
	}
	return compact, claims
}

func signReadbackEvidence(
	t *testing.T,
	possession internalrpcauth.ES256Key,
	credential model.ReadbackCredentialClaims,
	credentialCompact string,
	challenge model.ReadbackChallenge,
	now time.Time,
) string {
	t.Helper()
	credentialDigestRaw := sha256.Sum256([]byte(credentialCompact))
	credentialDigest := hex.EncodeToString(credentialDigestRaw[:])
	evidence := model.ReadbackAttestationClaims{
		Version: model.ContractVersion, Issuer: challenge.Intent.WorkloadSPIFFEID,
		Audience: readbackAudience, Subject: challenge.Intent.WorkloadID,
		JTI:     "66666666-6666-4666-8666-666666666666",
		Purpose: credential.Purpose, IntentID: challenge.Intent.IntentID,
		IntentKind: challenge.Intent.Kind, IntentRevision: challenge.Intent.IntentRevision,
		WorkloadID:               challenge.Intent.WorkloadID,
		WorkloadSPIFFEID:         challenge.Intent.WorkloadSPIFFEID,
		Role:                     challenge.Intent.Role,
		WorkloadGeneration:       challenge.Intent.WorkloadGeneration,
		CredentialGeneration:     challenge.Intent.CredentialGeneration,
		ReadbackCredentialJTI:    credential.JTI,
		ReadbackCredentialDigest: credentialDigest,
		PossessionKeyID:          challenge.Intent.PossessionKeyID,
		PossessionKeyGeneration:  challenge.Intent.PossessionKeyGeneration,
		PossessionKeyThumbprint:  challenge.Intent.PossessionKeyThumbprint,
		SourceRevision:           challenge.Intent.SourceRevision,
		ServedStateDigestSHA256:  challenge.Intent.ServedStateDigestSHA256,
		ChallengeID:              challenge.ChallengeID, ChallengeJTI: challenge.ChallengeJTI,
		ChallengeNonce: challenge.Nonce, ChallengeDigestSHA256: challenge.DigestSHA256,
		IssuedAt: now.Unix(), NotBefore: now.Unix(),
		ExpiresAt: now.Add(readbackChallengeTTL).Unix(),
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		evidence,
		possession,
		internalrpcauth.ProtectedHeaderExpectation{
			Type: readbackAttestationType, KeyID: possession.KeyID,
		},
	)
	if err != nil {
		t.Fatalf("sign readback evidence: %v", err)
	}
	return compact
}

func testPublicJWK(t *testing.T, key internalrpcauth.ES256Key) json.RawMessage {
	t.Helper()
	publicBytes, err := key.Public.Bytes()
	if err != nil || len(publicBytes) != 65 {
		t.Fatal("encode test public key")
	}
	raw, err := json.Marshal(map[string]any{
		"kty": "EC", "crv": "P-256", "use": "sig",
		"key_ops": []string{"verify"}, "alg": "ES256", "kid": key.KeyID,
		"x": base64.RawURLEncoding.EncodeToString(publicBytes[1:33]),
		"y": base64.RawURLEncoding.EncodeToString(publicBytes[33:65]),
	})
	if err != nil {
		t.Fatalf("marshal test public JWK: %v", err)
	}
	return raw
}

type memoryReadbackStore struct {
	mu                 sync.Mutex
	intent             model.ReadbackIntent
	challenges         map[string]model.ReadbackChallenge
	challengeByRequest map[string]string
	receipts           map[string]model.ReadbackReceipt
}

func newMemoryReadbackStore(intent model.ReadbackIntent) *memoryReadbackStore {
	return &memoryReadbackStore{
		intent: intent, challenges: make(map[string]model.ReadbackChallenge),
		challengeByRequest: make(map[string]string), receipts: make(map[string]model.ReadbackReceipt),
	}
}

func (store *memoryReadbackStore) ResolveReadbackIntent(
	_ context.Context,
	intentID string,
	peerSPIFFEID string,
) (model.ReadbackIntent, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if intentID != store.intent.IntentID ||
		peerSPIFFEID != store.intent.WorkloadSPIFFEID {
		return model.ReadbackIntent{}, repository.ErrNotFound
	}
	return store.intent, nil
}

func (store *memoryReadbackStore) IssueReadbackChallenge(
	_ context.Context,
	command repository.IssueReadbackChallengeCommand,
) (model.ReadbackChallenge, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if existingID, ok := store.challengeByRequest[command.IdempotencyKey]; ok {
		existing := store.challenges[existingID]
		if existing.SemanticRequestDigest != command.SemanticRequestDigest {
			return model.ReadbackChallenge{}, repository.ErrIdempotencyConflict
		}
		return existing, nil
	}
	challenge := model.ReadbackChallenge{
		ChallengeID: command.ChallengeID, ChallengeJTI: command.ChallengeJTI,
		Nonce: command.ChallengeNonce, DigestSHA256: command.ChallengeDigestSHA256,
		Intent: store.intent, ReadbackCredentialJTI: command.ReadbackCredentialJTI,
		ReadbackCredentialDigest: command.ReadbackCredentialDigest,
		IdempotencyKey:           command.IdempotencyKey,
		SemanticRequestDigest:    command.SemanticRequestDigest,
		IssuedAt:                 command.IssuedAt, ExpiresAt: command.ExpiresAt,
	}
	store.challenges[challenge.ChallengeID] = challenge
	store.challengeByRequest[command.IdempotencyKey] = challenge.ChallengeID
	return challenge, nil
}

func (store *memoryReadbackStore) LoadReadbackChallenge(
	_ context.Context,
	challengeID string,
	peerSPIFFEID string,
) (model.ReadbackChallenge, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	challenge, ok := store.challenges[challengeID]
	if !ok || peerSPIFFEID != store.intent.WorkloadSPIFFEID {
		return model.ReadbackChallenge{}, repository.ErrNotFound
	}
	return challenge, nil
}

func (store *memoryReadbackStore) ConsumeReadbackChallenge(
	_ context.Context,
	command repository.ConsumeReadbackChallengeCommand,
) (model.ReadbackReceipt, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if existing, ok := store.receipts[command.IdempotencyKey]; ok {
		if existing.SemanticRequestDigest != command.SemanticRequestDigest ||
			existing.EvidenceDigestSHA256 != command.EvidenceDigestSHA256 {
			return model.ReadbackReceipt{}, repository.ErrIdempotencyConflict
		}
		return existing, nil
	}
	challenge, ok := store.challenges[command.ChallengeID]
	if !ok {
		return model.ReadbackReceipt{}, repository.ErrNotFound
	}
	if challenge.ConsumedAt != nil {
		return model.ReadbackReceipt{}, repository.ErrReplay
	}
	if command.AcceptedAt.After(challenge.ExpiresAt) {
		return model.ReadbackReceipt{}, repository.ErrExpired
	}
	acceptedAt := command.AcceptedAt
	challenge.ConsumedAt = &acceptedAt
	store.challenges[command.ChallengeID] = challenge
	receipt := model.ReadbackReceipt{
		ReceiptID: command.ReceiptID, ChallengeID: command.ChallengeID,
		EvidenceJTI:           command.EvidenceJTI,
		EvidenceDigestSHA256:  command.EvidenceDigestSHA256,
		SemanticRequestDigest: command.SemanticRequestDigest,
		VerifierGeneration:    command.VerifierGeneration,
		AcceptedAt:            command.AcceptedAt, ExpiresAt: command.ExpiresAt,
		Intent: store.intent,
	}
	store.receipts[command.IdempotencyKey] = receipt
	return receipt, nil
}

func (*memoryReadbackStore) ReadbackReady(context.Context) error { return nil }

var _ repository.ReadbackStore = (*memoryReadbackStore)(nil)
