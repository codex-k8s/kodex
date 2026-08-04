// Package authority реализует серверное преобразование OIDC-идентичности
// в доменные полномочия.
package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/proofsigner"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const proofTTL = 15 * time.Second

var operationPattern = regexp.MustCompile(
	`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`,
)

// Operation — версионированная политика производителя, полученная из того же снимка.
type Operation struct {
	ProducerID                   string
	CredentialPurpose            string
	FullMethod                   string
	Permission                   string
	ProjectRequired              bool
	TenantOwnerOnly              bool
	CallerWorkload               string
	CallerSPIFFEID               string
	ActorKind                    string
	AuthoritySource              string
	ProofAudience                string
	AuthorizationContextAudience string
}

// Config задаёт точные назначение и аудитории доказательства, а также политику.
type Config struct {
	Issuer                       string
	ProofAudience                string
	AuthorizationContextAudience string
	PolicyRevision               uint64
	PolicyDigest                 string
	Operations                   map[string]Operation
}

// ReadinessState связывает обслуживаемую политику с независимо доверенным
// подписывающим компонентом.
type ReadinessState struct {
	PolicyRevision   uint64
	PolicyDigest     string
	TrustRevision    uint64
	TrustDigest      string
	SignerGeneration uint64
	PublicThumbprint string
}

type Service struct {
	repository domainrepo.Repository
	signer     proofsigner.Signer
	config     Config
	now        func() time.Time
}

type ResolveInput struct {
	Identity          authoritytype.ApplicationIdentity
	OperationID       string
	ResourceReference string
	IdempotencyKey    string
	CorrelationID     string
}

// New создаёт сервис доказательств с закрытым отказом.
func New(
	repository domainrepo.Repository,
	signer proofsigner.Signer,
	config Config,
) (*Service, error) {
	if repository == nil || signer == nil ||
		config.Issuer == "" || config.ProofAudience == "" ||
		config.AuthorizationContextAudience == "" ||
		config.PolicyRevision == 0 || !validDigest(config.PolicyDigest) ||
		len(config.Operations) == 0 {
		return nil, errors.New("authority proof service configuration is invalid")
	}
	for operationID, operation := range config.Operations {
		if !operationPattern.MatchString(operationID) ||
			!operationPattern.MatchString(operation.ProducerID) || operation.CredentialPurpose == "" ||
			operation.FullMethod == "" || operation.Permission == "" ||
			operation.CallerWorkload == "" ||
			operation.CallerSPIFFEID == "" ||
			(operation.ActorKind != "HUMAN" && operation.ActorKind != "WORKLOAD") ||
			operation.AuthoritySource == "" || operation.ProofAudience == "" ||
			operation.AuthorizationContextAudience == "" {
			return nil, errors.New("authority proof operation is invalid")
		}
	}
	return &Service{
		repository: repository,
		signer:     signer,
		config:     config,
		now:        time.Now,
	}, nil
}

