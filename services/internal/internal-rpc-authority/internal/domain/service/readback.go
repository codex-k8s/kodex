package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

const (
	readbackCredentialType        = "mattercodex-internal-rpc-readback-credential+jws"
	readbackAttestationType       = "mattercodex-internal-rpc-readback-attestation+jws"
	readbackCredentialPurpose     = "READBACK_CREDENTIAL"
	readbackAudience              = "urn:mattercodex:internal-rpc-authority-readback-attestor"
	readbackPublisherIssuer       = "spiffe://mattercodex.local/ns/mattercodex-system/sa/internal-rpc-authority-publisher"
	readbackCredentialTTL         = 30 * time.Second
	readbackChallengeTTL          = 30 * time.Second
	readbackAllowedClockSkew      = 5 * time.Second
	readbackChallengeFullMethod   = "/internalrpcauthority.v1.AuthorityReadbackAttestorService/IssueAttestationChallenge"
	readbackAttestationFullMethod = "/internalrpcauthority.v1.AuthorityReadbackAttestorService/AttestServedState"
)

type ReadbackAttestor struct {
	trust              map[string]VerificationKeyRecord
	store              repository.ReadbackStore
	verifierGeneration uint64
	now                func() time.Time
}

type ReadbackChallengeResult struct {
	Challenge model.ReadbackChallenge
}

type ReadbackAttestationResult struct {
	Receipt model.ReadbackReceipt
}

func NewReadbackAttestor(
	trust map[string]VerificationKeyRecord,
	store repository.ReadbackStore,
	verifierGeneration uint64,
) (*ReadbackAttestor, error) {
	if len(trust) == 0 || store == nil || verifierGeneration == 0 {
		return nil, errors.New("invalid readback attestor configuration")
	}
	if err := validateKeyRecords(trust, readbackCredentialPurpose); err != nil {
		return nil, err
	}
	return &ReadbackAttestor{
		trust:              publicKeyRecords(trust),
		store:              store,
		verifierGeneration: verifierGeneration,
		now:                time.Now,
	}, nil
}

