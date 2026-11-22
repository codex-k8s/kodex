package managedkubernetes

const (
	// MaximumStagedBytes bounds currently retained private payload storage per organization and direction.
	// Deletion receipts release this quota while immutable submission/ACK metadata remains retained.
	// MaximumStagedBytes ограничивает currently retained private payload storage организации по direction.
	// Deletion receipts освобождают quota; immutable submission/ACK metadata сохраняются.
	MaximumStagedBytes uint64 = 1 << 30
	// MaximumStagedObjects bounds outstanding object intents, including uncertain one-PUT outcomes.
	// It is reclaimed after exact-version cleanup, never counted across all historical submissions.
	// MaximumStagedObjects ограничивает outstanding object intents, включая uncertain one-PUT outcomes.
	// Освобождается после exact-version cleanup, не суммируется по всем historical submissions.
	MaximumStagedObjects uint32 = 32768
)
