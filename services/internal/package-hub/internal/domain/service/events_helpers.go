package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	packageevents "github.com/codex-k8s/kodex/libs/go/platformevents/packagehub"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/package-hub/internal/domain/types/value"
)

func (s *Service) event(eventType string, aggregateType string, aggregateID uuid.UUID, payload value.PackageEventPayload, occurredAt time.Time) (entity.OutboxEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, fmt.Errorf("marshal package event payload %s: %w", eventType, err)
	}
	return entity.OutboxEvent{Event: outboxlib.Event{
		ID:            s.ids.New(),
		EventType:     eventType,
		SchemaVersion: packageevents.SchemaVersion,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Payload:       raw,
		OccurredAt:    occurredAt,
	}}, nil
}

func (s *Service) verificationUpdatedEvent(version entity.PackageVersion, occurredAt time.Time) (entity.OutboxEvent, error) {
	return s.event(packageEventVerificationUpdated, packageAggregateVersion, version.ID, value.PackageEventPayload{
		PackageID:          version.PackageID.String(),
		PackageVersionID:   version.ID.String(),
		VerificationStatus: string(version.VerificationStatus),
		ReleaseStatus:      string(version.ReleaseStatus),
		Revision:           version.Revision,
	}, occurredAt)
}