func (attestor *ReadbackAttestor) IssueChallenge(
	ctx context.Context,
	peerSPIFFEID string,
	intentID string,
	credentialCompact string,
	idempotencyKey string,
) (ReadbackChallengeResult, error) {
	if !uuidPattern.MatchString(intentID) ||
		!uuidPattern.MatchString(idempotencyKey) ||
		peerSPIFFEID == "" {
		return ReadbackChallengeResult{}, failure.New(
			failure.InvalidRequest,
			"readback challenge request is invalid",
		)
	}
	credential, credentialDigest, err := attestor.verifyCredential(
		credentialCompact,
	)
	if err != nil {
		return ReadbackChallengeResult{}, err
	}
	intent, err := attestor.store.ResolveReadbackIntent(
		ctx,
		intentID,
		peerSPIFFEID,
	)
	if err != nil {
		return ReadbackChallengeResult{}, mapReadbackStoreFailure(
			"resolve pinned readback intent",
			err,
		)
	}
	if err := bindCredentialToIntent(credential, intent, peerSPIFFEID); err != nil {
		return ReadbackChallengeResult{}, err
	}
	semanticDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		Version          int    `json:"v"`
		FullMethod       string `json:"full_method"`
		IntentID         string `json:"pinned_intent_id"`
		CredentialDigest string `json:"readback_credential_digest_sha256"`
		PeerSPIFFEID     string `json:"mtls_spiffe_id"`
	}{
		Version:          model.ContractVersion,
		FullMethod:       readbackChallengeFullMethod,
		IntentID:         intent.IntentID,
		CredentialDigest: credentialDigest,
		PeerSPIFFEID:     peerSPIFFEID,
	})
	if err != nil {
		return ReadbackChallengeResult{}, failure.Wrap(
			failure.Internal,
			"create readback challenge semantic digest",
			err,
		)
	}
	challengeID, err := newUUID()
	if err != nil {
		return ReadbackChallengeResult{}, failure.Wrap(
			failure.Internal,
			"create readback challenge identifier",
			err,
		)
	}
	challengeJTI, err := newUUID()
	if err != nil {
		return ReadbackChallengeResult{}, failure.Wrap(
			failure.Internal,
			"create readback challenge jti",
			err,
		)
	}
	nonce, err := randomNonce()
	if err != nil {
		return ReadbackChallengeResult{}, failure.Wrap(
			failure.Internal,
			"create readback challenge nonce",
			err,
		)
	}
	issuedAt := attestor.now().UTC().Truncate(time.Second)
	expiresAt := issuedAt.Add(readbackChallengeTTL)
	challengeDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		Version                 int    `json:"v"`
		ChallengeID             string `json:"challenge_id"`
		ChallengeJTI            string `json:"challenge_jti"`
		Nonce                   string `json:"challenge_nonce"`
		Audience                string `json:"aud"`
		IntentID                string `json:"intent_id"`
		IntentRevision          uint64 `json:"intent_revision"`
		IntentDigest            string `json:"intent_digest_sha256"`
		CredentialJTI           string `json:"readback_credential_jti"`
		CredentialDigest        string `json:"readback_credential_digest_sha256"`
		WorkloadID              string `json:"workload_id"`
		Role                    string `json:"role"`
		WorkloadGeneration      uint64 `json:"workload_generation"`
		CredentialGeneration    uint64 `json:"credential_generation"`
		MaterialGeneration      uint64 `json:"material_generation"`
		PossessionKeyID         string `json:"possession_key_kid"`
		PossessionKeyGeneration uint64 `json:"possession_key_generation"`
		PossessionKeyThumbprint string `json:"possession_key_thumbprint_sha256"`
		IssuedAt                int64  `json:"iat"`
		ExpiresAt               int64  `json:"exp"`
	}{
		Version:                 model.ContractVersion,
		ChallengeID:             challengeID,
		ChallengeJTI:            challengeJTI,
		Nonce:                   nonce,
		Audience:                readbackAudience,
		IntentID:                intent.IntentID,
		IntentRevision:          intent.IntentRevision,
		IntentDigest:            intent.IntentDigestSHA256,
		CredentialJTI:           credential.JTI,
		CredentialDigest:        credentialDigest,
		WorkloadID:              intent.WorkloadID,
		Role:                    intent.Role,
		WorkloadGeneration:      intent.WorkloadGeneration,
		CredentialGeneration:    intent.CredentialGeneration,
		MaterialGeneration:      intent.MaterialGeneration,
		PossessionKeyID:         intent.PossessionKeyID,
		PossessionKeyGeneration: intent.PossessionKeyGeneration,
		PossessionKeyThumbprint: intent.PossessionKeyThumbprint,
		IssuedAt:                issuedAt.Unix(),
		ExpiresAt:               expiresAt.Unix(),
	})
	if err != nil {
		return ReadbackChallengeResult{}, failure.Wrap(
			failure.Internal,
			"create readback challenge digest",
			err,
		)
	}
	challenge, err := attestor.store.IssueReadbackChallenge(
		ctx,
		repository.IssueReadbackChallengeCommand{
			IntentID:                 intent.IntentID,
			PeerSPIFFEID:             peerSPIFFEID,
			ReadbackCredentialJTI:    credential.JTI,
			ReadbackCredentialDigest: credentialDigest,
			IdempotencyKey:           idempotencyKey,
			SemanticRequestDigest:    semanticDigest,
			ChallengeID:              challengeID,
			ChallengeJTI:             challengeJTI,
			ChallengeNonce:           nonce,
			ChallengeDigestSHA256:    challengeDigest,
			IssuedAt:                 issuedAt,
			ExpiresAt:                expiresAt,
		},
	)
	if err != nil {
		return ReadbackChallengeResult{}, mapReadbackStoreFailure(
			"persist readback challenge",
			err,
		)
	}
	return ReadbackChallengeResult{Challenge: challenge}, nil
}

