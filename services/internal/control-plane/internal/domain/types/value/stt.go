package value

import "github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"

// STTParameters groups model-side transcription options shared by persisted
// runtime profiles and wire formats; validation is delegated to modelprofile.
// STTParameters объединяет настройки транскрибации модели для сохраняемых
// runtime-профилей и форматов обмена; проверку выполняет modelprofile.
type STTParameters struct {
	Languages        []string `json:"languages,omitempty" yaml:"languages,omitempty" toml:"languages,omitempty"`
	Keywords         []string `json:"keywords,omitempty" yaml:"keywords,omitempty" toml:"keywords,omitempty"`
	Prompt           string   `json:"prompt,omitempty" yaml:"prompt,omitempty" toml:"prompt,omitempty"`
	Temperature      float64  `json:"temperature,omitempty" yaml:"temperature,omitempty" toml:"temperature,omitempty"`
	ChunkingStrategy string   `json:"chunkingStrategy,omitempty" yaml:"chunkingStrategy,omitempty" toml:"chunkingStrategy,omitempty"`
	Stream           bool     `json:"stream,omitempty" yaml:"stream,omitempty" toml:"stream,omitempty"`
}

// Validate checks these options against the selected model and language using
// the shared STT profile validator without changing the parameter values.
// Validate проверяет параметры для выбранной модели и языка общим валидатором
// профиля STT, не изменяя сами значения.
func (parameters STTParameters) Validate(model, language string) error {
	return modelprofile.Validate(model, language, modelprofile.Parameters{Languages: parameters.Languages, Keywords: parameters.Keywords,
		Prompt: parameters.Prompt, Temperature: parameters.Temperature, ChunkingStrategy: parameters.ChunkingStrategy, Stream: parameters.Stream})
}
