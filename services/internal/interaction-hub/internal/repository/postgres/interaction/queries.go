package interaction

import "fmt"

var (
	queryCommandResultCreate       = mustLoadQuery("command_result__create")
	queryCommandResultGet          = mustLoadQuery("command_result__get")
	queryMessageCreate             = mustLoadQuery("message__create")
	queryMessageGet                = mustLoadQuery("message__get")
	queryMessageList               = mustLoadQuery("message__list")
	queryOutboxEventClaim          = mustLoadQuery("outbox_event__claim")
	queryOutboxEventCreate         = mustLoadQuery("outbox_event__create")
	queryOutboxEventMarkFailed     = mustLoadQuery("outbox_event__mark_failed")
	queryOutboxEventMarkPermanent  = mustLoadQuery("outbox_event__mark_permanently_failed")
	queryOutboxEventMarkPublished  = mustLoadQuery("outbox_event__mark_published")
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
