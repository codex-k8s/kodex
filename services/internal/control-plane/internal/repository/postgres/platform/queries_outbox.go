package platform

import _ "embed"

var (
	// queryOutboxCheckOutboxTable checks that the owned outbox table is present in PostgreSQL before relay work; it does not validate broker readiness.
	// queryOutboxCheckOutboxTable проверяет наличие принадлежащей сервису таблицы outbox в PostgreSQL перед работой relay; готовность брокера не подтверждает.
	//go:embed sql/outbox_check_outbox_table.sql
	queryOutboxCheckOutboxTable string
	// queryOutboxClaimPublishableEvents claims due PENDING or expired CLAIMED events with SKIP LOCKED only after unpublished predecessors of the same ordering key are absent.
	// queryOutboxClaimPublishableEvents захватывает готовые PENDING либо просроченные CLAIMED события через SKIP LOCKED лишь при отсутствии неопубликованных предшественников того же ключа порядка.
	//go:embed sql/outbox_claim_publishable_events.sql
	queryOutboxClaimPublishableEvents string
	// queryOutboxMarkoutboxpublishedUpdateOutboxEventsStateBrokerReceiptPublishedAt marks a CLAIMED event PUBLISHED for the exact lease owner and stores broker receipt, clearing lease fields.
	// queryOutboxMarkoutboxpublishedUpdateOutboxEventsStateBrokerReceiptPublishedAt помечает CLAIMED событие PUBLISHED для точного владельца аренды и сохраняет квитанцию брокера, очищая поля аренды.
	//go:embed sql/outbox_markoutboxpublished_update_outbox_events_state_broker_receipt_published_at.sql
	queryOutboxMarkoutboxpublishedUpdateOutboxEventsStateBrokerReceiptPublishedAt string
	// queryOutboxMarkoutboxfailedUpdateOutboxEventsStateAvailableAtLeaseOwner records the supplied failure state and next availability only for a CLAIMED event held by the exact lease owner.
	// queryOutboxMarkoutboxfailedUpdateOutboxEventsStateAvailableAtLeaseOwner сохраняет заданное состояние ошибки и следующее время доступности только для CLAIMED события точного владельца аренды.
	//go:embed sql/outbox_markoutboxfailed_update_outbox_events_state_available_at_lease_owner.sql
	queryOutboxMarkoutboxfailedUpdateOutboxEventsStateAvailableAtLeaseOwner string
)
