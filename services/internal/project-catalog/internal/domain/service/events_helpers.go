package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	projectevents "github.com/codex-k8s/kodex/libs/go/platformevents/project"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/types/value"
)

type projectEventPayloadOption func(*value.ProjectEventPayload)

type projectPayloadStringField uint8

const (
	projectPayloadOrganizationID projectPayloadStringField = iota + 1
	projectPayloadSlug
	projectPayloadStatus
	projectPayloadProvider
	projectPayloadProviderOwner
	projectPayloadProviderName
	projectPayloadIconObjectURI
	projectPayloadPolicyID
	projectPayloadSourceCommit
	projectPayloadSourceBlob
	projectPayloadSourceRef
	projectPayloadSourcePath
	projectPayloadContentHash
	projectPayloadSummary
	projectPayloadProviderWorkItemProjectionID
	projectPayloadProviderWebURL
	projectPayloadOverrideID
	projectPayloadTargetType
	projectPayloadExpiresAt
	projectPayloadSourceID
	projectPayloadScopeType
	projectPayloadAccessMode
	projectPayloadBranchRulesID
	projectPayloadReleasePolicyID
	projectPayloadReleaseLineID
	projectPayloadPlacementPolicyID
)

func (s *Service) event(eventType string, aggregateType string, aggregateID uuid.UUID, payload value.ProjectEventPayload, occurredAt time.Time) (entity.OutboxEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return entity.OutboxEvent{}, fmt.Errorf("marshal project event payload %s: %w", eventType, err)
	}
	return entity.OutboxEvent{
		ID: s.ids.New(), EventType: eventType, SchemaVersion: projectevents.SchemaVersion,
		AggregateType: aggregateType, AggregateID: aggregateID, Payload: raw, OccurredAt: occurredAt,
	}, nil
}

func (s *Service) aggregateEvent(
	eventType string,
	aggregateType string,
	aggregateID uuid.UUID,
	occurredAt time.Time,
	options ...projectEventPayloadOption,
) (entity.OutboxEvent, error) {
	payload := value.ProjectEventPayload{}
	for _, option := range options {
		option(&payload)
	}
	return s.event(eventType, aggregateType, aggregateID, payload, occurredAt)
}

func payloadProjectID(id uuid.UUID) projectEventPayloadOption {
	return func(payload *value.ProjectEventPayload) { payload.ProjectID = id.String() }
}

func payloadRepositoryID(id uuid.UUID) projectEventPayloadOption {
	return func(payload *value.ProjectEventPayload) { payload.RepositoryID = id.String() }
}

func payloadField(field projectPayloadStringField, text string) projectEventPayloadOption {
	return func(payload *value.ProjectEventPayload) {
		switch field {
		case projectPayloadOrganizationID:
			payload.OrganizationID = text
		case projectPayloadSlug:
			payload.Slug = text
		case projectPayloadStatus:
			payload.Status = text
		case projectPayloadProvider:
			payload.Provider = text
		case projectPayloadProviderOwner:
			payload.ProviderOwner = text
		case projectPayloadProviderName:
			payload.ProviderName = text
		case projectPayloadIconObjectURI:
			payload.IconObjectURI = text
		case projectPayloadPolicyID:
			payload.PolicyID = text
		case projectPayloadSourceCommit:
			payload.SourceCommitSHA = text
		case projectPayloadSourceBlob:
			payload.SourceBlobSHA = text
		case projectPayloadSourceRef:
			payload.SourceRef = text
		case projectPayloadSourcePath:
			payload.SourcePath = text
		case projectPayloadContentHash:
			payload.ContentHash = text
		case projectPayloadSummary:
			payload.Summary = text
		case projectPayloadProviderWorkItemProjectionID:
			payload.ProviderWorkItemProjectionID = text
		case projectPayloadProviderWebURL:
			payload.ProviderWebURL = text
		case projectPayloadOverrideID:
			payload.OverrideID = text
		case projectPayloadTargetType:
			payload.TargetType = text
		case projectPayloadExpiresAt:
			payload.ExpiresAt = text
		case projectPayloadSourceID:
			payload.SourceID = text
		case projectPayloadScopeType:
			payload.ScopeType = text
		case projectPayloadAccessMode:
			payload.AccessMode = text
		case projectPayloadBranchRulesID:
			payload.BranchRulesID = text
		case projectPayloadReleasePolicyID:
			payload.ReleasePolicyID = text
		case projectPayloadReleaseLineID:
			payload.ReleaseLineID = text
		case projectPayloadPlacementPolicyID:
			payload.PlacementPolicyID = text
		}
	}
}

func payloadVersion(version int64) projectEventPayloadOption {
	return func(payload *value.ProjectEventPayload) { payload.Version = version }
}

func payloadPolicyVersion(version int64) projectEventPayloadOption {
	return func(payload *value.ProjectEventPayload) { payload.PolicyVersion = version }
}
