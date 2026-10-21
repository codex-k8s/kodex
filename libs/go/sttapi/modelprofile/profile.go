// Package modelprofile defines a dated, closed compatibility snapshot for
// file-based STT parameters; it is not a live account capability probe.
// RU: Пакет modelprofile задаёт датированный закрытый снимок совместимости
// параметров файловой транскрипции, а не проверку возможностей текущего аккаунта.
package modelprofile

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

// Version identifies the reviewed file-transcription compatibility snapshot.
// RU: Version обозначает проверенную ревизию совместимости файловой транскрипции.
const Version = "openai-file-2026-09-05.2"

// RecommendedModel is the preferred model in this fixed adapter snapshot.
// RU: RecommendedModel является предпочтительной моделью этого снимка адаптера.
const RecommendedModel = "gpt-transcribe"

// RecommendedMaximumBytes is the lower default size limit offered to callers.
// RU: RecommendedMaximumBytes задаёт рекомендуемый вызывающему коду предел размера.
const RecommendedMaximumBytes = 10 << 20

// RecommendedMaximumDuration is the lower default audio-duration limit.
// RU: RecommendedMaximumDuration задаёт рекомендуемый предел длительности аудио.
const RecommendedMaximumDuration = 120 * time.Second

// MinimumAudioBytes rejects recordings too small for the file-STT policy.
// RU: MinimumAudioBytes задаёт нижнюю границу размера записи по политике file STT.
const MinimumAudioBytes = 1024

// MaximumAudioBytes caps the accepted file size independently of defaults.
// RU: MaximumAudioBytes ограничивает допустимый размер файла независимо от default.
const MaximumAudioBytes = 25 << 20

// MinimumAudioDuration excludes recordings shorter than one second.
// RU: MinimumAudioDuration исключает записи короче одной секунды.
const MinimumAudioDuration = time.Second

// MaximumAudioDuration caps recording length at thirty minutes.
// RU: MaximumAudioDuration ограничивает запись тридцатью минутами.
const MaximumAudioDuration = 30 * time.Minute

// MinimumProviderTimeout is the smallest allowed provider-call budget.
// RU: MinimumProviderTimeout задаёт минимальный бюджет вызова провайдера.
const MinimumProviderTimeout = time.Second

// MaximumProviderTimeout is the largest allowed provider-call budget.
// RU: MaximumProviderTimeout задаёт максимальный бюджет вызова провайдера.
const MaximumProviderTimeout = 15 * time.Second

// Parameters holds optional request hints validated against the selected
// profile; streaming remains prohibited by the synchronous MVP policy.
// RU: Parameters содержит необязательные подсказки запроса, проверяемые по
// выбранному профилю; streaming запрещён синхронной политикой MVP.
type Parameters struct {
	Languages        []string
	Keywords         []string
	Prompt           string
	Temperature      float64
	ChunkingStrategy string
	Stream           bool
}

// Profile lists one model's supported parameter names, chunking choices and
// local bounds without exposing account credentials or probing the provider.
// RU: Profile перечисляет параметры, варианты разбивки и локальные пределы
// модели без реквизитов аккаунта и без обращения к провайдеру.
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

// Catalog carries the version and observation time of the compatibility
// snapshot plus independently allocated model profiles.
// RU: Catalog содержит версию и дату наблюдения снимка совместимости, а
// также отдельно созданные профили моделей.
type Catalog struct {
	Version    string
	ObservedAt time.Time
	Models     []Profile
}

// OpenAICatalog builds a fresh copy of the dated compatibility registry on
// every call. ObservedAt records documentation review, not a live account
// probe; changing a returned slice cannot mutate future catalog results.
// RU: OpenAICatalog при каждом вызове создаёт новую копию датированного реестра.
// ObservedAt фиксирует проверку документации, а не live-проверку аккаунта;
// изменение возвращённого среза не влияет на последующие результаты.
func OpenAICatalog() Catalog {
	result := Catalog{Version: Version, ObservedAt: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)}
	for _, name := range []string{"gpt-transcribe", "gpt-4o-transcribe", "gpt-4o-mini-transcribe", "gpt-4o-mini-transcribe-2025-12-15", "gpt-4o-mini-transcribe-2025-03-20", "whisper-1"} {
		profile := Profile{Model: name, Legacy: name == "whisper-1" || name == "gpt-4o-mini-transcribe-2025-03-20", FileStreamSupported: name != "whisper-1", MaximumPromptBytes: 896,
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

// Lookup returns the exact named profile from the closed catalog and refuses
// unknown or guessed model IDs without a provider call.
// RU: Lookup возвращает профиль лишь по точному имени из закрытого каталога
// и отклоняет неизвестные или угаданные ID без вызова провайдера.
func Lookup(model string) (Profile, bool) {
	for _, profile := range OpenAICatalog().Models {
		if profile.Model == model {
			return profile, true
		}
	}
	return Profile{}, false
}

// Validate rejects unknown models, unsupported hints, invalid UTF-8/NUL,
// duplicate or malformed two-letter language codes, oversized keywords and
// out-of-range temperature. It never assumes the provider will reject or
// ignore unsupported fields; streaming remains closed for synchronous STT.
// RU: Validate отклоняет неизвестные модели, неподдерживаемые подсказки,
// неверный UTF-8/NUL, повторные или ошибочные двухбуквенные коды языков,
// чрезмерные ключевые слова и температуру вне диапазона. Функция не полагается
// на отказ провайдера и запрещает streaming для синхронного STT.
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

// languageCode accepts only two lowercase ASCII letters for language hints;
// longer tags and uppercase codes do not pass this local format check.
// RU: languageCode принимает для языковой подсказки лишь две строчные
// ASCII-буквы; длинные теги и прописные буквы не проходят локальную проверку.
func languageCode(value string) bool {
	return len(value) == 2 && value[0] >= 'a' && value[0] <= 'z' && value[1] >= 'a' && value[1] <= 'z'
}
