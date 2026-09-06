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
	DefaultFixturePath = ""
	FixtureSHA256      = "56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e"
)

var ErrFixtureUnavailable = errors.New("external STT fixture is unavailable")

type Fixture struct {
	file  *os.File
	audio value.Audio
}

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

func (fixture *Fixture) Close() error {
	if fixture == nil || fixture.file == nil {
		return nil
	}
	return fixture.file.Close()
}

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

func normalizeRussian(input string) string {
	// MVP-UI-60 разрешает только регистр, whitespace и конечную пунктуацию.
	input = strings.TrimRightFunc(input, func(character rune) bool {
		return unicode.IsPunct(character) || unicode.IsSpace(character)
	})
	return strings.Join(strings.Fields(strings.ToLower(input)), " ")
}
