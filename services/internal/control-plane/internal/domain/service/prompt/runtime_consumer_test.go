package prompt

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

func TestCanonicalProducerRuntimeConsumer(t *testing.T) {
	for _, kind := range []string{"WARM", TargetAgent, TargetWorkflowStage, TargetAutomation, TargetSessionContinuation} {
		t.Run(kind, func(t *testing.T) {
			snapshot := semanticFixture()
			snapshot.TargetKind = kind
			var result Materialization
			var err error
			if kind == "WARM" {
				result, err = MaterializeWarm("Core", `{{slot "EFFECTIVE_CAPABILITIES"}} {"source":"PLATFORM"}`, "ins_example", strings.Repeat("a", 64), "agt_example", "ses_example")
				kind = TargetAgent
			} else {
				result, err = Materialize(`User {{slot "PURPOSE"}} {{slot "EFFECTIVE_CAPABILITIES"}}`, snapshot)
			}
			if err != nil || !result.Complete {
				t.Fatal("canonical producer failed")
			}
			input := runtimecontract.RunnerInput{Instructions: result.Prompt, Capabilities: result.EffectiveCapabilities,
				PromptServiceTemplateRevision: result.ServiceTemplateRevision, PromptServiceTemplateDigest: result.ServiceTemplateDigest, PromptTargetKind: kind}
			if _, err := runtimecontract.DecodePromptService(input); err != nil {
				t.Fatal(err)
			}
			for _, failure := range []string{"malformed", "unknown", "duplicate-key", "missing-slot", "duplicate-slot", "capability", "impersonation", "size", "revision", "target", "digest", "missing-content"} {
				t.Run(failure, func(t *testing.T) {
					candidate := input
					var envelope map[string]any
					if json.Unmarshal([]byte(input.Instructions), &envelope) != nil {
						t.Fatal("fixture decode")
					}
					sections := envelope["sections"].([]any)
					switch failure {
					case "malformed":
						candidate.Instructions = "{"
					case "unknown":
						envelope["unexpected"] = true
					case "duplicate-key":
						candidate.Instructions = strings.Replace(input.Instructions, `"revision":`, `"revision":"prompt-service-v2","revision":`, 1)
					case "missing-slot":
						envelope["sections"] = sections[:len(sections)-1]
					case "duplicate-slot":
						envelope["sections"] = append(sections, sections[len(sections)-1])
					case "capability":
						candidate.Capabilities = []string{"not-authorized"}
					case "impersonation":
						sections[0].(map[string]any)["slot"] = "EFFECTIVE_CAPABILITIES"
					case "missing-content":
						delete(sections[len(sections)-1].(map[string]any), "content")
					case "size":
						candidate.Instructions = strings.Repeat(" ", 256<<10) + input.Instructions
					case "revision":
						candidate.PromptServiceTemplateRevision = "future"
					case "target":
						candidate.PromptTargetKind = "UNKNOWN"
					case "digest":
						candidate.PromptServiceTemplateDigest = strings.Repeat("b", 64)
					}
					if candidate.Instructions == input.Instructions {
						raw, _ := json.Marshal(envelope)
						candidate.Instructions = string(raw)
					}
					if _, err := runtimecontract.DecodePromptService(candidate); err == nil {
						t.Fatal("invalid materialization accepted")
					}
				})
			}
		})
	}
}