func (attestor *ReadbackAttestor) Attest(
	ctx context.Context,
	peerSPIFFEID string,
	intentID string,
	challengeID string,
	credentialCompact string,
	evidenceCompact string,
	idempotencyKey string,
) (ReadbackAttestationResult, error) {
	if !uuidPattern.MatchString(intentID) ||
		!uuidPattern.MatchString(challengeID) ||
		!uuidPattern.MatchString(idempotencyKey) ||
		peerSPIFFEID == "" {
		return ReadbackAttestationResult{}, failure.New(
			failure.InvalidRequest,
			"readback attestation request is invalid",
		)
	}
	credential, credentialDigest, err := attestor.verifyCredential(
		credentialCompact,
	)
	if err != nil {
		return ReadbackAttestationResult{}, err
	}
	challenge, err := attestor.store.LoadReadbackChallenge(
		ctx,
		challengeID,
		peerSPIFFEID,
	)
	if err != nil {
		return ReadbackAttestationResult{}, mapReadbackStoreFailure(
			"load readback challenge",
			err,
		)
	}
	if challenge.Intent.IntentID != intentID ||
		challenge.ReadbackCredentialJTI != credential.JTI ||
		challenge.ReadbackCredentialDigest != credentialDigest {
		return ReadbackAttestationResult{}, failure.New(
			failure.BindingMismatch,
			"readback challenge binding failed",
		)
	}
	if err := bindCredentialToIntent(
		credential,
		challenge.Intent,
		peerSPIFFEID,
	); err != nil {
		return ReadbackAttestationResult{}, err
	}
	possessionKey, err := internalrpcauth.ParsePublicJWK(
		credential.PossessionPublicJWK,
	)
	if err != nil {
		return ReadbackAttestationResult{}, failure.Wrap(
			failure.Unauthenticated,
			"readback possession key rejected",
			err,
		)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		evidenceCompact,
		possessionKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  readbackAttestationType,
			KeyID: credential.PossessionKeyID,
		},
	)
	if err != nil {
		return ReadbackAttestationResult{}, failure.Wrap(
			failure.Unauthenticated,
			"readback attestation signature rejected",
			err,
		)
	}
	var evidence model.ReadbackAttestationClaims
	if err := internalrpcauth.DecodeCanonicalJSON(
		verified.CanonicalPayload,
		&evidence,
	); err != nil {
		return ReadbackAttestationResult{}, failure.Wrap(
			failure.Unauthenticated,
			"readback attestation claims rejected",
			err,
		)
	}
	now := attestor.now().UTC().Truncate(time.Second)
	if err := internalrpcauth.ValidateTimes(
		now,
		time.Unix(evidence.IssuedAt, 0),
		time.Unix(evidence.NotBefore, 0),
		time.Unix(evidence.ExpiresAt, 0),
		readbackChallengeTTL,
		readbackAllowedClockSkew,
	); err != nil {
		return ReadbackAttestationResult{}, failure.Wrap(
			failure.Unauthenticated,
			"readback attestation time binding failed",
			err,
		)
	}
	if err := bindEvidence(
		evidence,
		credential,
		credentialDigest,
		challenge,
		peerSPIFFEID,
	); err != nil {
		return ReadbackAttestationResult{}, err
	}
	evidenceDigestRaw := sha256.Sum256([]byte(evidenceCompact))
	evidenceDigest := hex.EncodeToString(evidenceDigestRaw[:])
	semanticDigest, err := internalrpcauth.CanonicalJSONSHA256(struct {
		Version          int    `json:"v"`
		FullMethod       string `json:"full_method"`
		ChallengeDigest  string `json:"challenge_digest_sha256"`
		CredentialDigest string `json:"readback_credential_digest_sha256"`
		EvidenceDigest   string `json:"evidence_compact_jws_digest_sha256"`
	}{
		Version:          model.ContractVersion,
		FullMethod:       readbackAttestationFullMethod,
		ChallengeDigest:  challenge.DigestSHA256,
		CredentialDigest: credentialDigest,
		EvidenceDigest:   evidenceDigest,
	})
	if err != nil {
		return ReadbackAttestationResult{}, failure.Wrap(
			failure.Internal,
			"create readback attestation semantic digest",
			err,
		)
	}
	receiptID, err := newUUID()
	if err != nil {
		return ReadbackAttestationResult{}, failure.Wrap(
			failure.Internal,
			"create readback receipt identifier",
			err,
		)
	}
	receipt, err := attestor.store.ConsumeReadbackChallenge(
		ctx,
		repository.ConsumeReadbackChallengeCommand{
			ChallengeID:           challenge.ChallengeID,
			PeerSPIFFEID:          peerSPIFFEID,
			ReceiptID:             receiptID,
			EvidenceJTI:           evidence.JTI,
			EvidenceDigestSHA256:  evidenceDigest,
			IdempotencyKey:        idempotencyKey,
			SemanticRequestDigest: semanticDigest,
			VerifierGeneration:    attestor.verifierGeneration,
			AcceptedAt:            now,
			ExpiresAt:             time.Unix(evidence.ExpiresAt, 0),
		},
	)
	if err != nil {
		return ReadbackAttestationResult{}, mapReadbackStoreFailure(
			"consume readback challenge",
			err,
		)
	}
	return ReadbackAttestationResult{Receipt: receipt}, nil
}

