// Package providersmoke реализует прямую code-first проверку OpenAI adapter.
package providersmoke

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"github.com/caarlos0/env/v11"
	"io"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/audio/ffmpeg"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/integration/provider/openai"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/testdata"
)

const (
	// DefaultFixturePath selects the embedded audio when no external fixture path is supplied.
	// RU: DefaultFixturePath выбирает встроенную запись, если внешний путь не задан.
	DefaultFixturePath = ""
	// FixtureSHA256 pins the exact 46,364-byte audio accepted for the live provider check.
	// RU: FixtureSHA256 закрепляет точные байты записи размером 46 364 для проверки провайдера.
	FixtureSHA256 = "56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e"
)

// ErrFixtureUnavailable means that an explicitly requested external file cannot be opened as a regular file.
// RU: ErrFixtureUnavailable означает, что запрошенный внешний файл нельзя открыть как обычный файл.
var ErrFixtureUnavailable = errors.New("external STT fixture is unavailable")

// Fixture owns an optional external audio file and its already validated, rewindable audio descriptor.
// RU: Fixture владеет необязательным внешним файлом и уже проверенным описанием записи с возможностью перемотки.
type Fixture struct {
	file  *os.File
	audio value.Audio
}

// VerifyFixture checks exact length and digest before decoding the entire audio stream; on failure
// it closes an external file, and on success the caller owns the returned fixture until Close.
// RU: VerifyFixture проверяет точный размер и дайджест до декодирования всей записи; при ошибке
// внешний файл закрывается, а при успехе вызывающий владеет фикстурой до Close.
func VerifyFixture(ctx context.Context, path string) (*Fixture, error) {
	var file *os.File
	var reader io.ReadSeeker = bytes.NewReader(testdata.RussianNumbers)
	size := int64(len(testdata.RussianNumbers))
	if path != "" {
		var err error
		file, err = os.Open(path)
		if err != nil {
			return nil, ErrFixtureUnavailable
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			file.Close()
			return nil, ErrFixtureUnavailable
		}
		reader, size = file, info.Size()
	}
	failed := true
	defer func() {
		if failed && file != nil {
			_ = file.Close()
		}
	}()
	if size != 46364 {
		return nil, errors.New("external STT fixture boundary is invalid")
	}
	digest := sha256.New()
	buffer := make([]byte, value.MaximumChunkBytes)
	if copied, copyErr := io.CopyBuffer(digest, io.LimitReader(reader, size+1), buffer); copyErr != nil || copied != size {
		return nil, errors.New("hash external STT fixture")
	}
	encoded := make([]byte, hex.EncodedLen(digest.Size()))
	hex.Encode(encoded, digest.Sum(nil))
	if subtle.ConstantTimeCompare(encoded, []byte(FixtureSHA256)) != 1 {
		return nil, errors.New("external STT fixture checksum mismatch")
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return nil, errors.New("seek external STT fixture")
	}
	audio, err := transcription.ValidateAudio(ctx, reader, size, "audio/mpeg", value.MaximumAbsoluteBytes, 5*time.Minute, ffmpeg.New(os.TempDir()))
	if err != nil {
		return nil, errors.New("validate external STT fixture")
	}
	failed = false
	return &Fixture{file: file, audio: audio}, nil
}

// Close releases only the external file owned by the fixture; embedded audio has no file handle.
// RU: Close освобождает только принадлежащий фикстуре внешний файл; у встроенной записи нет дескриптора.
func (fixture *Fixture) Close() error {
	if fixture == nil || fixture.file == nil {
		return nil
	}
	return fixture.file.Close()
}

// Run creates the configured egress client, sends the validated audio to the live provider and
// accepts only the five expected Russian number words after limited punctuation normalization.
// RU: Run создаёт клиент с заданным сетевым выходом, отправляет проверенную запись провайдеру
// и принимает только пять ожидаемых русских числительных после ограниченной нормализации пунктуации.
func (fixture *Fixture) Run(ctx context.Context, apiKey []byte) error {
	if fixture == nil || fixture.audio.Reader == nil || len(apiKey) == 0 {
		return errors.New("STT provider smoke configuration is incomplete")
	}
	var egress openai.EgressConfig
	if err := env.Parse(&egress); err != nil {
		return errors.New("STT smoke egress expectations are required")
	}
	client, err := openai.New(egress)
	if err != nil {
		return errors.New("configure STT provider smoke client")
	}
	text, err := client.Transcribe(ctx, value.ProviderRequest{Audio: fixture.audio, Model: value.DefaultModel, Language: value.DefaultLanguage, APIKey: apiKey})
	if err != nil {
		return errors.New("live STT provider smoke failed")
	}
	if normalizeRussian(text) != "раз два три четыре пять" {
		return errors.New("live STT provider smoke transcript mismatch")
	}
	return nil
}

// normalizeRussian folds case and separates Unicode whitespace or punctuation while preserving
// every letter, digit and other symbol, so changed or omitted transcript content still fails.
// RU: normalizeRussian приводит регистр и разделяет слова пробелами и пунктуацией Unicode,
// сохраняя каждую букву, цифру и прочий символ, чтобы изменение или пропуск содержания выявлялись.
func normalizeRussian(input string) string {
	// Совпадает с защищённой HTTP-приёмкой: провайдер может расставить
	// пунктуацию между словами, но символы и сами слова не отбрасываются.
	var normalized strings.Builder
	separator := true
	for _, character := range strings.ToLower(input) {
		if unicode.IsPunct(character) || unicode.IsSpace(character) {
			separator = normalized.Len() > 0
			continue
		}
		if separator && normalized.Len() > 0 {
			normalized.WriteByte(' ')
		}
		normalized.WriteRune(character)
		separator = false
	}
	return normalized.String()
}
