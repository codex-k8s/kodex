package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

func webhookPayloadDigest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func webhookForInboxStorage(webhook entity.WebhookEvent) (entity.WebhookEvent, error) {
	webhook.PayloadDigest = webhookPayloadDigest(webhook.PayloadJSON)
	if !webhookProcessingTerminal(webhook.ProcessingStatus) {
		return webhook, nil
	}
	payload, err := webhookPayloadEnvelopeJSON(webhook, value.WebhookPayloadStorageRedacted)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	webhook.PayloadJSON = payload
	return webhook, nil
}

func webhookProcessingTerminal(status enum.WebhookProcessingStatus) bool {
	switch status {
	case enum.WebhookProcessingStatusProcessed, enum.WebhookProcessingStatusIgnored:
		return true
	default:
		return false
	}
}

func webhookPayloadEnvelopeJSON(webhook entity.WebhookEvent, storage value.WebhookPayloadStorage) ([]byte, error) {
	retainUntil := ""
	if !webhook.RetainUntil.IsZero() {
		retainUntil = webhook.RetainUntil.UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(value.WebhookPayloadEnvelope{
		ProviderSlug:         string(webhook.ProviderSlug),
		DeliveryID:           webhook.DeliveryID,
		EventName:            webhook.EventName,
		RepositoryProviderID: webhook.RepositoryProviderID,
		PayloadSHA256:        webhook.PayloadDigest,
		PayloadStorage:       string(storage),
		RetainUntil:          retainUntil,
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}
