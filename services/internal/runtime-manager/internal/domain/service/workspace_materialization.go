package service

import (
	"context"
	"encoding/json"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

// PrepareRuntime reserves a slot and starts workspace materialization as one idempotent facade command.
func (s *Service) PrepareRuntime(ctx context.Context, input PrepareRuntimeInput) (PrepareRuntimeResult, error) {
	if err := validatePrepareRuntimeInput(input); err != nil {
		return PrepareRuntimeResult{}, err
	}
	projectID := input.WorkspacePolicy.ProjectID
	if err := s.authorizeCommand(ctx, input.Meta, actionSlotReserve, slotResource(uuid.Nil, &projectID)); err != nil {
		return PrepareRuntimeResult{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionWorkspaceStart, workspaceResource(uuid.Nil, &projectID)); err != nil {
		return PrepareRuntimeResult{}, err
	}
	if replay, ok, err := s.prepareRuntimeReplay(ctx, input.Meta); err != nil || ok {
		if err == nil {
			err = validatePrepareRuntimeReplayScope(replay, input)
		}
		return replay, err
	}
	now := commandTime(input.Meta, s.clock.Now())
	owner, err := leaseOwner(input.Meta)
	if err != nil {
		return PrepareRuntimeResult{}, err
	}
	repositoryIDs := repositoryIDsFromSources(input.WorkspacePolicy.Sources)
	request, err := prepareRuntimePlacementRequest(input, repositoryIDs)
	if err != nil {
		return PrepareRuntimeResult{}, err
	}
	placement, err := s.resolvePlacement(ctx, request)
	if err != nil {
		return PrepareRuntimeResult{}, err
	}
	fleetScopeID := placement.FleetScopeID
	clusterID := placement.ClusterID
	slotID := s.ids.New()
	workspaceID := s.ids.New()
	leaseUntil := now.Add(s.config.DefaultLeaseTTL)
	activeWorkspaceMaterializationID := workspaceID
	slot := entity.Slot{
		Base: entity.Base{
			ID:        slotID,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		SlotKey:                          s.slotKey(slotID),
		Status:                           enum.SlotStatusMaterializing,
		RuntimeMode:                      input.RuntimeMode,
		FleetScopeID:                     &fleetScopeID,
		ClusterID:                        &clusterID,
		NamespaceName:                    s.namespaceName(slotID),
		AgentRunID:                       input.AgentRunID,
		ProjectID:                        &projectID,
		RepositoryIDs:                    repositoryIDs,
		ActiveWorkspaceMaterializationID: &activeWorkspaceMaterializationID,
		RuntimeProfile:                   strings.TrimSpace(input.RuntimeProfile),
		Fingerprint:                      strings.TrimSpace(input.WorkspacePolicy.PolicyDigest),
		LeaseOwner:                       owner,
		LeaseUntil:                       &leaseUntil,
	}
	materialization := newWorkspaceMaterialization(workspaceID, slot.ID, input.WorkspacePolicy, now)
	slotEvent, err := s.slotEvent(eventSlotReserved, slot, now)
	if err != nil {
		return PrepareRuntimeResult{}, err
	}
	workspaceEvent, err := s.workspaceEvent(eventWorkspaceStarted, slot, materialization, now)
	if err != nil {
		return PrepareRuntimeResult{}, err
	}
	resultPayload, err := prepareRuntimeCommandPayload(materialization.ID)
	if err != nil {
		return PrepareRuntimeResult{}, err
	}
	result, err := commandResult(input.Meta, operationPrepareRuntime, aggregateTypeSlot, slot.ID, resultPayload, now)
	if err != nil {
		return PrepareRuntimeResult{}, err
	}
	if err := s.repository.PrepareRuntime(ctx, slot, materialization, slotEvent, workspaceEvent, result); err != nil {
		return PrepareRuntimeResult{}, err
	}
	return PrepareRuntimeResult{Slot: slot, WorkspaceMaterialization: materialization, RuntimeContext: runtimeContext(slot, materialization)}, nil
}

// StartWorkspaceMaterialization starts source preparation in an existing slot.
func (s *Service) StartWorkspaceMaterialization(ctx context.Context, input StartWorkspaceMaterializationInput) (entity.WorkspaceMaterialization, error) {
	if err := validateStartWorkspaceInput(input); err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	slot, err := s.repository.GetSlot(ctx, input.SlotID)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionWorkspaceStart, workspaceResource(uuid.Nil, slot.ProjectID)); err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	if err := validateWorkspacePolicyProject(slot, input.WorkspacePolicy.ProjectID); err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	if replay, ok, err := aggregateReplay(ctx, input.Meta, operationStartWorkspace, aggregateTypeWorkspace, nil, s.findCommandResult, s.repository.GetWorkspaceMaterialization); err != nil || ok {
		if err == nil {
			err = validateWorkspaceReplayScope(replay, input.SlotID, input.WorkspacePolicy.PolicyDigest)
		}
		return replay, err
	}
	if slot.Status == enum.SlotStatusCleaned || slot.Status == enum.SlotStatusCleanupPending || slot.Status == enum.SlotStatusFailed {
		return entity.WorkspaceMaterialization{}, errs.ErrPreconditionFailed
	}
	if slot.ActiveWorkspaceMaterializationID != nil {
		return entity.WorkspaceMaterialization{}, errs.ErrConflict
	}
	now := commandTime(input.Meta, s.clock.Now())
	previousSlotVersion := slot.Version
	materialization := newWorkspaceMaterialization(s.ids.New(), slot.ID, input.WorkspacePolicy, now)
	slot.Status = enum.SlotStatusMaterializing
	slot.ActiveWorkspaceMaterializationID = nullableUUID(materialization.ID)
	slot.Fingerprint = strings.TrimSpace(input.WorkspacePolicy.PolicyDigest)
	slot.UpdatedAt = now
	slot.Version++
	event, err := s.workspaceEvent(eventWorkspaceStarted, slot, materialization, now)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	result, err := commandResult(input.Meta, operationStartWorkspace, aggregateTypeWorkspace, materialization.ID, nil, now)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	return materialization, s.repository.CreateWorkspaceMaterialization(ctx, slot, materialization, previousSlotVersion, event, result)
}

// ReportWorkspaceMaterializationProgress updates preparation status, fingerprint and safe error details.
func (s *Service) ReportWorkspaceMaterializationProgress(ctx context.Context, input ReportWorkspaceMaterializationProgressInput) (entity.WorkspaceMaterialization, error) {
	if err := validateReportWorkspaceInput(input); err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	materialization, err := s.repository.GetWorkspaceMaterialization(ctx, input.WorkspaceMaterializationID)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	slot, err := s.repository.GetSlot(ctx, materialization.SlotID)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	if err := s.authorizeCommand(ctx, input.Meta, actionWorkspaceReport, workspaceResource(materialization.ID, slot.ProjectID)); err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	if replay, ok, err := aggregateReplay(ctx, input.Meta, operationReportWorkspace, aggregateTypeWorkspace, &input.WorkspaceMaterializationID, s.findCommandResult, s.repository.GetWorkspaceMaterialization); err != nil || ok {
		return replay, err
	}
	expected, err := expectedVersion(input.Meta)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	if materialization.Version != expected || terminalWorkspaceStatus(materialization.Status) {
		return entity.WorkspaceMaterialization{}, errs.ErrConflict
	}
	if !activeWorkspaceMaterialization(slot, materialization.ID) || slot.Status != enum.SlotStatusMaterializing {
		return entity.WorkspaceMaterialization{}, errs.ErrConflict
	}
	now := commandTime(input.Meta, s.clock.Now())
	previousSlotVersion := slot.Version
	previousStatus := string(materialization.Status)
	updateWorkspaceMaterialization(&materialization, input, now)
	updateSlotAfterWorkspaceProgress(&slot, materialization, now)
	event, err := s.workspaceProgressEvent(slot, materialization, previousStatus, now)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	result, err := commandResult(input.Meta, operationReportWorkspace, aggregateTypeWorkspace, materialization.ID, nil, now)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	return materialization, s.repository.UpdateWorkspaceMaterialization(ctx, slot, materialization, previousSlotVersion, expected, event, result)
}

// GetWorkspaceMaterialization returns authoritative source preparation state.
func (s *Service) GetWorkspaceMaterialization(ctx context.Context, input GetWorkspaceMaterializationInput) (entity.WorkspaceMaterialization, error) {
	if input.WorkspaceMaterializationID == uuid.Nil {
		return entity.WorkspaceMaterialization{}, errs.ErrInvalidArgument
	}
	materialization, err := s.repository.GetWorkspaceMaterialization(ctx, input.WorkspaceMaterializationID)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	slot, err := s.repository.GetSlot(ctx, materialization.SlotID)
	if err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	if err := s.authorizeQuery(ctx, input.Meta, actionWorkspaceRead, workspaceResource(materialization.ID, slot.ProjectID)); err != nil {
		return entity.WorkspaceMaterialization{}, err
	}
	return materialization, nil
}

// ListWorkspaceMaterializations returns source preparation attempts by slot, agent run and status.
func (s *Service) ListWorkspaceMaterializations(ctx context.Context, input ListWorkspaceMaterializationsInput) (ListWorkspaceMaterializationsResult, error) {
	if input.SlotID != nil {
		slot, err := s.repository.GetSlot(ctx, *input.SlotID)
		if err != nil {
			return ListWorkspaceMaterializationsResult{}, err
		}
		if err := s.authorizeQuery(ctx, input.Meta, actionWorkspaceList, workspaceResource(uuid.Nil, slot.ProjectID)); err != nil {
			return ListWorkspaceMaterializationsResult{}, err
		}
	} else if err := s.authorizeQuery(ctx, input.Meta, actionWorkspaceList, workspaceResource(uuid.Nil, nil)); err != nil {
		return ListWorkspaceMaterializationsResult{}, err
	}
	filter := query.WorkspaceMaterializationFilter{
		SlotID:     input.SlotID,
		AgentRunID: input.AgentRunID,
		Statuses:   append([]enum.WorkspaceMaterializationStatus(nil), input.Statuses...),
		Page:       input.Page,
	}
	items, page, err := s.repository.ListWorkspaceMaterializations(ctx, filter)
	return ListWorkspaceMaterializationsResult{WorkspaceMaterializations: items, Page: page}, err
}

func validatePrepareRuntimeInput(input PrepareRuntimeInput) error {
	if strings.TrimSpace(input.RuntimeProfile) == "" || !validRuntimeMode(input.RuntimeMode) {
		return errs.ErrInvalidArgument
	}
	if err := validateWorkspacePolicy(input.WorkspacePolicy); err != nil {
		return err
	}
	_, err := commandIdentity(input.Meta, operationPrepareRuntime)
	return err
}

func validateStartWorkspaceInput(input StartWorkspaceMaterializationInput) error {
	if input.SlotID == uuid.Nil {
		return errs.ErrInvalidArgument
	}
	if err := validateWorkspacePolicy(input.WorkspacePolicy); err != nil {
		return err
	}
	_, err := commandIdentity(input.Meta, operationStartWorkspace)
	return err
}

func validateReportWorkspaceInput(input ReportWorkspaceMaterializationProgressInput) error {
	if input.WorkspaceMaterializationID == uuid.Nil || !validWorkspaceStatus(input.Status) {
		return errs.ErrInvalidArgument
	}
	switch input.Status {
	case enum.WorkspaceMaterializationStatusCompleted:
		if strings.TrimSpace(input.Fingerprint) == "" {
			return errs.ErrInvalidArgument
		}
	case enum.WorkspaceMaterializationStatusFailed:
		if strings.TrimSpace(input.ErrorCode) == "" {
			return errs.ErrInvalidArgument
		}
	}
	_, err := commandIdentity(input.Meta, operationReportWorkspace)
	return err
}

func validateWorkspacePolicy(policy WorkspacePolicyInput) error {
	if policy.ProjectID == uuid.Nil || strings.TrimSpace(policy.PolicyDigest) == "" || len(policy.Sources) == 0 {
		return errs.ErrInvalidArgument
	}
	for _, source := range policy.Sources {
		if err := validateWorkspaceSource(source); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkspaceSource(source value.WorkspaceSource) error {
	if strings.TrimSpace(source.SourceID) == "" || strings.TrimSpace(source.LocalPath) == "" {
		return errs.ErrInvalidArgument
	}
	if !validWorkspaceSourceKind(source.Kind) || !validWorkspaceAccessMode(source.AccessMode) {
		return errs.ErrInvalidArgument
	}
	localPath := strings.TrimSpace(source.LocalPath)
	if strings.HasPrefix(localPath, "/") || strings.Contains(localPath, "\\") {
		return errs.ErrInvalidArgument
	}
	clean := path.Clean(localPath)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return errs.ErrInvalidArgument
	}
	if len(source.Metadata) > 0 && !json.Valid(source.Metadata) {
		return errs.ErrInvalidArgument
	}
	return nil
}

func validWorkspaceSourceKind(kind enum.WorkspaceSourceKind) bool {
	switch kind {
	case enum.WorkspaceSourceKindCode, enum.WorkspaceSourceKindDocumentation, enum.WorkspaceSourceKindGuidancePackage, enum.WorkspaceSourceKindGeneratedContext:
		return true
	default:
		return false
	}
}

func validWorkspaceAccessMode(mode enum.WorkspaceSourceAccessMode) bool {
	switch mode {
	case enum.WorkspaceSourceAccessModeRead, enum.WorkspaceSourceAccessModeWrite:
		return true
	default:
		return false
	}
}

func validWorkspaceStatus(status enum.WorkspaceMaterializationStatus) bool {
	switch status {
	case enum.WorkspaceMaterializationStatusPending, enum.WorkspaceMaterializationStatusRunning, enum.WorkspaceMaterializationStatusCompleted, enum.WorkspaceMaterializationStatusFailed, enum.WorkspaceMaterializationStatusCancelled:
		return true
	default:
		return false
	}
}

func terminalWorkspaceStatus(status enum.WorkspaceMaterializationStatus) bool {
	switch status {
	case enum.WorkspaceMaterializationStatusCompleted, enum.WorkspaceMaterializationStatusFailed, enum.WorkspaceMaterializationStatusCancelled:
		return true
	default:
		return false
	}
}

func newWorkspaceMaterialization(id uuid.UUID, slotID uuid.UUID, policy WorkspacePolicyInput, now time.Time) entity.WorkspaceMaterialization {
	startedAt := now
	return entity.WorkspaceMaterialization{
		Base: entity.Base{
			ID:        id,
			Version:   1,
			CreatedAt: now,
			UpdatedAt: now,
		},
		SlotID:       slotID,
		Status:       enum.WorkspaceMaterializationStatusRunning,
		PolicyDigest: strings.TrimSpace(policy.PolicyDigest),
		Sources:      normalizeWorkspaceSources(policy.Sources),
		StartedAt:    &startedAt,
	}
}

func normalizeWorkspaceSources(sources []value.WorkspaceSource) []value.WorkspaceSource {
	result := make([]value.WorkspaceSource, 0, len(sources))
	for _, source := range sources {
		normalized := source
		normalized.SourceID = strings.TrimSpace(source.SourceID)
		normalized.Provider = strings.TrimSpace(source.Provider)
		normalized.ProviderOwner = strings.TrimSpace(source.ProviderOwner)
		normalized.ProviderName = strings.TrimSpace(source.ProviderName)
		normalized.SourceRef = strings.TrimSpace(source.SourceRef)
		normalized.CommitSHA = strings.TrimSpace(source.CommitSHA)
		normalized.LocalPath = path.Clean(strings.TrimSpace(source.LocalPath))
		normalized.Digest = strings.TrimSpace(source.Digest)
		if len(normalized.Metadata) == 0 {
			normalized.Metadata = json.RawMessage(`{}`)
		}
		result = append(result, normalized)
	}
	return result
}

func repositoryIDsFromSources(sources []value.WorkspaceSource) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(sources))
	result := make([]uuid.UUID, 0, len(sources))
	for _, source := range sources {
		if source.RepositoryID == nil {
			continue
		}
		if _, ok := seen[*source.RepositoryID]; ok {
			continue
		}
		seen[*source.RepositoryID] = struct{}{}
		result = append(result, *source.RepositoryID)
	}
	return result
}

func updateWorkspaceMaterialization(materialization *entity.WorkspaceMaterialization, input ReportWorkspaceMaterializationProgressInput, now time.Time) {
	materialization.Status = input.Status
	materialization.Fingerprint = strings.TrimSpace(input.Fingerprint)
	materialization.LastErrorCode = strings.TrimSpace(input.ErrorCode)
	materialization.LastErrorMessage = strings.TrimSpace(input.ErrorMessage)
	if input.StartedAt != nil {
		materialization.StartedAt = input.StartedAt
	}
	if input.FinishedAt != nil {
		materialization.FinishedAt = input.FinishedAt
	}
	if terminalWorkspaceStatus(input.Status) && materialization.FinishedAt == nil {
		finishedAt := now
		materialization.FinishedAt = &finishedAt
	}
	materialization.UpdatedAt = now
	materialization.Version++
}

func updateSlotAfterWorkspaceProgress(slot *entity.Slot, materialization entity.WorkspaceMaterialization, now time.Time) {
	switch materialization.Status {
	case enum.WorkspaceMaterializationStatusCompleted:
		slot.Status = enum.SlotStatusReady
		slot.ActiveWorkspaceMaterializationID = nil
		slot.Fingerprint = materialization.Fingerprint
		slot.LastErrorCode = ""
		slot.LastErrorMessage = ""
	case enum.WorkspaceMaterializationStatusFailed:
		slot.Status = enum.SlotStatusFailed
		slot.ActiveWorkspaceMaterializationID = nil
		slot.LastErrorCode = materialization.LastErrorCode
		slot.LastErrorMessage = materialization.LastErrorMessage
	case enum.WorkspaceMaterializationStatusCancelled:
		slot.Status = enum.SlotStatusCleanupPending
		slot.ActiveWorkspaceMaterializationID = nil
	default:
		slot.Status = enum.SlotStatusMaterializing
		slot.ActiveWorkspaceMaterializationID = nullableUUID(materialization.ID)
	}
	slot.UpdatedAt = now
	slot.Version++
}

func validateWorkspacePolicyProject(slot entity.Slot, projectID uuid.UUID) error {
	if slot.ProjectID == nil || *slot.ProjectID != projectID {
		return errs.ErrConflict
	}
	return nil
}

func activeWorkspaceMaterialization(slot entity.Slot, materializationID uuid.UUID) bool {
	return slot.ActiveWorkspaceMaterializationID != nil && *slot.ActiveWorkspaceMaterializationID == materializationID
}

func validateWorkspaceReplayScope(materialization entity.WorkspaceMaterialization, slotID uuid.UUID, policyDigest string) error {
	if materialization.SlotID != slotID || materialization.PolicyDigest != strings.TrimSpace(policyDigest) {
		return errs.ErrConflict
	}
	return nil
}

func validatePrepareRuntimeReplayScope(replay PrepareRuntimeResult, input PrepareRuntimeInput) error {
	if replay.Slot.ProjectID == nil || *replay.Slot.ProjectID != input.WorkspacePolicy.ProjectID {
		return errs.ErrConflict
	}
	if !sameUUIDPtr(replay.Slot.AgentRunID, input.AgentRunID) {
		return errs.ErrConflict
	}
	if replay.Slot.RuntimeProfile != strings.TrimSpace(input.RuntimeProfile) || replay.WorkspaceMaterialization.PolicyDigest != strings.TrimSpace(input.WorkspacePolicy.PolicyDigest) {
		return errs.ErrConflict
	}
	return nil
}
