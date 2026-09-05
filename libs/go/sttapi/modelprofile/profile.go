// Package modelprofile задаёт закрытый реестр совместимости file STT.
package modelprofile

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const Version = "openai-file-2026-09-05.1"
const RecommendedModel = "gpt-transcribe"
const RecommendedMaximumBytes = 10 << 20
const RecommendedMaximumDuration = 120 * time.Second

type Parameters struct {
	Languages        []string
	Keywords         []string
	Prompt           string
	Temperature      float64
	ChunkingStrategy string
	Stream           bool
}

type Profile struct {
	Model               string
	Legacy              bool
	ParameterNames      []string
	ChunkingStrategies  []string
	FileStreamSupported bool
	MaximumPromptBytes  uint32
	MaximumKeywords     uint32
	MaximumKeywordBytes uint32
}

type Catalog struct {
	Version    string
	ObservedAt time.Time
	Models     []Profile
}

// observed_at означает дату проверки документации, не live account probe.
func OpenAICatalog() Catalog {
	result := Catalog{Version: Version, ObservedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)}
	for _, name := range []string{"gpt-transcribe", "gpt-4o-transcribe", "gpt-4o-mini-transcribe", "gpt-4o-mini-transcribe-2025-12-15", "whisper-1"} {
		profile := Profile{Model: name, Legacy: name == "whisper-1", FileStreamSupported: name != "whisper-1", MaximumPromptBytes: 896,
			ParameterNames: []string{"language", "prompt", "temperature", "chunking_strategy", "stream"}, ChunkingStrategies: []string{""}}
		if name != "whisper-1" {
			profile.ChunkingStrategies = append(profile.ChunkingStrategies, "auto")
		}
		if name == RecommendedModel {
			profile.ParameterNames = []string{"languages", "keywords", "prompt", "temperature", "chunking_strategy", "stream"}
			profile.MaximumKeywords, profile.MaximumKeywordBytes = 64, 128
		}
		result.Models = append(result.Models, profile)
	}
	return result
}

func Lookup(model string) (Profile, bool) {
	for _, profile := range OpenAICatalog().Models {
		if profile.Model == model {
			return profile, true
		}
	}
	return Profile{}, false
}

// Validate не принимает неподдерживаемый параметр, даже если provider мог бы
// его молча проигнорировать. Stream закрыт синхронной политикой диктовки MVP57.
func Validate(model, language string, parameters Parameters) error {
	profile, ok := Lookup(model)
	if !ok || math.IsNaN(parameters.Temperature) || math.IsInf(parameters.Temperature, 0) || parameters.Temperature < 0 || parameters.Temperature > 1 ||
		parameters.Stream || !utf8.ValidString(parameters.Prompt) || len(parameters.Prompt) > int(profile.MaximumPromptBytes) || strings.ContainsRune(parameters.Prompt, 0) {
		return errors.New("STT model parameters are invalid")
	}
	chunkingValid := false
	for _, strategy := range profile.ChunkingStrategies {
		if strategy == parameters.ChunkingStrategy {
			chunkingValid = true
		}
	}
	if !chunkingValid {
		return errors.New("STT chunking strategy is unsupported")
	}
	if model == RecommendedModel {
		if (language != "" && len(parameters.Languages) > 0) || len(parameters.Languages) > 8 {
			return errors.New("STT language parameters are incompatible")
		}
	} else if len(parameters.Languages) > 0 || len(parameters.Keywords) > 0 {
		return errors.New("STT model context parameters are unsupported")
	}
	if language != "" && !languageCode(language) {
		return errors.New("STT language is invalid")
	}
	seen := map[string]bool{}
	for _, code := range parameters.Languages {
		if !languageCode(code) || seen[code] {
			return errors.New("STT language hints are invalid")
		}
		seen[code] = true
	}
	if len(parameters.Keywords) > int(profile.MaximumKeywords) {
		return errors.New("STT keyword count exceeded")
	}
	for _, keyword := range parameters.Keywords {
		if strings.TrimSpace(keyword) == "" || !utf8.ValidString(keyword) || len(keyword) > int(profile.MaximumKeywordBytes) || strings.ContainsAny(keyword, "<>\r\n\x00") {
			return errors.New("STT keyword is invalid")
		}
	}
	return nil
}

func languageCode(value string) bool {
	return len(value) == 2 && value[0] >= 'a' && value[0] <= 'z' && value[1] >= 'a' && value[1] <= 'z'
}
