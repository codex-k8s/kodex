package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

type webhookNormalizationResult struct {
	status           enum.WebhookProcessingStatus
	lastError        string
	facts            value.ProviderWebhookFacts
	projectionUpdate providerrepo.ProjectionUpdate
	providerEvents   []entity.ProviderEvent
	outboxEvents     []entity.OutboxEvent
}

func (s *Service) normalizeWebhook(ctx context.Context, webhook entity.WebhookEvent) (webhookNormalizationResult, error) {
	receivedEvent, err := s.webhookReceivedOutbox(webhook)
	if err != nil {
		return webhookNormalizationResult{}, err
	}
	facts, ok, err := s.normalizeProviderPayload(webhook)
	return s.normalizeWebhookFacts(ctx, webhook, receivedEvent, facts, ok, err)
}

func (s *Service) normalizeWebhookFacts(
	ctx context.Context,
	webhook entity.WebhookEvent,
	receivedEvent entity.OutboxEvent,
	facts value.ProviderWebhookFacts,
	ok bool,
	err error,
) (webhookNormalizationResult, error) {
	if err != nil {
		return webhookNormalizationResult{
			status:       enum.WebhookProcessingStatusFailed,
			lastError:    webhookLastErrorPayloadInvalid,
			outboxEvents: []entity.OutboxEvent{receivedEvent},
		}, nil
	}
	if !ok {
		return webhookNormalizationResult{
			status:       enum.WebhookProcessingStatusIgnored,
			outboxEvents: []entity.OutboxEvent{receivedEvent},
		}, nil
	}
	eventType, aggregateType, aggregateID, err := providerEventShape(facts)
	if err != nil {
		return webhookNormalizationResult{
			status:       enum.WebhookProcessingStatusFailed,
			lastError:    webhookLastErrorPayloadInvalid,
			outboxEvents: []entity.OutboxEvent{receivedEvent},
		}, nil
	}

	providerEventID := s.ids.New()
	providerEventPayload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:          string(webhook.ProviderSlug),
		WebhookEventID:        webhook.ID.String(),
		ProviderEventID:       providerEventID.String(),
		DeliveryID:            webhook.DeliveryID,
		EventName:             webhook.EventName,
		ProviderRepositoryID:  facts.RepositoryProviderID,
		RepositoryFullName:    facts.RepositoryFullName,
		ProviderWorkItemID:    facts.ProviderWorkItemID,
		ProviderCommentID:     facts.ProviderCommentID,
		Kind:                  facts.Kind,
		Number:                facts.Number,
		SignalKey:             repositoryChangeSignalKey(facts),
		SignalKind:            repositoryChangeSignalKind(facts),
		BaseBranch:            repositoryChangeBaseBranch(facts),
		HeadSHA:               repositoryChangeCommitSHA(facts),
		PathSummaryStatus:     repositoryChangePathSummaryStatus(facts),
		ChangedPathCount:      repositoryChangeChangedPathCount(facts),
		ServicesPolicyChanged: repositoryChangeServicesPolicyChanged(facts),
		DeployRelevantChanged: repositoryChangeDeployRelevantChanged(facts),
		ChangeFingerprint:     repositoryChangeFingerprint(facts),
	})
	if err != nil {
		return webhookNormalizationResult{}, err
	}
	sourceID := webhook.ID
	providerEvent := entity.ProviderEvent{
		ID:                   providerEventID,
		SourceWebhookEventID: &sourceID,
		EventType:            eventType,
		AggregateType:        aggregateType,
		AggregateID:          aggregateID,
		PayloadJSON:          providerEventPayload,
		OccurredAt:           facts.OccurredAt,
	}
	normalizedOutbox, err := s.webhookNormalizedOutbox(webhook, providerEvent, facts)
	if err != nil {
		return webhookNormalizationResult{}, err
	}
	projectionUpdate, projectionOutbox, err := s.projectionUpdateFromFacts(ctx, webhook, facts)
	if err != nil {
		return webhookNormalizationResult{}, err
	}
	outboxEvents := append([]entity.OutboxEvent{receivedEvent, normalizedOutbox}, projectionOutbox...)
	return webhookNormalizationResult{
		status:           enum.WebhookProcessingStatusProcessed,
		facts:            facts,
		projectionUpdate: projectionUpdate,
		providerEvents:   []entity.ProviderEvent{providerEvent},
		outboxEvents:     outboxEvents,
	}, nil
}

func (s *Service) webhookReceivedOutbox(webhook entity.WebhookEvent) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(webhook.ProviderSlug),
		WebhookEventID:       webhook.ID.String(),
		DeliveryID:           webhook.DeliveryID,
		EventName:            webhook.EventName,
		ProviderRepositoryID: webhook.RepositoryProviderID,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), providerEventWebhookReceived, providerAggregateWebhookEvent, webhook.ID, payload, webhook.ReceivedAt), nil
}

func (s *Service) webhookNormalizedOutbox(webhook entity.WebhookEvent, providerEvent entity.ProviderEvent, facts value.ProviderWebhookFacts) (entity.OutboxEvent, error) {
	payload, err := marshalProviderEventPayload(value.ProviderEventPayload{
		ProviderSlug:         string(webhook.ProviderSlug),
		WebhookEventID:       webhook.ID.String(),
		ProviderEventID:      providerEvent.ID.String(),
		DeliveryID:           webhook.DeliveryID,
		EventName:            webhook.EventName,
		ProviderRepositoryID: facts.RepositoryProviderID,
		RepositoryFullName:   facts.RepositoryFullName,
		ProviderWorkItemID:   facts.ProviderWorkItemID,
		ProviderCommentID:    facts.ProviderCommentID,
		Kind:                 facts.Kind,
		Number:               facts.Number,
	})
	if err != nil {
		return entity.OutboxEvent{}, err
	}
	return outboxEventRecord(s.ids.New(), providerEventWebhookNormalized, providerAggregateProviderEvent, providerEvent.ID, payload, providerEvent.OccurredAt), nil
}

