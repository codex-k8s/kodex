package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	runtimerepo "github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/repository/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

const (
	defaultCleanupBatchLimit = 50
	maxCleanupBatchLimit     = 200
)

// CreateOrUpdateCleanupPolicy creates or updates runtime retention policy.
func (s *Service) CreateOrUpdateCleanupPolicy(ctx context.Context, input CreateOrUpdateCleanupPolicyInput) (entity.CleanupPolicy, error) {
	if err := validateCleanupPolicyInput(input); err != nil {
		return entity.CleanupPolicy{}, err
	}
	now := commandTime(input.Meta, s.clock.Now())
	if input.CleanupPolicyID == nil {
		if err := s.authorizeCommand(ctx, input.Meta, actionCleanupUpsert, cleanupPolicyResource(nil, input.ScopeType, input.ScopeID)); err != nil {
			return entity.CleanupPolicy{}, err
		}
		if replay, ok, err := s.cleanupPolicyReplay(ctx, input.Meta, operationUpsertCleanup, nil); err != nil || ok {
			if err == nil {
				err = validateCleanupPolicyReplay(replay, input)
			}
			return replay, err
		}
		policy := entity.CleanupPolicy{
			Base:             entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
			ScopeType:        input.ScopeType,
			ScopeID:          cleanScopeID(input.ScopeID),
			TTLSeconds:       input.TTLSeconds,
			FailedTTLSeconds: input.FailedTTLSeconds,
			KeepShortLogTail: input.KeepShortLogTail,
			Status:           input.Status,
		}
		result, err := commandResult(input.Meta, operationUpsertCleanup, aggregateTypeCleanup, policy.ID, nil, now)
		if err != nil {
			return entity.CleanupPolicy{}, err
		}
		return policy, s.repository.CreateCleanupPolicy(ctx, policy, result)
	}
	current, err := s.repository.GetCleanupPolicy(ctx, *input.CleanupPolicyID)
	if err != nil {
		return entity.CleanupPolicy{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionCleanupUpsert, cleanupPolicyResource(&current.ID, current.ScopeType, current.ScopeID)); err != nil {
		return entity.CleanupPolicy{}, err
	}
	if cleanupPolicyScopeChanged(current, input) {
		if err := s.authorizeCommand(ctx, input.Meta, actionCleanupUpsert, cleanupPolicyResource(input.CleanupPolicyID, input.ScopeType, input.ScopeID)); err != nil {
			return entity.CleanupPolicy{}, err
		}
	}
	if replay, ok, err := s.cleanupPolicyReplay(ctx, input.Meta, operationUpsertCleanup, input.CleanupPolicyID); err != nil || ok {
		if err == nil {
			err = validateCleanupPolicyReplay(replay, input)
		}
		return replay, err
	}
	expected, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.CleanupPolicy{}, err
	}
	policy := current
	policy.ScopeType = input.ScopeType
	policy.ScopeID = cleanScopeID(input.ScopeID)
	policy.TTLSeconds = input.TTLSeconds
	policy.FailedTTLSeconds = input.FailedTTLSeconds
	policy.KeepShortLogTail = input.KeepShortLogTail
	policy.Status = input.Status
	policy.UpdatedAt = now
	policy.Version = expected + 1
	result, err := commandResult(input.Meta, operationUpsertCleanup, aggregateTypeCleanup, policy.ID, nil, now)
	if err != nil {
		return entity.CleanupPolicy{}, err
	}
	return policy, s.repository.UpdateCleanupPolicy(ctx, policy, expected, result)
}

