package modelprofile

import (
	"math"
	"strings"
	"testing"
)

func TestClosedModelCompatibility(t *testing.T) {
	for _, profile := range OpenAICatalog().Models {
		if err := Validate(profile.Model, "ru", Parameters{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, model := range []string{"gpt-transcribe-next", "gpt-live-transcribe", "gpt-4o-transcribe-diarize", "gpt-4o-mini-transcribe-2099-01-01", "../models"} {
		if Validate(model, "", Parameters{}) == nil {
			t.Fatal("unknown model accepted")
		}
	}
	for _, parameters := range []Parameters{
		{Languages: []string{"RU"}}, {Languages: []string{"ru", "ru"}}, {Keywords: []string{"bad\nterm"}}, {Keywords: []string{"<bad>"}},
		{Keywords: []string{strings.Repeat("a", 129)}}, {Keywords: make([]string, 65)}, {Prompt: strings.Repeat("a", 897)},
		{Temperature: math.NaN()}, {Temperature: math.Inf(1)}, {Temperature: -1}, {Temperature: 1.1}, {Stream: true}, {ChunkingStrategy: "server_vad"},
	} {
		if Validate(RecommendedModel, "", parameters) == nil {
			t.Fatal("invalid parameters accepted")
		}
	}
	valid := Parameters{Languages: []string{"ru", "en"}, Keywords: []string{"Kodex"}, Prompt: "Technical discussion", Temperature: 0.5, ChunkingStrategy: "auto"}
	if err := Validate(RecommendedModel, "", valid); err != nil {
		t.Fatal(err)
	}
	if Validate(RecommendedModel, "ru", valid) == nil {
		t.Fatal("ambiguous language hints accepted")
	}
	if Validate("gpt-4o-transcribe", "ru", valid) == nil {
		t.Fatal("new model hints accepted by legacy model")
	}
	if Validate("whisper-1", "ru", Parameters{ChunkingStrategy: "auto"}) == nil {
		t.Fatal("unsupported chunking accepted")
	}
	first := OpenAICatalog()
	first.Models[0].ParameterNames[0] = "tampered"
	if OpenAICatalog().Models[0].ParameterNames[0] != "languages" {
		t.Fatal("catalog aliases mutable storage")
	}
}

func TestDocumentedMiniSnapshotsKeepExactCompatibility(t *testing.T) {
	for _, snapshot := range []string{"gpt-4o-mini-transcribe-2025-12-15", "gpt-4o-mini-transcribe-2025-03-20"} {
		profile, ok := Lookup(snapshot)
		if !ok || profile.Legacy != (snapshot == "gpt-4o-mini-transcribe-2025-03-20") ||
			Validate(snapshot, "ru", Parameters{ChunkingStrategy: "auto"}) != nil ||
			Validate(snapshot, "", Parameters{Languages: []string{"ru", "en"}}) == nil {
			t.Fatal("совместимость документированного snapshot потеряна")
		}
	}
}
