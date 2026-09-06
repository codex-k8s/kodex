package providersmoke

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestNormalizeRussianAllowedDifferences(t *testing.T) {
	for name, input := range map[string]string{
		"exact":                "раз два три четыре пять",
		"case":                 "РАЗ Два ТРИ четыре ПЯТЬ",
		"whitespace":           "\t раз\nдва\r\nтри\u00a0четыре\u2003пять  ",
		"terminal_punctuation": "раз два три четыре пять...?!",
		"terminal_whitespace":  " Раз два три четыре пять ! \n",
	} {
		t.Run(name, func(t *testing.T) {
			if normalizeRussian(input) != "раз два три четыре пять" {
				t.Fatal("allowed normalization did not match")
			}
		})
	}
}

func TestNormalizeRussianPreservesSignificantDifferences(t *testing.T) {
	for name, input := range map[string]string{
		"internal_punctuation": "р@аз два три четыре пять",
		"internal_symbol":      "ра$з два три четыре пять",
		"between_words":        "раз, два три четыре пять",
		"leading_punctuation":  "!раз два три четыре пять",
		"terminal_symbol":      "раз два три четыре пять★",
		"zero_width":           "ра\u200bз два три четыре пять",
		"missing_word":         "раз два четыре пять",
		"reordered_words":      "два раз три четыре пять",
		"joined_words":         "раздва три четыре пять",
		"empty":                "",
	} {
		t.Run(name, func(t *testing.T) {
			if normalizeRussian(input) == "раз два три четыре пять" {
				t.Fatal("significant difference was discarded")
			}
		})
	}
	if normalizeRussian("Ёж") != "ёж" || normalizeRussian("Ёж") == normalizeRussian("Еж") {
		t.Fatal("distinct letters were conflated")
	}
}

func TestLiveProviderRussianNumberFixture(t *testing.T) {
	path := os.Getenv("KODEX_STT_ACCEPTANCE_FIXTURE")
	fixture, err := VerifyFixture(t.Context(), path)
	if err != nil {
		t.Fatalf("fixture preflight: %v", err)
	}
	defer fixture.Close()
	key := []byte(os.Getenv("KODEX_STT_PROVIDER_SMOKE_OPENAI_API_KEY"))
	defer clear(key)
	if len(key) == 0 {
		t.Skip("NOT RUN: fixture checksum is valid; KODEX_STT_PROVIDER_SMOKE_OPENAI_API_KEY is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := fixture.Run(ctx, key); err != nil {
		t.Fatal(err)
	}
}

func TestFixturePreflight(t *testing.T) {
	fixture, err := VerifyFixture(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	if fixture.audio.SizeBytes != 46364 || fixture.audio.Duration <= 0 {
		t.Fatal("неверная фикстура")
	}
}
