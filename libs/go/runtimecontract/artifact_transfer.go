package runtimecontract

import "time"

const (
	// MaximumArtifactTransferChunkBytes bounds one chunk so a large artifact cannot become one unbounded transport message.
	// MaximumArtifactTransferChunkBytes ограничивает одну порцию, чтобы большой артефакт не превращался в неограниченное транспортное сообщение.
	MaximumArtifactTransferChunkBytes = 64 << 10
	// MaximumArtifactTransferBytes reuses the input artifact byte budget for a complete transfer.
	// MaximumArtifactTransferBytes использует байтовый лимит входного артефакта для полной передачи.
	MaximumArtifactTransferBytes = MaximumInputArtifactBytes
	// MaximumArtifactTransferDuration caps the lifetime budget of a single artifact transfer.
	// MaximumArtifactTransferDuration ограничивает время одной передачи артефакта.
	MaximumArtifactTransferDuration = 2 * time.Minute
)
