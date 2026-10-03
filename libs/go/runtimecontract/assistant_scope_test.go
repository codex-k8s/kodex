package runtimecontract

import (
	"encoding/json"
	"testing"
)

func TestAssistantScopeHasOneStrictWireSource(t *testing.T) {
	for _, scope := range []AssistantScope{AssistantScopeNone, AssistantScopeSystem, AssistantScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			input := validRunnerInputFixture()
			input.AssistantScope = scope
			if scope == AssistantScopeProject {
				input.AssistantProfileRef = "asstprof_abcdefgh"
			}
			sealScopeFixture(t, &input)
			raw, err := EncodeRunnerInput(input)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeRunnerInput(raw)
			if err != nil || decoded.AssistantScope != scope || decoded.AssistantProfileRef != input.AssistantProfileRef {
				t.Fatal("exact assistant scope wire binding was lost")
			}
			var object map[string]any
			if json.Unmarshal(raw, &object) != nil {
				t.Fatal("invalid fixture JSON")
			}
			object["system_assistant"] = true
			legacy, _ := json.Marshal(object)
			if _, err := DecodeRunnerInput(legacy); err == nil {
				t.Fatal("legacy assistant discriminator was accepted")
			}
		})
	}
}

func TestAssistantScopeRejectsDetachedOrUnknownAuthority(t *testing.T) {
	tests := map[string]func(*RunnerInput){
		"missing scope":                   func(input *RunnerInput) { input.AssistantScope = "" },
		"unknown scope":                   func(input *RunnerInput) { input.AssistantScope = "ADMIN" },
		"ordinary without project":        func(input *RunnerInput) { input.ProjectRef = "" },
		"ordinary with assistant profile": func(input *RunnerInput) { input.AssistantProfileRef = "asstprof_abcdefgh" },
		"system with project profile": func(input *RunnerInput) {
			input.AssistantScope = AssistantScopeSystem
			input.AssistantProfileRef = "asstprof_abcdefgh"
		},
		"project without profile": func(input *RunnerInput) { input.AssistantScope = AssistantScopeProject },
		"project without project": func(input *RunnerInput) {
			input.AssistantScope = AssistantScopeProject
			input.AssistantProfileRef = "asstprof_abcdefgh"
			input.ProjectRef = ""
		},
		"system without admitted image": func(input *RunnerInput) {
			input.AssistantScope = AssistantScopeSystem
			input.EnvironmentImage.ArtifactRef = ""
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := validRunnerInputFixture()
			mutate(&input)
			if input.Validate() == nil {
				t.Fatal("invalid assistant authority was accepted")
			}
		})
	}
}

func TestProjectAssistantHasToolkitButCannotUseSystemWarmAuthority(t *testing.T) {
	input := validRunnerInputFixture()
	input.AssistantScope, input.AssistantProfileRef = AssistantScopeProject, "asstprof_abcdefgh"
	sealScopeFixture(t, &input)
	if input.Validate() != nil || !input.IsAssistant() || input.IsSystemAssistant() {
		t.Fatal("project assistant scope is invalid")
	}
	if !containsString(RuntimeMCPToolNames(input), "propose_configuration_plan") {
		t.Fatal("project assistant toolkit is absent")
	}
	if _, err := WarmCompatibilityDigest(input); err == nil {
		t.Fatal("project assistant obtained system warm authority")
	}
	previousExecution, previousMCP := input.ExecutionBindingDigest, input.MCPBindingDigest
	input.AssistantProfileRef = "asstprof_other123"
	execution, mcp, err := RuntimeExecutionBindingDigests(input)
	if err != nil || execution == previousExecution || mcp == previousMCP {
		t.Fatal("assistant profile was not pinned into exact execution authority")
	}
}

func sealScopeFixture(t *testing.T, input *RunnerInput) {
	t.Helper()
	execution, mcp, err := RuntimeExecutionBindingDigests(*input)
	if err != nil {
		t.Fatal(err)
	}
	input.ExecutionBindingDigest, input.MCPBindingDigest = execution, mcp
}

func TestRuntimeRevisionDigestPinsAssistantScopeAndProfile(t *testing.T) {
	input := validRunnerInputFixture()
	source := RuntimeRevisionCredentialSource{SecretName: "runtime-provider-fixture-r1", SecretUID: "uid", SecretResourceVersion: "1"}
	previous, err := RuntimeRevisionDigest(input, source)
	if err != nil {
		t.Fatal(err)
	}
	input.AssistantScope = AssistantScopeSystem
	systemDigest, err := RuntimeRevisionDigest(input, source)
	if err != nil || previous == systemDigest {
		t.Fatal("assistant scope was omitted from owner revision digest")
	}
	input.AssistantScope, input.AssistantProfileRef = AssistantScopeProject, "asstprof_abcdefgh"
	projectDigest, err := RuntimeRevisionDigest(input, source)
	if err != nil || projectDigest == systemDigest {
		t.Fatal("project assistant scope/profile was omitted from owner revision digest")
	}
	input.AssistantProfileRef = "asstprof_other123"
	otherDigest, err := RuntimeRevisionDigest(input, source)
	if err != nil || otherDigest == projectDigest {
		t.Fatal("foreign assistant profile shared owner revision digest")
	}
}
