package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/repository"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types"
	"github.com/codex-k8s/kodex/services/internal/internal-rpc-authority/internal/domain/types/value"
)

const (
	// contextKeyPurpose restricts context-verification records to authorization-context signing use, distinct from proof keys or possession of a public key alone.
	// contextKeyPurpose ограничивает записи проверки контекста назначением подписи authorization context отдельно от proof keys или одного наличия публичного ключа.
	contextKeyPurpose = "AUTHORIZATION_CONTEXT"
	// proofKeyPurpose marks the trusted upstream authority-proof signer purpose required before issuing a fresh context; it does not authorize an operation independently of its binding.
	// proofKeyPurpose обозначает назначение доверенного upstream-подписанта authority proof до выдачи свежего контекста; операция без своей привязки не разрешается.
	proofKeyPurpose = "AUTHORITY_PROOF"
	// keyStatusCurrent is the active signer status required for issuing from a proof and for the policy signer; verification may separately allow previous context keys.
	// keyStatusCurrent — активный статус подписанта для выдачи по proof и подписанта политики; проверка контекста отдельно может допускать предыдущие ключи.
	keyStatusCurrent = "CURRENT"
	// keyStatusPrevious permits explicitly retained verification keys on the context-read path, not new issuance from a previous proof signer.
	// keyStatusPrevious допускает явно сохранённые ключи проверки на пути чтения контекста, но не новую выдачу от предыдущего подписанта proof.
	keyStatusPrevious = "PREVIOUS"
	// maxProofTTL bounds upstream proof claims to fifteen seconds during issuance time validation; authorization-context lifetime is governed separately by the operation binding.
	// maxProofTTL ограничивает claims upstream proof пятнадцатью секундами при проверке времени выдачи; срок authorization context отдельно задаётся привязкой операции.
	maxProofTTL = 15 * time.Second
	// authorizationMetadataLKGWindow caps an accepted database-observed metadata lease remainder at thirty seconds. It does not permit offline issuance when the authoritative freshness read fails.
	// authorizationMetadataLKGWindow ограничивает принимаемый остаток наблюдённой БД аренды метаданных тридцатью секундами. При отказе авторитетного чтения свежести автономная выдача не разрешается.
	authorizationMetadataLKGWindow = 30 * time.Second
)

