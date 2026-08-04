package resource

import (
	"context"
	"errors"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

func (service *Service) ClaimInteractionDelivery(ctx context.Context,
	input ClaimInteractionDeliveryInput) (ClaimInteractionDeliveryResult, error) {
	if err := authorize(input.Principal, permissionDeliverInteraction); err != nil {
		return ClaimInteractionDeliveryResult{}, err
	}
	if input.Principal.CallerWorkload != service.ownerGateDeliveryWorkload ||
		input.Principal.CallerSPIFFEID != service.ownerGateDeliverySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil {
		return ClaimInteractionDeliveryResult{}, errs.ErrPermissionDenied
	}
	leaseToken := service.leaseToken(input.IdempotencyKey, input.Principal.AuthorityGeneration,
		1, input.Principal.AuthorityGeneration, input.Principal.CallerWorkload, input.IdempotencyKey)
	var work domainrepo.InteractionDeliveryWork
	var credential string
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: input.Principal.OrganizationID,
		ProjectID: input.Principal.ProjectID, ActorID: input.Principal.ActorID}, func(tx domainrepo.Transaction) error {
		var claimErr error
		work, claimErr = tx.ClaimInteractionDelivery(ctx, input.Principal.OrganizationID,
			input.Principal.ProjectID, input.Principal.CallerWorkload, hashString(leaseToken), service.turnLeaseDuration)
		if errors.Is(claimErr, errs.ErrNotFound) {
			work = domainrepo.InteractionDeliveryWork{}
			return nil
		}
		if claimErr != nil {
			return claimErr
		}
		issued, issueErr := service.issueInteractionReadback(ctx, tx, input.Principal, work.ID, false,
			uuid.NewSHA1(uuid.NameSpaceURL, []byte("interaction-readback:"+work.ID+":"+leaseToken)).String())
		credential = issued.Credential
		return issueErr
	})
	if err != nil {
		return ClaimInteractionDeliveryResult{}, err
	}
	return ClaimInteractionDeliveryResult{Work: work, LeaseToken: leaseToken, ReadbackCredential: credential}, nil
}

func (service *Service) IssueInteractionDeliveryReadback(ctx context.Context,
	input IssueInteractionDeliveryReadbackInput) (IssueInteractionDeliveryReadbackResult, error) {
	if err := authorize(input.Principal, permissionDeliverInteraction); err != nil {
		return IssueInteractionDeliveryReadbackResult{}, err
	}
	if input.Principal.CallerWorkload != service.ownerGateDeliveryWorkload ||
		input.Principal.CallerSPIFFEID != service.ownerGateDeliverySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.DeliveryID) != nil {
		return IssueInteractionDeliveryReadbackResult{}, errs.ErrPermissionDenied
	}
	var result IssueInteractionDeliveryReadbackResult
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: input.Principal.OrganizationID,
		ProjectID: input.Principal.ProjectID, ActorID: input.Principal.ActorID}, func(tx domainrepo.Transaction) error {
		var issueErr error
		result, issueErr = service.issueInteractionReadback(ctx, tx, input.Principal, input.DeliveryID,
			input.Readiness, uuid.NewSHA1(uuid.NameSpaceURL, []byte("interaction-readback:"+input.IdempotencyKey)).String())
		return issueErr
	})
	return result, err
}

