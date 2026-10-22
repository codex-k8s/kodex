package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
)

const (
	// readbackCredentialType pins the protected JWS typ for publisher-issued readback credentials,
	// distinct from workload-signed evidence.
	// readbackCredentialType закрепляет защищённый JWS typ выпущенных издателем readback credentials,
	// отличный от свидетельства с подписью нагрузки.
	readbackCredentialType = "kodex-internal-rpc-readback-credential+jws"
	// readbackAttestationType pins the evidence JWS typ verified using the exact possession key from the
	// signed credential, not the publisher's signing key.
	// readbackAttestationType закрепляет JWS typ свидетельства, проверяемого точным ключом владения из
	// подписанного credential, а не ключом издателя.
	readbackAttestationType = "kodex-internal-rpc-readback-attestation+jws"
	// readbackCredentialPurpose restricts accepted trust records to the readback credential role;
	// credential purpose is separately bound to the resolved intent kind.
	// readbackCredentialPurpose ограничивает принимаемые записи доверия ролью readback credential;
	// назначение самого credential отдельно связывается с разрешённым видом намерения.
	readbackCredentialPurpose = "READBACK_CREDENTIAL"
	// readbackAudience pins the common attestor audience in credentials, challenge digests and evidence,
	// without replacing peer or intent binding.
	// readbackAudience закрепляет общую аудиторию attestor в credentials, хешах запросов и свидетельстве,
	// не заменяя привязки стороны и намерения.
	readbackAudience = "urn:kodex:internal-rpc-authority-readback-attestor"
	// readbackPublisherIssuer is the exact expected publisher SPIFFE issuer for credentials; evidence
	// instead binds its issuer to the observed peer.
	// readbackPublisherIssuer — точный ожидаемый SPIFFE issuer издателя credentials; issuer свидетельства
	// вместо этого связывается с наблюдаемой стороной.
	readbackPublisherIssuer = "spiffe://kodex.local/ns/kodex-system/sa/internal-rpc-authority-publisher"
	// readbackCredentialTTL bounds credential validity checked by verifyCredential; it does not extend a
	// persisted intent or challenge.
	// readbackCredentialTTL ограничивает срок credential, проверяемый verifyCredential; срок сохранённого
	// намерения или запроса не продлевает.
	readbackCredentialTTL = 5 * time.Minute
	// readbackChallengeTTL bounds newly issued challenges and accepted evidence lifetimes; durable
	// one-time consumption remains the store's responsibility.
	// readbackChallengeTTL ограничивает новые запросы и сроки принимаемых свидетельств; устойчивое
	// одноразовое потребление остаётся обязанностью store.
	readbackChallengeTTL = 30 * time.Second
	// readbackAllowedClockSkew supplies the permitted time tolerance to credential and evidence
	// validators, not a retry or lease extension.
	// readbackAllowedClockSkew задаёт допустимое расхождение времени проверкам credential и свидетельства,
	// не повтор или продление lease.
	readbackAllowedClockSkew = 5 * time.Second
	// readbackChallengeFullMethod binds challenge idempotency's semantic digest to its exact RPC, so a
	// method name is not omitted from the hashed request.
	// readbackChallengeFullMethod включает точный RPC в смысловой хеш идемпотентности запроса, не оставляя
	// имя метода за пределами хешируемого ввода.
	readbackChallengeFullMethod = "/internalrpcauthority.v1.AuthorityReadbackAttestorService/IssueAttestationChallenge"
	// readbackAttestationFullMethod separates attestation-consumption semantic digests from challenge
	// issuance, alongside exact challenge, credential and evidence digests.
	// readbackAttestationFullMethod отличает смысловой хеш потребления свидетельства от выпуска запроса
	// вместе с точными хешами challenge, credential и evidence.
	readbackAttestationFullMethod = "/internalrpcauthority.v1.AuthorityReadbackAttestorService/AttestServedState"
)

// ReadbackAttestor verifies independently signed credentials and possession evidence using a locked
// trust view and a durable peer-scoped challenge store. Its in-memory map is not the replay or
// rollback boundary.
// ReadbackAttestor проверяет независимо подписанные credentials и свидетельство владения с защищённой
// блокировкой картой доверия и устойчивым store запросов в области стороны. Карта в памяти не является
// границей повторов или откатов.
type ReadbackAttestor struct {
	mu                 sync.RWMutex
	trust              map[string]VerificationKeyRecord
	trustState         model.ReadbackTrustState
	store              repository.ReadbackStore
	verifierGeneration uint64
	now                func() time.Time
}