// RunCleanupBatch removes expired runtime data and records visible cleanup failures.
func (s *Service) RunCleanupBatch(ctx context.Context, input RunCleanupBatchInput) (RunCleanupBatchResult, error) {
	if err := validateCleanupBatchInput(input); err != nil {
		return RunCleanupBatchResult{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionCleanupRun, cleanupPolicyResource(input.CleanupPolicyID, enum.RuntimeScopePlatform, "")); err != nil {
		return RunCleanupBatchResult{}, err
	}
	if replay, ok, err := s.cleanupBatchReplay(ctx, input.Meta); err != nil || ok {
		return replay, err
	}
	now := commandTime(input.Meta, s.clock.Now())
	if !input.LeaseUntil.After(now) {
		return RunCleanupBatchResult{}, errs.ErrInvalidArgument
	}
	limit := input.Limit
	if limit <= 0 {
		limit = defaultCleanupBatchLimit
	}
	if limit > maxCleanupBatchLimit {
		limit = maxCleanupBatchLimit
	}
	filter := query.CleanupBatchFilter{
		CleanupPolicyID: input.CleanupPolicyID,
		Limit:           limit,
		LeaseOwner:      strings.TrimSpace(input.LeaseOwner),
		LeaseUntil:      input.LeaseUntil,
		Now:             now,
	}
	recordFactory := func(result runtimerepo.CleanupBatchResult) ([]entity.OutboxEvent, entity.CommandResult, error) {
		events := make([]entity.OutboxEvent, 0, len(result.CleanedSlots)+len(result.FailedSlots))
		for _, slot := range result.CleanedSlots {
			event, err := s.slotEvent(eventSlotCleaned, slot, now)
			if err != nil {
				return nil, entity.CommandResult{}, err
			}
			events = append(events, event)
		}
		for _, slot := range result.FailedSlots {
			event, err := s.cleanupFailedEvent(slot, input.CleanupPolicyID, now)
			if err != nil {
				return nil, entity.CommandResult{}, err
			}
			events = append(events, event)
		}
		payload, err := cleanupBatchPayload(result)
		if err != nil {
			return nil, entity.CommandResult{}, err
		}
		aggregateID := uuid.Nil
		if input.CleanupPolicyID != nil {
			aggregateID = *input.CleanupPolicyID
		}
		command, err := commandResult(input.Meta, operationRunCleanup, aggregateTypeCleanup, aggregateID, payload, now)
		if err != nil {
			return nil, entity.CommandResult{}, err
		}
		return events, command, nil
	}
	result, err := s.repository.RunCleanupBatch(ctx, filter, recordFactory)
	return cleanupBatchResultFromRepo(result), err
}

// CreateOrUpdatePrewarmPool creates or updates desired prewarmed slot capacity.
func (s *Service) CreateOrUpdatePrewarmPool(ctx context.Context, input CreateOrUpdatePrewarmPoolInput) (entity.PrewarmPool, error) {
	if err := validatePrewarmPoolInput(input); err != nil {
		return entity.PrewarmPool{}, err
	}
	fleetScopeID, err := s.defaultFleetScopeID(input.FleetScopeID)
	if err != nil {
		return entity.PrewarmPool{}, err
	}
	now := commandTime(input.Meta, s.clock.Now())
	if input.PrewarmPoolID == nil {
		if err := s.authorizeCommand(ctx, input.Meta, actionPrewarmUpsert, prewarmPoolResource(nil, input.ScopeType, input.ScopeID)); err != nil {
			return entity.PrewarmPool{}, err
		}
		if replay, ok, err := s.prewarmPoolReplay(ctx, input.Meta, operationUpsertPrewarm, nil); err != nil || ok {
			if err == nil {
				err = validatePrewarmPoolReplay(replay, input, fleetScopeID)
			}
			return replay, err
		}
		pool := entity.PrewarmPool{
			Base:               entity.Base{ID: s.ids.New(), Version: 1, CreatedAt: now, UpdatedAt: now},
			ScopeType:          input.ScopeType,
			ScopeID:            cleanScopeID(input.ScopeID),
			RuntimeProfile:     strings.TrimSpace(input.RuntimeProfile),
			FleetScopeID:       fleetScopeID,
			TargetSize:         input.TargetSize,
			Status:             input.Status,
			LastCapacityStatus: enum.CapacityStatusInsufficient,
		}
		if pool.TargetSize == 0 {
			pool.LastCapacityStatus = enum.CapacityStatusOK
		}
		result, err := commandResult(input.Meta, operationUpsertPrewarm, aggregateTypePrewarmPool, pool.ID, nil, now)
		if err != nil {
			return entity.PrewarmPool{}, err
		}
		return pool, s.repository.CreatePrewarmPool(ctx, pool, result)
	}
	current, err := s.repository.GetPrewarmPool(ctx, *input.PrewarmPoolID)
	if err != nil {
		return entity.PrewarmPool{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionPrewarmUpsert, prewarmPoolResource(&current.ID, current.ScopeType, current.ScopeID)); err != nil {
		return entity.PrewarmPool{}, err
	}
	if prewarmPoolScopeChanged(current, input) {
		if err := s.authorizeCommand(ctx, input.Meta, actionPrewarmUpsert, prewarmPoolResource(input.PrewarmPoolID, input.ScopeType, input.ScopeID)); err != nil {
			return entity.PrewarmPool{}, err
		}
	}
	if replay, ok, err := s.prewarmPoolReplay(ctx, input.Meta, operationUpsertPrewarm, input.PrewarmPoolID); err != nil || ok {
		if err == nil {
			err = validatePrewarmPoolReplay(replay, input, fleetScopeID)
		}
		return replay, err
	}
	expected, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.PrewarmPool{}, err
	}
	pool := current
	pool.ScopeType = input.ScopeType
	pool.ScopeID = cleanScopeID(input.ScopeID)
	pool.RuntimeProfile = strings.TrimSpace(input.RuntimeProfile)
	pool.FleetScopeID = fleetScopeID
	pool.TargetSize = input.TargetSize
	pool.Status = input.Status
	if pool.TargetSize == 0 {
		pool.LastCapacityStatus = enum.CapacityStatusOK
	}
	pool.UpdatedAt = now
	pool.Version = expected + 1
	result, err := commandResult(input.Meta, operationUpsertPrewarm, aggregateTypePrewarmPool, pool.ID, nil, now)
	if err != nil {
		return entity.PrewarmPool{}, err
	}
	return pool, s.repository.UpdatePrewarmPool(ctx, pool, expected, result)
}

// ReconcilePrewarmPool adjusts prewarmed slots toward the configured pool target.
func (s *Service) ReconcilePrewarmPool(ctx context.Context, input ReconcilePrewarmPoolInput) (entity.PrewarmPool, error) {
	if err := validateReconcilePrewarmPoolInput(input); err != nil {
		return entity.PrewarmPool{}, err
	}
	pool, err := s.repository.GetPrewarmPool(ctx, input.PrewarmPoolID)
	if err != nil {
		return entity.PrewarmPool{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionPrewarmReconcile, prewarmPoolResource(&pool.ID, pool.ScopeType, pool.ScopeID)); err != nil {
		return entity.PrewarmPool{}, err
	}
	if replay, ok, err := s.prewarmPoolReplay(ctx, input.Meta, operationReconcilePool, &input.PrewarmPoolID); err != nil || ok {
		return replay, err
	}
	now := commandTime(input.Meta, s.clock.Now())
	if !input.LeaseUntil.After(now) {
		return entity.PrewarmPool{}, errs.ErrInvalidArgument
	}
	filter := query.PrewarmPoolReconcileFilter{
		PrewarmPoolID: input.PrewarmPoolID,
		LeaseOwner:    strings.TrimSpace(input.LeaseOwner),
		LeaseUntil:    input.LeaseUntil,
		Now:           now,
	}
	recordFactory := func(state runtimerepo.PrewarmPoolReconcileState) (runtimerepo.PrewarmPoolReconcileRecord, []entity.OutboxEvent, entity.CommandResult, error) {
		record, err := s.prewarmReconcileRecord(state, now)
		if err != nil {
			return runtimerepo.PrewarmPoolReconcileRecord{}, nil, entity.CommandResult{}, err
		}
		events, err := s.prewarmReconcileEvents(record, now)
		if err != nil {
			return runtimerepo.PrewarmPoolReconcileRecord{}, nil, entity.CommandResult{}, err
		}
		command, err := commandResult(input.Meta, operationReconcilePool, aggregateTypePrewarmPool, record.Pool.ID, nil, now)
		if err != nil {
			return runtimerepo.PrewarmPoolReconcileRecord{}, nil, entity.CommandResult{}, err
		}
		return record, events, command, nil
	}
	return s.repository.ReconcilePrewarmPool(ctx, filter, recordFactory)
}

func validateCleanupPolicyInput(input CreateOrUpdateCleanupPolicyInput) error {
	if _, err := commandIdentity(input.Meta, operationUpsertCleanup); err != nil {
		return err
	}
	if !validCleanupScope(input.ScopeType, input.ScopeID) || input.TTLSeconds <= 0 || input.FailedTTLSeconds <= 0 || !validCleanupPolicyStatus(input.Status) {
		return errs.ErrInvalidArgument
	}
	if input.CleanupPolicyID != nil && input.Meta.ExpectedVersion == nil {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validateCleanupBatchInput(input RunCleanupBatchInput) error {
	if _, err := commandIdentity(input.Meta, operationRunCleanup); err != nil {
		return err
	}
	if strings.TrimSpace(input.LeaseOwner) == "" || input.LeaseUntil.IsZero() {
		return errs.ErrInvalidArgument
	}
	if input.Limit < 0 {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validatePrewarmPoolInput(input CreateOrUpdatePrewarmPoolInput) error {
	if _, err := commandIdentity(input.Meta, operationUpsertPrewarm); err != nil {
		return err
	}
	if !validPrewarmScope(input.ScopeType, input.ScopeID) || strings.TrimSpace(input.RuntimeProfile) == "" || input.TargetSize < 0 || !validPrewarmPoolStatus(input.Status) {
		return errs.ErrInvalidArgument
	}
	if input.PrewarmPoolID != nil && input.Meta.ExpectedVersion == nil {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validateReconcilePrewarmPoolInput(input ReconcilePrewarmPoolInput) error {
	if input.PrewarmPoolID == uuid.Nil || strings.TrimSpace(input.LeaseOwner) == "" || input.LeaseUntil.IsZero() {
		return errs.ErrInvalidArgument
	}
	_, err := commandIdentity(input.Meta, operationReconcilePool)
	return err
}

func validCleanupScope(scope enum.RuntimeScopeType, scopeID string) bool {
	switch scope {
	case enum.RuntimeScopePlatform:
		return cleanScopeID(scopeID) == ""
	case enum.RuntimeScopeProject, enum.RuntimeScopeRepository, enum.RuntimeScopeRuntimeProfile:
		return cleanScopeID(scopeID) != ""
	default:
		return false
	}
}

func validPrewarmScope(scope enum.PrewarmPoolScopeType, scopeID string) bool {
	switch scope {
	case enum.PrewarmPoolScopePlatform:
		return cleanScopeID(scopeID) == ""
	case enum.PrewarmPoolScopeOrganization, enum.PrewarmPoolScopeProject, enum.PrewarmPoolScopeRepository:
		return cleanScopeID(scopeID) != ""
	default:
		return false
	}
}

func validCleanupPolicyStatus(status enum.CleanupPolicyStatus) bool {
	switch status {
	case enum.CleanupPolicyStatusActive, enum.CleanupPolicyStatusDisabled, enum.CleanupPolicyStatusSuperseded:
		return true
	default:
		return false
	}
}

func validPrewarmPoolStatus(status enum.PrewarmPoolStatus) bool {
	switch status {
	case enum.PrewarmPoolStatusActive, enum.PrewarmPoolStatusPaused, enum.PrewarmPoolStatusDisabled:
		return true
	default:
		return false
	}
}

func cleanScopeID(scopeID string) string {
	return strings.TrimSpace(scopeID)
}

func cleanupPolicyResource(policyID *uuid.UUID, scope enum.RuntimeScopeType, scopeID string) resourceRef {
	return scopedRuntimeResource(accesscatalog.ResourceRuntimeCleanupPolicy, policyID, string(scope), scopeID)
}

func prewarmPoolResource(poolID *uuid.UUID, scope enum.PrewarmPoolScopeType, scopeID string) resourceRef {
	return scopedRuntimeResource(accesscatalog.ResourceRuntimePrewarmPool, poolID, string(scope), scopeID)
}

func scopedRuntimeResource(resourceType string, resourceUUID *uuid.UUID, scope string, scopeID string) resourceRef {
	resourceID := ""
	if resourceUUID != nil {
		resourceID = resourceUUID.String()
	}
	return resourceRef{
		Type:      resourceType,
		ID:        resourceID,
		ScopeType: accessScopeType(scope),
		ScopeID:   cleanScopeID(scopeID),
	}
}

func accessScopeType(scope string) string {
	switch scope {
	case accesscatalog.ScopeOrganization, accesscatalog.ScopeProject, accesscatalog.ScopeRepository:
		return scope
	default:
		return accesscatalog.ScopeGlobal
	}
}

func cleanupPolicyScopeChanged(policy entity.CleanupPolicy, input CreateOrUpdateCleanupPolicyInput) bool {
	return policy.ScopeType != input.ScopeType || policy.ScopeID != cleanScopeID(input.ScopeID)
}

func prewarmPoolScopeChanged(pool entity.PrewarmPool, input CreateOrUpdatePrewarmPoolInput) bool {
	return pool.ScopeType != input.ScopeType || pool.ScopeID != cleanScopeID(input.ScopeID)
}

func validateCleanupPolicyReplay(policy entity.CleanupPolicy, input CreateOrUpdateCleanupPolicyInput) error {
	if cleanupPolicyScopeChanged(policy, input) ||
		policy.TTLSeconds != input.TTLSeconds ||
		policy.FailedTTLSeconds != input.FailedTTLSeconds ||
		policy.KeepShortLogTail != input.KeepShortLogTail ||
		policy.Status != input.Status {
		return errs.ErrConflict
	}
	if input.CleanupPolicyID != nil && policy.ID != *input.CleanupPolicyID {
		return errs.ErrConflict
	}
	return nil
}

func validatePrewarmPoolReplay(pool entity.PrewarmPool, input CreateOrUpdatePrewarmPoolInput, fleetScopeID *uuid.UUID) error {
	if prewarmPoolScopeChanged(pool, input) ||
		pool.RuntimeProfile != strings.TrimSpace(input.RuntimeProfile) ||
		!sameUUIDPtr(pool.FleetScopeID, fleetScopeID) ||
		pool.TargetSize != input.TargetSize ||
		pool.Status != input.Status {
		return errs.ErrConflict
	}
	if input.PrewarmPoolID != nil && pool.ID != *input.PrewarmPoolID {
		return errs.ErrConflict
	}
	return nil
}

func (s *Service) cleanupPolicyReplay(ctx context.Context, meta value.CommandMeta, operation string, expectedID *uuid.UUID) (entity.CleanupPolicy, bool, error) {
	return aggregateReplay(ctx, meta, operation, aggregateTypeCleanup, expectedID, s.findCommandResult, s.repository.GetCleanupPolicy)
}

func (s *Service) prewarmPoolReplay(ctx context.Context, meta value.CommandMeta, operation string, expectedID *uuid.UUID) (entity.PrewarmPool, bool, error) {
	aggregateType := aggregateTypePrewarmPool
	load := s.repository.GetPrewarmPool
	findResult := s.findCommandResult
	return aggregateReplay(ctx, meta, operation, aggregateType, expectedID, findResult, load)
}

type cleanupBatchResultPayload struct {
	ClaimedCount    int         `json:"claimed_count"`
	CleanedCount    int         `json:"cleaned_count"`
	FailedCount     int         `json:"failed_count"`
	AffectedSlotIDs []uuid.UUID `json:"affected_slot_ids"`
}

func (s *Service) cleanupBatchReplay(ctx context.Context, meta value.CommandMeta) (RunCleanupBatchResult, bool, error) {
	result, ok, err := s.findCommandResult(ctx, meta, operationRunCleanup, aggregateTypeCleanup)
	if err != nil || !ok {
		return RunCleanupBatchResult{}, ok, err
	}
	var payload cleanupBatchResultPayload
	if err := json.Unmarshal(result.ResultPayload, &payload); err != nil {
		return RunCleanupBatchResult{}, true, errs.ErrConflict
	}
	return RunCleanupBatchResult{
		ClaimedCount:    payload.ClaimedCount,
		CleanedCount:    payload.CleanedCount,
		FailedCount:     payload.FailedCount,
		AffectedSlotIDs: append([]uuid.UUID(nil), payload.AffectedSlotIDs...),
	}, true, nil
}

func cleanupBatchPayload(result runtimerepo.CleanupBatchResult) ([]byte, error) {
	return json.Marshal(cleanupBatchResultPayload{
		ClaimedCount:    result.ClaimedCount,
		CleanedCount:    result.CleanedCount,
		FailedCount:     result.FailedCount,
		AffectedSlotIDs: append([]uuid.UUID(nil), result.AffectedSlotIDs...),
	})
}

func cleanupBatchResultFromRepo(result runtimerepo.CleanupBatchResult) RunCleanupBatchResult {
	return RunCleanupBatchResult{
		ClaimedCount:    result.ClaimedCount,
		CleanedCount:    result.CleanedCount,
		FailedCount:     result.FailedCount,
		AffectedSlotIDs: append([]uuid.UUID(nil), result.AffectedSlotIDs...),
	}
}

func (s *Service) cleanupFailedEvent(slot entity.Slot, cleanupPolicyID *uuid.UUID, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload := value.RuntimeEventPayload{
		SlotID:       slot.ID.String(),
		SlotKey:      slot.SlotKey,
		Status:       string(slot.Status),
		ErrorCode:    slot.LastErrorCode,
		ErrorMessage: slot.LastErrorMessage,
		Version:      slot.Version,
	}
	if cleanupPolicyID != nil {
		payload.CleanupPolicyID = cleanupPolicyID.String()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return entity.OutboxEvent{
		Event:         newRuntimeEvent(s.ids.New(), eventCleanupFailed, aggregateTypeCleanup, aggregateIDOrNil(cleanupPolicyID), raw, occurredAt),
		NextAttemptAt: occurredAt,
	}, nil
}

func (s *Service) prewarmReconcileRecord(state runtimerepo.PrewarmPoolReconcileState, now time.Time) (runtimerepo.PrewarmPoolReconcileRecord, error) {
	pool := state.Pool
	pool.UpdatedAt = now
	pool.Version++
	if pool.Status != enum.PrewarmPoolStatusActive {
		pool.LastCapacityStatus = enum.CapacityStatusDegraded
		return runtimerepo.PrewarmPoolReconcileRecord{Pool: pool}, nil
	}
	if pool.ScopeType == enum.PrewarmPoolScopeOrganization {
		pool.LastCapacityStatus = enum.CapacityStatusInsufficient
		return runtimerepo.PrewarmPoolReconcileRecord{Pool: pool}, nil
	}
	missing := pool.TargetSize - state.CurrentSize
	record := runtimerepo.PrewarmPoolReconcileRecord{Pool: pool}
	if missing > 0 {
		record.CreatedSlots = s.newPrewarmedSlots(pool, missing, now)
	}
	if missing < 0 {
		cleanupCount := minInt64(int64(len(state.ExcessSlots)), -missing)
		record.CleanupSlots = make([]entity.Slot, 0, cleanupCount)
		for index := int64(0); index < cleanupCount; index++ {
			slot := state.ExcessSlots[index]
			slot.Status = enum.SlotStatusCleanupPending
			slot.UpdatedAt = now
			slot.Version++
			record.CleanupSlots = append(record.CleanupSlots, slot)
		}
	}
	finalSize := state.CurrentSize + int64(len(record.CreatedSlots)) - int64(len(record.CleanupSlots))
	record.Pool.LastCapacityStatus = enum.CapacityStatusOK
	if finalSize < pool.TargetSize {
		record.Pool.LastCapacityStatus = enum.CapacityStatusInsufficient
	}
	return record, nil
}

func (s *Service) newPrewarmedSlots(pool entity.PrewarmPool, count int64, now time.Time) []entity.Slot {
	slots := make([]entity.Slot, 0, count)
	for index := int64(0); index < count; index++ {
		slotID := s.ids.New()
		slot := entity.Slot{
			Base: entity.Base{
				ID:        slotID,
				Version:   1,
				CreatedAt: now,
				UpdatedAt: now,
			},
			SlotKey:        s.slotKey(slotID),
			Status:         enum.SlotStatusPrewarmed,
			RuntimeMode:    enum.RuntimeModeCodeOnly,
			IsPrewarmed:    true,
			FleetScopeID:   pool.FleetScopeID,
			RuntimeProfile: pool.RuntimeProfile,
			NamespaceName:  s.namespaceName(slotID),
		}
		if s.config.DefaultClusterID != uuid.Nil {
			slot.ClusterID = &s.config.DefaultClusterID
		}
		applyPrewarmScope(&slot, pool)
		slots = append(slots, slot)
	}
	return slots
}

func applyPrewarmScope(slot *entity.Slot, pool entity.PrewarmPool) {
	scopeID, err := uuid.Parse(pool.ScopeID)
	if err != nil {
		return
	}
	switch pool.ScopeType {
	case enum.PrewarmPoolScopeProject:
		slot.ProjectID = &scopeID
	case enum.PrewarmPoolScopeRepository:
		slot.RepositoryIDs = []uuid.UUID{scopeID}
	}
}

func (s *Service) prewarmReconcileEvents(record runtimerepo.PrewarmPoolReconcileRecord, now time.Time) ([]entity.OutboxEvent, error) {
	events := make([]entity.OutboxEvent, 0, len(record.CleanupSlots)+1)
	for _, slot := range record.CleanupSlots {
		event, err := s.slotEvent(eventSlotCleanupRequested, slot, now)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	event, err := s.prewarmCapacityChangedEvent(record.Pool, now)
	if err != nil {
		return nil, err
	}
	events = append(events, event)
	return events, nil
}

func (s *Service) prewarmCapacityChangedEvent(pool entity.PrewarmPool, occurredAt time.Time) (entity.OutboxEvent, error) {
	payload := value.RuntimeEventPayload{
		PrewarmPoolID:  pool.ID.String(),
		CapacityStatus: string(pool.LastCapacityStatus),
		RuntimeProfile: string(pool.RuntimeProfile),
		TargetSize:     pool.TargetSize,
		Version:        pool.Version,
	}
	if pool.FleetScopeID != nil {
		payload.FleetScopeID = pool.FleetScopeID.String()
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return entity.OutboxEvent{
		Event:         newRuntimeEvent(s.ids.New(), eventPrewarmChanged, aggregateTypePrewarmPool, pool.ID, raw, occurredAt),
		NextAttemptAt: occurredAt,
	}, nil
}

func aggregateIDOrNil(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

func minInt64(left int64, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
