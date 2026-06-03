package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/accesscatalog"
	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
)

func (s *Service) retryWebhookFromSafeStorage(ctx context.Context, webhook entity.WebhookEvent) (entity.WebhookEvent, error) {
	envelope, ok := webhookPayloadEnvelope(webhook)
	if !ok {
		return s.completeWebhookReprocessDiagnostic(ctx, webhook, webhookLastErrorPayloadUnavailable)
	}
	if webhookEnvelopeExpired(envelope) {
		return s.completeWebhookReprocessDiagnostic(ctx, webhook, string(value.WebhookPayloadCleanupReasonExpired))
	}
	if facts, ok := webhookFactsFromSafeEnvelope(webhook, envelope); ok {
		return s.retryWebhookWithFacts(ctx, webhook, facts)
	}
	facts, ok, err := s.refetchWebhookFacts(ctx, webhook, envelope)
	if err != nil {
		return s.completeWebhookRefetchError(ctx, webhook, err)
	}
	if !ok {
		return s.completeWebhookReprocessDiagnostic(ctx, webhook, webhookLastErrorRefetchUnavailable)
	}
	return s.retryWebhookWithFacts(ctx, webhook, facts)
}

func (s *Service) retryWebhookWithFacts(ctx context.Context, webhook entity.WebhookEvent, facts value.ProviderWebhookFacts) (entity.WebhookEvent, error) {
	receivedEvent, err := s.webhookReceivedOutbox(webhook)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	normalization, err := s.normalizeWebhookFacts(ctx, webhook, receivedEvent, facts, true, nil)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	webhook.ProcessingStatus = normalization.status
	webhook.LastError = normalization.lastError
	webhook, err = webhookForInboxStorage(webhook, normalization.facts)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	stored, err := s.repository.ProcessWebhookEvent(ctx, webhook, normalization.projectionUpdate, normalization.providerEvents, normalization.outboxEvents[1:])
	if errors.Is(err, errs.ErrNotFound) {
		return s.currentWebhookAfterConcurrentProcessing(ctx, webhook.ID)
	}
	return stored, err
}

func webhookFactsFromSafeEnvelope(webhook entity.WebhookEvent, envelope value.WebhookPayloadEnvelope) (value.ProviderWebhookFacts, bool) {
	if strings.TrimSpace(envelope.SignalKey) == "" ||
		strings.TrimSpace(envelope.SignalKind) == "" ||
		strings.TrimSpace(envelope.RepositoryFullName) == "" ||
		strings.TrimSpace(envelope.RepositoryProviderID) == "" ||
		strings.TrimSpace(envelope.BaseBranch) == "" ||
		strings.TrimSpace(envelope.CommitSHA) == "" ||
		strings.TrimSpace(envelope.PathSummaryStatus) == "" ||
		strings.TrimSpace(envelope.PathDigest) == "" ||
		strings.TrimSpace(envelope.ChangeFingerprint) == "" {
		return value.ProviderWebhookFacts{}, false
	}
	observedAt := webhook.ReceivedAt.UTC()
	return value.ProviderWebhookFacts{
		FactKind:             value.ProviderWebhookFactKindRepositoryChange,
		Kind:                 strings.TrimSpace(envelope.SignalKind),
		RepositoryFullName:   strings.TrimSpace(envelope.RepositoryFullName),
		RepositoryProviderID: strings.TrimSpace(envelope.RepositoryProviderID),
		OccurredAt:           observedAt,
		RepositoryChange: &value.ProviderRepositoryChangeSignalSnapshot{
			SignalKey:             strings.TrimSpace(envelope.SignalKey),
			EventKind:             strings.TrimSpace(envelope.SignalKind),
			RepositoryFullName:    strings.TrimSpace(envelope.RepositoryFullName),
			ProviderRepositoryID:  strings.TrimSpace(envelope.RepositoryProviderID),
			Ref:                   strings.TrimSpace(envelope.Ref),
			BaseBranch:            strings.TrimSpace(envelope.BaseBranch),
			CommitSHA:             strings.TrimSpace(envelope.CommitSHA),
			BeforeSHA:             strings.TrimSpace(envelope.BeforeSHA),
			SourceRef:             strings.TrimSpace(envelope.SourceRef),
			PullRequestNumber:     envelope.Number,
			PullRequestProviderID: strings.TrimSpace(envelope.PullRequestProviderID),
			PullRequestURL:        strings.TrimSpace(envelope.PullRequestURL),
			PathSummaryStatus:     strings.TrimSpace(envelope.PathSummaryStatus),
			ChangedPathCount:      envelope.ChangedPathCount,
			PathDigest:            strings.TrimSpace(envelope.PathDigest),
			PathCategories:        envelope.PathCategories,
			ServicesPolicyChanged: envelope.ServicesPolicyChanged,
			DeployRelevantChanged: envelope.DeployRelevantChanged,
			ChangeFingerprint:     strings.TrimSpace(envelope.ChangeFingerprint),
			ObservedAt:            observedAt,
		},
	}, true
}