// ActivateTrust validates key records and served-state shape, advances the store's forward-only trust
// watermark, then replaces the public-key view and state together under the mutex. Durable acceptance
// precedes the in-memory swap; they are not one cross-store transaction.
// ActivateTrust проверяет записи ключей и форму served state, продвигает устойчивый watermark доверия,
// затем вместе заменяет публичные ключи и состояние под mutex. Устойчивое принятие предшествует замене
// в памяти; общей транзакцией между ними это не является.
func (attestor *ReadbackAttestor) ActivateTrust(
	ctx context.Context,
	trust map[string]VerificationKeyRecord,
	state model.ReadbackTrustState,
) error {
	if err := validateKeyRecords(trust, readbackCredentialPurpose); err != nil {
		return err
	}
	if state.RootID == "" ||
		!digestPattern.MatchString(state.RootFingerprintSHA256) ||
		state.ManifestBundleRevision == 0 ||
		!digestPattern.MatchString(state.ManifestBundleDigestSHA256) ||
		state.TrustSourceRevision == 0 ||
		!digestPattern.MatchString(state.TrustSetDigestSHA256) ||
		state.TrustKeySetRevision == 0 ||
		state.SignerGeneration == 0 ||
		!digestPattern.MatchString(state.PredecessorStateDigestSHA256) ||
		!digestPattern.MatchString(state.ServedStateDigestSHA256) ||
		state.ServedAt.IsZero() {
		return failure.New(
			failure.SnapshotRejected,
			"readback trust state is invalid",
		)
	}
	if err := attestor.store.ActivateReadbackTrust(ctx, state); err != nil {
		return failure.Wrap(
			failure.SnapshotRejected,
			"readback trust watermark rejected",
			err,
		)
	}
	attestor.mu.Lock()
	attestor.trust = publicKeyRecords(trust)
	attestor.trustState = state
	attestor.mu.Unlock()
	return nil
}

// TrustState returns a value copy of the in-memory served trust state under a read lock, without
// rereading or proving current durable readiness.
// TrustState возвращает копию обслуживаемого состояния доверия из памяти под read lock, не перечитывая
// и не доказывая текущую устойчивую готовность.
func (attestor *ReadbackAttestor) TrustState() model.ReadbackTrustState {
	attestor.mu.RLock()
	defer attestor.mu.RUnlock()
	return attestor.trustState
}

// ReadbackChallengeResult carries the store-returned authoritative challenge after issuance or
// idempotent rejoin, rather than the unpersisted random proposal alone.
// ReadbackChallengeResult несёт возвращённый store авторитетный запрос после выпуска либо
// идемпотентного повторного получения, а не только ещё не сохранённое случайное предложение.
type ReadbackChallengeResult struct {
	Challenge model.ReadbackChallenge
}

// ReadbackAttestationResult carries the durable consumption receipt returned by the store; possession
// verification alone does not create this result.
// ReadbackAttestationResult несёт возвращённое store устойчивое подтверждение потребления; одной
// проверки владения для этого результата недостаточно.
type ReadbackAttestationResult struct {
	Receipt model.ReadbackReceipt
}

// NewReadbackAttestor requires nonempty readback-purpose public trust, a store and verifier
// generation, copies public records/audiences and installs the clock. It leaves served trust
// unactivated; it does not persist a watermark or contact the store.
// NewReadbackAttestor требует непустое публичное доверие readback, store и поколение verifier,
// копирует публичные записи с аудиториями и задаёт часы. Обслуживаемое доверие остаётся
// неактивированным; watermark не сохраняется и вызова store нет.
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

// IssueChallenge validates request IDs, verifies the signed credential and resolves the intent inside
// the peer boundary before exact binding. It hashes method/credential/intent semantics, generates
// UUIDs and a random nonce, then lets the store persist or rejoin one bounded challenge; random
// proposals alone are not the returned authority.
// IssueChallenge проверяет ID запроса и подписанный credential, разрешает намерение внутри области
// стороны и проверяет точные привязки. Хеширует смысл метода, credential и намерения, создаёт UUID и
// случайный nonce, затем передаёт store сохранение либо повторное получение ограниченного запроса;
// сами случайные предложения не становятся возвращаемым авторитетным состоянием.
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

