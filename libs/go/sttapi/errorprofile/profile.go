// Package errorprofile defines stable, public STT error identifiers and a
// bounded retry hint without exposing provider diagnostics or credentials.
// RU: Пакет errorprofile задаёт стабильные публичные коды ошибок STT и
// ограниченную подсказку повтора без диагностики и реквизитов провайдера.
package errorprofile

import "time"

const (
	// Domain groups public transcription errors under one stable namespace.
	// RU: Domain объединяет публичные ошибки транскрипции в одном пространстве имён.
	Domain = "kodex.stt"
	// TranscriptionRateLimited identifies a bounded provider rate-limit outcome.
	// RU: TranscriptionRateLimited обозначает ограничение частоты у провайдера.
	TranscriptionRateLimited = "TRANSCRIPTION_RATE_LIMITED"
	// MaximumRetryAfter caps the retry hint exposed to a caller at five minutes.
	// RU: MaximumRetryAfter ограничивает подсказку ожидания пятью минутами.
	MaximumRetryAfter = 5 * time.Minute
)