func (attestor *ReadbackAttestor) Ready(ctx context.Context) error {
	if err := attestor.store.ReadbackReady(ctx); err != nil {
		return failure.Wrap(
			failure.PersistenceUnavailable,
			"readback attestor persistence is unavailable",
			err,
		)
	}
	return nil
}

func (attestor *ReadbackAttestor) verifyCredential(
	compact string,
) (model.ReadbackCredentialClaims, string, error) {
	header, err := internalrpcauth.ParseProtectedHeader(compact)
	if err != nil || header.Type != readbackCredentialType {
		return model.ReadbackCredentialClaims{}, "", failure.Wrap(
			failure.Unauthenticated,
			"readback credential protected header rejected",
			err,
		)
	}
	record, ok := attestor.trust[header.KeyID]
	if !ok {
		return model.ReadbackCredentialClaims{}, "", failure.New(
			failure.Unauthenticated,
			"readback credential signer is not trusted",
		)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		record.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  readbackCredentialType,
			KeyID: header.KeyID,
		},
	)
	if err != nil {
		return model.ReadbackCredentialClaims{}, "", failure.Wrap(
			failure.Unauthenticated,
			"readback credential signature rejected",
			err,
		)
	}
	var claims model.ReadbackCredentialClaims
	if err := internalrpcauth.DecodeCanonicalJSON(
		verified.CanonicalPayload,
		&claims,
	); err != nil {
		return model.ReadbackCredentialClaims{}, "", failure.Wrap(
			failure.Unauthenticated,
			"readback credential claims rejected",
			err,
		)
	}
	now := attestor.now().UTC().Truncate(time.Second)
	if err := internalrpcauth.ValidateTimes(
		now,
		time.Unix(claims.IssuedAt, 0),
		time.Unix(claims.NotBefore, 0),
		time.Unix(claims.ExpiresAt, 0),
		readbackCredentialTTL,
		readbackAllowedClockSkew,
	); err != nil {
		return model.ReadbackCredentialClaims{}, "", failure.Wrap(
			failure.Unauthenticated,
			"readback credential time binding failed",
			err,
		)
	}
	if claims.Version != model.ContractVersion ||
		claims.Issuer != readbackPublisherIssuer ||
		claims.Audience != readbackAudience ||
		record.Issuer != claims.Issuer ||
		record.Generation != claims.SignerGeneration ||
		record.Status != keyStatusCurrent ||
		record.Purpose != readbackCredentialPurpose ||
		!keyAllowsAudience(record, claims.Audience) ||
		now.Before(record.NotBefore) ||
		!now.Before(record.NotAfter) ||
		!uuidPattern.MatchString(claims.JTI) {
		return model.ReadbackCredentialClaims{}, "", failure.New(
			failure.BindingMismatch,
			"readback credential signer binding failed",
		)
	}
	digest := sha256.Sum256([]byte(compact))
	return claims, hex.EncodeToString(digest[:]), nil
}