func webhookEnvelopeExpired(envelope value.WebhookPayloadEnvelope) bool {
	storage := value.WebhookPayloadStorage(strings.TrimSpace(envelope.PayloadStorage))
	reason := value.WebhookPayloadCleanupReason(strings.TrimSpace(envelope.PayloadCleanupReason))
	return storage == value.WebhookPayloadStorageExpired || reason == value.WebhookPayloadCleanupReasonExpired
}

func (s *Service) completeWebhookReprocessDiagnostic(ctx context.Context, webhook entity.WebhookEvent, reason string) (entity.WebhookEvent, error) {
	webhook.ProcessingStatus = enum.WebhookProcessingStatusIgnored
	webhook.LastError = strings.TrimSpace(reason)
	var err error
	webhook, err = s.webhookForReprocessDiagnosticStorage(webhook, reason)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	stored, err := s.repository.ProcessWebhookEvent(ctx, webhook, providerrepo.ProjectionUpdate{}, nil, nil)
	if errors.Is(err, errs.ErrNotFound) {
		return s.currentWebhookAfterConcurrentProcessing(ctx, webhook.ID)
	}
	return stored, err
}

func (s *Service) completeWebhookRefetchError(ctx context.Context, webhook entity.WebhookEvent, err error) (entity.WebhookEvent, error) {
	var providerErr *providerclient.Error
	if errors.As(err, &providerErr) {
		switch providerErr.Kind {
		case providerclient.ErrorKindRateLimited:
			return s.completeWebhookReprocessFailure(ctx, webhook, webhookLastErrorProviderRateLimited, errs.ErrPreconditionFailed)
		case providerclient.ErrorKindTransient:
			return s.completeWebhookReprocessFailure(ctx, webhook, webhookLastErrorProviderTransient, errs.ErrDependencyUnavailable)
		default:
			return s.completeWebhookReprocessDiagnostic(ctx, webhook, webhookLastErrorRefetchUnavailable)
		}
	}
	if errors.Is(err, errs.ErrDependencyUnavailable) {
		return s.completeWebhookReprocessFailure(ctx, webhook, webhookLastErrorProviderTransient, err)
	}
	return s.completeWebhookReprocessDiagnostic(ctx, webhook, webhookLastErrorRefetchUnavailable)
}

func (s *Service) completeWebhookReprocessFailure(ctx context.Context, webhook entity.WebhookEvent, reason string, returnErr error) (entity.WebhookEvent, error) {
	webhook.ProcessingStatus = enum.WebhookProcessingStatusFailed
	webhook.LastError = strings.TrimSpace(reason)
	var err error
	webhook, err = s.webhookForReprocessDiagnosticStorage(webhook, reason)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	stored, err := s.repository.ProcessWebhookEvent(ctx, webhook, providerrepo.ProjectionUpdate{}, nil, nil)
	if errors.Is(err, errs.ErrNotFound) {
		return s.currentWebhookAfterConcurrentProcessing(ctx, webhook.ID)
	}
	if err != nil {
		return stored, err
	}
	return stored, returnErr
}

