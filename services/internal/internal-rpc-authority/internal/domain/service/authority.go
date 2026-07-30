package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/failure"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/model"
	"github.com/codex-k8s/matter-codex/services/internal/internal-rpc-authority/internal/domain/repository"
)

const (
	contextProtectedType = "mattercodex-internal-rpc-auth+jws"
	proofProtectedType   = "mattercodex-internal-rpc-authority-proof+jws"
	contextKeyPurpose    = "AUTHORIZATION_CONTEXT"
	proofKeyPurpose      = "AUTHORITY_PROOF"
	keyStatusCurrent     = "CURRENT"
	keyStatusPrevious    = "PREVIOUS"
	maxProofTTL          = 15 * time.Second
)

var (
	operationPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`)
	digestPattern    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	uuidPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

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
}

type KeyMaterial struct {
	SigningKey       internalrpcauth.ES256Key
	VerificationKeys map[string]VerificationKeyRecord
	ProofKeys        map[string]VerificationKeyRecord
	ReadbackKey      internalrpcauth.ES256Key
}

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

func NewAuthority(
	policy model.PolicySnapshot,
	keys KeyMaterial,
	store repository.Store,
) (*Authority, error) {
	if policy.Version != model.ContractVersion ||
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
		(keys.SigningKey.Private != nil && len(keys.ProofKeys) == 0) ||
		keys.ReadbackKey.Private == nil {
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
			binding.AuthorityProofIssuer == "" ||
			binding.AuthorityProofAudience == "" ||
			len(binding.AuthoritySources) == 0 {
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

func (authority *Authority) Issue(
	ctx context.Context,
	operationID string,
	proofCompact string,
) (string, model.AuthorizationClaims, error) {
	binding, ok := authority.bindings[operationID]
	if !ok {
		return "", model.AuthorizationClaims{}, failure.New(
			failure.OperationNotAllowed,
			"operation is not allowed",
		)
	}
	header, err := internalrpcauth.ParseProtectedHeader(proofCompact)
	if err != nil || header.Type != proofProtectedType {
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
			Type:  proofProtectedType,
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
	now := authority.now().UTC().Truncate(time.Second)
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
		proof.OperationID != operationID ||
		proof.Issuer != binding.AuthorityProofIssuer ||
		proof.Audience != binding.AuthorityProofAudience ||
		proof.AuthorizationContextAudience != binding.Audience ||
		proof.Caller.WorkloadID != binding.CallerWorkloadID ||
		proof.Caller.SPIFFEID != binding.CallerSPIFFEID ||
		proof.ProofRevision == 0 ||
		proof.SignerGeneration == 0 ||
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
	expiresAt := now.Add(time.Duration(binding.TokenTTLSeconds) * time.Second)
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
		FullMethod:         binding.FullMethod,
		OperationID:        binding.OperationID,
		Authority:          proof.Authority,
		Permission:         binding.Permission,
		JTI:                jti,
		IssuedAt:           now.Unix(),
		NotBefore:          now.Unix(),
		ExpiresAt:          expiresAt.Unix(),
		ReplayMode:         model.ReplayModeOneTime,
		SourceRevision:     authority.policy.SourceRevision,
		SourceDigestSHA256: authority.policy.SourceDigestSHA256,
		KeySetRevision:     authority.policy.KeySetRevision,
		PolicyRevision:     authority.policy.PolicyRevision,
		SignerGeneration:   authority.policy.SignerGeneration,
	}
	compact, err := internalrpcauth.SignCanonicalJSON(
		claims,
		authority.signingKey,
		internalrpcauth.ProtectedHeaderExpectation{
			Type:  contextProtectedType,
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
	return compact, claims, nil
}

func (authority *Authority) Verify(
	ctx context.Context,
	compact string,
	observedFullMethod string,
	downstreamSPIFFEID string,
) (model.AuthorizationClaims, error) {
	header, err := internalrpcauth.ParseProtectedHeader(compact)
	if err != nil || header.Type != contextProtectedType {
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
			Type:  contextProtectedType,
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
	now := authority.now().UTC().Truncate(time.Second)
	if err := internalrpcauth.ValidateTimes(
		now,
		claims.IssuedTime(),
		claims.NotBeforeTime(),
		claims.ExpiryTime(),
		time.Duration(binding.TokenTTLSeconds)*time.Second,
		time.Duration(authority.policy.AllowedClockSkewSeconds)*time.Second,
	); err != nil {
		return model.AuthorizationClaims{}, failure.Wrap(
			failure.Unauthenticated,
			"authorization context time binding failed",
			err,
		)
	}
	if claims.Version != model.ContractVersion ||
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
		claims.Target.SPIFFEID != downstreamSPIFFEID ||
		claims.FullMethod != binding.FullMethod ||
		claims.FullMethod != observedFullMethod ||
		claims.Permission != binding.Permission ||
		claims.SourceRevision != authority.policy.SourceRevision ||
		claims.SourceDigestSHA256 != authority.policy.SourceDigestSHA256 ||
		claims.KeySetRevision != authority.policy.KeySetRevision ||
		claims.PolicyRevision != authority.policy.PolicyRevision ||
		claims.SignerGeneration != authority.policy.SignerGeneration ||
		claims.JTI == "" {
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
			Kind:      repository.ReservationAuthorizationContext,
			ScopeID:   binding.TargetWorkloadID,
			JTI:       claims.JTI,
			Digest:    hex.EncodeToString(tokenDigest[:]),
			ExpiresAt: claims.ExpiryTime(),
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

func keyAllowsAudience(record VerificationKeyRecord, audience string) bool {
	_, ok := record.Audiences[audience]
	return ok
}

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
	return nil
}

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

func (authority *Authority) Ready(ctx context.Context) error {
	return authority.store.Ready(ctx, authority.SnapshotState())
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" +
		encoded[16:20] + "-" + encoded[20:32], nil
}
