// Package authority реализует server-side OIDC-to-domain authority resolution.
package authority

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/internalrpcauth"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/client/proofsigner"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	authoritytype "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/authority"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

const proofTTL = 15 * time.Second

var operationPattern = regexp.MustCompile(
	`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)+$`,
)

// Operation — versioned producer-side policy derived from the same snapshot.
type Operation struct {
	FullMethod      string
	Permission      string
	ProjectRequired bool
	TenantOwnerOnly bool
}

// Config задаёт exact proof purpose/audiences and policy.
type Config struct {
	Issuer                       string
	ProofAudience                string
	AuthorizationContextAudience string
	CallerWorkload               string
	CallerSPIFFEID               string
	Operations                   map[string]Operation
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

// New создаёт fail-closed proof service.
func New(
	repository domainrepo.Repository,
	signer proofsigner.Signer,
	config Config,
) (*Service, error) {
	if repository == nil || signer == nil ||
		config.Issuer == "" || config.ProofAudience == "" ||
		config.AuthorizationContextAudience == "" ||
		config.CallerWorkload == "" || config.CallerSPIFFEID == "" ||
		len(config.Operations) == 0 {
		return nil, errors.New("authority proof service configuration is invalid")
	}
	for operationID, operation := range config.Operations {
		if !operationPattern.MatchString(operationID) ||
			operation.FullMethod == "" || operation.Permission == "" {
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

// Resolve выдаёт proof после server-side ownership/eligibility resolution.
func (service *Service) Resolve(
	ctx context.Context,
	input ResolveInput,
) (authoritytype.Proof, error) {
	operation, ok := service.config.Operations[input.OperationID]
	if !ok || value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.CorrelationID) != nil ||
		validateApplicationIdentity(input.Identity) != nil ||
		input.Identity.CallerWorkload != service.config.CallerWorkload ||
		input.Identity.CallerSPIFFEID != service.config.CallerSPIFFEID {
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
		SubjectDigest     string
		OrganizationID    string
		ProjectID         string
		OperationID       string
		ResourceReference string
	}{
		input.Identity.SubjectDigest,
		input.Identity.OrganizationID,
		input.Identity.ProjectID,
		input.OperationID,
		input.ResourceReference,
	})
	if err != nil {
		return authoritytype.Proof{}, errs.ErrInternal
	}
	keyHash := sha256String(input.IdempotencyKey)
	var result authoritytype.Proof
	err = service.repository.Transact(
		ctx,
		input.Identity.OrganizationID,
		input.Identity.ProjectID,
		func(tx domainrepo.Transaction) error {
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
			revision, err := tx.NextProofRevision(ctx)
			if err != nil {
				return err
			}
			signerState, err := service.signer.Check(ctx)
			if err != nil {
				return errs.ErrUnavailable
			}
			now := service.now().UTC().Truncate(time.Second)
			provenance := authoritytype.Provenance{
				Source:       "OIDC_SESSION",
				Reference:    input.Identity.SessionJTI,
				Revision:     input.Identity.SessionRevision,
				DigestSHA256: input.Identity.CredentialDigest,
			}
			claims := authoritytype.ProofClaims{
				Version:  1,
				Issuer:   service.config.Issuer,
				Audience: service.config.ProofAudience,
				Caller: authoritytype.Workload{
					WorkloadID: service.config.CallerWorkload,
					SPIFFEID:   service.config.CallerSPIFFEID,
				},
				OperationID:                  input.OperationID,
				AuthorizationContextAudience: service.config.AuthorizationContextAudience,
				Authority: authoritytype.Authority{
					ActorKind: "HUMAN",
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
				ProofRevision:    revision,
				SignerGeneration: signerState.SignerGeneration,
				JTI:              uuid.NewString(),
				IssuedAt:         now.Unix(),
				NotBefore:        now.Unix(),
				ExpiresAt:        now.Add(proofTTL).Unix(),
			}
			compact, proofDigest, err := service.signer.Sign(ctx, claims)
			if err != nil {
				return errs.ErrUnavailable
			}
			result = authoritytype.Proof{
				CompactJWS:       compact,
				ExpiresAt:        now.Add(proofTTL),
				ProofRevision:    revision,
				ProofDigest:      proofDigest,
				PolicyRevision:   signerState.PolicyRevision,
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

func (service *Service) Operation(operationID string) (Operation, bool) {
	operation, ok := service.config.Operations[operationID]
	return operation, ok
}

func (service *Service) Check(ctx context.Context) (proofsigner.State, error) {
	if err := service.repository.Check(ctx); err != nil {
		return proofsigner.State{}, err
	}
	return service.signer.Check(ctx)
}

func validateApplicationIdentity(identity authoritytype.ApplicationIdentity) error {
	if value.ValidateID(identity.ActorID) != nil ||
		value.ValidateID(identity.OrganizationID) != nil ||
		(identity.ProjectID != "" && value.ValidateID(identity.ProjectID) != nil) ||
		value.ValidateID(identity.SessionJTI) != nil ||
		identity.SessionRevision == 0 ||
		len(identity.SubjectDigest) != 64 ||
		len(identity.CredentialDigest) != 64 {
		return errors.New("application identity is invalid")
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