func bindCredentialToIntent(
	credential model.ReadbackCredentialClaims,
	intent model.ReadbackIntent,
	peerSPIFFEID string,
) error {
	expectedPurpose := "SNAPSHOT_READBACK"
	if intent.Kind == "KEY_DELIVERY" {
		expectedPurpose = "KEY_DELIVERY_READBACK"
	}
	thumbprintKey, err := internalrpcauth.ParsePublicJWK(
		credential.PossessionPublicJWK,
	)
	if err != nil {
		return failure.Wrap(
			failure.Unauthenticated,
			"readback credential possession key rejected",
			err,
		)
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(thumbprintKey)
	if err != nil {
		return failure.Wrap(
			failure.Unauthenticated,
			"readback credential possession thumbprint rejected",
			err,
		)
	}
	if credential.Subject != intent.WorkloadID ||
		credential.Purpose != expectedPurpose ||
		credential.IntentID != intent.IntentID ||
		credential.IntentKind != intent.Kind ||
		credential.IntentRevision != intent.IntentRevision ||
		credential.IntentDigestSHA256 != intent.IntentDigestSHA256 ||
		credential.WorkloadID != intent.WorkloadID ||
		credential.WorkloadSPIFFEID != peerSPIFFEID ||
		credential.WorkloadSPIFFEID != intent.WorkloadSPIFFEID ||
		credential.Role != intent.Role ||
		credential.WorkloadGeneration != intent.WorkloadGeneration ||
		credential.CredentialGeneration != intent.CredentialGeneration ||
		credential.MaterialGeneration != intent.MaterialGeneration ||
		credential.PossessionKeyID != intent.PossessionKeyID ||
		credential.PossessionKeyGeneration != intent.PossessionKeyGeneration ||
		credential.PossessionKeyThumbprint != intent.PossessionKeyThumbprint ||
		thumbprint != intent.PossessionKeyThumbprint {
		return failure.New(
			failure.BindingMismatch,
			"readback credential pinned intent binding failed",
		)
	}
	return nil
}

func bindEvidence(
	evidence model.ReadbackAttestationClaims,
	credential model.ReadbackCredentialClaims,
	credentialDigest string,
	challenge model.ReadbackChallenge,
	peerSPIFFEID string,
) error {
	intent := challenge.Intent
	if evidence.Version != model.ContractVersion ||
		evidence.Issuer != peerSPIFFEID ||
		evidence.Audience != readbackAudience ||
		evidence.Subject != intent.WorkloadID ||
		evidence.JTI == "" ||
		evidence.Purpose != credential.Purpose ||
		evidence.IntentID != intent.IntentID ||
		evidence.IntentKind != intent.Kind ||
		evidence.IntentRevision != intent.IntentRevision ||
		evidence.WorkloadID != intent.WorkloadID ||
		evidence.WorkloadSPIFFEID != peerSPIFFEID ||
		evidence.Role != intent.Role ||
		evidence.WorkloadGeneration != intent.WorkloadGeneration ||
		evidence.CredentialGeneration != intent.CredentialGeneration ||
		evidence.ReadbackCredentialJTI != credential.JTI ||
		evidence.ReadbackCredentialDigest != credentialDigest ||
		evidence.PossessionKeyID != intent.PossessionKeyID ||
		evidence.PossessionKeyGeneration != intent.PossessionKeyGeneration ||
		evidence.PossessionKeyThumbprint != intent.PossessionKeyThumbprint ||
		evidence.SourceRevision != intent.SourceRevision ||
		evidence.ServedStateDigestSHA256 != intent.ServedStateDigestSHA256 ||
		evidence.ChallengeID != challenge.ChallengeID ||
		evidence.ChallengeJTI != challenge.ChallengeJTI ||
		evidence.ChallengeNonce != challenge.Nonce ||
		evidence.ChallengeDigestSHA256 != challenge.DigestSHA256 {
		return failure.New(
			failure.BindingMismatch,
			"readback attestation evidence binding failed",
		)
	}
	return nil
}

func mapReadbackStoreFailure(message string, err error) error {
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return failure.Wrap(failure.NotFound, message, err)
	case errors.Is(err, repository.ErrIdempotencyConflict):
		return failure.Wrap(failure.ReplayDetected, message, err)
	case errors.Is(err, repository.ErrReplay):
		return failure.Wrap(failure.ReplayDetected, message, err)
	case errors.Is(err, repository.ErrExpired):
		return failure.Wrap(failure.Unauthenticated, message, err)
	default:
		return failure.Wrap(failure.PersistenceUnavailable, message, err)
	}
}

func randomNonce() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
