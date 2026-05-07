package service

import (
	"time"

	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
)

const (
	providerEventWebhookReceived   = providerevents.EventWebhookReceived
	providerEventWebhookNormalized = providerevents.EventWebhookNormalized
	providerEventWorkItemSynced    = providerevents.EventWorkItemSynced
	providerEventCommentSynced     = providerevents.EventCommentSynced

	providerAggregateWebhookEvent  = providerevents.AggregateWebhookEvent
	providerAggregateProviderEvent = providerevents.AggregateProviderEvent
	providerAggregateWorkItem      = providerevents.AggregateWorkItem
	providerAggregateComment       = providerevents.AggregateComment

	providerEventSchemaVersion = providerevents.SchemaVersion
	webhookPayloadRetention    = 30 * 24 * time.Hour
)
