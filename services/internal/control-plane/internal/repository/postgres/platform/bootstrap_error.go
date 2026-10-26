package platform

// bootstrapError keeps the exact startup phase safe for Error formatting while retaining
// the original dependency cause only for private errors.Is/As inspection.
// bootstrapError оставляет Error безопасным фиксированным этапом, сохраняя исходную
// причину зависимости только для приватной проверки errors.Is/As.
type bootstrapError struct {
	phase string
	cause error
}

// Error exposes only the caller-owned fixed bootstrap phase, never driver payload or SQL.
// Error раскрывает лишь фиксированный этап bootstrap, исключая driver payload и SQL.
func (failure *bootstrapError) Error() string { return failure.phase }

// Unwrap preserves typed database and cancellation causes without changing runtime output.
// Unwrap сохраняет типизированные ошибки БД и отмены без изменения runtime output.
func (failure *bootstrapError) Unwrap() error { return failure.cause }

// bootstrapFailure wraps a fixed phase with a private cause; a nil cause preserves a
// preexisting semantic validation failure without inventing a retryable database error.
// bootstrapFailure связывает фиксированный этап с приватной причиной; nil сохраняет
// прежний semantic validation failure без выдуманной retryable ошибки БД.
func bootstrapFailure(phase string, cause error) error {
	return &bootstrapError{phase: phase, cause: cause}
}
