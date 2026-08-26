// Package authorityproof выпускает короткоживущие proof после exact mTLS,
// application credential и server-owned domain authorization.
package authorityproof

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth"
	"github.com/codex-k8s/kodex/libs/go/oidcverifier"
	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/google/uuid"
)

const (
	workerGrantType    = "kodex-application-grant+jws"
	maximumFileSize    = 1 << 20
	workerGrantTTL     = 4 * time.Minute
	controlPlaneSPIFFE = "spiffe://kodex.local/ns/kodex-system/sa/control-plane"
)

type AuthorityOwner interface {
	ResolveProofAuthority(context.Context, platformrepo.ProofPrincipalInput) (platformrepo.ProofAuthority, error)
	AcceptWorkerGrant(context.Context, platformrepo.WorkerGrantInput) error
	NextAuthorityProofRevision(context.Context) (uint64, error)
	Ready(context.Context) error
}

type Config struct {
	PolicyFile, SignerPrivateJWKFile, SignerTrustFile string
	WorkerGrantTrustFiles                             map[string]string
	OIDC                                              oidcverifier.Config
	Now                                               func() time.Time
}

type ResolveInput struct {
	PeerSPIFFEID, Authorization, OperationID, ProjectReference, IdempotencyKey, CorrelationID string
}

type ResolveResult struct {
	CompactJWS, DigestSHA256                        string
	ExpiresAt                                       time.Time
	ProofRevision, PolicyRevision, SignerGeneration uint64
}

type Readiness struct {
	PolicyRevision, SignerGeneration           uint64
	PolicyDigestSHA256, SignerThumbprintSHA256 string
}

type Service struct {
	owner            AuthorityOwner
	oidc             *oidcverifier.Verifier
	policy           policyDocument
	policyDigest     string
	bindings         map[string]operationBinding
	producers        map[string]proofProducer
	signer           internalrpcauth.ES256Key
	signerGeneration uint64
	signerThumbprint string
	workerKeys       map[string]internalrpcauth.ES256Key
	oidcIssuer       string
	oidcAudience     string
	now              func() time.Time
}

type policyDocument struct {
	Version        int    `json:"v"`
	PolicyRevision uint64 `json:"policy_revision"`
	Policy         struct {
		TrustDomain             string             `json:"trust_domain"`
		DefaultDecision         string             `json:"default_decision"`
		TokenTTLSeconds         int64              `json:"token_ttl_seconds"`
		AllowedClockSkewSeconds int64              `json:"allowed_clock_skew_seconds"`
		MaxCompactJWSBytes      int                `json:"max_compact_jws_bytes"`
		ProofProducers          []proofProducer    `json:"authority_proof_producers"`
		OperationBindings       []operationBinding `json:"operation_bindings"`
	} `json:"policy"`
}

type proofProducer struct {
	ProducerID                         string   `json:"producer_id"`
	CallerWorkloadID                   string   `json:"caller_workload_id"`
	CallerSPIFFEID                     string   `json:"caller_spiffe_id"`
	OwnerWorkloadID                    string   `json:"owner_workload_id"`
	OwnerSPIFFEID                      string   `json:"owner_spiffe_id"`
	FullMethod                         string   `json:"full_method"`
	TLSServerName                      string   `json:"tls_server_name"`
	TransportTrustBundleID             string   `json:"transport_trust_bundle_id"`
	ApplicationCredential              string   `json:"application_credential"`
	ApplicationCredentialMetadata      string   `json:"application_credential_metadata"`
	ApplicationCredentialIssuer        string   `json:"application_credential_issuer"`
	ApplicationCredentialAudience      string   `json:"application_credential_audience"`
	ApplicationCredentialTrustBundleID string   `json:"application_credential_trust_bundle_id"`
	AuthorityProofIssuer               string   `json:"authority_proof_issuer"`
	AuthorityProofAudience             string   `json:"authority_proof_audience"`
	AuthorityProofTrustBundleID        string   `json:"authority_proof_trust_bundle_id"`
	AuthorityProofMaxAgeSeconds        int64    `json:"authority_proof_max_age_seconds"`
	DeadlineMilliseconds               int64    `json:"deadline_milliseconds"`
	MaxAttempts                        int      `json:"max_attempts"`
	RetryableGRPCCodes                 []string `json:"retryable_grpc_codes"`
	IdempotencyScope                   string   `json:"idempotency_scope"`
	AuthoritySources                   []string `json:"authority_sources"`
	AllowedOperationIDs                []string `json:"allowed_operation_ids"`
	ServerResolvedFields               []string `json:"server_resolved_fields"`
}