// Resolve выдаёт доказательство после серверной проверки владения и допустимости.
func (service *Service) Resolve(
	ctx context.Context,
	input ResolveInput,
) (authoritytype.Proof, error) {
	operation, ok := service.config.Operations[input.OperationID]
	if !ok || value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.CorrelationID) != nil ||
		validateApplicationIdentity(input.Identity) != nil ||
		!credentialMatches(operation, input.Identity) ||
		(input.Identity.BoundContinuationID != "" && !slices.Contains(input.Identity.AllowedOperationIDs, input.OperationID)) {
		return authoritytype.Proof{}, errs.ErrUnauthenticated
	}
	if operation.ProjectRequired {
		if value.ValidateID(input.Identity.ProjectID) != nil {
			return authoritytype.Proof{}, errs.ErrPermissionDenied
		}
		if input.ResourceReference != "" &&
			value.ValidateID(input.ResourceReference) != nil {
			return authoritytype.Proof{}, errs.ErrInvalidInput
		}
	} else if input.Identity.ProjectID != "" || input.ResourceReference != "" {
		return authoritytype.Proof{}, errs.ErrPermissionDenied
	}
	if operation.TenantOwnerOnly && !input.Identity.TenantOwner {
		return authoritytype.Proof{}, errs.ErrPermissionDenied
	}
	requestHash, err := canonicalDigest(struct {
		ProducerID                  string
		CredentialPurpose           string
		CredentialGeneration        uint64
		SubjectDigest               string
		CredentialDigest            string
		SessionJTI                  string
		SessionID                   string
		SessionRevision             uint64
		OrganizationID              string
		ProjectID                   string
		OperationID                 string
		ResourceReference           string
		BoundSessionID              string
		BoundTurnID                 string
		BoundAttempt                uint32
		BoundInputSHA256            string
		BoundGeneration             uint64
		BoundRuntimeRevisionID      string
		BoundRuntimeRevisionVersion uint64
		BoundRuntimeRevisionSHA256  string
		BoundContinuationID         string
		BoundContinuationVersion    uint64
		BoundContinuationFence      uint64
		BoundInvocationID           string
	}{
		input.Identity.ProducerID,
		input.Identity.CredentialPurpose,
		input.Identity.CredentialGeneration,
		input.Identity.SubjectDigest,
		input.Identity.CredentialDigest,
		input.Identity.SessionJTI,
		input.Identity.SessionID,
		input.Identity.SessionRevision,
		input.Identity.OrganizationID,
		input.Identity.ProjectID,
		input.OperationID,
		input.ResourceReference,
		input.Identity.BoundSessionID,
		input.Identity.BoundTurnID,
		input.Identity.BoundAttempt,
		input.Identity.BoundInputSHA256,
		input.Identity.BoundGeneration,
		input.Identity.BoundRuntimeRevisionID,
		input.Identity.BoundRuntimeRevisionVersion,
		input.Identity.BoundRuntimeRevisionSHA256,
		input.Identity.BoundContinuationID,
		input.Identity.BoundContinuationVersion,
		input.Identity.BoundContinuationFence,
		input.Identity.BoundInvocationID,
	})
	if err != nil {
		return authoritytype.Proof{}, errs.ErrInternal
	}
	keyHash := sha256String(input.IdempotencyKey)
	var result authoritytype.Proof
	err = service.repository.Transact(
		ctx,
		domainrepo.Scope{
			OrganizationID: input.Identity.OrganizationID,
			ProjectID:      input.Identity.ProjectID,
			ActorID:        input.Identity.ActorID,
		},
		func(tx domainrepo.Transaction) error {
			if input.Identity.CallerWorkload == "control-api-gateway" {
				session := domainrepo.OwnerSessionState{
					OrganizationID:         input.Identity.OrganizationID,
					ActorID:                input.Identity.ActorID,
					SessionID:              input.Identity.SessionID,
					CredentialDigestSHA256: input.Identity.CredentialDigest,
					CurrentRevision:        input.Identity.SessionRevision,
				}
				switch input.OperationID {
				case "control.owner-session.admit", "control.readiness.check",
					"control.gateway-public-tls.prepare", "control.gateway-public-tls.confirm", "control.gateway-public-tls.check":
				case "control.owner-session.revoke":
					if err := tx.RequireOwnerSession(ctx, session, true); err != nil {
						return errs.ErrPermissionDenied
					}
				default:
					if err := tx.RequireOwnerSession(ctx, session, false); err != nil {
						return errs.ErrPermissionDenied
					}
				}
			}
			receipt, receiptErr := tx.GetReceipt(
				ctx,
				input.Identity.OrganizationID,
				"resolve_authority_proof",
				keyHash,
			)
			if receiptErr == nil {
				if receipt.RequestHash != requestHash ||
					json.Unmarshal(receipt.Payload, &result) != nil {
					return errs.ErrIdempotencyConflict
				}
				return nil
			}
			if !errors.Is(receiptErr, errs.ErrNotFound) {
				return receiptErr
			}
			if input.Identity.BoundContinuationID != "" {
				if err := tx.AdmitContinuationGrantVerifierState(
					ctx,
					input.Identity.BoundSignerKeysetRevision,
					input.Identity.BoundSignerHighWatermark,
					input.Identity.BoundSignerServedGeneration,
					input.Identity.BoundSignerKeysetSHA256,
					input.Identity.BoundSignerGeneration,
				); err != nil {
					return errs.ErrPermissionDenied
				}
				continuation, err := tx.GetIntegrationContinuationForUpdate(ctx, input.Identity.BoundContinuationID)
				if err != nil || continuation.OrganizationID != input.Identity.OrganizationID ||
					continuation.ProjectID != input.Identity.ProjectID ||
					continuation.SessionID != input.Identity.BoundSessionID ||
					continuation.TurnID != input.Identity.BoundTurnID ||
					continuation.Attempt != input.Identity.BoundAttempt ||
					continuation.ImmutableInputSHA256 != input.Identity.BoundInputSHA256 ||
					continuation.RuntimeRevisionID != input.Identity.BoundRuntimeRevisionID ||
					continuation.RuntimeRevisionVersion != input.Identity.BoundRuntimeRevisionVersion ||
					continuation.RuntimeRevisionSHA256 != input.Identity.BoundRuntimeRevisionSHA256 ||
					continuation.GrantGeneration != input.Identity.BoundGeneration ||
					!continuationGrantVersionAllowed(continuation.Version, continuation.Fence,
						input.Identity.BoundContinuationVersion, input.Identity.BoundContinuationFence) ||
					continuation.InvocationID != input.Identity.BoundInvocationID {
					return errs.ErrPermissionDenied
				}
			}
			var projectAuthority *authoritytype.Identity
			if operation.ProjectRequired {
				project, err := tx.AuthorizeProject(
					ctx,
					input.Identity.OrganizationID,
					input.Identity.ProjectID,
					input.Identity.ActorID,
					operation.Permission,
					input.ResourceReference,
				)
				if err != nil {
					return err
				}
				projectDigest, err := canonicalDigest(project)
				if err != nil {
					return errs.ErrInternal
				}
				projectAuthority = &authoritytype.Identity{
					ID: project.ID,
					Provenance: authoritytype.Provenance{
						Source:       "DOMAIN_STATE",
						Reference:    project.ID,
						Revision:     project.Version,
						DigestSHA256: projectDigest,
					},
				}
			}
			if input.Identity.BoundTurnID != "" {
				turn, err := tx.GetForUpdate(
					ctx,
					input.Identity.OrganizationID,
					input.Identity.ProjectID,
					input.Identity.BoundTurnID,
				)
				if err != nil {
					return err
				}
				turnSpec, ok := turn.Spec.(entity.TurnSpec)
				if !ok || turn.Kind != enum.KindTurn ||
					turn.OwnerActorID != input.Identity.ActorID ||
					turnSpec.SessionID != input.Identity.BoundSessionID ||
					turnSpec.Attempt != input.Identity.BoundAttempt ||
					turnSpec.EffectiveInputSHA256 != input.Identity.BoundInputSHA256 ||
					(input.ResourceReference != "" &&
						input.ResourceReference != turn.ID) {
					return errs.ErrPermissionDenied
				}
			}
			revision, err := tx.NextProofRevision(ctx)
			if err != nil {
				return err
			}
			now := service.now().UTC().Truncate(time.Second)
			provenance := authoritytype.Provenance{
				Source:       operation.AuthoritySource,
				Reference:    input.Identity.SessionJTI,
				Revision:     input.Identity.SessionRevision,
				DigestSHA256: input.Identity.CredentialDigest,
			}
			if input.Identity.CallerWorkload == "control-api-gateway" {
				provenance.Reference = input.Identity.SessionID
			}
			if input.Identity.BoundTurnID != "" {
				provenance.Reference = fmt.Sprintf(
					"%s/%d/%d",
					input.Identity.BoundTurnID,
					input.Identity.BoundAttempt,
					input.Identity.BoundGeneration,
				)
				provenance.Revision = input.Identity.BoundGeneration
				provenance.DigestSHA256 = input.Identity.BoundInputSHA256
			}
			claims := authoritytype.ProofClaims{
				Version:  1,
				Issuer:   service.config.Issuer,
				Audience: operation.ProofAudience,
				Caller: authoritytype.Workload{
					WorkloadID: operation.CallerWorkload,
					SPIFFEID:   operation.CallerSPIFFEID,
				},
				OperationID:                  input.OperationID,
				AuthorizationContextAudience: operation.AuthorizationContextAudience,
				Authority: authoritytype.Authority{
					ActorKind: operation.ActorKind,
					Actor: authoritytype.Identity{
						ID:         input.Identity.ActorID,
						Provenance: provenance,
					},
					Tenant: authoritytype.Identity{
						ID:         input.Identity.OrganizationID,
						Provenance: provenance,
					},
					Project: projectAuthority,
				},
				ProofRevision: revision,
				JTI:           uuid.NewString(),
				IssuedAt:      now.Unix(),
				NotBefore:     now.Unix(),
				ExpiresAt:     now.Add(proofTTL).Unix(),
			}
			compact, proofDigest, signerState, err := service.signer.Sign(ctx, claims)
			if err != nil {
				return errs.ErrUnavailable
			}
			result = authoritytype.Proof{
				CompactJWS:       compact,
				ExpiresAt:        now.Add(proofTTL),
				ProofRevision:    revision,
				ProofDigest:      proofDigest,
				PolicyRevision:   service.config.PolicyRevision,
				SignerGeneration: signerState.SignerGeneration,
			}
			payload, err := json.Marshal(result)
			if err != nil {
				return errs.ErrInternal
			}
			return tx.SaveReceipt(ctx, domainrepo.Receipt{
				OrganizationID: input.Identity.OrganizationID,
				ProjectID:      input.Identity.ProjectID,
				Scope:          "resolve_authority_proof",
				KeyHash:        keyHash,
				RequestHash:    requestHash,
				Payload:        payload,
				CreatedAt:      now,
			})
		},
	)
	return result, err
}

