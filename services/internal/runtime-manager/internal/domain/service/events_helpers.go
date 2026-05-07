package service

import (
	"encoding/json"
	"fmt"
	"time"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	runtimeevents "github.com/codex-k8s/kodex/libs/go/platformevents/runtime"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

type slotEventPayloadOption func(*value.RuntimeEventPayload)

func (s *Service) slotEvent(eventType string, slot entity.Slot, occurredAt time.Time, options ...slotEventPayloadOption) (entity.OutboxEvent, error) {
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

func payloadPreviousStatus(status string) slotEventPayloadOption {
	return func(payload *value.RuntimeEventPayload) {
		payload.PreviousStatus = status
	}
}