type operationBinding struct {
	OperationID         string    `json:"operation_id"`
	CallerWorkloadID    string    `json:"caller_workload_id"`
	CallerSPIFFEID      string    `json:"caller_spiffe_id"`
	Issuer              string    `json:"issuer"`
	TargetWorkloadID    string    `json:"target_workload_id"`
	TargetSPIFFEID      string    `json:"target_spiffe_id"`
	Audience            string    `json:"audience"`
	FullMethod          string    `json:"full_method"`
	TargetTLSServerName string    `json:"target_tls_server_name"`
	TargetTrustBundleID string    `json:"target_trust_bundle_id"`
	Permission          string    `json:"permission"`
	ProducerID          string    `json:"authority_proof_producer_id"`
	AuthoritySources    []string  `json:"authority_sources"`
	ProjectRequired     bool      `json:"project_required"`
	LocalCaller         localPeer `json:"local_caller"`
	LocalTarget         localPeer `json:"local_target"`
}

type localPeer struct {
	UID         uint32 `json:"uid"`
	PrimaryGID  uint32 `json:"primary_gid"`
	SharedFSGID uint32 `json:"shared_fs_gid"`
}

type provenance struct {
	Source       string `json:"source"`
	Reference    string `json:"reference"`
	Revision     uint64 `json:"revision"`
	DigestSHA256 string `json:"digest_sha256"`
}

type identity struct {
	ID         string     `json:"id"`
	Provenance provenance `json:"provenance"`
}
type workload struct {
	WorkloadID string `json:"workload_id"`
	SPIFFEID   string `json:"spiffe_id"`
}
type authority struct {
	ActorKind string    `json:"actor_kind"`
	Actor     identity  `json:"actor"`
	Tenant    identity  `json:"tenant"`
	Project   *identity `json:"project,omitempty"`
}
type proofClaims struct {
	Version                      int       `json:"v"`
	Issuer                       string    `json:"iss"`
	Audience                     string    `json:"aud"`
	Caller                       workload  `json:"caller"`
	OperationID                  string    `json:"operation_id"`
	AuthorizationContextAudience string    `json:"authorization_context_audience"`
	Authority                    authority `json:"authority"`
	ProofRevision                uint64    `json:"proof_revision"`
	SignerGeneration             uint64    `json:"signer_generation"`
	CallerCredentialRevision     uint64    `json:"caller_credential_revision"`
	JTI                          string    `json:"jti"`
	IssuedAt                     int64     `json:"iat"`
	NotBefore                    int64     `json:"nbf"`
	ExpiresAt                    int64     `json:"exp"`
}

type workerGrantClaims struct {
	Version        int    `json:"v"`
	Issuer         string `json:"iss"`
	Audience       string `json:"aud"`
	Subject        string `json:"sub"`
	CallerSPIFFEID string `json:"caller_spiffe_id"`
	WorkloadID     string `json:"workload_id"`
	OrganizationID string `json:"organization_id"`
	ProjectID      string `json:"project_id"`
	TenantOwner    bool   `json:"tenant_owner"`
	Revision       uint64 `json:"revision"`
	JTI            string `json:"jti"`
	IssuedAt       int64  `json:"iat"`
	NotBefore      int64  `json:"nbf"`
	ExpiresAt      int64  `json:"exp"`
}

