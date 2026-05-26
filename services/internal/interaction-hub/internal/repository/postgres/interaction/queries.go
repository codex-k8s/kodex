package interaction

import "fmt"

var (
	queryCommandResultCreate       = mustLoadQuery("command_result__create")
	queryCommandResultGet          = mustLoadQuery("command_result__get")
	queryDeliveryAttemptCreate     = mustLoadQuery("delivery_attempt__create")
	queryDeliveryAttemptGet        = mustLoadQuery("delivery_attempt__get")
	queryDeliveryAttemptGetByID    = mustLoadQuery("delivery_attempt__get_by_delivery_id")
	queryDeliveryAttemptList       = mustLoadQuery("delivery_attempt__list")
	queryDeliveryAttemptUpdate     = mustLoadQuery("delivery_attempt__update")
	queryDeliveryRouteFindActive   = mustLoadQuery("delivery_route__find_active")
	queryDeliveryRouteGet          = mustLoadQuery("delivery_route__get")
	queryMessageCreate             = mustLoadQuery("message__create")
	queryMessageGet                = mustLoadQuery("message__get")
	queryMessageList               = mustLoadQuery("message__list")
	queryNotificationCreate        = mustLoadQuery("notification__create")
	queryNotificationGet           = mustLoadQuery("notification__get")
	queryOutboxEventClaim          = mustLoadQuery("outbox_event__claim")
	queryOutboxEventCreate         = mustLoadQuery("outbox_event__create")
	queryOutboxEventMarkFailed     = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanent  = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished  = mustLoadQuery("outbox_event__mark_published")
	queryRequestCreate             = mustLoadQuery("request__create")
	queryRequestGet                = mustLoadQuery("request__get")
	queryRequestList               = mustLoadQuery("request__list")
	queryRequestListExpirable      = mustLoadQuery("request__list_expirable")
	queryRequestUpdateStatus       = mustLoadQuery("request__update_status")
	queryResponseCreate            = mustLoadQuery("response__create")
	queryResponseGet               = mustLoadQuery("response__get")
	querySubscriptionCreate        = mustLoadQuery("subscription__create")
	querySubscriptionGet           = mustLoadQuery("subscription__get")
	querySubscriptionList          = mustLoadQuery("subscription__list")
	querySubscriptionUpdate        = mustLoadQuery("subscription__update")
	queryThreadCreate              = mustLoadQuery("thread__create")
	queryThreadGet                 = mustLoadQuery("thread__get")
	queryThreadUpdateLatestMessage = mustLoadQuery("thread__update_latest_message")
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
