package provider

import "fmt"

var (
	queryAccountRuntimeStateGet                = mustLoadQuery("account_runtime_state__get")
	queryAccountRuntimeStateList               = mustLoadQuery("account_runtime_state__list")
	queryAccountRuntimeStateUpsert             = mustLoadQuery("account_runtime_state__upsert")
	queryAccountRuntimeStateUpsertFromSnapshot = mustLoadQuery("account_runtime_state__upsert_from_snapshot")
	queryWebhookEventGet                       = mustLoadQuery("webhook_event__get")
	queryWebhookEventGetByDelivery             = mustLoadQuery("webhook_event__get_by_delivery")
	queryWebhookEventInsert                    = mustLoadQuery("webhook_event__insert")
	queryWebhookEventList                      = mustLoadQuery("webhook_event__list")
	queryWebhookEventUpdateProcessing          = mustLoadQuery("webhook_event__update_processing")
	queryProviderEventInsert                   = mustLoadQuery("provider_event__insert")
	queryProviderEventList                     = mustLoadQuery("provider_event__list")
	queryWorkItemProjectionGet                 = mustLoadQuery("work_item_projection__get")
	queryWorkItemProjectionList                = mustLoadQuery("work_item_projection__list")
	queryWorkItemProjectionUpsert              = mustLoadQuery("work_item_projection__upsert")
	queryCommentProjectionList                 = mustLoadQuery("comment_projection__list")
	queryCommentProjectionGetByProviderID      = mustLoadQuery("comment_projection__get_by_provider_id")
	queryCommentProjectionUpsert               = mustLoadQuery("comment_projection__upsert")
	queryRelationshipList                      = mustLoadQuery("relationship__list")
	queryRelationshipDeleteMissingWatermark    = mustLoadQuery("relationship__delete_missing_watermark")
	queryRelationshipUpsert                    = mustLoadQuery("relationship__upsert")
	queryLimitSnapshotGetReplay                = mustLoadQuery("limit_snapshot__get_replay")
	queryLimitSnapshotList                     = mustLoadQuery("limit_snapshot__list")
	queryLimitSnapshotUpsert                   = mustLoadQuery("limit_snapshot__upsert")
	queryOutboxEventClaim                      = mustLoadQuery("outbox_event__claim")
	queryOutboxEventCreate                     = mustLoadQuery("outbox_event__create")
	queryOutboxEventMarkFailed                 = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanentlyFailed      = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished              = mustLoadQuery("outbox_event__mark_published")
	queryProviderOperationGetReplay            = mustLoadQuery("provider_operation__get_replay")
	queryProviderOperationInsert               = mustLoadQuery("provider_operation__insert")
	queryProviderOperationList                 = mustLoadQuery("provider_operation__list")
)

func mustLoadQuery(name string) string {
	query, err := loadQuery(name)
	if err != nil {
		panic(err)
	}
	return query
}

func loadQuery(name string) (string, error) {
	data, err := SQLFiles.ReadFile("sql/" + name + ".sql")
	if err != nil {
		return "", fmt.Errorf("load sql query %s: %w", name, err)
	}
	return string(data), nil
}