func New(ctx context.Context, owner AuthorityOwner, config Config) (*Service, error) {
	if ctx == nil || owner == nil || config.PolicyFile == "" || config.SignerPrivateJWKFile == "" || config.SignerTrustFile == "" || len(config.WorkerGrantTrustFiles) == 0 {
		return nil, errors.New("authority proof service configuration is invalid")
	}
	raw, err := readBounded(config.PolicyFile)
	if err != nil {
		return nil, errors.New("read authority proof policy")
	}
	var document policyDocument
	if err := internalrpcauth.DecodeStrictJSON(raw, &document); err != nil || document.Version != 1 || document.PolicyRevision == 0 || document.Policy.TrustDomain != "kodex.local" || document.Policy.DefaultDecision != "DENY" {
		return nil, errors.New("authority proof policy is invalid")
	}
	digest := sha256.Sum256(raw)
	privateRaw, err := readBounded(config.SignerPrivateJWKFile)
	if err != nil {
		return nil, errors.New("read authority proof signing key")
	}
	signer, err := internalrpcauth.ParsePrivateJWK(privateRaw)
	if err != nil {
		return nil, errors.New("parse authority proof signing key")
	}
	generation, err := verifySignerTrust(config.SignerTrustFile, signer, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	thumbprint, err := internalrpcauth.PublicJWKThumbprintSHA256(signer.PublicOnly())
	if err != nil {
		return nil, errors.New("derive authority proof signer thumbprint")
	}
	workerKeys := make(map[string]internalrpcauth.ES256Key, len(config.WorkerGrantTrustFiles))
	for workloadID, path := range config.WorkerGrantTrustFiles {
		keyRaw, readErr := readBounded(path)
		if readErr != nil {
			return nil, fmt.Errorf("read %s worker grant trust", workloadID)
		}
		key, parseErr := internalrpcauth.ParsePublicJWK(keyRaw)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s worker grant trust", workloadID)
		}
		workerKeys[workloadID] = key
	}
	oidc, err := oidcverifier.New(ctx, config.OIDC)
	if err != nil {
		return nil, errors.New("initialize authority OIDC verifier")
	}
	service := &Service{
		owner: owner, oidc: oidc, policy: document, policyDigest: hex.EncodeToString(digest[:]),
		signer: signer, signerGeneration: generation, signerThumbprint: thumbprint,
		workerKeys: workerKeys, oidcIssuer: config.OIDC.Issuer, oidcAudience: config.OIDC.Audience,
		now: config.Now,
	}
	if service.now == nil {
		service.now = time.Now
	}
	if err := service.indexPolicy(); err != nil {
		oidc.Close()
		return nil, err
	}
	return service, nil
}

func (service *Service) indexPolicy() error {
	service.bindings = make(map[string]operationBinding, len(service.policy.Policy.OperationBindings))
	service.producers = make(map[string]proofProducer, len(service.policy.Policy.ProofProducers))
	for _, producer := range service.policy.Policy.ProofProducers {
		if producer.ProducerID == "" || producer.OwnerWorkloadID != "control-plane" || producer.OwnerSPIFFEID != controlPlaneSPIFFE || producer.AuthorityProofIssuer != controlPlaneSPIFFE || producer.AuthorityProofMaxAgeSeconds < 1 || producer.AuthorityProofMaxAgeSeconds > 15 {
			return errors.New("authority proof producer is invalid")
		}
		if producer.ApplicationCredentialMetadata != "authorization" ||
			producer.CallerWorkloadID == "control-api-gateway" &&
				(producer.ApplicationCredential != "OIDC_BEARER" || producer.ApplicationCredentialIssuer != service.oidcIssuer || producer.ApplicationCredentialAudience != service.oidcAudience) ||
			producer.CallerWorkloadID != "control-api-gateway" && producer.ApplicationCredential != "PLATFORM_WORKER_GRANT" {
			return errors.New("authority proof application credential binding is invalid")
		}
		if _, duplicate := service.producers[producer.ProducerID]; duplicate {
			return errors.New("authority proof producer is duplicated")
		}
		service.producers[producer.ProducerID] = producer
	}
	for _, binding := range service.policy.Policy.OperationBindings {
		producer, ok := service.producers[binding.ProducerID]
		if !ok || binding.OperationID == "" || binding.CallerWorkloadID != producer.CallerWorkloadID || binding.CallerSPIFFEID != producer.CallerSPIFFEID || binding.Audience == "" || binding.FullMethod == "" || binding.Permission != binding.OperationID || !contains(producer.AllowedOperationIDs, binding.OperationID) {
			return errors.New("authority proof operation binding is invalid")
		}
		if _, duplicate := service.bindings[binding.OperationID]; duplicate {
			return errors.New("authority proof operation binding is duplicated")
		}
		service.bindings[binding.OperationID] = binding
	}
	return nil
}