func credentialMatches(operation Operation, identity authoritytype.ApplicationIdentity) bool {
	return identity.CallerWorkload == operation.CallerWorkload &&
		identity.CallerSPIFFEID == operation.CallerSPIFFEID &&
		identity.ProducerID == operation.ProducerID &&
		identity.CredentialPurpose == operation.CredentialPurpose &&
		identity.CredentialGeneration > 0
}

func continuationGrantVersionAllowed(currentVersion, currentFence, grantedVersion, grantedFence uint64) bool {
	return currentVersion == grantedVersion && currentFence == grantedFence ||
		currentVersion == grantedVersion+1 && currentFence == grantedFence+1
}

func (service *Service) Operation(operationID string) (Operation, bool) {
	operation, ok := service.config.Operations[operationID]
	return operation, ok
}

func (service *Service) Check(ctx context.Context) (ReadinessState, error) {
	if err := service.repository.Check(ctx); err != nil {
		return ReadinessState{}, err
	}
	signerState, err := service.signer.Check(ctx)
	if err != nil {
		return ReadinessState{}, err
	}
	return ReadinessState{
		PolicyRevision:   service.config.PolicyRevision,
		PolicyDigest:     service.config.PolicyDigest,
		TrustRevision:    signerState.TrustRevision,
		TrustDigest:      signerState.TrustDigest,
		SignerGeneration: signerState.SignerGeneration,
		PublicThumbprint: signerState.PublicThumbprint,
	}, nil
}

