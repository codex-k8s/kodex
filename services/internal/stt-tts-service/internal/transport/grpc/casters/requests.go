// Package casters maps accepted STT transport values to domain input and published RPC responses without issuing authority or performing I/O.
// RU: Пакет casters переводит принятые STT transport значения в доменный ввод и опубликованные RPC ответы без выдачи полномочий или I/O.
package casters

import (
	"io"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// TranscriptionInput retains the accepted principal/request identity and seekable audio descriptor without reading or validating the audio itself.
// RU: TranscriptionInput сохраняет принятые principal/request идентичность и описание аудио с перемоткой без самостоятельного чтения или проверки записи.
func TranscriptionInput(principal value.Principal, correlationID string, reader io.ReadSeeker, sizeBytes int64, mediaType string) transcription.Input {
	return transcription.Input{
		Principal: principal, RequestID: principal.RequestID, CorrelationID: correlationID,
		AudioReader: reader, AudioSizeBytes: sizeBytes, MediaType: mediaType,
	}
}