func (service *Service) Resolve(ctx context.Context, input ResolveInput) (ResolveResult, error) {
	binding, ok := service.bindings[input.OperationID]
	if !ok {
		return ResolveResult{}, errors.New("operation is not permitted")
	}
	producer := service.producers[binding.ProducerID]
	if input.PeerSPIFFEID != binding.CallerSPIFFEID || input.PeerSPIFFEID != producer.CallerSPIFFEID || uuid.Validate(input.IdempotencyKey) != nil || uuid.Validate(input.CorrelationID) != nil {
		return ResolveResult{}, errors.New("authority proof request binding is invalid")
	}
	if binding.ProjectRequired && input.ProjectReference == "" || !binding.ProjectRequired && input.ProjectReference != "" {
		return ResolveResult{}, errors.New("authority proof project binding is invalid")
	}
	credential := strings.TrimPrefix(input.Authorization, "Bearer ")
	if credential == input.Authorization || credential == "" || strings.TrimSpace(credential) != credential {
		return ResolveResult{}, errors.New("application credential is invalid")
	}
	principal := platformrepo.ProofPrincipalInput{CallerWorkload: producer.CallerWorkloadID, Operation: input.OperationID, ProjectRef: input.ProjectReference}
	actorKind := "SERVICE"
	actorSource := "DOMAIN_STATE"
	actorReference := ""
	actorRevision := uint64(1)
	callerCredentialRevision := uint64(1)
	credentialDigest := sha256.Sum256([]byte(credential))
	if producer.CallerWorkloadID == "control-api-gateway" {
		verified, err := service.oidc.VerifyToken(ctx, credential)
		if errors.Is(err, oidcverifier.ErrSigningKeysUnavailable) {
			return ResolveResult{}, domainerrs.ErrUnavailable
		}
		if err != nil {
			return ResolveResult{}, domainerrs.ErrUnauthorized
		}
		principal.ExternalActorID, principal.ExternalTenantID = verified.Subject, verified.OrganizationID
		principal.ExternalDisplayName, principal.ExternalEmailHint = verified.DisplayName, verified.EmailHint
		actorKind, actorSource, actorReference, actorRevision = "HUMAN", "OIDC_SESSION", verified.SessionID, verified.SessionRevision
		callerCredentialRevision = verified.SessionRevision
	} else {
		grant, err := service.verifyWorkerGrant(credential, producer)
		if err != nil {
			return ResolveResult{}, err
		}
		if err := service.owner.AcceptWorkerGrant(ctx, platformrepo.WorkerGrantInput{
			WorkloadID: producer.CallerWorkloadID, Revision: grant.Revision,
			IssuedAt: time.Unix(grant.IssuedAt, 0), ExpiresAt: time.Unix(grant.ExpiresAt, 0),
		}); err != nil {
			return ResolveResult{}, err
		}
		principal.ExternalActorID, principal.ExternalTenantID = "kodex-system-subject", "kodex-installation"
		callerCredentialRevision = grant.Revision
	}
	resolved, err := service.owner.ResolveProofAuthority(ctx, principal)
	if err != nil {
		return ResolveResult{}, err
	}
	if actorReference == "" {
		actorReference = resolved.ActorID
		actorRevision = resolved.ActorVersion
	}
	actor := identity{ID: resolved.ActorID, Provenance: provenance{Source: actorSource, Reference: actorReference, Revision: actorRevision, DigestSHA256: hex.EncodeToString(credentialDigest[:])}}
	tenant := domainIdentity("DOMAIN_STATE", resolved.OrganizationID, resolved.OrganizationVersion)
	proofAuthority := authority{ActorKind: actorKind, Actor: actor, Tenant: tenant}
	if binding.ProjectRequired {
		if resolved.ProjectID == "" {
			return ResolveResult{}, errors.New("project authority is unavailable")
		}
		project := domainIdentity("DOMAIN_STATE", resolved.ProjectID, resolved.ProjectVersion)
		proofAuthority.Project = &project
	}
	revision, err := service.owner.NextAuthorityProofRevision(ctx)
	if err != nil {
		return ResolveResult{}, err
	}
	now := service.now().UTC().Truncate(time.Second)
	expiresAt := now.Add(time.Duration(producer.AuthorityProofMaxAgeSeconds) * time.Second)
	claims := proofClaims{
		Version: 1, Issuer: producer.AuthorityProofIssuer, Audience: producer.AuthorityProofAudience,
		Caller:      workload{WorkloadID: producer.CallerWorkloadID, SPIFFEID: producer.CallerSPIFFEID},
		OperationID: input.OperationID, AuthorizationContextAudience: binding.Audience, Authority: proofAuthority,
		ProofRevision: revision, SignerGeneration: service.signerGeneration, CallerCredentialRevision: callerCredentialRevision, JTI: uuid.NewString(),
		IssuedAt: now.Unix(), NotBefore: now.Unix(), ExpiresAt: expiresAt.Unix(),
	}
	compact, err := internalrpcauth.SignCanonicalJSON(claims, service.signer, internalrpcauth.ProtectedHeaderExpectation{Type: internalrpcauth.AuthorityProofProtectedType, KeyID: service.signer.KeyID})
	if err != nil {
		return ResolveResult{}, errors.New("sign authority proof")
	}
	proofDigest := sha256.Sum256([]byte(compact))
	return ResolveResult{CompactJWS: compact, DigestSHA256: hex.EncodeToString(proofDigest[:]), ExpiresAt: expiresAt, ProofRevision: revision, PolicyRevision: service.policy.PolicyRevision, SignerGeneration: service.signerGeneration}, nil
}

