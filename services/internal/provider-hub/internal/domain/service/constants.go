package service

import (
	"time"

	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
)

const (
	providerEventWebhookReceived                 = providerevents.EventWebhookReceived
	providerEventWebhookNormalized               = providerevents.EventWebhookNormalized
	providerEventWorkItemSynced                  = providerevents.EventWorkItemSynced
	providerEventCommentSynced                   = providerevents.EventCommentSynced
	providerEventRelationshipSynced              = providerevents.EventRelationshipSynced
	providerEventSyncCursorAdvanced              = providerevents.EventSyncCursorAdvanced
	providerEventOperationCompleted              = providerevents.EventOperationCompleted
	providerEventOperationFailed                 = providerevents.EventOperationFailed
	providerEventRepositoryCreated               = providerevents.EventRepositoryCreated
	providerEventRepositoryBootstrapCompleted    = providerevents.EventRepositoryBootstrapCompleted
	providerEventRepositoryAdoptionPRCreated     = providerevents.EventRepositoryAdoptionPRCreated
	providerEventRepositoryAdoptionScanCompleted = providerevents.EventRepositoryAdoptionScanCompleted
	providerEventRepositoryBootstrapMerged       = providerevents.EventRepositoryBootstrapMerged
	providerEventRepositoryAdoptionMerged        = providerevents.EventRepositoryAdoptionMerged

	providerAggregateWebhookEvent           = providerevents.AggregateWebhookEvent
	providerAggregateProviderEvent          = providerevents.AggregateProviderEvent
	providerAggregateProviderOperation      = providerevents.AggregateProviderOperation
	providerAggregateRepository             = providerevents.AggregateRepository
	providerAggregateRepositoryAdoptionScan = providerevents.AggregateRepositoryAdoptionScan
	providerAggregateRepositoryMergeSignal  = providerevents.AggregateRepositoryMergeSignal
	providerAggregateWorkItem               = providerevents.AggregateWorkItem
	providerAggregateComment                = providerevents.AggregateComment
	providerAggregateRelationship           = providerevents.AggregateRelationship
	providerAggregateSyncCursor             = providerevents.AggregateSyncCursor

	providerEventSchemaVersion        = providerevents.SchemaVersion
	defaultWebhookPayloadRetention    = 7 * 24 * time.Hour
	defaultWebhookPayloadCleanupLimit = int32(100)
	maxWebhookPayloadCleanupLimit     = int32(500)
)