func (s *Service) webhookForReprocessDiagnosticStorage(webhook entity.WebhookEvent, reason string) (entity.WebhookEvent, error) {
	if strings.TrimSpace(webhook.PayloadDigest) == "" && len(webhook.PayloadJSON) > 0 {
		webhook.PayloadDigest = webhookPayloadDigest(webhook.PayloadJSON)
	}
	storage := value.WebhookPayloadStorageSafeEnvelope
	cleanupReason := value.WebhookPayloadCleanupReasonRemoved
	occurredAt := time.Time{}
	if reason == string(value.WebhookPayloadCleanupReasonExpired) {
		storage = value.WebhookPayloadStorageExpired
		cleanupReason = value.WebhookPayloadCleanupReasonExpired
		occurredAt = s.clock.Now().UTC()
	}
	if envelope, ok := webhookPayloadEnvelope(webhook); ok {
		envelope.ProviderSlug = string(webhook.ProviderSlug)
		envelope.DeliveryID = webhook.DeliveryID
		envelope.EventName = webhook.EventName
		envelope.RepositoryProviderID = webhook.RepositoryProviderID
		envelope.PayloadSHA256 = webhook.PayloadDigest
		envelope.PayloadStorage = string(storage)
		envelope.PayloadCleanupReason = string(cleanupReason)
		if !occurredAt.IsZero() {
			envelope.PayloadExpiredAt = occurredAt.Format(time.RFC3339Nano)
		}
		if !webhook.RetainUntil.IsZero() {
			envelope.RetainUntil = webhook.RetainUntil.UTC().Format(time.RFC3339Nano)
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			return entity.WebhookEvent{}, err
		}
		webhook.PayloadJSON = payload
		return webhook, nil
	}
	payload, err := webhookPayloadEnvelopeJSONWithCleanup(webhook, storage, cleanupReason, occurredAt)
	if err != nil {
		return entity.WebhookEvent{}, err
	}
	webhook.PayloadJSON = payload
	return webhook, nil
}

type webhookRefetchAccount struct {
	externalAccountID uuid.UUID
	scopeID           string
}

func (s *Service) refetchWebhookFacts(ctx context.Context, webhook entity.WebhookEvent, envelope value.WebhookPayloadEnvelope) (value.ProviderWebhookFacts, bool, error) {
	if s.accountUsage == nil || s.secretResolver == nil {
		return value.ProviderWebhookFacts{}, false, nil
	}
	refetcher := s.providerWebhookRefetchers[webhook.ProviderSlug]
	if refetcher == nil {
		return value.ProviderWebhookFacts{}, false, nil
	}
	account, ok, err := s.webhookRefetchAccount(ctx, webhook, envelope)
	if err != nil || !ok {
		return value.ProviderWebhookFacts{}, ok, err
	}
	usage, err := s.accountUsage.ResolveExternalAccountUsage(ctx, ExternalAccountUsageInput{
		ExternalAccountID: account.externalAccountID,
		ActionKey:         accesscatalog.ActionProviderReconciliationRun,
		ScopeType:         providerUsageScopeRepository,
		ScopeID:           account.scopeID,
	})
	if err != nil {
		return value.ProviderWebhookFacts{}, false, err
	}
	if enum.ProviderSlug(strings.TrimSpace(string(usage.ProviderSlug))) != webhook.ProviderSlug {
		return value.ProviderWebhookFacts{}, false, nil
	}
	secret, err := s.secretResolver.Resolve(ctx, secretresolver.SecretRef{StoreType: usage.SecretStoreType, StoreRef: usage.SecretStoreRef})
	if err != nil {
		return value.ProviderWebhookFacts{}, false, mapSecretResolverError(err)
	}
	defer secret.Clear()

	result, err := refetcher.RefetchWebhook(ctx, providerclient.WebhookRefetchRequest{
		Credential: providerclient.AccountCredential{
			ExternalAccountID: account.externalAccountID,
			ProviderSlug:      webhook.ProviderSlug,
			Token:             secret,
		},
		Webhook:    webhook,
		Envelope:   envelope,
		ObservedAt: s.clock.Now().UTC(),
	})
	if err != nil || !result.OK {
		return value.ProviderWebhookFacts{}, result.OK, err
	}
	return result.Facts, true, nil
}

