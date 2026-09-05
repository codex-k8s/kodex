package transcription

import (
	"context"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// ValidateAudio не доверяет длительности контейнера: adapter декодирует весь
// поток и считает samples до выдачи credential и billable provider call.
func ValidateAudio(ctx context.Context, reader io.ReadSeeker, sizeBytes int64, declaredMediaType string, maximumBytes int64, maximumDuration time.Duration, decoder AudioDecoder) (value.Audio, error) {
	if ctx == nil || decoder == nil || reader == nil || sizeBytes <= 0 {
		return value.Audio{}, errs.ErrInvalidRequest
	}
	if maximumBytes < 1 || sizeBytes > maximumBytes || sizeBytes > value.MaximumAbsoluteBytes {
		return value.Audio{}, errs.ErrAudioTooLarge
	}
	if maximumDuration <= 0 || maximumDuration > 30*time.Minute {
		return value.Audio{}, errs.ErrAudioTooLong
	}
	mediaType, parameters, err := mime.ParseMediaType(declaredMediaType)
	if err != nil {
		return value.Audio{}, errs.ErrUnsupportedAudio
	}
	mediaType = strings.ToLower(mediaType)
	var format, extension string
	switch mediaType {
	case "audio/mpeg", "audio/mp3", "audio/mpga":
		format, extension, mediaType = "mp3", "mp3", "audio/mpeg"
	case "audio/wav", "audio/x-wav", "audio/wave":
		format, extension, mediaType = "wav", "wav", "audio/wav"
	case "audio/flac", "audio/x-flac":
		format, extension, mediaType = "flac", "flac", "audio/flac"
	case "audio/webm", "video/webm":
		format, extension, mediaType = "matroska,webm", "webm", "audio/webm"
	case "audio/ogg", "application/ogg":
		format, extension, mediaType = "ogg", "ogg", "audio/ogg"
	case "audio/mp4", "audio/m4a", "audio/x-m4a", "video/mp4":
		format, extension, mediaType = "mov,mp4,m4a,3gp,3g2,mj2", "m4a", "audio/mp4"
	default:
		return value.Audio{}, errs.ErrUnsupportedAudio
	}
	for key, parameter := range parameters {
		if key != "codecs" || (parameter != "opus" && parameter != "vorbis" && parameter != "mp4a.40.2") {
			return value.Audio{}, errs.ErrUnsupportedAudio
		}
	}
	duration, err := decoder.Duration(ctx, reader, sizeBytes, format, maximumDuration)
	if err != nil {
		return value.Audio{}, err
	}
	if duration <= 0 {
		return value.Audio{}, errs.ErrUnsupportedAudio
	}
	if duration > maximumDuration {
		return value.Audio{}, errs.ErrAudioTooLong
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return value.Audio{}, errs.ErrInvalidRequest
	}
	return value.Audio{Reader: reader, SizeBytes: sizeBytes, MediaType: mediaType, FileName: "audio." + extension, Duration: duration}, nil
}