func (service *Service) verifyWorkerGrant(compact string, producer proofProducer) (workerGrantClaims, error) {
	key, ok := service.workerKeys[producer.CallerWorkloadID]
	if !ok {
		return workerGrantClaims{}, errors.New("worker grant trust is unavailable")
	}
	verified, err := internalrpcauth.VerifyCanonicalJSON(compact, key, internalrpcauth.ProtectedHeaderExpectation{Type: workerGrantType, KeyID: key.KeyID})
	if err != nil {
		return workerGrantClaims{}, errors.New("worker application grant is rejected")
	}
	var claims workerGrantClaims
	if err := internalrpcauth.DecodeCanonicalJSON(verified.CanonicalPayload, &claims); err != nil {
		return workerGrantClaims{}, errors.New("worker application grant claims are rejected")
	}
	now := service.now().UTC().Truncate(time.Second)
	if err := internalrpcauth.ValidateTimes(now, time.Unix(claims.IssuedAt, 0), time.Unix(claims.NotBefore, 0), time.Unix(claims.ExpiresAt, 0), workerGrantTTL, 5*time.Second); err != nil ||
		claims.Version != 1 || claims.Issuer != producer.ApplicationCredentialIssuer || claims.Audience != producer.ApplicationCredentialAudience || claims.WorkloadID != producer.CallerWorkloadID || claims.CallerSPIFFEID != producer.CallerSPIFFEID || claims.Revision == 0 || uuid.Validate(claims.JTI) != nil || claims.ProjectID != "" || claims.TenantOwner {
		return workerGrantClaims{}, errors.New("worker application grant binding is rejected")
	}
	return claims, nil
}

func (service *Service) RefreshOIDC(ctx context.Context) error { return service.oidc.Refresh(ctx) }
func (service *Service) Close()                                { service.oidc.Close() }
func (service *Service) Ready(ctx context.Context) (Readiness, error) {
	if err := service.owner.Ready(ctx); err != nil {
		return Readiness{}, err
	}
	return Readiness{PolicyRevision: service.policy.PolicyRevision, SignerGeneration: service.signerGeneration, PolicyDigestSHA256: service.policyDigest, SignerThumbprintSHA256: service.signerThumbprint}, nil
}

func domainIdentity(source, id string, revision uint64) identity {
	digest := sha256.Sum256([]byte(source + "\x00" + id + "\x00" + fmt.Sprint(revision)))
	return identity{ID: id, Provenance: provenance{Source: source, Reference: id, Revision: revision, DigestSHA256: hex.EncodeToString(digest[:])}}
}

func readBounded(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > maximumFileSize {
		return nil, errors.New("protected file is unavailable")
	}
	return bytes.TrimSuffix(raw, []byte{'\n'}), nil
}

func verifySignerTrust(path string, signer internalrpcauth.ES256Key, now time.Time) (uint64, error) {
	raw, err := readBounded(path)
	if err != nil {
		return 0, errors.New("read authority proof signer trust")
	}
	document, decodeErr := internalrpcauth.DecodeAuthorityProofTrustDocument(raw)
	if decodeErr == nil {
		for _, record := range document.Keys {
			key, parseErr := internalrpcauth.ParsePublicJWK(record.JWK)
			if parseErr == nil && record.Issuer == controlPlaneSPIFFE && record.Generation > 0 && record.Status == "CURRENT" && record.Purpose == "AUTHORITY_PROOF" && contains(record.Audiences, "urn:kodex:internal-rpc-authority-issuer:control-api-gateway") && !now.Before(time.Unix(record.NotBefore, 0)) && now.Before(time.Unix(record.NotAfter, 0)) && sameKey(key, signer) {
				return record.Generation, nil
			}
		}
	}
	return 0, errors.New("authority proof signer CURRENT trust binding is rejected")
}

func sameKey(left, right internalrpcauth.ES256Key) bool {
	leftRaw, leftErr := internalrpcauth.MarshalPublicJWK(left.PublicOnly())
	rightRaw, rightErr := internalrpcauth.MarshalPublicJWK(right.PublicOnly())
	return leftErr == nil && rightErr == nil && bytes.Equal(leftRaw, rightRaw)
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
