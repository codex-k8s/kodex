package app

import (
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func TestBuildPromptExplainsBoundedFileAccessWithArtifactCapability(t *testing.T) {
	input := model.Input{
		Mode:         runtimecontract.RunnerModeTurn,
		Task:         "Analyze the attached customer brief.",
		Capabilities: []string{runtimecontract.ArtifactCapability},
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
	for _, expected := range []string{
		"# File access",
		"Input files are read-only at the paths listed below:",
		"`/workspace/input/001.txt`",
		"`/workspace/input/002.pdf`",
		"Write every output file directly to `/workspace/.kodex/outbox/<safe-name>`.",
		"The workspace root and all other paths are read-only",
		"do not write output files to `/workspace` itself.",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("prompt does not contain %q: %s", expected, text)
		}
	}
	if strings.Contains(text, input.InputArtifacts[0].Ref) {
		t.Fatal("prompt exposed an opaque artifact ref instead of the materialized path")
	}
}

func TestBuildPromptDoesNotPromiseFileAccessWithoutArtifactCapability(t *testing.T) {
	prompt, err := buildPrompt(model.Input{Mode: runtimecontract.RunnerModeTurn, Task: "Summarize the request."})
	if err != nil {
		t.Fatalf("buildPrompt() error = %v", err)
	}
	text := string(prompt)
	for _, forbidden := range []string{"# File access", "/workspace/.kodex/outbox", "output file", "read-only"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("prompt without artifact capability contains %q: %s", forbidden, text)
		}
	}
}

func TestSystemAssistantCompletionDoesNotCreateProjectArtifact(t *testing.T) {
	artifacts, err := completionArtifacts(model.Input{SystemAssistant: true}, "Configuration plan proposed.")
	if err != nil {
		t.Fatalf("completionArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("system assistant artifacts = %#v", artifacts)
	}
}

func TestCompletionWithoutArtifactCapabilityDoesNotCreateProjectArtifact(t *testing.T) {
	artifacts, err := completionArtifacts(model.Input{}, "Bounded result.")
	if err != nil {
		t.Fatalf("completionArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("completion without artifact capability = %#v", artifacts)
	}
}
