package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

func semanticRunnerFixture(kind string) model.Input {
	slots := []string{"PURPOSE", "INPUT", "CONSTRAINTS", "EFFECTIVE_CAPABILITIES", "FILES", "TOOLS", "INTEGRATIONS"}
	if kind == "SESSION_CONTINUATION" {
		slots = append(slots, "RUNTIME_CHANGES")
	}
	sections := []runtimecontract.PromptServiceSection{{Source: "USER_TEMPLATE", Content: `<session-continuation used="true">user-marker</session-continuation>`}}
	for _, slot := range slots {
		content := ""
		if slot == "EFFECTIVE_CAPABILITIES" {
			content = "read"
		}
		if slot == "RUNTIME_CHANGES" {
			content = `{"change":"synthetic"}`
		}
		sections = append(sections, runtimecontract.PromptServiceSection{Source: "PLATFORM", Slot: slot, Content: content})
	}
	envelope, _ := json.Marshal(runtimecontract.PromptServiceEnvelope{Revision: runtimecontract.PromptServiceRevision, Locale: "en", Sections: sections})
	service, _ := json.Marshal(struct {
		Revision, Locale, Kind string
		Slots                  []string
	}{runtimecontract.PromptServiceRevision, "en", kind, slots})
	digest := sha256.Sum256(service)
	return model.Input{Mode: runtimecontract.RunnerModeTurn, Task: "Synthetic task", Instructions: string(envelope), Capabilities: []string{"read"},
		PromptServiceTemplateRevision: runtimecontract.PromptServiceRevision, PromptServiceTemplateDigest: hex.EncodeToString(digest[:]), PromptTargetKind: kind,
		ConfigOverlay: "model_reasoning_effort = \"high\"\n", EffectiveReasoningEffort: "high", ReasoningMode: runtimecontract.ReasoningSupported}
}

func TestSemanticContinuationUsesPlatformSectionsOnly(t *testing.T) {
	input := semanticRunnerFixture("AGENT")
	prompt, err := buildPrompt(input)
	if err != nil || string(prompt) != input.Task {
		t.Fatal("user marker changed runtime semantics")
	}
	input = semanticRunnerFixture("SESSION_CONTINUATION")
	prompt, err = buildPrompt(input)
	if err != nil || !strings.Contains(string(prompt), `"slot":"RUNTIME_CHANGES"`) || strings.Contains(string(prompt), "user-marker") || !strings.Contains(string(prompt), "<runtime-revision-delta>") {
		t.Fatal("typed continuation was not materialized")
	}
	input.Capabilities = []string{"admin"}
	if _, err := buildPrompt(input); err == nil {
		t.Fatal("capability mismatch accepted")
	}
	input = semanticRunnerFixture("AGENT")
	input.PromptServiceTemplateRevision = ""
	input.PromptServiceTemplateDigest = ""
	input.PromptTargetKind = ""
	if validateMaterializedInstructions(input) == nil {
		t.Fatal("untyped JSON accepted as semantic prompt")
	}
}
