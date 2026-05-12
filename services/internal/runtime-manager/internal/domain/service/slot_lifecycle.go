package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// ReserveSlot creates a runtime slot using fleet-manager placement.
func (s *Service) ReserveSlot(ctx context.Context, input ReserveSlotInput) (entity.Slot, error) {
	if err := validateReserveInput(input); err != nil {
		return entity.Slot{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionSlotReserve, slotResource(uuid.Nil, input.ProjectID)); err != nil {
		return entity.Slot{}, err
	}
	request, err := slotPlacementRequest(input)
	if err != nil {
		return entity.Slot{}, err
	}
	placementFingerprint, err := placementRequestFingerprint(request)
	if err != nil {
		return entity.Slot{}, err
	}
	if replay, result, ok, err := s.reserveSlotReplay(ctx, input.Meta); err != nil || ok {
		if err == nil {
			err = validateSlotReplayScope(replay, input, result, placementFingerprint)
		}
		return replay, err
	}
	now := commandTime(input.Meta, s.clock.Now())
	owner, err := leaseOwner(input.Meta)
	if err != nil {
		return entity.Slot{}, err
	}
	placement, err := s.resolvePlacement(ctx, request)
	if err != nil {
		return entity.Slot{}, err
	}
	fleetScopeID := placement.FleetScopeID
	clusterID := placement.ClusterID
	filter := query.ReusableSlotFilter{
		RuntimeProfile: strings.TrimSpace(input.RuntimeProfile),
		RuntimeMode:    input.RuntimeMode,
		Fingerprint:    strings.TrimSpace(input.WorkspacePolicyDigest),
		AgentRunID:     input.AgentRunID,
		ProjectID:      input.ProjectID,
		RepositoryIDs:  append([]uuid.UUID(nil), input.RepositoryIDs...),
		FleetScopeID:   &fleetScopeID,
		ClusterID:      &clusterID,
		LeaseOwner:     owner,
		LeaseUntil:     now.Add(s.config.DefaultLeaseTTL),
		Now:            now,
	}
	reused, err := s.repository.ClaimReusableSlot(ctx, filter, func(slot entity.Slot) (entity.OutboxEvent, entity.CommandResult, error) {
		return s.slotReservationRecords(input.Meta, slot, placementFingerprint, now)
	})
	if err == nil {
		return reused, nil
	}
	if !errors.Is(err, errs.ErrNotFound) {
		return entity.Slot{}, err
	}
	slotID := s.ids.New()
	leaseUntil := filter.LeaseUntil
	slot := entity.Slot{
		Base: entity.Base{
			ID:        slotID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		SlotKey:        s.slotKey(slotID),
		Status:         enum.SlotStatusReserved,
		RuntimeMode:    input.RuntimeMode,
		FleetScopeID:   &fleetScopeID,
		ClusterID:      &clusterID,
		NamespaceName:  s.namespaceName(slotID),
		AgentRunID:     input.AgentRunID,
		ProjectID:      input.ProjectID,
		RepositoryIDs:  append([]uuid.UUID(nil), input.RepositoryIDs...),
		RuntimeProfile: strings.TrimSpace(input.RuntimeProfile),
		Fingerprint:    strings.TrimSpace(input.WorkspacePolicyDigest),
		LeaseOwner:     owner,
		LeaseUntil:     &leaseUntil,
	}
	event, err := s.slotEvent(eventSlotReserved, slot, now)
	if err != nil {
		return entity.Slot{}, err
	}
	resultPayload, err := commandPayloadWithPlacementFingerprint(placementFingerprint)
	if err != nil {
		return entity.Slot{}, err
	}
	result, err := commandResult(input.Meta, operationReserveSlot, aggregateTypeSlot, slot.ID, resultPayload, now)
	if err != nil {
		return entity.Slot{}, err
	}
	return slot, s.repository.CreateSlot(ctx, slot, event, result)
}

// ExtendSlotLease extends the lease for an active slot.
func (s *Service) ExtendSlotLease(ctx context.Context, input ExtendSlotLeaseInput) (entity.Slot, error) {
	if err := validateExistingSlotCommand(input.SlotID, input.Meta, operationExtendSlotLease); err != nil {
		return entity.Slot{}, err
	}
	if strings.TrimSpace(input.LeaseOwner) == "" || input.LeaseUntil.IsZero() {
		return entity.Slot{}, errs.ErrInvalidArgument
	}
	slot, err := s.repository.GetSlot(ctx, input.SlotID)
	if err != nil {
		return entity.Slot{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionSlotExtendLease, slotResource(slot.ID, slot.ProjectID)); err != nil {
		return entity.Slot{}, err
	}
	if replay, ok, err := s.slotReplay(ctx, input.Meta, operationExtendSlotLease, &input.SlotID); err != nil || ok {
		return replay, err
	}
	expected, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.Slot{}, err
	}
	if err := validateActiveLeaseMutation(slot, input.LeaseOwner, s.clock.Now()); err != nil {
		return entity.Slot{}, err
	}
	if !input.LeaseUntil.After(s.clock.Now()) {
		return entity.Slot{}, errs.ErrPreconditionFailed
	}
	now := commandTime(input.Meta, s.clock.Now())
	previousStatus := string(slot.Status)
	slot.LeaseUntil = &input.LeaseUntil
	slot.UpdatedAt = now
	slot.Version = expected + 1
	event, err := s.slotEvent(eventSlotLeaseExtended, slot, now, payloadPreviousStatus(previousStatus))
	if err != nil {
		return entity.Slot{}, err
	}
	result, err := commandResult(input.Meta, operationExtendSlotLease, aggregateTypeSlot, slot.ID, nil, now)
	if err != nil {
		return entity.Slot{}, err
	}
	return slot, s.repository.UpdateSlot(ctx, slot, expected, event, &result)
}

// ReleaseSlot moves a slot to cleanup pending after the caller is done.
func (s *Service) ReleaseSlot(ctx context.Context, input ReleaseSlotInput) (entity.Slot, error) {
	if err := validateExistingSlotCommand(input.SlotID, input.Meta, operationReleaseSlot); err != nil {
		return entity.Slot{}, err
	}
	if strings.TrimSpace(input.LeaseOwner) == "" {
		return entity.Slot{}, errs.ErrInvalidArgument
	}
	slot, err := s.repository.GetSlot(ctx, input.SlotID)
	if err != nil {
		return entity.Slot{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionSlotRelease, slotResource(slot.ID, slot.ProjectID)); err != nil {
		return entity.Slot{}, err
	}
	if replay, ok, err := s.slotReplay(ctx, input.Meta, operationReleaseSlot, &input.SlotID); err != nil || ok {
		return replay, err
	}
	expected, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.Slot{}, err
	}
	if err := validateActiveLeaseMutation(slot, input.LeaseOwner, s.clock.Now()); err != nil {
		return entity.Slot{}, err
	}
	now := commandTime(input.Meta, s.clock.Now())
	previousStatus := string(slot.Status)
	slot.Status = enum.SlotStatusCleanupPending
	slot.ActiveWorkspaceMaterializationID = nil
	slot.LeaseOwner = ""
	slot.LeaseUntil = nil
	slot.UpdatedAt = now
	slot.Version = expected + 1
	event, err := s.slotEvent(eventSlotReleased, slot, now, payloadPreviousStatus(previousStatus))
	if err != nil {
		return entity.Slot{}, err
	}
	result, err := commandResult(input.Meta, operationReleaseSlot, aggregateTypeSlot, slot.ID, nil, now)
	if err != nil {
		return entity.Slot{}, err
	}
	return slot, s.repository.UpdateSlot(ctx, slot, expected, event, &result)
}

// MarkSlotFailed persists a classified slot failure.
func (s *Service) MarkSlotFailed(ctx context.Context, input MarkSlotFailedInput) (entity.Slot, error) {
	if err := validateExistingSlotCommand(input.SlotID, input.Meta, operationMarkSlotFailed); err != nil {
		return entity.Slot{}, err
	}
	if strings.TrimSpace(input.ErrorCode) == "" {
		return entity.Slot{}, errs.ErrInvalidArgument
	}
	slot, err := s.repository.GetSlot(ctx, input.SlotID)
	if err != nil {
		return entity.Slot{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionSlotFail, slotResource(slot.ID, slot.ProjectID)); err != nil {
		return entity.Slot{}, err
	}
	if replay, ok, err := s.slotReplay(ctx, input.Meta, operationMarkSlotFailed, &input.SlotID); err != nil || ok {
		return replay, err
	}
	expected, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.Slot{}, err
	}
	if slot.Status == enum.SlotStatusCleaned {
		return entity.Slot{}, errs.ErrPreconditionFailed
	}
	now := commandTime(input.Meta, s.clock.Now())
	previousStatus := string(slot.Status)
	slot.Status = enum.SlotStatusFailed
	slot.ActiveWorkspaceMaterializationID = nil
	slot.LastErrorCode = strings.TrimSpace(input.ErrorCode)
	slot.LastErrorMessage = strings.TrimSpace(input.ErrorMessage)
	slot.UpdatedAt = now
	slot.Version = expected + 1
	event, err := s.slotEvent(eventSlotFailed, slot, now, payloadPreviousStatus(previousStatus))
	if err != nil {
		return entity.Slot{}, err
	}
	result, err := commandResult(input.Meta, operationMarkSlotFailed, aggregateTypeSlot, slot.ID, nil, now)
	if err != nil {
		return entity.Slot{}, err
	}
	return slot, s.repository.UpdateSlot(ctx, slot, expected, event, &result)
}

// GetSlot returns authoritative slot state.
func (s *Service) GetSlot(ctx context.Context, input GetSlotInput) (entity.Slot, error) {
	slotID, err := requireSlotID(input.SlotID)
	if err != nil {
		return entity.Slot{}, err
	}
	slot, err := s.repository.GetSlot(ctx, slotID)
	if err != nil {
		return entity.Slot{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, actionSlotRead, slotResource(slot.ID, slot.ProjectID)); err != nil {
		return entity.Slot{}, err
	}
	return slot, nil
}

// ListSlots returns runtime slots by filters.
func (s *Service) ListSlots(ctx context.Context, input ListSlotsInput) (ListSlotsResult, error) {
	if err := s.authorizeQuery(ctx, input.Meta, actionSlotList, slotResource(uuid.Nil, input.ProjectID)); err != nil {
		return ListSlotsResult{}, err
	}
	filter := query.SlotFilter{
		ProjectID:      input.ProjectID,
		Statuses:       append([]enum.SlotStatus(nil), input.Statuses...),
		RuntimeProfile: strings.TrimSpace(input.RuntimeProfile),
		FleetScopeID:   input.FleetScopeID,
		AgentRunID:     input.AgentRunID,
		Page:           input.Page,
	}
	slots, page, err := s.repository.ListSlots(ctx, filter)
	return ListSlotsResult{Slots: slots, Page: page}, err
}

func validateReserveInput(input ReserveSlotInput) error {
	if strings.TrimSpace(input.RuntimeProfile) == "" || strings.TrimSpace(input.WorkspacePolicyDigest) == "" {
		return errs.ErrInvalidArgument
	}
	if !validRuntimeMode(input.RuntimeMode) {
		return errs.ErrInvalidArgument
	}
	if _, err := commandIdentity(input.Meta, operationReserveSlot); err != nil {
		return err
	}
	return nil
}

func requireSlotID(slotID uuid.UUID) (uuid.UUID, error) {
	if slotID == uuid.Nil {
		return uuid.Nil, errs.ErrInvalidArgument
	}
	return slotID, nil
}

func validateExistingSlotCommand(slotID uuid.UUID, meta value.CommandMeta, operation string) error {
	if slotID == uuid.Nil {
		return errs.ErrInvalidArgument
	}
	_, err := commandIdentity(meta, operation)
	return err
}

func validateActiveLeaseMutation(slot entity.Slot, leaseOwner string, now time.Time) error {
	if !activeSlotStatus(slot.Status) {
		return errs.ErrPreconditionFailed
	}
	if strings.TrimSpace(slot.LeaseOwner) == "" || slot.LeaseOwner != strings.TrimSpace(leaseOwner) {
		return errs.ErrConflict
	}
	if slot.LeaseUntil == nil || !slot.LeaseUntil.After(now) {
		return errs.ErrConflict
	}
	return nil
}

func (s *Service) reserveSlotReplay(ctx context.Context, meta value.CommandMeta) (entity.Slot, entity.CommandResult, bool, error) {
	return aggregateReplayWithResult(ctx, meta, operationReserveSlot, aggregateTypeSlot, s.findCommandResult, s.repository.GetSlot)
}

func validateSlotReplayScope(slot entity.Slot, input ReserveSlotInput, result entity.CommandResult, placementFingerprint string) error {
	if !sameUUIDPtr(slot.ProjectID, input.ProjectID) || !sameUUIDPtr(slot.AgentRunID, input.AgentRunID) {
		return errs.ErrConflict
	}
	if slot.RuntimeProfile != strings.TrimSpace(input.RuntimeProfile) ||
		slot.RuntimeMode != input.RuntimeMode ||
		slot.Fingerprint != strings.TrimSpace(input.WorkspacePolicyDigest) {
		return errs.ErrConflict
	}
	if !slices.Equal(normalizedPlacementUUIDs(slot.RepositoryIDs), normalizedPlacementUUIDs(input.RepositoryIDs)) {
		return errs.ErrConflict
	}
	return validatePlacementReplayFingerprint(result, placementFingerprint)
}

func sameUUIDPtr(left *uuid.UUID, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func commandTime(meta value.CommandMeta, fallback time.Time) time.Time {
	if !meta.OccurredAt.IsZero() {
		return meta.OccurredAt
	}
	return fallback
}

func validRuntimeMode(mode enum.RuntimeMode) bool {
	switch mode {
	case enum.RuntimeModeCodeOnly, enum.RuntimeModeFullEnv, enum.RuntimeModeReadOnlyProduction, enum.RuntimeModePlatformJob:
		return true
	default:
		return false
	}
}

func activeSlotStatus(status enum.SlotStatus) bool {
	switch status {
	case enum.SlotStatusPrewarmed, enum.SlotStatusReserved, enum.SlotStatusMaterializing, enum.SlotStatusReady, enum.SlotStatusInUse:
		return true
	default:
		return false
	}
}
