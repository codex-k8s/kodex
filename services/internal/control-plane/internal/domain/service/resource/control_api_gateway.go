package resource

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func accessConfigurationKind(kind enum.Kind) bool {
	switch kind {
	case enum.KindTeam, enum.KindRole, enum.KindPromptProfile:
		return true
	default:
		return false
	}
}

// DetachAccessResource разрешает source в trusted owner boundary до OCC и
// меняет только server-owned ownership metadata в той же транзакции.
func (service *Service) DetachAccessResource(
	ctx context.Context,
	input DetachAccessResourceInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionAccessDetach); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.ResourceID) != nil || input.ExpectedVersion == 0 ||
		!accessConfigurationKind(input.ExpectedKind) {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity        commandIdentity
		ResourceID      string
		ExpectedVersion uint64
		ExpectedKind    enum.Kind
	}{identity(input.Principal), input.ResourceID, input.ExpectedVersion, input.ExpectedKind})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"detach_access_configuration", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			current, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.ResourceID)
			if err != nil {
				return entity.Resource{}, err
			}
			if current.Kind != input.ExpectedKind {
				return entity.Resource{}, errs.ErrNotFound
			}
			if current.Version != input.ExpectedVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			configured, ok := current.Spec.(entity.ConfiguredSpec)
			if !ok || configured.ConfigurationOwnership().ManagedBy != "GIT" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			nextSpec, err := entity.WithConfigurationOwnership(
				current.Spec, entity.ConfigurationOwnership{ManagedBy: "UI"},
			)
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			updated, err := current.Update(current.Name, nextSpec, service.now().UTC().Truncate(time.Microsecond))
			if err != nil {
				return entity.Resource{}, errs.ErrStateConflict
			}
			if err := tx.Update(ctx, updated, current.Version); err != nil {
				return entity.Resource{}, err
			}
			return updated, service.appendMutationRecords(ctx, tx, input.Principal,
				"detach_access_configuration", updated)
		})
}

