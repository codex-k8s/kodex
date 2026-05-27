package service

import (
	"time"

	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
)

const (
	providerEventWebhookReceived              = providerevents.EventWebhookReceived
	providerEventWebhookNormalized            = providerevents.EventWebhookNormalized
	providerEventWorkItemSynced               = providerevents.EventWorkItemSynced
	providerEventCommentSynced                = providerevents.EventCommentSynced
	providerEventRelationshipSynced           = providerevents.EventRelationshipSynced
	providerEventSyncCursorAdvanced           = providerevents.EventSyncCursorAdvanced
	providerEventOperationCompleted           = providerevents.EventOperationCompleted
	providerEventOperationFailed              = providerevents.EventOperationFailed
	providerEventRepositoryCreated            = providerevents.EventRepositoryCreated
	providerEventRepositoryBootstrapCompleted = providerevents.EventRepositoryBootstrapCompleted
	providerEventRepositoryAdoptionPRCreated  = providerevents.EventRepositoryAdoptionPRCreated
	providerEventRepositoryBootstrapMerged    = providerevents.EventRepositoryBootstrapMerged
	providerEventRepositoryAdoptionMerged     = providerevents.EventRepositoryAdoptionMerged

	providerAggregateWebhookEvent          = providerevents.AggregateWebhookEvent
	providerAggregateProviderEvent         = providerevents.AggregateProviderEvent
	providerAggregateProviderOperation     = providerevents.AggregateProviderOperation
	providerAggregateRepository            = providerevents.AggregateRepository
	providerAggregateRepositoryMergeSignal = providerevents.AggregateRepositoryMergeSignal
	providerAggregateWorkItem              = providerevents.AggregateWorkItem
	providerAggregateComment               = providerevents.AggregateComment
	providerAggregateRelationship          = providerevents.AggregateRelationship
	providerAggregateSyncCursor            = providerevents.AggregateSyncCursor

	providerEventSchemaVersion = providerevents.SchemaVersion
	webhookPayloadRetention    = 30 * 24 * time.Hour
)
