// Package acceptance реализует code-first проверку внешнего fixture.
package acceptance

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/clients/openai"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

const (
	DefaultFixturePath = "/home/s/projects/matter-codex/.agents/mvp-finish/1-2-3-4-5.mp3"
	FixtureSHA256      = "56a17fd3675e5913e912c404a203bc1062daf3c3c1ec79d5210d20fe28539e8e"
)

var ErrFixtureUnavailable = errors.New("external STT fixture is unavailable")

type Fixture struct {
	file  *os.File
	audio value.Audio
}

func VerifyFixture(path string) (*Fixture, error) {
	if path == "" {
		path = DefaultFixturePath
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, ErrFixtureUnavailable
	}
	failed := true
	defer func() {
		if failed {
			_ = file.Close()
		}
	}()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > value.MaximumAbsoluteBytes {
		return nil, errors.New("external STT fixture boundary is invalid")
	}
	digest := sha256.New()
	buffer := make([]byte, value.MaximumChunkBytes)
	if copied, copyErr := io.CopyBuffer(digest, file, buffer); copyErr != nil || copied != info.Size() {
		return nil, errors.New("hash external STT fixture")
	}
	encoded := make([]byte, hex.EncodedLen(digest.Size()))
	hex.Encode(encoded, digest.Sum(nil))
	if subtle.ConstantTimeCompare(encoded, []byte(FixtureSHA256)) != 1 {
		return nil, errors.New("external STT fixture checksum mismatch")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, errors.New("seek external STT fixture")
	}
	audio, err := transcription.ValidateAudio(file, info.Size(), "audio/mpeg", value.MaximumAbsoluteBytes, 5*time.Minute)
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
	if fixture == nil || fixture.file == nil || len(apiKey) == 0 {
		return errors.New("STT acceptance configuration is incomplete")
	}
	client, err := openai.New()
	if err != nil {
		return errors.New("configure STT acceptance client")
	}
	text, err := client.Transcribe(ctx, value.ProviderRequest{Audio: fixture.audio, Model: value.DefaultModel, Language: value.DefaultLanguage, APIKey: apiKey})
	if err != nil {
		return errors.New("live STT acceptance failed")
	}
	if normalizeRussian(text) != "раз два три четыре пять" {
		return errors.New("live STT acceptance transcript mismatch")
	}
	return nil
}

var spaces = regexp.MustCompile(`\s+`)

func normalizeRussian(input string) string {
	return spaces.ReplaceAllString(strings.TrimSpace(strings.Map(func(character rune) rune {
		if character == 'ё' || character == 'Ё' {
			return 'е'
		}
		if unicode.IsLetter(character) || unicode.IsDigit(character) || unicode.IsSpace(character) {
			return unicode.ToLower(character)
		}
		return -1
	}, input)), " ")
}
