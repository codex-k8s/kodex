package app

import (
	"strings"
	"testing"

	"github.com/codex-k8s/matter-codex/libs/go/runtimecontract"
	"github.com/codex-k8s/matter-codex/services/jobs/agent-runner/internal/model"
)

func TestBuildPromptNamesOnlyMaterializedInputPaths(t *testing.T) {
	input := model.Input{
		Mode: runtimecontract.RunnerModeTurn,
		Task: "Analyze the attached customer brief.",
		InputArtifacts: []runtimecontract.RunnerInputArtifact{
			{Ref: "artifact_abcdefgh", FileName: "customer brief.txt", MediaType: "text/plain"},
			{Ref: "artifact_ijklmnop", FileName: "terms.pdf", MediaType: "application/pdf"},
		},
	}
	prompt, err := buildPrompt(input)
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	text := string(prompt)
	for _, expected := range []string{"input/001.txt", "input/002.pdf"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("prompt does not contain %q: %s", expected, text)
		}
	}
	if strings.Contains(text, input.InputArtifacts[0].Ref) {
		t.Fatal("prompt exposed an opaque artifact ref instead of the materialized path")
	}
}
