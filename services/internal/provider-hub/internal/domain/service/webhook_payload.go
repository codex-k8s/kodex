package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

const (
	webhookLastErrorPayloadInvalid     = "provider_payload_invalid"
	webhookLastErrorPayloadUnavailable = "payload_unavailable"
)

func webhookPayloadDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func webhookForInboxStorage(webhook entity.WebhookEvent, facts value.ProviderWebhookFacts) (entity.WebhookEvent, error) {
	if strings.TrimSpace(webhook.PayloadDigest) == "" {
		webhook.PayloadDigest = webhookPayloadDigest(webhook.PayloadJSON)
	}
	payload, err := webhookPayloadEnvelopeJSONWithFacts(webhook, value.WebhookPayloadStorageSafeEnvelope, "", time.Time{}, facts)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	webhook.PayloadJSON = payload
	return webhook, nil
}

func webhookPayloadEnvelopeJSON(webhook entity.WebhookEvent, storage value.WebhookPayloadStorage) ([]byte, error) {
	return webhookPayloadEnvelopeJSONWithFacts(webhook, storage, "", time.Time{}, value.ProviderWebhookFacts{})
}

func webhookPayloadEnvelopeJSONWithCleanup(webhook entity.WebhookEvent, storage value.WebhookPayloadStorage, reason value.WebhookPayloadCleanupReason, occurredAt time.Time) ([]byte, error) {
	return webhookPayloadEnvelopeJSONWithFacts(webhook, storage, reason, occurredAt, value.ProviderWebhookFacts{})
}

func webhookPayloadEnvelopeJSONWithFacts(
	webhook entity.WebhookEvent,
	storage value.WebhookPayloadStorage,
	reason value.WebhookPayloadCleanupReason,
	occurredAt time.Time,
	facts value.ProviderWebhookFacts,
) ([]byte, error) {
	retainUntil := ""
	if !webhook.RetainUntil.IsZero() {
		retainUntil = webhook.RetainUntil.UTC().Format(time.RFC3339Nano)
	}
	expiredAt := ""
	if !occurredAt.IsZero() {
		expiredAt = occurredAt.UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(value.WebhookPayloadEnvelope{
		ProviderSlug:         string(webhook.ProviderSlug),
		DeliveryID:           webhook.DeliveryID,
		EventName:            webhook.EventName,
		RepositoryProviderID: webhook.RepositoryProviderID,
		RepositoryFullName:   strings.TrimSpace(facts.RepositoryFullName),
		ProviderWorkItemID:   strings.TrimSpace(facts.ProviderWorkItemID),
		ProviderCommentID:    strings.TrimSpace(facts.ProviderCommentID),
		FactKind:             string(facts.FactKind),
		Kind:                 strings.TrimSpace(facts.Kind),
		Number:               facts.Number,
		PayloadSHA256:        webhook.PayloadDigest,
		PayloadStorage:       string(storage),
		PayloadCleanupReason: string(reason),
		PayloadExpiredAt:     expiredAt,
		RetainUntil:          retainUntil,
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func webhookPayloadStorage(webhook entity.WebhookEvent) (value.WebhookPayloadStorage, value.WebhookPayloadCleanupReason, bool) {
	var envelope value.WebhookPayloadEnvelope
	if err := json.Unmarshal(webhook.PayloadJSON, &envelope); err != nil {
		return "", "", false
	}
	storage := value.WebhookPayloadStorage(strings.TrimSpace(envelope.PayloadStorage))
	if storage == "" {
		return "", "", false
	}
	return storage, value.WebhookPayloadCleanupReason(strings.TrimSpace(envelope.PayloadCleanupReason)), true
}

func webhookPayloadUnavailableReason(webhook entity.WebhookEvent) string {
	storage, reason, ok := webhookPayloadStorage(webhook)
	if !ok {
		return ""
	}
	if storage == value.WebhookPayloadStorageExpired || reason == value.WebhookPayloadCleanupReasonExpired {
		return string(value.WebhookPayloadCleanupReasonExpired)
	}
	return webhookLastErrorPayloadUnavailable
}

func webhookPayloadUnavailableForReprocess(webhook entity.WebhookEvent) bool {
	storage, _, ok := webhookPayloadStorage(webhook)
	if !ok {
		return false
	}
	switch storage {
	case value.WebhookPayloadStorageSafeEnvelope,
		value.WebhookPayloadStorageRetained,
		value.WebhookPayloadStorageRedacted,
		value.WebhookPayloadStorageExpired:
		return true
	default:
		return false
	}
}