func validateApplicationIdentity(identity authoritytype.ApplicationIdentity) error {
	if !operationPattern.MatchString(identity.ProducerID) || identity.CredentialPurpose == "" ||
		identity.CredentialGeneration == 0 || value.ValidateID(identity.ActorID) != nil ||
		value.ValidateID(identity.OrganizationID) != nil ||
		(identity.ProjectID != "" && value.ValidateID(identity.ProjectID) != nil) ||
		value.ValidateID(identity.SessionJTI) != nil ||
		(identity.CallerWorkload == "control-api-gateway" && value.ValidateID(identity.SessionID) != nil) ||
		identity.SessionRevision == 0 ||
		len(identity.SubjectDigest) != 64 ||
		len(identity.CredentialDigest) != 64 {
		return errors.New("application identity is invalid")
	}
	interactionGrantIsBound := identity.CallerWorkload == "interaction-gateway" && (identity.BoundSessionID != "" || identity.BoundTurnID != "" || identity.BoundAttempt != 0 ||
		identity.BoundInputSHA256 != "" || identity.BoundGeneration != 0)
	if (identity.CallerWorkload == "agent-runner" ||
		identity.CallerWorkload == "runtime-controller" ||
		interactionGrantIsBound ||
		identity.CallerWorkload == "runtime-restore-verifier" ||
		identity.CallerWorkload == "runtime-cleanup-authorizer") &&
		(value.ValidateID(identity.BoundSessionID) != nil ||
			value.ValidateID(identity.BoundTurnID) != nil ||
			identity.BoundAttempt == 0 || identity.BoundAttempt > 100 ||
			!validDigest(identity.BoundInputSHA256) ||
			identity.BoundGeneration == 0) {
		return errors.New("agent session grant binding is invalid")
	}
	if identity.BoundContinuationID != "" &&
		(value.ValidateID(identity.BoundContinuationID) != nil || identity.BoundContinuationVersion == 0 ||
			identity.BoundContinuationFence == 0 || value.ValidateID(identity.BoundInvocationID) != nil ||
			value.ValidateID(identity.BoundRuntimeRevisionID) != nil || identity.BoundRuntimeRevisionVersion == 0 ||
			!validDigest(identity.BoundRuntimeRevisionSHA256) || len(identity.AllowedOperationIDs) == 0 ||
			identity.BoundSignerKeysetRevision == 0 || identity.BoundSignerHighWatermark == 0 ||
			identity.BoundSignerServedGeneration != identity.BoundSignerHighWatermark ||
			!validDigest(identity.BoundSignerKeysetSHA256) || identity.BoundSignerGeneration == 0) {
		return errors.New("integration continuation grant binding is invalid")
	}
	return nil
}

func canonicalDigest(value any) (string, error) {
	return internalrpcauth.CanonicalJSONSHA256(value)
}

func sha256String(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, symbol := range value {
		if (symbol < '0' || symbol > '9') && (symbol < 'a' || symbol > 'f') {
			return false
		}
	}
	return true
}
