package model

import "github.com/codex-k8s/kodex/libs/go/runtimecontract"

// validateNativeSource checks only the owner-selected opaque export binding.
// The archive worker does not decode history or implement a second session engine.
// validateNativeSource проверяет лишь owner-selected привязку непрозрачного export.
// Archive worker не декодирует историю и не реализует второй session engine.
func validateNativeSource(task Task) error {
	return (runtimecontract.NativeSessionCheckpoint{
		SessionID: task.NativeSessionID, Version: runtimecontract.OpenCodeVersion,
		Generation: task.ContentGeneration, RelativePath: task.SourceRelativePath,
		SHA256: task.SourceSHA256, SizeBytes: task.SourceSizeBytes,
	}).Validate()
}
