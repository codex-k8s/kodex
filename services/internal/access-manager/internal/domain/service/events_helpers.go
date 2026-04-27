package service

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/access-manager/internal/domain/types/value"
)

const schemaVersionAccessEventV1 = 1

type accessPayloadStringField uint8

const (
	accessPayloadOrganizationID accessPayloadStringField = iota + 1
	accessPayloadKind
	accessPayloadStatus
	accessPayloadGroupID
	accessPayloadScopeType
	accessPayloadScopeID
	accessPayloadExternalAccountID
	accessPayloadExternalProviderID
	accessPayloadAccountType
	accessPayloadExternalAccountBindingID
	accessPayloadUsageScopeType
	accessPayloadUsageScopeID
)

type accessEventPayloadOption func(*value.AccessEventPayload)

func (s *Service) event(eventType string, aggregateType string, aggregateID uuid.UUID, payload value.AccessEventPayload, occurredAt time.Time) entity.OutboxEvent {
	eventID := s.ids.New()
	raw, _ := json.Marshal(payload)
	return entity.OutboxEvent{
		ID: eventID, EventType: eventType, SchemaVersion: schemaVersionAccessEventV1,
		AggregateType: aggregateType, AggregateID: aggregateID, Payload: raw, OccurredAt: occurredAt,
	}
}

func (s *Service) membershipEvent(eventType string, membership entity.Membership, occurredAt time.Time, reasonCode string) entity.OutboxEvent {
	return s.event(eventType, "membership", membership.ID, value.AccessEventPayload{
		MembershipID: membership.ID.String(),
		SubjectType:  string(membership.SubjectType),
		SubjectID:    membership.SubjectID.String(),
		TargetType:   string(membership.TargetType),
		TargetID:     membership.TargetID.String(),
		ReasonCode:   strings.TrimSpace(reasonCode),
		Version:      membership.Version,
	}, occurredAt)
}

func (s *Service) createdEvent(
	eventType string,
	aggregateType string,
	aggregateID uuid.UUID,
	occurredAt time.Time,
	options ...accessEventPayloadOption,
) entity.OutboxEvent {
	payload := value.AccessEventPayload{}
	for _, option := range options {
		option(&payload)
	}
	return s.event(eventType, aggregateType, aggregateID, payload, occurredAt)
}

func payloadString(field accessPayloadStringField, text string) accessEventPayloadOption {
	return func(payload *value.AccessEventPayload) {
		switch field {
		case accessPayloadOrganizationID:
			payload.OrganizationID = text
		case accessPayloadKind:
			payload.Kind = text
		case accessPayloadStatus:
			payload.Status = text
		case accessPayloadGroupID:
			payload.GroupID = text
		case accessPayloadScopeType:
			payload.ScopeType = text
		case accessPayloadScopeID:
			payload.ScopeID = text
		case accessPayloadExternalAccountID:
			payload.ExternalAccountID = text
		case accessPayloadExternalProviderID:
			payload.ExternalProviderID = text
		case accessPayloadAccountType:
			payload.AccountType = text
		case accessPayloadExternalAccountBindingID:
			payload.ExternalAccountBindingID = text
		case accessPayloadUsageScopeType:
			payload.UsageScopeType = text
		case accessPayloadUsageScopeID:
			payload.UsageScopeID = text
		}
	}
}

func payloadVersion(version int64) accessEventPayloadOption {
	return func(payload *value.AccessEventPayload) {
		payload.Version = version
	}
}