func outboxEventRecord(id uuid.UUID, eventType string, aggregateType string, aggregateID uuid.UUID, payload []byte, occurredAt time.Time) entity.OutboxEvent {
	event := outboxlib.NewEvent(id, eventType, providerEventSchemaVersion, aggregateType, aggregateID, payload, occurredAt, 0)
	return outboxlib.RecordFromParts(event, outboxlib.RecordDelivery{}, outboxlib.RecordFailure{})
}

func (s *Service) normalizeProviderPayload(webhook entity.WebhookEvent) (value.ProviderWebhookFacts, bool, error) {
	if err := validateJSONObject(webhook.PayloadJSON); err != nil {
		return value.ProviderWebhookFacts{}, false, err
	}
	normalizer := s.webhookNormalizers[webhook.ProviderSlug]
	if normalizer == nil {
		return value.ProviderWebhookFacts{}, false, nil
	}
	facts, ok, err := normalizer.NormalizeWebhook(webhook)
	if !ok || err != nil {
		return facts, ok, err
	}
	if facts.RepositoryProviderID == "" {
		facts.RepositoryProviderID = webhook.RepositoryProviderID
	}
	if facts.OccurredAt.IsZero() {
		facts.OccurredAt = webhook.ReceivedAt.UTC()
	}
	return facts, true, nil
}

func providerEventShape(facts value.ProviderWebhookFacts) (string, string, string, error) {
	switch facts.FactKind {
	case value.ProviderWebhookFactKindWorkItem:
		if facts.ProviderWorkItemID == "" {
			return "", "", "", fmt.Errorf("provider webhook facts miss provider work item id")
		}
		return providerEventWorkItemSynced, providerAggregateWorkItem, facts.ProviderWorkItemID, nil
	case value.ProviderWebhookFactKindComment:
		if facts.ProviderCommentID == "" {
			return "", "", "", fmt.Errorf("provider webhook facts miss provider comment id")
		}
		return providerEventCommentSynced, providerAggregateComment, facts.ProviderCommentID, nil
	case value.ProviderWebhookFactKindRepositoryChange:
		if facts.RepositoryChange == nil || strings.TrimSpace(facts.RepositoryChange.SignalKey) == "" {
			return "", "", "", fmt.Errorf("provider webhook facts miss repository change signal key")
		}
		return providerEventRepositoryChanged, providerAggregateRepositoryChangeSignal, strings.TrimSpace(facts.RepositoryChange.SignalKey), nil
	default:
		return "", "", "", fmt.Errorf("unsupported provider webhook fact kind %q", facts.FactKind)
	}
}

func repositoryChangeSignalKey(facts value.ProviderWebhookFacts) string {
	if facts.RepositoryChange == nil {
		return ""
	}
	return strings.TrimSpace(facts.RepositoryChange.SignalKey)
}

func repositoryChangeSignalKind(facts value.ProviderWebhookFacts) string {
	if facts.RepositoryChange == nil {
		return ""
	}
	return strings.TrimSpace(facts.RepositoryChange.EventKind)
}

func repositoryChangeBaseBranch(facts value.ProviderWebhookFacts) string {
	if facts.RepositoryChange == nil {
		return ""
	}
	return strings.TrimSpace(facts.RepositoryChange.BaseBranch)
}

func repositoryChangeCommitSHA(facts value.ProviderWebhookFacts) string {
	if facts.RepositoryChange == nil {
		return ""
	}
	return strings.TrimSpace(facts.RepositoryChange.CommitSHA)
}

func repositoryChangePathSummaryStatus(facts value.ProviderWebhookFacts) string {
	if facts.RepositoryChange == nil {
		return ""
	}
	return strings.TrimSpace(facts.RepositoryChange.PathSummaryStatus)
}

func repositoryChangeChangedPathCount(facts value.ProviderWebhookFacts) int64 {
	if facts.RepositoryChange == nil {
		return 0
	}
	return facts.RepositoryChange.ChangedPathCount
}

func repositoryChangeServicesPolicyChanged(facts value.ProviderWebhookFacts) bool {
	return facts.RepositoryChange != nil && facts.RepositoryChange.ServicesPolicyChanged
}

func repositoryChangeDeployRelevantChanged(facts value.ProviderWebhookFacts) bool {
	return facts.RepositoryChange != nil && facts.RepositoryChange.DeployRelevantChanged
}

func repositoryChangeFingerprint(facts value.ProviderWebhookFacts) string {
	if facts.RepositoryChange == nil {
		return ""
	}
	return strings.TrimSpace(facts.RepositoryChange.ChangeFingerprint)
}

func marshalProviderEventPayload(payload value.ProviderEventPayload) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func canonicalJSONObject(raw []byte) ([]byte, error) {
	if err := validateJSONObject(raw); err != nil {
		return nil, err
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, raw); err != nil {
		return nil, errs.ErrInvalidArgument
	}
	return compacted.Bytes(), nil
}

func validateJSONObject(raw []byte) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return errs.ErrInvalidArgument
	}
	if !json.Valid(trimmed) {
		return errs.ErrInvalidArgument
	}
	return nil
}
