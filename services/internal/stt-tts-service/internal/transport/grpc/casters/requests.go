// Package casters преобразует transport-модели в доменные значения.
package casters

import (
	"io"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

func TranscriptionInput(principal value.Principal, correlationID string, reader io.ReadSeeker, sizeBytes int64, mediaType string) transcription.Input {
	return transcription.Input{
		Principal: principal, RequestID: principal.RequestID, CorrelationID: correlationID,
		AudioReader: reader, AudioSizeBytes: sizeBytes, MediaType: mediaType,
	}
}
