package service

import (
	"encoding/json"
	"fmt"
	"time"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	runtimeevents "github.com/codex-k8s/kodex/libs/go/platformevents/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

type runtimeEventPayloadOption func(*value.RuntimeEventPayload)

func (s *Service) slotEvent(eventType string, slot entity.Slot, occurredAt time.Time, options ...runtimeEventPayloadOption) (entity.OutboxEvent, error) {
	payload := value.RuntimeEventPayload{
		SlotID:         slot.ID.String(),
		SlotKey:        slot.SlotKey,
		Status:         string(slot.Status),
		RuntimeProfile: slot.RuntimeProfile,
		Fingerprint:    slot.Fingerprint,
		NamespaceName:  slot.NamespaceName,
		Version:        slot.Version,
	}
	if slot.FleetScopeID != nil {
		payload.FleetScopeID = slot.FleetScopeID.String()
	}
	if slot.ClusterID != nil {
		payload.ClusterID = slot.ClusterID.String()
	}
	if slot.AgentRunID != nil {
		payload.AgentRunID = slot.AgentRunID.String()
	}
	if slot.ProjectID != nil {
		payload.ProjectID = slot.ProjectID.String()
	}
	if slot.LeaseUntil != nil {
		payload.LeaseUntil = slot.LeaseUntil.UTC().Format(time.RFC3339Nano)
	}
	if slot.LastErrorCode != "" {
		payload.ErrorCode = slot.LastErrorCode
	}
	if slot.LastErrorMessage != "" {
		payload.ErrorMessage = slot.LastErrorMessage
	}
	for _, option := range options {
		option(&payload)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, fmt.Errorf("marshal runtime event payload %s: %w", eventType, err)
	}
	return entity.OutboxEvent{
		Event:         outboxlib.NewEvent(s.ids.New(), eventType, runtimeevents.SchemaVersion, aggregateTypeSlot, slot.ID, raw, occurredAt, 0),
		NextAttemptAt: occurredAt,
	}, nil
}

func payloadPreviousStatus(status string) runtimeEventPayloadOption {
	return func(payload *value.RuntimeEventPayload) {
		payload.PreviousStatus = status
	}
}

func (s *Service) workspaceEvent(
	eventType string,
	slot entity.Slot,
	materialization entity.WorkspaceMaterialization,
	occurredAt time.Time,
	options ...runtimeEventPayloadOption,
) (entity.OutboxEvent, error) {
	payload := value.RuntimeEventPayload{
		WorkspaceMaterializationID: materialization.ID.String(),
		SlotID:                     slot.ID.String(),
		SlotKey:                    slot.SlotKey,
		Status:                     string(materialization.Status),
		RuntimeProfile:             slot.RuntimeProfile,
		Fingerprint:                materialization.Fingerprint,
		NamespaceName:              slot.NamespaceName,
		Version:                    materialization.Version,
	}
	if slot.FleetScopeID != nil {
		payload.FleetScopeID = slot.FleetScopeID.String()
	}
	if slot.ClusterID != nil {
		payload.ClusterID = slot.ClusterID.String()
	}
	if slot.AgentRunID != nil {
		payload.AgentRunID = slot.AgentRunID.String()
	}
	if slot.ProjectID != nil {
		payload.ProjectID = slot.ProjectID.String()
	}
	if materialization.LastErrorCode != "" {
		payload.ErrorCode = materialization.LastErrorCode
	}
	if materialization.LastErrorMessage != "" {
		payload.ErrorMessage = materialization.LastErrorMessage
	}
	for _, option := range options {
		option(&payload)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, fmt.Errorf("marshal runtime event payload %s: %w", eventType, err)
	}
	return entity.OutboxEvent{
		Event:         outboxlib.NewEvent(s.ids.New(), eventType, runtimeevents.SchemaVersion, aggregateTypeWorkspace, materialization.ID, raw, occurredAt, 0),
		NextAttemptAt: occurredAt,
	}, nil
}

func (s *Service) workspaceProgressEvent(
	slot entity.Slot,
	materialization entity.WorkspaceMaterialization,
	previousStatus string,
	occurredAt time.Time,
) (*entity.OutboxEvent, error) {
	var eventType string
	switch materialization.Status {
	case enum.WorkspaceMaterializationStatusCompleted:
		eventType = eventWorkspaceCompleted
	case enum.WorkspaceMaterializationStatusFailed:
		eventType = eventWorkspaceFailed
	default:
		return nil, nil
	}
	event, err := s.workspaceEvent(eventType, slot, materialization, occurredAt, payloadPreviousStatus(previousStatus))
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func (s *Service) jobEvent(eventType string, job entity.Job, occurredAt time.Time, options ...runtimeEventPayloadOption) (entity.OutboxEvent, error) {
	payload := value.RuntimeEventPayload{
		JobID:        job.ID.String(),
		JobType:      string(job.JobType),
		Status:       string(job.Status),
		Version:      job.Version,
		FullLogRef:   job.FullLogRef,
		ErrorCode:    job.LastErrorCode,
		ErrorMessage: job.LastErrorMessage,
	}
	if job.SlotID != nil {
		payload.SlotID = job.SlotID.String()
	}
	if job.AgentRunID != nil {
		payload.AgentRunID = job.AgentRunID.String()
	}
	if job.ProjectID != nil {
		payload.ProjectID = job.ProjectID.String()
	}
	if job.RepositoryID != nil {
		payload.RepositoryID = job.RepositoryID.String()
	}
	if job.FleetScopeID != nil {
		payload.FleetScopeID = job.FleetScopeID.String()
	}
	if job.ClusterID != nil {
		payload.ClusterID = job.ClusterID.String()
	}
	for _, option := range options {
		option(&payload)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, fmt.Errorf("marshal runtime event payload %s: %w", eventType, err)
	}
	return entity.OutboxEvent{
		Event:         outboxlib.NewEvent(s.ids.New(), eventType, runtimeevents.SchemaVersion, aggregateTypeJob, job.ID, raw, occurredAt, 0),
		NextAttemptAt: occurredAt,
	}, nil
}

func payloadJobStep(step entity.JobStep) runtimeEventPayloadOption {
	return func(payload *value.RuntimeEventPayload) {
		payload.JobStepID = step.ID.String()
		payload.StepKey = step.StepKey
		payload.Status = string(step.Status)
		payload.ErrorCode = step.ErrorCode
		payload.ErrorMessage = step.ErrorMessage
	}
}