func (service *Service) ValidateInteractionDeliveryReadback(ctx context.Context,
	input ValidateInteractionDeliveryReadbackInput) (bool, error) {
	if err := authorize(input.Principal, permissionDeliverInteraction); err != nil {
		return false, err
	}
	if input.Principal.CallerWorkload != service.ownerGateDeliveryWorkload ||
		input.Principal.CallerSPIFFEID != service.ownerGateDeliverySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.GrantID) != nil ||
		value.ValidateID(input.DeliveryID) != nil || value.ValidateID(input.OrganizationID) != nil ||
		value.ValidateID(input.ProjectID) != nil || input.OrganizationID != input.Principal.OrganizationID ||
		input.ProjectID != input.Principal.ProjectID || !validSHA256Text(input.CredentialSHA256) || input.Generation == 0 {
		return false, errs.ErrPermissionDenied
	}
	var active bool
	err := service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: input.Principal.OrganizationID,
		ProjectID: input.Principal.ProjectID, ActorID: input.Principal.ActorID}, func(tx domainrepo.Transaction) error {
		var validateErr error
		active, validateErr = tx.ValidateInteractionDeliveryReadbackGrant(ctx, input.GrantID, input.DeliveryID,
			input.OrganizationID, input.ProjectID, input.CredentialSHA256, input.Generation)
		return validateErr
	})
	return active, err
}

func (service *Service) issueInteractionReadback(ctx context.Context, tx domainrepo.Transaction,
	principal value.Principal, deliveryID string, readiness bool, jti string) (IssueInteractionDeliveryReadbackResult, error) {
	now, err := tx.CurrentTime(ctx)
	if err != nil {
		return IssueInteractionDeliveryReadbackResult{}, err
	}
	expires := now.Add(5 * time.Minute)
	issued, err := service.interactionReadbackIssuer.Issue(ctx, InteractionReadbackClaims{
		Subject: principal.ActorID, OrganizationID: principal.OrganizationID, ProjectID: principal.ProjectID,
		DeliveryID: deliveryID, JTI: jti, Readiness: readiness, IssuedAt: now, ExpiresAt: expires,
	})
	if err != nil {
		return IssueInteractionDeliveryReadbackResult{}, errs.ErrInternal
	}
	grant := domainrepo.InteractionDeliveryReadbackGrant{ID: jti, OrganizationID: principal.OrganizationID,
		ProjectID: principal.ProjectID, ActorID: principal.ActorID, DeliveryID: deliveryID,
		ProducerID: issued.ProducerID, Purpose: issued.Purpose, WorkloadID: issued.WorkloadID,
		CallerSPIFFEID: issued.CallerSPIFFEID, Operation: issued.Operation, Permission: issued.Permission,
		CredentialSHA256: issued.SHA256, Generation: issued.Generation, KeysetRevision: issued.KeysetRevision,
		KeysetHighWatermark: issued.KeysetHighWatermark, KeysetSHA256: issued.KeysetSHA256,
		IssuedAt: now, ExpiresAt: expires, Readiness: readiness}
	if err := tx.SaveInteractionDeliveryReadbackGrant(ctx, grant); err != nil {
		return IssueInteractionDeliveryReadbackResult{}, err
	}
	return IssueInteractionDeliveryReadbackResult{DeliveryID: deliveryID, Credential: issued.Compact, ExpiresAt: expires}, nil
}

func (service *Service) RecordInteractionDelivery(ctx context.Context,
	input RecordInteractionDeliveryInput) error {
	if err := authorize(input.Principal, permissionDeliverInteraction); err != nil {
		return err
	}
	if input.Principal.CallerWorkload != service.ownerGateDeliveryWorkload ||
		input.Principal.CallerSPIFFEID != service.ownerGateDeliverySPIFFEID ||
		value.ValidateIdempotencyKey(input.IdempotencyKey) != nil || value.ValidateID(input.DeliveryID) != nil ||
		input.Fence == 0 || len(input.LeaseToken) != 64 || !validSHA256Text(input.ProviderReceiptSHA256) {
		return errs.ErrInvalidInput
	}
	return service.repository.Transact(ctx, domainrepo.Scope{OrganizationID: input.Principal.OrganizationID,
		ProjectID: input.Principal.ProjectID, ActorID: input.Principal.ActorID}, func(tx domainrepo.Transaction) error {
		return tx.CompleteInteractionDelivery(ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
			input.DeliveryID, input.Fence, hashString(input.LeaseToken), input.ProviderReceiptSHA256)
	})
}