// CopyAccessResource создаёт новую UI-owned сущность только из locked source.
func (service *Service) CopyAccessResource(
	ctx context.Context,
	input CopyAccessResourceInput,
) (entity.Resource, error) {
	if err := authorize(input.Principal, permissionAccessCopy); err != nil {
		return entity.Resource{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.SourceResourceID) != nil || input.ExpectedSourceVersion == 0 ||
		!accessConfigurationKind(input.ExpectedKind) || value.ValidateName(input.Name) != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity              commandIdentity
		SourceResourceID      string
		ExpectedSourceVersion uint64
		ExpectedKind          enum.Kind
		Name                  string
	}{
		identity(input.Principal), input.SourceResourceID, input.ExpectedSourceVersion,
		input.ExpectedKind, input.Name,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return service.withResourceReceipt(ctx, input.Principal, input.IdempotencyKey,
		"copy_access_configuration", requestHash, func(tx domainrepo.Transaction) (entity.Resource, error) {
			source, err := tx.GetForUpdate(ctx, input.Principal.OrganizationID,
				input.Principal.ProjectID, input.SourceResourceID)
			if err != nil {
				return entity.Resource{}, err
			}
			if source.Kind != input.ExpectedKind {
				return entity.Resource{}, errs.ErrNotFound
			}
			if source.Version != input.ExpectedSourceVersion {
				return entity.Resource{}, errs.ErrVersionMismatch
			}
			configured, ok := source.Spec.(entity.ConfiguredSpec)
			if !ok || configured.ConfigurationOwnership().ManagedBy != "GIT" {
				return entity.Resource{}, errs.ErrStateConflict
			}
			created, err := copyAccessResource(source, input.Principal.ActorID, input.Name,
				service.now().UTC().Truncate(time.Microsecond))
			if err != nil {
				return entity.Resource{}, errs.ErrInvalidInput
			}
			if err := service.validateAccessMutation(ctx, tx, input.Principal, created.Kind, created.Spec); err != nil {
				return entity.Resource{}, err
			}
			if err := service.validateReferences(ctx, tx, created); err != nil {
				return entity.Resource{}, err
			}
			if err := tx.Insert(ctx, created); err != nil {
				return entity.Resource{}, err
			}
			return created, service.appendMutationRecords(ctx, tx, input.Principal,
				"copy_access_configuration", created)
		})
}

func copyAccessResource(source entity.Resource, ownerActorID, name string, now time.Time) (entity.Resource, error) {
	copiedSpec, err := entity.WithConfigurationOwnership(source.Spec, entity.ConfigurationOwnership{
		ManagedBy: "UI", SourceRef: source.ID, SourceRevision: source.Version,
	})
	if err != nil {
		return entity.Resource{}, errs.ErrStateConflict
	}
	created, err := entity.NewPausedAccessConfiguration(uuid.NewString(), source.OrganizationID, source.ProjectID,
		source.ParentID, ownerActorID, source.Kind, name, copiedSpec, now)
	if err != nil {
		return entity.Resource{}, errs.ErrInvalidInput
	}
	return created, nil
}

func (service *Service) ListRuntimeIncidents(
	ctx context.Context,
	input ListRuntimeIncidentsInput,
) ([]domainrepo.RuntimeIncident, error) {
	if err := authorize(input.Principal, permissionRuntimeIncidentRead); err != nil {
		return nil, err
	}
	input.Filter.OrganizationID = input.Principal.OrganizationID
	input.Filter.ProjectID = input.Principal.ProjectID
	input.Filter.ActorID = input.Principal.ActorID
	if input.Principal.ProjectID == "" || input.Filter.Validate() != nil {
		return nil, errs.ErrInvalidInput
	}
	return service.repository.ListRuntimeIncidents(ctx, input.Filter)
}

func (service *Service) AdmitOwnerSession(
	ctx context.Context,
	input OwnerSessionInput,
) (domainrepo.OwnerSessionState, error) {
	if err := authorize(input.Principal, permissionOwnerSessionAdmit); err != nil {
		return domainrepo.OwnerSessionState{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.Principal.AuthorityReference) != nil ||
		input.Principal.AuthorityRevision == 0 || !validSHA256Text(input.Principal.AuthorityDigest) {
		return domainrepo.OwnerSessionState{}, errs.ErrInvalidInput
	}
	state := domainrepo.OwnerSessionState{
		OrganizationID: input.Principal.OrganizationID, ActorID: input.Principal.ActorID,
		SessionID:              input.Principal.AuthorityReference,
		CredentialDigestSHA256: input.Principal.AuthorityDigest,
		CurrentRevision:        input.Principal.AuthorityRevision,
	}
	return service.ownerSessionCommand(ctx, input.Principal, input.IdempotencyKey,
		"admit_owner_session", state, func(tx domainrepo.Transaction) (domainrepo.OwnerSessionState, error) {
			state.UpdatedAt = service.now().UTC().Truncate(time.Microsecond)
			return tx.AdmitOwnerSession(ctx, state)
		})
}

func (service *Service) RevokeOwnerSession(
	ctx context.Context,
	input OwnerSessionInput,
) (domainrepo.OwnerSessionState, error) {
	if err := authorize(input.Principal, permissionOwnerSessionRevoke); err != nil {
		return domainrepo.OwnerSessionState{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		input.ExpectedRevision == 0 || input.ExpectedRevision != input.Principal.AuthorityRevision ||
		value.ValidateID(input.Principal.AuthorityReference) != nil ||
		!validSHA256Text(input.Principal.AuthorityDigest) {
		return domainrepo.OwnerSessionState{}, errs.ErrInvalidInput
	}
	state := domainrepo.OwnerSessionState{
		OrganizationID: input.Principal.OrganizationID, ActorID: input.Principal.ActorID,
		SessionID:              input.Principal.AuthorityReference,
		CredentialDigestSHA256: input.Principal.AuthorityDigest,
		CurrentRevision:        input.ExpectedRevision,
	}
	return service.ownerSessionCommand(ctx, input.Principal, input.IdempotencyKey,
		"revoke_owner_session", state, func(tx domainrepo.Transaction) (domainrepo.OwnerSessionState, error) {
			state.UpdatedAt = service.now().UTC().Truncate(time.Microsecond)
			return tx.RevokeOwnerSession(ctx, state)
		})
}

type ownerSessionMutation func(domainrepo.Transaction) (domainrepo.OwnerSessionState, error)

func (service *Service) ownerSessionCommand(ctx context.Context, principal value.Principal,
	idempotencyKey, scope string, requested domainrepo.OwnerSessionState,
	apply ownerSessionMutation,
) (domainrepo.OwnerSessionState, error) {
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		State    domainrepo.OwnerSessionState
	}{identity(principal), requested})
	if err != nil {
		return domainrepo.OwnerSessionState{}, errs.ErrInvalidInput
	}
	keyHash := hashString(idempotencyKey)
	var result domainrepo.OwnerSessionState
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID, ActorID: principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, receiptErr := tx.GetReceipt(ctx, principal.OrganizationID, scope, keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || json.Unmarshal(receipt.Payload, &result) != nil {
				return errs.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		result, receiptErr = apply(tx)
		if receiptErr != nil {
			return receiptErr
		}
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
			Scope: scope, KeyHash: keyHash, RequestHash: requestHash, Payload: payload,
			CreatedAt: service.now().UTC().Truncate(time.Microsecond),
		})
	})
	return result, err
}

const gatewayPublicTLSOverlap = 15 * time.Minute

