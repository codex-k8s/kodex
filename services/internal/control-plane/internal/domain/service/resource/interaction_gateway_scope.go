package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
)

const interactionGatewayAuthoritySource = "WORKLOAD_READINESS"

type interactionGatewayProjectReceipt struct {
	ProjectID string `json:"projectId"`
}

// selectInteractionGatewayProject выбирает tenant partition на стороне owner.
// Статический workload credential намеренно не содержит project: один gateway
// обслуживает все проекты организации, а очередность хранится в PostgreSQL.
func (service *Service) selectInteractionGatewayProject(
	ctx context.Context,
	principal value.Principal,
	operation, idempotencyKey string,
) (value.Principal, error) {
	if principal.ProjectID != "" || principal.AuthoritySource != interactionGatewayAuthoritySource ||
		(operation != "OWNER_GATE_CLAIM" &&
			operation != "OWNER_GATE_EXPIRE" &&
			operation != "DELIVERY_CLAIM") {
		return value.Principal{}, errs.ErrPermissionDenied
	}
	requestHash, err := canonicalHash(struct {
		Identity  commandIdentity
		Operation string
	}{identity(principal), operation})
	if err != nil {
		return value.Principal{}, errs.ErrInvalidInput
	}
	keyHash := hashString(idempotencyKey)
	scopeName := "interaction_gateway_partition_" + strings.ToLower(operation)
	var selected interactionGatewayProjectReceipt
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: principal.OrganizationID,
		ActorID:        principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, receiptErr := tx.GetReceipt(ctx, principal.OrganizationID, scopeName, keyHash)
		if receiptErr == nil {
			if receipt.RequestHash != requestHash || json.Unmarshal(receipt.Payload, &selected) != nil ||
				value.ValidateID(selected.ProjectID) != nil {
				return errs.ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(receiptErr, errs.ErrNotFound) {
			return receiptErr
		}
		projectID, selectErr := tx.NextInteractionGatewayProject(ctx, principal.OrganizationID, operation)
		if selectErr != nil {
			return selectErr
		}
		selected.ProjectID = projectID
		payload, marshalErr := json.Marshal(selected)
		if marshalErr != nil {
			return errs.ErrInternal
		}
		now, timeErr := tx.CurrentTime(ctx)
		if timeErr != nil {
			return timeErr
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: principal.OrganizationID,
			Scope:          scopeName,
			KeyHash:        keyHash,
			RequestHash:    requestHash,
			Payload:        payload,
			CreatedAt:      now,
		})
	})
	if err != nil {
		return value.Principal{}, err
	}
	principal.ProjectID = selected.ProjectID
	return principal, nil
}

// scopeInteractionGatewayProject связывает service-wide authority с exact
// project locator из уже выданной work item. Все последующие repository
// операции повторно проверяют organization, project, resource, fence и lease.
func scopeInteractionGatewayProject(principal value.Principal, projectID string) (value.Principal, error) {
	if principal.ProjectID != "" || principal.AuthoritySource != interactionGatewayAuthoritySource ||
		value.ValidateID(projectID) != nil {
		return value.Principal{}, errs.ErrPermissionDenied
	}
	principal.ProjectID = projectID
	return principal, nil
}