var (
	// operationPattern accepts the lowercase dotted/hyphenated operation grammar used in policy and continuation lineage; matching text is not registration or permission.
	// operationPattern допускает строчную грамматику operation с точками и дефисами для политики и происхождения continuation; совпадение текста не означает регистрацию и разрешение.
	operationPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`)
	// digestPattern accepts exactly sixty-four lowercase hexadecimal characters for SHA-256-shaped bindings without computing or authenticating the referenced digest.
	// digestPattern допускает ровно шестьдесят четыре строчных hex-символа привязки вида SHA-256 без вычисления или аутентификации указанного дайджеста.
	digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
	// uuidPattern accepts lowercase RFC-variant UUID-shaped identifiers with version digits one through eight; resource existence and ownership are checked elsewhere.
	// uuidPattern допускает строчные UUID-подобные идентификаторы с вариантом RFC и цифрой версии от одного до восьми; существование и владение ресурсом проверяются отдельно.
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// Authority holds one policy/key snapshot and durable-store port for context issuance, continuation and verification. Its mutex protects local activation/lease bookkeeping, not durable replay authority or the store's transaction lifecycle.
// Authority хранит снимок политики и ключей и порт устойчивого хранилища для выдачи, continuation и проверки контекста. Mutex защищает локальный учёт активации и аренды, а не устойчивые полномочия replay или жизненный цикл транзакций store.
type Authority struct {
	policy               model.PolicySnapshot
	bindings             map[string]model.OperationBinding
	signingKey           internalrpcauth.ES256Key
	verificationKeys     map[string]VerificationKeyRecord
	proofKeys            map[string]VerificationKeyRecord
	store                repository.Store
	now                  func() time.Time
	activationMu         sync.RWMutex
	attestationReceiptID string
	metadataValidUntil   time.Time
	metadataReceiptID    string
}

// KeyMaterial supplies an optional private context signer and purpose-scoped verification/proof records. NewAuthority validates their policy relationship and stores public-only verification views.
// KeyMaterial предоставляет необязательный закрытый подписант контекста и записи проверки и proof с назначениями. NewAuthority проверяет связь с политикой и сохраняет публичные представления проверяющих ключей.
type KeyMaterial struct {
	SigningKey       internalrpcauth.ES256Key
	VerificationKeys map[string]VerificationKeyRecord
	ProofKeys        map[string]VerificationKeyRecord
}

// VerificationKeyRecord binds one key to issuer, generation, lifecycle status, purpose, audiences and validity interval. The record is configuration evidence, not an independently authenticated request actor.
// VerificationKeyRecord связывает ключ с издателем, поколением, статусом, назначением, аудиториями и сроком. Запись является материалом конфигурации, а не самостоятельно аутентифицированным actor запроса.
type VerificationKeyRecord struct {
	Key        internalrpcauth.ES256Key
	Issuer     string
	Generation uint64
	Status     string
	Purpose    string
	Audiences  map[string]struct{}
	NotBefore  time.Time
	NotAfter   time.Time
}

// NewAuthority validates contract/ABI, DENY policy, bounded lifetimes, source/predecessor pins, current policy signer and distinct operation bindings. It requires proof keys when private signing is enabled and clones public verification/audience views; it does not activate the snapshot or query durable freshness.
// NewAuthority проверяет контракт и ABI, политику DENY, ограниченные сроки, привязки источника и предшественника, текущего подписанта политики и уникальные привязки операций. При закрытой подписи требуются proof keys; копируются публичные представления проверки и аудиторий, но снимок не активируется и устойчивая свежесть не читается.
func NewAuthority(
	policy model.PolicySnapshot,
	keys KeyMaterial,
	store repository.Store,
) (*Authority, error) {
	if policy.Version != model.ContractVersion ||
		policy.AuthorityABIVersion != model.AuthorityABIVersion ||
		policy.DefaultDecision != "DENY" ||
		policy.TokenTTLSeconds <= 0 ||
		policy.TokenTTLSeconds > 30 ||
		policy.AllowedClockSkewSeconds < 0 ||
		policy.AllowedClockSkewSeconds > 5 ||
		policy.SourceRevision == 0 ||
		(policy.SourceRevision == 1 &&
			(policy.PredecessorRevision != 0 ||
				policy.PredecessorDigestSHA256 != strings.Repeat("0", 64))) ||
		(policy.SourceRevision > 1 &&
			(policy.PredecessorRevision != policy.SourceRevision-1 ||
				!digestPattern.MatchString(policy.PredecessorDigestSHA256))) ||
		policy.KeySetRevision == 0 ||
		policy.PolicyRevision == 0 ||
		policy.SignerGeneration == 0 ||
		!digestPattern.MatchString(policy.SourceDigestSHA256) ||
		policy.Issuer == "" ||
		policy.SignerKeyID == "" ||
		store == nil ||
		len(keys.VerificationKeys) == 0 ||
		(keys.SigningKey.Private != nil && len(keys.ProofKeys) == 0) {
		return nil, failure.New(failure.SnapshotRejected, "invalid authority policy snapshot")
	}
	if keys.SigningKey.Private != nil && keys.SigningKey.KeyID != policy.SignerKeyID {
		return nil, failure.New(failure.SnapshotRejected, "signing key does not match policy")
	}
	signerRecord, ok := keys.VerificationKeys[policy.SignerKeyID]
	if !ok ||
		signerRecord.Status != keyStatusCurrent ||
		signerRecord.Generation != policy.SignerGeneration ||
		signerRecord.Issuer != policy.Issuer ||
		signerRecord.Purpose != contextKeyPurpose {
		return nil, failure.New(failure.SnapshotRejected, "policy signing key is not trusted")
	}
	if err := validateKeyRecords(keys.VerificationKeys, contextKeyPurpose); err != nil {
		return nil, failure.Wrap(failure.SnapshotRejected, "authorization key registry rejected", err)
	}
	if err := validateKeyRecords(keys.ProofKeys, proofKeyPurpose); err != nil {
		return nil, failure.Wrap(failure.SnapshotRejected, "authority proof key registry rejected", err)
	}
	bindings := make(map[string]model.OperationBinding, len(policy.OperationBindings))
	for _, binding := range policy.OperationBindings {
		if !operationPattern.MatchString(binding.OperationID) ||
			binding.TokenTTLSeconds <= 0 ||
			binding.TokenTTLSeconds > policy.TokenTTLSeconds ||
			binding.FullMethod == "" ||
			binding.Permission == "" ||
			binding.CallerSPIFFEID == "" ||
			binding.TargetSPIFFEID == "" ||
			(binding.Continuation == nil &&
				(binding.AuthorityProofIssuer == "" || binding.AuthorityProofAudience == "")) ||
			(binding.Continuation != nil &&
				(binding.AuthorityProofIssuer != "" || binding.AuthorityProofAudience != "")) ||
			len(binding.AuthoritySources) == 0 ||
			!validRequestProfile(binding.RequestProfile) ||
			binding.Continuation != nil &&
				(!operationPattern.MatchString(binding.Continuation.ParentOperationID) || binding.Continuation.ParentFullMethod == "") {
			return nil, failure.New(failure.SnapshotRejected, "invalid operation binding")
		}
		if _, duplicate := bindings[binding.OperationID]; duplicate {
			return nil, failure.New(failure.SnapshotRejected, "duplicate operation binding")
		}
		bindings[binding.OperationID] = binding
	}
	return &Authority{
		policy:           policy,
		bindings:         bindings,
		signingKey:       keys.SigningKey,
		verificationKeys: publicKeyRecords(keys.VerificationKeys),
		proofKeys:        publicKeyRecords(keys.ProofKeys),
		store:            store,
		now:              time.Now,
	}, nil
}

// Issue requires fresh metadata, verifies a current purpose-bound proof and its request/caller/authority bindings, then reserves that proof before creating, signing and registering a fresh context. Later failure does not undo the proof reservation and returns no usable token; this path uses binding TTL rather than continuation's parent-expiry clamp.
// Issue требует свежие метаданные, проверяет текущий proof с назначением и его привязки запроса, caller и authority, затем резервирует proof до создания, подписи и регистрации свежего контекста. Поздний отказ не отменяет reservation и не выдаёт пригодный токен; используется TTL привязки, а не ограничение сроком родителя continuation.
func (authority *Authority) Issue(
	ctx context.Context,
	operationID string,
	proofCompact string,
	requestDigest string,
) (string, model.AuthorizationClaims, error) {
	now := authority.now().UTC()
	_, err := authority.freshMetadataDeadline(ctx)
	if err != nil {
		return "", model.AuthorizationClaims{}, err
	}
	binding, ok := authority.bindings[operationID]
	if !ok || binding.Continuation != nil || binding.Issuer != authority.policy.Issuer || binding.CallerSPIFFEID != authority.policy.Issuer {
		return "", model.AuthorizationClaims{}, failure.New(
			failure.OperationNotAllowed,
			"operation is not allowed",
		)
	}
	header, err := internalrpcauth.ParseProtectedHeader(proofCompact)
	if err != nil || header.Type != internalrpcauth.AuthorityProofProtectedType {
		return "", model.AuthorizationClaims{}, failure.Wrap(
			failure.Unauthenticated,
			"authority proof protected header failed",
			err,
		)
	}
	proofKey, ok := authority.proofKeys[header.KeyID]
	if !ok {
		return "", model.AuthorizationClaims{}, failure.New(
			failure.Unauthenticated,
			"authority proof key is not trusted",
		)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		proofCompact,
		proofKey.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  internalrpcauth.AuthorityProofProtectedType,
			KeyID: header.KeyID,
		},
	)
	if err != nil {
		return "", model.AuthorizationClaims{}, failure.Wrap(
			failure.Unauthenticated,
			"authority proof verification failed",
			err,
		)
	}
	var proof model.AuthorityProof
	if err := internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &proof); err != nil {
		return "", model.AuthorizationClaims{}, failure.Wrap(
			failure.Unauthenticated,
			"authority proof claims are invalid",
			err,
		)
	}
	if err := internalrpcauth.ValidateTimes(
		now,
		time.Unix(proof.IssuedAt, 0),
		time.Unix(proof.NotBefore, 0),
		time.Unix(proof.ExpiresAt, 0),
		maxProofTTL,
		time.Duration(authority.policy.AllowedClockSkewSeconds)*time.Second,
	); err != nil {
		return "", model.AuthorizationClaims{}, failure.Wrap(
			failure.Unauthenticated,
			"authority proof time binding failed",
			err,
		)
	}
	if proof.Version != model.ContractVersion ||
		proof.AuthorityABIVersion != model.AuthorityABIVersion ||
		proof.OperationID != operationID ||
		proof.RequestBindingMode != binding.RequestProfile.Mode ||
		proof.RequestDigestSHA256 != requestDigest ||
		!validRequestDigest(binding.RequestProfile.Mode, requestDigest) ||
		proof.Issuer != binding.AuthorityProofIssuer ||
		proof.Audience != binding.AuthorityProofAudience ||
		proof.AuthorizationContextAudience != binding.Audience ||
		proof.Caller.WorkloadID != binding.CallerWorkloadID ||
		proof.Caller.SPIFFEID != binding.CallerSPIFFEID ||
		proof.ProofRevision == 0 ||
		proof.SignerGeneration == 0 ||
		proof.CallerCredentialRevision == 0 ||
		proof.JTI == "" {
		return "", model.AuthorizationClaims{}, failure.New(
			failure.BindingMismatch,
			"authority proof binding failed",
		)
	}
	if proofKey.Issuer != proof.Issuer ||
		proofKey.Generation != proof.SignerGeneration ||
		proofKey.Status != keyStatusCurrent ||
		proofKey.Purpose != proofKeyPurpose ||
		!keyAllowsAudience(proofKey, proof.Audience) ||
		now.Before(proofKey.NotBefore) ||
		!now.Before(proofKey.NotAfter) {
		return "", model.AuthorizationClaims{}, failure.New(
			failure.BindingMismatch,
			"authority proof signer binding failed",
		)
	}
	if err := validateAuthority(proof.Authority, binding); err != nil {
		return "", model.AuthorizationClaims{}, failure.Wrap(
			failure.AuthorityRejected,
			"authority provenance rejected",
			err,
		)
	}
	if !validCredentialAuthentication(
		proof.CredentialAuthentication,
		proof.Authority.ActorKind,
		now,
		time.Duration(authority.policy.AllowedClockSkewSeconds)*time.Second,
	) {
		return "", model.AuthorizationClaims{}, failure.New(
			failure.BindingMismatch,
			"credential authentication binding failed",
		)
	}
	proofDigest := sha256.Sum256([]byte(proofCompact))
	if err := authority.store.Reserve(ctx, repository.Reservation{
		Kind:        repository.ReservationAuthorityProof,
		ScopeID:     binding.CallerWorkloadID,
		OperationID: binding.OperationID,
		Issuer:      proof.Issuer,
		Revision:    proof.ProofRevision,
		JTI:         proof.JTI,
		Digest:      hex.EncodeToString(proofDigest[:]),
		ExpiresAt:   time.Unix(proof.ExpiresAt, 0),
	}); err != nil {
		if !errors.Is(err, repository.ErrReplay) {
			return "", model.AuthorizationClaims{}, failure.Wrap(
				failure.PersistenceUnavailable,
				"authority proof replay store unavailable",
				err,
			)
		}
		return "", model.AuthorizationClaims{}, failure.Wrap(
			failure.ReplayDetected,
			"authority proof replay rejected",
			err,
		)
	}
	jti, err := newUUID()
	if err != nil {
		return "", model.AuthorizationClaims{}, failure.Wrap(
			failure.Internal,
			"create authorization context identifier",
			err,
		)
	}
	expiresAt := now.Truncate(time.Second).Add(time.Duration(binding.TokenTTLSeconds) * time.Second)
	claims := model.AuthorizationClaims{
		Version:  model.ContractVersion,
		Issuer:   binding.Issuer,
		Audience: binding.Audience,
		Subject:  binding.CallerSPIFFEID,
		Caller:   proof.Caller,
		Target: model.Workload{
			WorkloadID: binding.TargetWorkloadID,
			SPIFFEID:   binding.TargetSPIFFEID,
		},
		FullMethod:               binding.FullMethod,
		OperationID:              binding.OperationID,
		Authority:                proof.Authority,
		Permission:               binding.Permission,
		JTI:                      jti,
		IssuedAt:                 now.Unix(),
		NotBefore:                now.Unix(),
		ExpiresAt:                expiresAt.Unix(),
		ReplayMode:               model.ReplayModeOneTime,
		SourceRevision:           authority.policy.SourceRevision,
		SourceDigestSHA256:       authority.policy.SourceDigestSHA256,
		KeySetRevision:           authority.policy.KeySetRevision,
		PolicyRevision:           authority.policy.PolicyRevision,
		SignerGeneration:         authority.policy.SignerGeneration,
		CallerCredentialRevision: proof.CallerCredentialRevision,
		CredentialAuthentication: proof.CredentialAuthentication,
		RequestDigestSHA256:      requestDigest,
		RequestBindingMode:       binding.RequestProfile.Mode,
		AuthorityABIVersion:      model.AuthorityABIVersion,
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		claims,
		authority.signingKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  internalrpcauth.AuthorizationContextProtectedType,
			KeyID: authority.policy.SignerKeyID,
		},
	)
	if err != nil {
		return "", model.AuthorizationClaims{}, failure.Wrap(
			failure.Internal,
			"sign authorization context",
			err,
		)
	}
	if err := authority.registerIssuedContext(ctx, compact, claims, ""); err != nil {
		return "", model.AuthorizationClaims{}, err
	}
	return compact, claims, nil
}

// IssueContinuation verifies a configured parent JWS, inherits its root authority and credential authentication, bounds expiry by metadata and parent, and derives the child JTI from parent/operation/request. It signs, requires durable accepted-parent/child reservation and registers the exact parent digest before returning; any failure returns no token without a new identity retry.
// IssueContinuation проверяет настроенный parent JWS, наследует корневые полномочия и credential authentication, ограничивает срок метаданными и родителем и выводит child JTI из parent, operation и request. После подписи требуются устойчивые accepted-parent и child reservation и регистрация точного дайджеста родителя; при отказе токен не выдаётся и новая идентичность не создаётся.
func (authority *Authority) IssueContinuation(
	ctx context.Context,
	operationID, parentCompact, requestID, correlationID, requestDigest string,
) (string, model.AuthorizationClaims, error) {
	now := authority.now().UTC()
	metadataValidUntil, err := authority.freshMetadataDeadline(ctx)
	if err != nil {
		return "", model.AuthorizationClaims{}, err
	}
	binding, ok := authority.bindings[operationID]
	if !ok || binding.Continuation == nil || binding.Issuer != authority.policy.Issuer || binding.CallerSPIFFEID != authority.policy.Issuer || !validRequestDigest(binding.RequestProfile.Mode, requestDigest) ||
		!uuidPattern.MatchString(requestID) || correlationID == "" || len(correlationID) > 128 {
		return "", model.AuthorizationClaims{}, failure.New(failure.OperationNotAllowed, "continuation operation is not allowed")
	}
	parent, parentBinding, parentDigest, err := authority.decodeContinuationParent(parentCompact, now)
	if err != nil {
		return "", model.AuthorizationClaims{}, err
	}
	profile := binding.Continuation
	if parent.OperationID != profile.ParentOperationID || parent.FullMethod != profile.ParentFullMethod ||
		parentBinding.OperationID != profile.ParentOperationID ||
		parent.Target.WorkloadID != binding.CallerWorkloadID || parent.Target.SPIFFEID != binding.CallerSPIFFEID {
		return "", model.AuthorizationClaims{}, failure.New(failure.BindingMismatch, "continuation parent binding failed")
	}
	rootJTI, rootOperation, rootMethod := parent.JTI, parent.OperationID, parent.FullMethod
	rootRevision, rootDigest := parent.SourceRevision, parent.SourceDigestSHA256
	if parent.Continuation != nil {
		rootJTI, rootOperation, rootMethod = parent.Continuation.RootJTI, parent.Continuation.RootOperationID, parent.Continuation.RootFullMethod
		rootRevision, rootDigest = parent.Continuation.RootSourceRevision, parent.Continuation.RootSourceDigestSHA256
	}
	expiresAt := authorizationExpiry(now, time.Duration(binding.TokenTTLSeconds)*time.Second, metadataValidUntil)
	if parent.ExpiryTime().Before(expiresAt) {
		expiresAt = parent.ExpiryTime()
	}
	claims := model.AuthorizationClaims{
		Version: model.ContractVersion, Issuer: binding.Issuer, Audience: binding.Audience,
		Subject: binding.CallerSPIFFEID, Caller: model.Workload{WorkloadID: binding.CallerWorkloadID, SPIFFEID: binding.CallerSPIFFEID},
		Target:     model.Workload{WorkloadID: binding.TargetWorkloadID, SPIFFEID: binding.TargetSPIFFEID},
		FullMethod: binding.FullMethod, OperationID: binding.OperationID, Authority: parent.Authority,
		Permission: binding.Permission, JTI: deterministicContinuationJTI(parent.JTI, operationID, requestID),
		IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: expiresAt.Unix(), ReplayMode: model.ReplayModeOneTime,
		SourceRevision: authority.policy.SourceRevision, SourceDigestSHA256: authority.policy.SourceDigestSHA256,
		KeySetRevision: authority.policy.KeySetRevision, PolicyRevision: authority.policy.PolicyRevision,
		SignerGeneration: authority.policy.SignerGeneration, CallerCredentialRevision: parent.CallerCredentialRevision,
		CredentialAuthentication: parent.CredentialAuthentication, RequestDigestSHA256: requestDigest,
		RequestBindingMode: binding.RequestProfile.Mode, AuthorityABIVersion: model.AuthorityABIVersion,
		Continuation: &model.ContinuationLineage{
			RootJTI: rootJTI, RootOperationID: rootOperation, RootFullMethod: rootMethod,
			RootSourceRevision: rootRevision, RootSourceDigestSHA256: rootDigest,
			ParentJTI: parent.JTI, ParentOperationID: parent.OperationID, ParentFullMethod: parent.FullMethod,
			RequestID: requestID, CorrelationID: correlationID,
		},
	}
	compact, err := internalrpcauth.SignCanonicalJSON(claims, authority.signingKey, internalrpcauth.ProtectedHeaderExpectation{Type: internalrpcauth.AuthorizationContextProtectedType, KeyID: authority.policy.SignerKeyID})
	if err != nil {
		return "", model.AuthorizationClaims{}, failure.Wrap(failure.Internal, "sign continuation context", err)
	}
	childDigest := sha256.Sum256([]byte(compact))
	if err := authority.store.ReserveContinuation(ctx,
		repository.Reservation{Kind: repository.ReservationAuthorizationContext, ScopeID: binding.CallerWorkloadID, JTI: parent.JTI, Digest: parentDigest, ExpiresAt: parent.ExpiryTime()},
		repository.Reservation{Kind: repository.ReservationAuthorityProof, ScopeID: binding.CallerWorkloadID, OperationID: operationID, Issuer: binding.Issuer, Revision: parent.SourceRevision, JTI: claims.JTI, Digest: hex.EncodeToString(childDigest[:]), ExpiresAt: expiresAt},
	); err != nil {
		if errors.Is(err, repository.ErrReplay) || errors.Is(err, repository.ErrParentNotAccepted) {
			return "", model.AuthorizationClaims{}, failure.Wrap(failure.ReplayDetected, "continuation replay rejected", err)
		}
		return "", model.AuthorizationClaims{}, failure.Wrap(failure.PersistenceUnavailable, "continuation replay store unavailable", err)
	}
	if err := authority.registerIssuedContext(ctx, compact, claims, parentDigest); err != nil {
		return "", model.AuthorizationClaims{}, err
	}
	return compact, claims, nil
}

// decodeContinuationParent verifies canonical context JWS, configured key/purpose/status/audience, claim lifetime and exact operation/policy/provenance bindings, returning claims plus compact-byte digest. It does not consume the parent replay reservation; the continuation store path proves prior acceptance.
// decodeContinuationParent проверяет канонический context JWS, настроенные ключ, назначение, статус и аудиторию, время claims и точные привязки операции, политики и происхождения, возвращая claims и дайджест compact-байтов. Replay reservation родителя не потребляется; прежнее принятие доказывает путь continuation store.
func (authority *Authority) decodeContinuationParent(compact string, now time.Time) (model.AuthorizationClaims, model.OperationBinding, string, error) {
	header, err := internalrpcauth.ParseProtectedHeader(compact)
	if err != nil || header.Type != internalrpcauth.AuthorizationContextProtectedType {
		return model.AuthorizationClaims{}, model.OperationBinding{}, "", failure.Wrap(failure.Unauthenticated, "continuation parent header failed", err)
	}
	key, ok := authority.verificationKeys[header.KeyID]
	if !ok {
		return model.AuthorizationClaims{}, model.OperationBinding{}, "", failure.New(failure.Unauthenticated, "continuation parent key is not trusted")
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(compact, key.Key, internalrpcauth.ProtectedHeaderExpectation{Type: internalrpcauth.AuthorizationContextProtectedType, KeyID: header.KeyID})
	if err != nil {
		return model.AuthorizationClaims{}, model.OperationBinding{}, "", failure.Wrap(failure.Unauthenticated, "continuation parent verification failed", err)
	}
	var claims model.AuthorizationClaims
	if err := internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims); err != nil {
		return model.AuthorizationClaims{}, model.OperationBinding{}, "", failure.Wrap(failure.Unauthenticated, "continuation parent claims failed", err)
	}
	binding, ok := authority.bindings[claims.OperationID]
	if !ok || validateAuthorizationTimes(now, claims, binding, authority.policy.AllowedClockSkewSeconds) != nil ||
		claims.Version != model.ContractVersion || claims.AuthorityABIVersion != model.AuthorityABIVersion || claims.ReplayMode != model.ReplayModeOneTime ||
		claims.Issuer != binding.Issuer || key.Issuer != claims.Issuer || key.Generation != claims.SignerGeneration ||
		(key.Status != keyStatusCurrent && key.Status != keyStatusPrevious) || key.Purpose != contextKeyPurpose || !keyAllowsAudience(key, claims.Audience) ||
		claims.Audience != binding.Audience || claims.Subject != binding.CallerSPIFFEID || claims.Caller.WorkloadID != binding.CallerWorkloadID ||
		claims.Caller.SPIFFEID != binding.CallerSPIFFEID || claims.Target.WorkloadID != binding.TargetWorkloadID || claims.Target.SPIFFEID != binding.TargetSPIFFEID ||
		claims.FullMethod != binding.FullMethod || claims.Permission != binding.Permission || claims.SourceRevision != authority.policy.SourceRevision ||
		claims.SourceDigestSHA256 != authority.policy.SourceDigestSHA256 || claims.KeySetRevision != authority.policy.KeySetRevision ||
		claims.PolicyRevision != authority.policy.PolicyRevision || claims.RequestBindingMode != binding.RequestProfile.Mode ||
		!validRequestDigest(binding.RequestProfile.Mode, claims.RequestDigestSHA256) || !uuidPattern.MatchString(claims.JTI) {
		return model.AuthorizationClaims{}, model.OperationBinding{}, "", failure.New(failure.BindingMismatch, "continuation parent binding failed")
	}
	if err := validateAuthority(claims.Authority, binding); err != nil {
		return model.AuthorizationClaims{}, model.OperationBinding{}, "", failure.Wrap(failure.AuthorityRejected, "continuation parent authority failed", err)
	}
	digest := sha256.Sum256([]byte(compact))
	return claims, binding, hex.EncodeToString(digest[:]), nil
}

// validCredentialAuthentication accepts absent optional metadata; present metadata requires HUMAN, nonfuture positive authentication time, bounded trimmed ACR and distinct nonempty bounded AMR entries. It validates supplied shape, not an OIDC session or MFA event.
// validCredentialAuthentication допускает отсутствие необязательных метаданных; присутствующие требуют HUMAN, положительное не-будущее время входа, ограниченный ACR без внешних пробелов и уникальные непустые ограниченные AMR. Проверяется форма, а не OIDC-сессия или событие MFA.
func validCredentialAuthentication(value *model.CredentialAuthentication, actorKind string, now time.Time, clockSkew time.Duration) bool {
	if value == nil {
		return true
	}
	if actorKind != "HUMAN" || value.AuthenticatedAt <= 0 || time.Unix(value.AuthenticatedAt, 0).After(now.Add(clockSkew)) ||
		strings.TrimSpace(value.ACR) != value.ACR || len(value.ACR) > 128 || len(value.AMR) > 16 {
		return false
	}
	seen := make(map[string]struct{}, len(value.AMR))
	for _, method := range value.AMR {
		if method == "" || strings.TrimSpace(method) != method || len(method) > 64 {
			return false
		}
		if _, duplicate := seen[method]; duplicate {
			return false
		}
		seen[method] = struct{}{}
	}
	return true
}

// Verify requires fresh durable metadata, validates canonical JWS/key/time and exact operation/request/policy/caller/target bindings, then atomically accepts the snapshot and reserves replay through the store. Observed peer is compared to claims.Caller despite the parameter name; no claims are returned on persistence or replay refusal.
// Verify требует свежие устойчивые метаданные, проверяет канонический JWS, ключ, время и точные привязки операции, запроса, политики, caller и target, затем атомарно принимает снимок и резервирует replay через store. Наблюдённый peer сравнивается с claims.Caller вопреки имени параметра; при отказе хранения или replay claims не возвращаются.
func (authority *Authority) Verify(
	ctx context.Context,
	compact string,
	observedFullMethod string,
	downstreamSPIFFEID string,
	observedRequestDigest string,
) (model.AuthorizationClaims, error) {
	if _, err := authority.freshMetadataDeadline(ctx); err != nil {
		return model.AuthorizationClaims{}, err
	}
	header, err := internalrpcauth.ParseProtectedHeader(compact)
	if err != nil || header.Type != internalrpcauth.AuthorizationContextProtectedType {
		return model.AuthorizationClaims{}, failure.Wrap(
			failure.Unauthenticated,
			"authorization context protected header failed",
			err,
		)
	}
	verificationKey, ok := authority.verificationKeys[header.KeyID]
	if !ok {
		return model.AuthorizationClaims{}, failure.New(
			failure.Unauthenticated,
			"authorization context key is not trusted",
		)
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(
		compact,
		verificationKey.Key,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  internalrpcauth.AuthorizationContextProtectedType,
			KeyID: header.KeyID,
		},
	)
	if err != nil {
		return model.AuthorizationClaims{}, failure.Wrap(
			failure.Unauthenticated,
			"authorization context verification failed",
			err,
		)
	}
	var claims model.AuthorizationClaims
	if err := internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims); err != nil {
		return model.AuthorizationClaims{}, failure.Wrap(
			failure.Unauthenticated,
			"authorization claims are invalid",
			err,
		)
	}
	binding, ok := authority.bindings[claims.OperationID]
	if !ok {
		return model.AuthorizationClaims{}, failure.New(
			failure.OperationNotAllowed,
			"operation is not allowed",
		)
	}
	now := authority.now().UTC()
	if err := validateAuthorizationTimes(now, claims, binding, authority.policy.AllowedClockSkewSeconds); err != nil {
		return model.AuthorizationClaims{}, failure.Wrap(
			failure.Unauthenticated,
			"authorization context time binding failed",
			err,
		)
	}
	if claims.Version != model.ContractVersion ||
		claims.AuthorityABIVersion != model.AuthorityABIVersion ||
		claims.RequestBindingMode != binding.RequestProfile.Mode ||
		claims.RequestDigestSHA256 != observedRequestDigest ||
		!validRequestDigest(binding.RequestProfile.Mode, observedRequestDigest) ||
		claims.ReplayMode != model.ReplayModeOneTime ||
		claims.Issuer != binding.Issuer ||
		verificationKey.Issuer != claims.Issuer ||
		verificationKey.Generation != claims.SignerGeneration ||
		(verificationKey.Status != keyStatusCurrent &&
			verificationKey.Status != keyStatusPrevious) ||
		verificationKey.Purpose != contextKeyPurpose ||
		!keyAllowsAudience(verificationKey, claims.Audience) ||
		now.Before(verificationKey.NotBefore) ||
		!now.Before(verificationKey.NotAfter) ||
		claims.Audience != binding.Audience ||
		claims.Subject != binding.CallerSPIFFEID ||
		claims.Caller.WorkloadID != binding.CallerWorkloadID ||
		claims.Caller.SPIFFEID != binding.CallerSPIFFEID ||
		claims.Target.WorkloadID != binding.TargetWorkloadID ||
		claims.Target.SPIFFEID != binding.TargetSPIFFEID ||
		!matchesObservedCaller(claims, downstreamSPIFFEID) ||
		claims.FullMethod != binding.FullMethod ||
		claims.FullMethod != observedFullMethod ||
		claims.Permission != binding.Permission ||
		claims.SourceRevision != authority.policy.SourceRevision ||
		claims.SourceDigestSHA256 != authority.policy.SourceDigestSHA256 ||
		claims.KeySetRevision != authority.policy.KeySetRevision ||
		claims.PolicyRevision != authority.policy.PolicyRevision ||
		claims.JTI == "" ||
		(binding.Continuation == nil) != (claims.Continuation == nil) ||
		claims.Continuation != nil && !validContinuation(*claims.Continuation, binding) {
		return model.AuthorizationClaims{}, failure.New(
			failure.BindingMismatch,
			"authorization context binding failed",
		)
	}
	tokenDigest := sha256.Sum256([]byte(compact))
	if err := authority.store.AcceptVerification(
		ctx,
		authority.SnapshotState(),
		repository.Reservation{
			Kind:             repository.ReservationAuthorizationContext,
			CallerWorkloadID: claims.Caller.WorkloadID,
			SignerGeneration: claims.SignerGeneration,
			ScopeID:          binding.TargetWorkloadID,
			JTI:              claims.JTI,
			Digest:           hex.EncodeToString(tokenDigest[:]),
			ExpiresAt:        claims.ExpiryTime(),
		},
	); err != nil {
		switch {
		case errors.Is(err, repository.ErrReplay):
			return model.AuthorizationClaims{}, failure.Wrap(
				failure.ReplayDetected,
				"authorization context replay rejected",
				err,
			)
		case errors.Is(err, repository.ErrSnapshotRollback):
			return model.AuthorizationClaims{}, failure.Wrap(
				failure.SnapshotRejected,
				"snapshot watermark rejected",
				err,
			)
		default:
			return model.AuthorizationClaims{}, failure.Wrap(
				failure.PersistenceUnavailable,
				"authorization context replay store unavailable",
				err,
			)
		}
	}
	return claims, nil
}

// matchesObservedCaller compares the transport-observed SPIFFE identity exactly with the signed caller SPIFFE identity, never with the token's target workload.
// matchesObservedCaller точно сравнивает наблюдённую транспортом SPIFFE-идентичность с подписанной SPIFFE-идентичностью caller, а не target workload токена.
func matchesObservedCaller(
	claims model.AuthorizationClaims,
	observedSPIFFEID string,
) bool {
	return claims.Caller.SPIFFEID == observedSPIFFEID
}

// validateKeyRecords rejects mismatched IDs, absent public or present private material, wrong purpose/status, empty audience sets and invalid validity intervals. An empty registry passes this loop; constructor-level requirements decide whether one is allowed.
// validateKeyRecords отклоняет несовпавшие ID, отсутствие публичного или наличие закрытого материала, неверные назначение и статус, пустые аудитории и неверные интервалы срока. Пустой реестр проходит цикл; допустимость пустоты определяет конструктор.
func validateKeyRecords(
	records map[string]VerificationKeyRecord,
	expectedPurpose string,
) error {
	for keyID, record := range records {
		if keyID == "" ||
			record.Key.KeyID != keyID ||
			record.Key.Public == nil ||
			record.Key.Private != nil ||
			record.Issuer == "" ||
			record.Generation == 0 ||
			record.Purpose != expectedPurpose ||
			(record.Status != "CURRENT" &&
				record.Status != "NEXT" &&
				record.Status != "PREVIOUS") ||
			len(record.Audiences) == 0 ||
			record.NotBefore.IsZero() ||
			record.NotAfter.IsZero() ||
			!record.NotBefore.Before(record.NotAfter) {
			return errors.New("invalid verification key record")
		}
	}
	return nil
}

// publicKeyRecords copies the registry entries into a fresh map, strips private-key references via PublicOnly and clones each audience map. It does not regenerate keys or cryptographically authenticate the records.
// publicKeyRecords копирует записи реестра в новую карту, убирает закрытые ссылки через PublicOnly и копирует каждую карту аудиторий. Ключи не генерируются заново и записи криптографически не аутентифицируются.
func publicKeyRecords(
	values map[string]VerificationKeyRecord,
) map[string]VerificationKeyRecord {
	result := make(map[string]VerificationKeyRecord, len(values))
	for keyID, value := range values {
		value.Key = value.Key.PublicOnly()
		audiences := make(map[string]struct{}, len(value.Audiences))
		for audience := range value.Audiences {
			audiences[audience] = struct{}{}
		}
		value.Audiences = audiences
		result[keyID] = value
	}
	return result
}

// keyAllowsAudience performs exact membership lookup in the record's audience set; it neither normalizes audience strings nor validates other key properties.
// keyAllowsAudience точно проверяет членство аудитории в наборе записи без нормализации строки и проверки остальных свойств ключа.
func keyAllowsAudience(record VerificationKeyRecord, audience string) bool {
	_, ok := record.Audiences[audience]
	return ok
}

// ActivateSnapshot validates receipt shape and persists exact snapshot activation before updating the local receipt under a mutex and refreshing metadata. A failed refresh can follow a successful durable activation; it does not roll that state back.
// ActivateSnapshot проверяет форму квитанции и сохраняет активацию точного снимка до обновления локальной квитанции под mutex и чтения свежести. Отказ refresh может следовать за успешной устойчивой активацией и не откатывает её.
func (authority *Authority) ActivateSnapshot(
	ctx context.Context,
	attestationReceiptID string,
) error {
	if !uuidPattern.MatchString(attestationReceiptID) {
		return failure.New(
			failure.SnapshotRejected,
			"snapshot readback receipt is invalid",
		)
	}
	state := authority.SnapshotState()
	state.AttestationReceiptID = attestationReceiptID
	if err := authority.store.ActivateSnapshot(ctx, state); err != nil {
		if errors.Is(err, repository.ErrSnapshotRollback) {
			return failure.Wrap(
				failure.SnapshotRejected,
				"persist served snapshot readback",
				err,
			)
		}
		return failure.Wrap(
			failure.PersistenceUnavailable,
			"persist served snapshot readback",
			err,
		)
	}
	authority.activationMu.Lock()
	authority.attestationReceiptID = attestationReceiptID
	authority.activationMu.Unlock()
	_, err := authority.freshMetadataDeadline(ctx)
	return err
}

// SnapshotState combines fixed policy/history pins with the mutex-protected local receipt ID in a new repository value. It does not query the database or assert that the returned receipt remains fresh.
// SnapshotState объединяет фиксированные привязки политики и истории с локальным ID квитанции под mutex в новом repository value. БД не опрашивается и актуальная свежесть квитанции не утверждается.
func (authority *Authority) SnapshotState() repository.SnapshotState {
	authority.activationMu.RLock()
	receiptID := authority.attestationReceiptID
	authority.activationMu.RUnlock()
	return repository.SnapshotState{
		SourceRevision:          authority.policy.SourceRevision,
		SourceDigestSHA256:      authority.policy.SourceDigestSHA256,
		PredecessorRevision:     authority.policy.PredecessorRevision,
		PredecessorDigestSHA256: authority.policy.PredecessorDigestSHA256,
		KeySetRevision:          authority.policy.KeySetRevision,
		PolicyRevision:          authority.policy.PolicyRevision,
		SignerGeneration:        authority.policy.SignerGeneration,
		History:                 repositoryHistory(authority.policy.History),
		AttestationReceiptID:    receiptID,
	}
}

// repositoryHistory copies revision/digest pairs into a fresh ordered repository slice, preserving empty-as-nonnil and input order without validating a signed history chain.
// repositoryHistory копирует пары ревизии и дайджеста в новый упорядоченный repository slice, сохраняя пустой результат как ненулевой срез и исходный порядок без проверки подписанной цепочки истории.
func repositoryHistory(values []model.RevisionDigest) []repository.RevisionDigest {
	result := make([]repository.RevisionDigest, 0, len(values))
	for _, value := range values {
		result = append(result, repository.RevisionDigest{
			Revision:     value.Revision,
			DigestSHA256: value.DigestSHA256,
		})
	}
	return result
}

// validateAuthority checks the closed actor kinds, UUID-shaped identities/references, bounded positive provenance revisions/digests, allowed sources and exact project-presence policy. It checks signed provenance structure, not a fresh lookup of each domain-owned identity.
// validateAuthority проверяет закрытые actor kinds, UUID-подобные идентичности и ссылки, ограниченные положительные ревизии и дайджесты происхождения, разрешённые источники и точную политику наличия проекта. Проверяется структура подписанного происхождения, а не свежее чтение каждой доменной идентичности.
func validateAuthority(authority model.Authority, binding model.OperationBinding) error {
	allowedActor := map[string]struct{}{
		"HUMAN": {}, "AGENT": {}, "SERVICE": {}, "AUTOMATION": {},
	}
	if _, ok := allowedActor[authority.ActorKind]; !ok {
		return fmt.Errorf("unsupported actor kind")
	}
	allowedSources := make(map[string]struct{}, len(binding.AuthoritySources))
	for _, source := range binding.AuthoritySources {
		allowedSources[source] = struct{}{}
	}
	for _, identity := range []*model.Identity{
		&authority.Actor,
		&authority.Tenant,
		authority.Project,
	} {
		if identity == nil {
			continue
		}
		if !uuidPattern.MatchString(identity.ID) ||
			!uuidPattern.MatchString(identity.Provenance.Reference) ||
			identity.Provenance.Revision == 0 ||
			identity.Provenance.Revision > 9007199254740991 ||
			!digestPattern.MatchString(identity.Provenance.DigestSHA256) {
			return fmt.Errorf("invalid identity provenance")
		}
		if _, ok := allowedSources[identity.Provenance.Source]; !ok {
			return fmt.Errorf("authority source is not allowed")
		}
	}
	if binding.ProjectRequired && authority.Project == nil {
		return fmt.Errorf("project authority is required")
	}
	if !binding.ProjectRequired && authority.Project != nil {
		return fmt.Errorf("project authority is forbidden")
	}
	return nil
}

// validRequestProfile accepts only unary/stream modes and required/forbidden selectors for resource, version, attempt and idempotency. It does not bind a concrete request's values.
// validRequestProfile допускает только режимы unary и stream и селекторы required либо forbidden для resource, version, attempt и idempotency. Значения конкретного запроса не привязываются.
func validRequestProfile(value model.RequestProfile) bool {
	if value.Mode != model.RequestBindingUnary && value.Mode != model.RequestBindingStream {
		return false
	}
	for _, binding := range []string{value.Resource, value.Version, value.Attempt, value.Idempotency} {
		if binding != model.ProfileBindingRequired && binding != model.ProfileBindingForbidden {
			return false
		}
	}
	return true
}

// validRequestDigest requires an empty digest for streams and lowercase SHA-256-shaped text otherwise. Mode registration is a separate policy-profile check, not performed here.
// validRequestDigest требует пустой дайджест для stream, иначе текст вида строчного SHA-256. Регистрация режима проверяется профилем политики отдельно, не здесь.
func validRequestDigest(mode, digest string) bool {
	if mode == model.RequestBindingStream {
		return digest == ""
	}
	return digestPattern.MatchString(digest)
}

// validateAuthorizationTimes rejects already expired claims, uses binding TTL for fresh contexts and a positive shortened TTL capped by the binding for continuations, then applies common time/skew validation.
// validateAuthorizationTimes отклоняет уже просроченные claims, использует TTL привязки для свежих контекстов и положительный сокращённый TTL не выше привязки для continuation, затем применяет общую проверку времени и расхождения часов.
func validateAuthorizationTimes(
	now time.Time,
	claims model.AuthorizationClaims,
	binding model.OperationBinding,
	allowedClockSkewSeconds int64,
) error {
	if !now.Before(claims.ExpiryTime()) {
		return errors.New("authorization context expired")
	}

	maximumTTL := time.Duration(binding.TokenTTLSeconds) * time.Second
	actualTTL := maximumTTL
	if binding.Continuation != nil {
		actualTTL = claims.ExpiryTime().Sub(claims.IssuedTime())
		if actualTTL <= 0 || actualTTL > maximumTTL {
			return errors.New("invalid continuation token lifetime")
		}
	}
	return internalrpcauth.ValidateTimes(
		now,
		claims.IssuedTime(),
		claims.NotBeforeTime(),
		claims.ExpiryTime(),
		actualTTL,
		time.Duration(allowedClockSkewSeconds)*time.Second,
	)
}

// validContinuation checks root/parent/request UUID shapes, configured parent operation/method, root provenance and bounded nonempty correlation. It does not by itself authenticate a parent JWS or prove a delegation edge.
// validContinuation проверяет форму root, parent и request UUID, настроенные operation и method родителя, корневое происхождение и ограниченную непустую корреляцию. Самостоятельно parent JWS не аутентифицируется и ребро делегирования не доказывается.
func validContinuation(value model.ContinuationLineage, binding model.OperationBinding) bool {
	return binding.Continuation != nil && uuidPattern.MatchString(value.RootJTI) &&
		uuidPattern.MatchString(value.ParentJTI) && uuidPattern.MatchString(value.RequestID) &&
		operationPattern.MatchString(value.RootOperationID) && operationPattern.MatchString(value.ParentOperationID) &&
		value.RootFullMethod != "" && value.ParentFullMethod == binding.Continuation.ParentFullMethod &&
		value.ParentOperationID == binding.Continuation.ParentOperationID && value.RootSourceRevision > 0 &&
		digestPattern.MatchString(value.RootSourceDigestSHA256) && value.CorrelationID != "" && len(value.CorrelationID) <= 128
}

// deterministicContinuationJTI preserves SHA-256 of exactly parent, operation and request joined by NUL without a trailing separator, with version-five/RFC-variant formatting. It is stable correlation, not randomness, a grant or standard SHA-1 UUIDv5.
// deterministicContinuationJTI сохраняет SHA-256 ровно parent, operation и request, соединённых NUL без завершающего разделителя, с форматом версии пять и варианта RFC. Это устойчивая корреляция, а не случайность, grant или стандартный SHA-1 UUIDv5.
func deterministicContinuationJTI(parentJTI, operationID, requestID string) string {
	return value.JoinedAuthorityUUID(parentJTI, operationID, requestID)
}

// Ready first requires fresh authoritative metadata, then checks the store's exact snapshot/readiness path. A previously cached lease is not a fallback for a failed freshness read.
// Ready сначала требует свежие авторитетные метаданные, затем проверяет точный путь снимка и готовности store. Прежняя кешированная аренда не заменяет отказавшее чтение свежести.
func (authority *Authority) Ready(ctx context.Context) error {
	if _, err := authority.freshMetadataDeadline(ctx); err != nil {
		return err
	}
	return authority.store.Ready(ctx, authority.SnapshotState())
}

// freshMetadataDeadline always reads durable freshness for an activated snapshot, bounds the DB remainder to thirty seconds and starts the local deadline before the network call. The same receipt cannot extend its cached deadline; local receipt/lease fields are updated under lock before a final expiry check, and store failure is not an offline success.
// freshMetadataDeadline всегда читает устойчивую свежесть активированного снимка, ограничивает остаток БД тридцатью секундами и начинает локальный срок до сетевого вызова. Та же квитанция не продлевает кешированный срок; поля квитанции и аренды обновляются под блокировкой до финальной проверки срока, а отказ store не превращается в автономный успех.
func (authority *Authority) freshMetadataDeadline(ctx context.Context) (time.Time, error) {
	started := authority.now()
	state := authority.SnapshotState()
	if state.AttestationReceiptID == "" {
		return time.Time{}, failure.New(failure.PersistenceUnavailable, "authorization snapshot is not activated")
	}
	freshness, err := authority.store.Freshness(ctx, state)
	if err != nil {
		return time.Time{}, failure.Wrap(failure.PersistenceUnavailable, "authorization snapshot freshness unavailable", err)
	}
	remaining := freshness.ValidUntil.Sub(freshness.ObservedAt)
	if !uuidPattern.MatchString(freshness.ReceiptID) || remaining <= 0 || remaining > authorizationMetadataLKGWindow {
		return time.Time{}, failure.New(failure.PersistenceUnavailable, "authorization metadata freshness rejected")
	}
	// До запроса сохраняется monotonic clock: сетевой RTT расходует, а не
	// продлевает остаток, выданный DB clock. Повтор receipt не обновляет lease.
	deadline := started.Add(remaining)
	authority.activationMu.Lock()
	if authority.metadataReceiptID == freshness.ReceiptID &&
		!authority.metadataValidUntil.IsZero() && authority.metadataValidUntil.Before(deadline) {
		deadline = authority.metadataValidUntil
	}
	// Freshness разрешает только exact source/key/policy/signer этого workload.
	// Другой Pod может обновить общий watermark: рабочий accept использует тот
	// же authoritative receipt, что readiness. Старые issued bindings неизменны.
	authority.attestationReceiptID = freshness.ReceiptID
	authority.metadataReceiptID = freshness.ReceiptID
	authority.metadataValidUntil = deadline
	authority.activationMu.Unlock()
	if !authority.now().Before(deadline) {
		return time.Time{}, failure.New(failure.PersistenceUnavailable, "authorization metadata last-known-good window expired")
	}
	return deadline, nil
}

// authorizationExpiry chooses the earlier metadata deadline or now-plus-TTL; only the TTL-selected branch is UTC second-truncated, preserving the existing metadata-deadline precision.
// authorizationExpiry выбирает более ранние срок метаданных или now плюс TTL; только ветвь TTL обрезается до секунды UTC, сохраняя прежнюю точность срока метаданных.
func authorizationExpiry(now time.Time, ttl time.Duration, validUntil time.Time) time.Time {
	expiresAt := now.Add(ttl)
	if validUntil.Before(expiresAt) {
		return validUntil
	}
	return expiresAt.UTC().Truncate(time.Second)
}

// newUUID supplies a fresh server-generated authorization-context JTI using
// the shared cryptographic version-four encoder. It does not register the
// issued context, reserve replay state or grant authority on its own.
// newUUID выдаёт свежий назначенный сервером JTI контекста авторизации через
// общий криптографический кодировщик версии четыре. Самостоятельно контекст
// не регистрируется, replay state не резервируется и полномочия не выдаются.
func newUUID() (string, error) {
	return value.NewAuthorityUUID()
}

// registerIssuedContext hashes exact compact bytes, records caller/target/time and optional parent lineage against the current snapshot, and maps store failure to unavailable. It does not issue another JTI or return a token before registration succeeds.
// registerIssuedContext хеширует точные compact-байты, записывает caller, target, время и необязательное происхождение родителя для текущего снимка и переводит отказ store в unavailable. Другой JTI не выдаётся и токен до успешной регистрации не возвращается.
func (authority *Authority) registerIssuedContext(ctx context.Context, compact string, claims model.AuthorizationClaims, parentDigest string) error {
	digest := sha256.Sum256([]byte(compact))
	binding := repository.IssuedContextBinding{JTI: claims.JTI, Digest: hex.EncodeToString(digest[:]), CallerWorkloadID: claims.Caller.WorkloadID, TargetWorkloadID: claims.Target.WorkloadID, IssuedAt: time.Unix(claims.IssuedAt, 0), ExpiresAt: claims.ExpiryTime(), ParentDigest: parentDigest}
	if claims.Continuation != nil {
		binding.ParentJTI = claims.Continuation.ParentJTI
	}
	if err := authority.store.RegisterIssuedContext(ctx, authority.SnapshotState(), binding); err != nil {
		return failure.Wrap(failure.PersistenceUnavailable, "issued context registration unavailable", err)
	}
	return nil
}
