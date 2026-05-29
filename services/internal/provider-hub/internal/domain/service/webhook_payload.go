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
	webhookLastErrorPayloadInvalid      = "provider_payload_invalid"
	webhookLastErrorPayloadUnavailable  = "payload_unavailable"
	webhookLastErrorRefetchUnavailable  = "refetch_unavailable"
	webhookLastErrorProviderRateLimited = "provider_rate_limited"
	webhookLastErrorProviderTransient   = "provider_transient_error"
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
	mergedAt := ""
	if facts.MergeSignal != nil && !facts.MergeSignal.MergedAt.IsZero() {
		mergedAt = facts.MergeSignal.MergedAt.UTC().Format(time.RFC3339Nano)
	}
	pullRequestProviderID := ""
	pullRequestURL := ""
	baseBranch := ""
	headBranch := ""
	mergeCommitSHA := ""
	sourceRef := ""
	if facts.MergeSignal != nil {
		pullRequestProviderID = strings.TrimSpace(facts.MergeSignal.PullRequestProviderID)
		pullRequestURL = strings.TrimSpace(facts.MergeSignal.PullRequestURL)
		baseBranch = strings.TrimSpace(facts.MergeSignal.BaseBranch)
		headBranch = strings.TrimSpace(facts.MergeSignal.HeadBranch)
		mergeCommitSHA = strings.TrimSpace(facts.MergeSignal.MergeCommitSHA)
		sourceRef = strings.TrimSpace(facts.MergeSignal.SourceRef)
	}
	payload, err := json.Marshal(value.WebhookPayloadEnvelope{
		ProviderSlug:          string(webhook.ProviderSlug),
		DeliveryID:            webhook.DeliveryID,
		EventName:             webhook.EventName,
		RepositoryProviderID:  webhook.RepositoryProviderID,
		RepositoryFullName:    strings.TrimSpace(facts.RepositoryFullName),
		ProviderWorkItemID:    strings.TrimSpace(facts.ProviderWorkItemID),
		ProviderCommentID:     strings.TrimSpace(facts.ProviderCommentID),
		FactKind:              string(facts.FactKind),
		Kind:                  strings.TrimSpace(facts.Kind),
		Number:                facts.Number,
		PullRequestProviderID: pullRequestProviderID,
		PullRequestURL:        pullRequestURL,
		BaseBranch:            baseBranch,
		HeadBranch:            headBranch,
		MergeCommitSHA:        mergeCommitSHA,
		SourceRef:             sourceRef,
		MergedAt:              mergedAt,
		PayloadSHA256:         webhook.PayloadDigest,
		PayloadStorage:        string(storage),
		PayloadCleanupReason:  string(reason),
		PayloadExpiredAt:      expiredAt,
		RetainUntil:           retainUntil,
	})
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func webhookPayloadEnvelope(webhook entity.WebhookEvent) (value.WebhookPayloadEnvelope, bool) {
	var envelope value.WebhookPayloadEnvelope
	if err := json.Unmarshal(webhook.PayloadJSON, &envelope); err != nil {
		return value.WebhookPayloadEnvelope{}, false
	}
	if strings.TrimSpace(envelope.PayloadStorage) == "" {
		return value.WebhookPayloadEnvelope{}, false
	}
	return envelope, true
}
