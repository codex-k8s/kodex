package value

import "github.com/codex-k8s/kodex/libs/go/sttapi/modelprofile"

type STTParameters struct {
	Languages        []string `json:"languages,omitempty" yaml:"languages,omitempty" toml:"languages,omitempty"`
	Keywords         []string `json:"keywords,omitempty" yaml:"keywords,omitempty" toml:"keywords,omitempty"`
	Prompt           string   `json:"prompt,omitempty" yaml:"prompt,omitempty" toml:"prompt,omitempty"`
	Temperature      float64  `json:"temperature,omitempty" yaml:"temperature,omitempty" toml:"temperature,omitempty"`
	ChunkingStrategy string   `json:"chunkingStrategy,omitempty" yaml:"chunkingStrategy,omitempty" toml:"chunkingStrategy,omitempty"`
	Stream           bool     `json:"stream,omitempty" yaml:"stream,omitempty" toml:"stream,omitempty"`
}

func (parameters STTParameters) Validate(model, language string) error {
	return modelprofile.Validate(model, language, modelprofile.Parameters{Languages: parameters.Languages, Keywords: parameters.Keywords,
		Prompt: parameters.Prompt, Temperature: parameters.Temperature, ChunkingStrategy: parameters.ChunkingStrategy, Stream: parameters.Stream})
}
