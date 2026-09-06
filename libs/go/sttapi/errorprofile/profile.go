// Package errorprofile задаёт публичные безопасные ошибки STT и границы hint.
package errorprofile

import "time"

const (
	Domain                   = "kodex.stt"
	TranscriptionRateLimited = "TRANSCRIPTION_RATE_LIMITED"
	MaximumRetryAfter        = 5 * time.Minute
)
