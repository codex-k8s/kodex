package openai

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/service/transcription"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

const defaultAcceptanceFixture = "/home/s/projects/matter-codex/.agents/mvp-finish/1-2-3-4-5.mp3"

func TestLiveRussianNumberFixture(t *testing.T) {
	credential := os.Getenv("KODEX_STT_ACCEPTANCE_OPENAI_API_KEY")
	fixture := os.Getenv("KODEX_STT_ACCEPTANCE_FIXTURE")
	if fixture == "" {
		fixture = defaultAcceptanceFixture
	}
	raw, err := os.ReadFile(fixture)
	if err != nil {
		if credential == "" {
			t.Skip("NOT RUN: external fixture and test credential are not configured")
		}
		t.Fatalf("не удалось прочитать внешний fixture: %v", err)
	}
	audio, err := transcription.ValidateAudio(raw, "audio/mpeg", value.MaximumAbsoluteBytes, 5*time.Minute)
	if err != nil {
		t.Fatalf("fixture не прошёл локальную проверку: %v", err)
	}
	if credential == "" {
		t.Skip("NOT RUN: fixture is valid; KODEX_STT_ACCEPTANCE_OPENAI_API_KEY is not configured")
	}
	client, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	text, err := client.Transcribe(ctx, value.ProviderRequest{Audio: audio, Model: value.DefaultModel, Language: value.DefaultLanguage, APIKey: []byte(credential)})
	if err != nil {
		t.Fatalf("live OpenAI acceptance: %v", err)
	}
	if normalizeRussian(text) != "раз два три четыре пять" {
		t.Fatalf("неожиданный нормализованный transcript")
	}
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
