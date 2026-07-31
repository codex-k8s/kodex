package resource

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	domainrepo "github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/repository/controlplane"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
)

// ListOutboxFailures возвращает только bounded metadata terminal predecessors.
func (service *Service) ListOutboxFailures(
	ctx context.Context,
	input ListOutboxFailuresInput,
) ([]domainrepo.OutboxFailure, error) {
	if err := authorize(input.Principal, permissionReadOutbox); err != nil {
		return nil, err
	}
	if (input.AfterEventID != "" && value.ValidateID(input.AfterEventID) != nil) ||
		input.Limit < 1 || input.Limit > 100 {
		return nil, errs.ErrInvalidInput
	}
	var result []domainrepo.OutboxFailure
	err := service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		var err error
		result, err = tx.ListTerminalOutbox(
			ctx, input.Principal.OrganizationID, input.Principal.ProjectID,
			input.AfterEventID, input.Limit,
		)
		return err
	})
	return result, err
}

// RepairOutboxEvent повторно открывает exact earliest terminal predecessor.
func (service *Service) RepairOutboxEvent(
	ctx context.Context,
	input RepairOutboxEventInput,
) (domainrepo.OutboxFailure, error) {
	if err := authorize(input.Principal, permissionRepairOutbox); err != nil {
		return domainrepo.OutboxFailure{}, err
	}
	if value.ValidateIdempotencyKey(input.IdempotencyKey) != nil ||
		value.ValidateID(input.EventID) != nil || input.ExpectedSequence == 0 ||
		input.ExpectedAttempts == 0 || value.ValidateStableKey(input.ReasonCode) != nil ||
		!validSHA256Text(input.EvidenceSHA256) {
		return domainrepo.OutboxFailure{}, errs.ErrInvalidInput
	}
	requestHash, err := canonicalHash(struct {
		Identity commandIdentity
		EventID  string
		Sequence uint64
		Attempts uint32
		Reason   string
		Evidence string
	}{identity(input.Principal), input.EventID, input.ExpectedSequence,
		input.ExpectedAttempts, input.ReasonCode, input.EvidenceSHA256})
	if err != nil {
		return domainrepo.OutboxFailure{}, errs.ErrInvalidInput
	}
	keyHash := hashString(strings.Join([]string{
		input.Principal.OrganizationID,
		input.Principal.ProjectID,
		"REQUEUE",
		input.EventID,
		strconv.FormatUint(input.ExpectedSequence, 10),
		input.IdempotencyKey,
	}, "\n"))
	var result domainrepo.OutboxFailure
	err = service.repository.Transact(ctx, domainrepo.Scope{
		OrganizationID: input.Principal.OrganizationID,
		ProjectID:      input.Principal.ProjectID,
		ActorID:        input.Principal.ActorID,
	}, func(tx domainrepo.Transaction) error {
		receipt, receiptErr := tx.GetReceipt(
			ctx, input.Principal.OrganizationID, "repair_outbox_event", keyHash,
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
		now := service.now().UTC().Truncate(time.Microsecond)
		result, err = tx.RepairTerminalOutbox(ctx, domainrepo.OutboxRepair{
			EventID: input.EventID, ExpectedSequence: input.ExpectedSequence,
			ExpectedAttempts: input.ExpectedAttempts, ReasonCode: input.ReasonCode,
			EvidenceSHA256: input.EvidenceSHA256, ActorID: input.Principal.ActorID,
			CorrelationID:      input.Principal.CorrelationID,
			PolicyRevision:     input.Principal.PolicyRevision,
			IdempotencyKeyHash: keyHash, RequestHash: requestHash, RepairedAt: now,
		})
		if err != nil {
			return err
		}
		if err := tx.AppendAudit(ctx, domainrepo.Audit{
			ID: uuid.NewString(), OrganizationID: input.Principal.OrganizationID,
			ProjectID: input.Principal.ProjectID, ActorID: input.Principal.ActorID,
			Action: "repair_outbox_event", ResourceID: input.EventID,
			ResourceKind: "OUTBOX_EVENT", ResourceVersion: input.ExpectedSequence,
			Outcome: "succeeded", CorrelationID: input.Principal.CorrelationID,
			PolicyRevision: input.Principal.PolicyRevision, OccurredAt: now,
		}); err != nil {
			return err
		}
		payload, err := json.Marshal(result)
		if err != nil {
			return errs.ErrInternal
		}
		return tx.SaveReceipt(ctx, domainrepo.Receipt{
			OrganizationID: input.Principal.OrganizationID,
			ProjectID:      input.Principal.ProjectID, Scope: "repair_outbox_event",
			KeyHash: keyHash, RequestHash: requestHash, Payload: payload,
			CreatedAt: now,
		})
	})
	return result, err
}