// Attest verifies a credential against the peer-scoped stored challenge, proves possession through its
// exact public key, validates evidence time/bindings and hashes exact compact evidence plus request
// semantics. Only the store consumes the challenge and returns a durable receipt under
// idempotency/replay rules.
// Attest проверяет credential против сохранённого запроса в области стороны, доказывает владение
// точным публичным ключом, проверяет сроки и привязки свидетельства и хеширует точный compact evidence
// со смыслом запроса. Только store потребляет запрос и возвращает устойчивый receipt по правилам
// идемпотентности и защиты от повторов.
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

// Ready rejects an unactivated in-memory trust state and asks the store to verify that exact served
// state against durable readiness; mere database reachability is insufficient.
// Ready отклоняет неактивированное состояние доверия в памяти и просит store сверить точное served
// state с устойчивой готовностью; одной доступности БД недостаточно.
func (attestor *ReadbackAttestor) Ready(ctx context.Context) error {
	state := attestor.TrustState()
	if state.ServedStateDigestSHA256 == "" {
		return failure.New(
			failure.PersistenceUnavailable,
			"readback trust is not activated",
		)
	}
	if err := attestor.store.ReadbackReady(ctx, state); err != nil {
		return failure.Wrap(
			failure.PersistenceUnavailable,
			"readback attestor persistence is unavailable",
			err,
		)
	}
	return nil
}

// verifyCredential selects a trusted key from the protected header, verifies canonical JWS and strict
// claims, then checks time, contract, fixed issuer/audience, current key purpose/generation/lifetime
// and UUID JTI. It returns the exact compact-envelope SHA-256; intent/peer binding and replay
// acceptance happen separately.
// verifyCredential выбирает доверенный ключ по защищённому заголовку, проверяет канонический JWS и
// строгие claims, затем сроки, контракт, фиксированные issuer/audience, назначение, поколение и срок
// CURRENT ключа и UUID JTI. Возвращает SHA-256 точного compact-конверта; привязки намерения и стороны
// и принятие без повтора выполняются отдельно.
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
	attestor.mu.RLock()
	record, ok := attestor.trust[header.KeyID]
	attestor.mu.RUnlock()
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

// bindCredentialToIntent binds credential subject/purpose, exact intent revision/digest,
// workload/peer/role/generations and possession-key metadata to the resolved intent, verifying the
// public-key thumbprint. It does not load, consume or extend that intent.
// bindCredentialToIntent связывает субъект и назначение credential, точные ревизию и хеш намерения,
// нагрузку, сторону, роль, поколения и метаданные ключа владения с разрешённым намерением, проверяя
// отпечаток публичного ключа. Намерение не загружает, не потребляет и не продлевает.
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

// bindEvidence compares evidence contract/peer/subject/purpose, intent and credential bindings,
// possession metadata, served revision/digest and exact challenge ID/JTI/nonce/digest. It requires a
// nonempty evidence JTI, not UUID syntax; signature, time and durable consumption are separate
// caller/store checks.
// bindEvidence сравнивает контракт, сторону, субъект и назначение свидетельства, привязки намерения и
// credential, метаданные владения, обслуживаемые ревизию и хеш и точные ID, JTI, nonce и хеш запроса.
// Требует непустой JTI свидетельства, не синтаксис UUID; подпись, сроки и устойчивое потребление
// проверяются отдельно вызывающим и store.
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

// mapReadbackStoreFailure maps missing state, idempotency/replay and expiry to closed domain failures;
// unknown store errors become persistence-unavailable while retaining their cause.
// mapReadbackStoreFailure переводит отсутствие состояния, конфликт идемпотентности, повтор и истечение
// в закрытые доменные ошибки; неизвестная ошибка store становится persistence-unavailable с
// сохранением причины.
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

// randomNonce reads 32 cryptographically random bytes and returns unpadded URL-safe Base64,
// propagating entropy-source failure rather than creating a fallback nonce.
// randomNonce читает 32 криптографически случайных байта и возвращает URL-safe Base64 без padding,
// передавая ошибку источника энтропии вместо запасного nonce.
func randomNonce() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}