func (s *Service) webhookRefetchAccount(ctx context.Context, webhook entity.WebhookEvent, envelope value.WebhookPayloadEnvelope) (webhookRefetchAccount, bool, error) {
	if !webhookRefetchableGitHubPullRequest(webhook, envelope) {
		return webhookRefetchAccount{}, false, nil
	}
	workItem, err := s.repository.GetWorkItemProjection(ctx, query.ProviderTargetLookup{
		ProviderSlug:       webhook.ProviderSlug,
		RepositoryFullName: strings.TrimSpace(envelope.RepositoryFullName),
		Kind:               enum.WorkItemKindPullRequest,
		Number:             envelope.Number,
	})
	if errors.Is(err, errs.ErrNotFound) {
		return webhookRefetchAccount{}, false, nil
	}
	if err != nil {
		return webhookRefetchAccount{}, false, err
	}
	if workItem.RepositoryID == nil {
		return webhookRefetchAccount{}, false, nil
	}
	signal := webhookEnvelopeMergeSignal(envelope, webhook.ReceivedAt)
	if signal.SourceRef == "" && signal.HeadBranch == "" {
		return webhookRefetchAccount{}, false, nil
	}
	for _, kind := range []enum.RepositoryMergeSignalKind{enum.RepositoryMergeSignalKindBootstrap, enum.RepositoryMergeSignalKindAdoption} {
		operation, _, ok, err := s.onboardingMergeOperation(ctx, kind, workItem, signal)
		if err != nil || !ok {
			if err != nil {
				return webhookRefetchAccount{}, false, err
			}
			continue
		}
		return webhookRefetchAccount{
			externalAccountID: operation.ExternalAccountID,
			scopeID:           workItem.RepositoryID.String(),
		}, true, nil
	}
	return webhookRefetchAccount{}, false, nil
}

func webhookRefetchableGitHubPullRequest(webhook entity.WebhookEvent, envelope value.WebhookPayloadEnvelope) bool {
	return webhook.ProviderSlug == enum.ProviderSlugGitHub &&
		strings.TrimSpace(webhook.EventName) == "pull_request" &&
		strings.TrimSpace(envelope.RepositoryFullName) != "" &&
		strings.TrimSpace(envelope.Kind) == string(enum.WorkItemKindPullRequest) &&
		envelope.Number > 0
}

func webhookEnvelopeMergeSignal(envelope value.WebhookPayloadEnvelope, fallback time.Time) value.ProviderRepositoryMergeSignalSnapshot {
	mergedAt := fallback.UTC()
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(envelope.MergedAt)); err == nil {
		mergedAt = parsed.UTC()
	}
	return value.ProviderRepositoryMergeSignalSnapshot{
		PullRequestProviderID: strings.TrimSpace(envelope.PullRequestProviderID),
		PullRequestURL:        strings.TrimSpace(envelope.PullRequestURL),
		BaseBranch:            strings.TrimSpace(envelope.BaseBranch),
		HeadBranch:            strings.TrimSpace(envelope.HeadBranch),
		MergeCommitSHA:        strings.TrimSpace(envelope.MergeCommitSHA),
		SourceRef:             strings.TrimSpace(envelope.SourceRef),
		MergedAt:              mergedAt,
	}
}