func (service *Service) PrepareGatewayPublicTLS(
	ctx context.Context,
	input PrepareGatewayPublicTLSInput,
) (domainrepo.GatewayPublicTLSState, error) {
	if err := authorize(input.Principal, permissionGatewayPublicTLSPrepare); err != nil {
		return domainrepo.GatewayPublicTLSState{}, err
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	if input.Principal.CallerWorkload != controlAPIGatewayWorkload ||
		input.Principal.CallerSPIFFEID != controlAPIGatewaySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.Generation == 0 ||
		!validSHA256Text(input.CertificateSHA256) ||
		(input.PredecessorGeneration == 0) != (input.PredecessorCertificateSHA256 == "") ||
		(input.Generation == 1 && input.PredecessorGeneration != 0) ||
		(input.Generation > 1 && input.PredecessorGeneration+1 != input.Generation) ||
		(input.PredecessorCertificateSHA256 != "" && !validSHA256Text(input.PredecessorCertificateSHA256)) ||
		input.NotBefore.IsZero() || !input.NotAfter.After(input.NotBefore) ||
		input.NotAfter.Sub(input.NotBefore) > 24*time.Hour || input.NotBefore.After(now) ||
		!input.NotAfter.After(now.Add(5*time.Minute)) {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	scope := domainrepo.GatewayPublicTLSState{
		OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
		WorkloadID: controlAPIGatewayWorkload,
	}
	candidate := domainrepo.GatewayPublicTLSMaterial{
		Generation: input.Generation, CertificateSHA256: input.CertificateSHA256,
		NotBefore: input.NotBefore.UTC(), NotAfter: input.NotAfter.UTC(),
	}
	requestHash, err := canonicalHash(struct {
		Identity                     commandIdentity
		Candidate                    domainrepo.GatewayPublicTLSMaterial
		PredecessorGeneration        uint64
		PredecessorCertificateSHA256 string
	}{
		identity(input.Principal), candidate, input.PredecessorGeneration,
		input.PredecessorCertificateSHA256,
	})
	if err != nil {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result domainrepo.GatewayPublicTLSState
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
		ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, receiptErr := tx.GetReceipt(ctx, input.Principal.OrganizationID,
			"prepare_gateway_public_tls", keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || json.Unmarshal(receipt.Payload, &result) != nil {
				return errs.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		result, receiptErr = tx.PrepareGatewayPublicTLS(ctx, scope, candidate,
			input.PredecessorGeneration, input.PredecessorCertificateSHA256, now)
		if receiptErr != nil {
			return receiptErr
		}
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
			Scope: "prepare_gateway_public_tls", KeyHash: keyHash, RequestHash: requestHash,
			Payload: payload, CreatedAt: now,
		})
	})
	return result, err
}

func (service *Service) ConfirmGatewayPublicTLS(
	ctx context.Context,
	input ConfirmGatewayPublicTLSInput,
) (domainrepo.GatewayPublicTLSState, error) {
	if err := authorize(input.Principal, permissionGatewayPublicTLSConfirm); err != nil {
		return domainrepo.GatewayPublicTLSState{}, err
	}
	if input.Principal.CallerWorkload != controlAPIGatewayWorkload ||
		input.Principal.CallerSPIFFEID != controlAPIGatewaySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || input.Generation == 0 ||
		!validSHA256Text(input.CertificateSHA256) {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	now := service.now().UTC().Truncate(time.Microsecond)
	scope := domainrepo.GatewayPublicTLSState{
		OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
		WorkloadID: controlAPIGatewayWorkload,
	}
	requestHash, err := canonicalHash(struct {
		Identity          commandIdentity
		Generation        uint64
		CertificateSHA256 string
	}{identity(input.Principal), input.Generation, input.CertificateSHA256})
	if err != nil {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	keyHash := hashString(input.IdempotencyKey)
	var result domainrepo.GatewayPublicTLSState
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
		ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, receiptErr := tx.GetReceipt(ctx, input.Principal.OrganizationID,
			"confirm_gateway_public_tls", keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || json.Unmarshal(receipt.Payload, &result) != nil {
				return errs.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		result, receiptErr = tx.ConfirmGatewayPublicTLS(ctx, scope, input.Generation,
			input.CertificateSHA256, now, now.Add(gatewayPublicTLSOverlap))
		if receiptErr != nil {
			return receiptErr
		}
		payload, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
			Scope: "confirm_gateway_public_tls", KeyHash: keyHash, RequestHash: requestHash,
			Payload: payload, CreatedAt: now,
		})
	})
	return result, err
}

func (service *Service) CheckGatewayPublicTLS(
	ctx context.Context,
	input CheckGatewayPublicTLSInput,
) (domainrepo.GatewayPublicTLSState, error) {
	if err := authorize(input.Principal, permissionGatewayPublicTLSCheck); err != nil {
		return domainrepo.GatewayPublicTLSState{}, err
	}
	if input.Principal.CallerWorkload != controlAPIGatewayWorkload ||
		input.Principal.CallerSPIFFEID != controlAPIGatewaySPIFFEID || input.Generation == 0 ||
		!validSHA256Text(input.CertificateSHA256) {
		return domainrepo.GatewayPublicTLSState{}, errs.ErrInvalidInput
	}
	scope := domainrepo.GatewayPublicTLSState{
		OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
		WorkloadID: controlAPIGatewayWorkload,
	}
	var result domainrepo.GatewayPublicTLSState
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID, ProjectID: input.Principal.ProjectID,
		ActorID: input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		var readErr error
		result, readErr = tx.CheckGatewayPublicTLS(ctx, scope, input.Generation,
			input.CertificateSHA256, service.now().UTC().Truncate(time.Microsecond))
		return readErr
	})
	return result, err
}
