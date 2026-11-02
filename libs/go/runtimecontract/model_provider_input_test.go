package runtimecontract

import (
	"strings"
	"testing"
)

// modelProviderRunnerFixture reuses the complete inert runner fixture with an explicit managed-only engine.
// modelProviderRunnerFixture использует полный неактивный fixture с явным managed-only engine.
func modelProviderRunnerFixture() RunnerInput {
	input := validRunnerInputFixture()
	input.Provider, input.Model = "opencode", "model-a"
	input.ReasoningMode, input.EffectiveReasoningEffort = ReasoningUnsupported, ""
	input.ModelProvider = &ModelProviderConfiguration{Schema: ModelProviderConfigurationSchema, ManagedBy: "git",
		SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: OpenCodeVersion,
		ToolMode: "managed_mcp", ProviderID: "test-gateway", BaseURL: "https://gateway.example.invalid/v1",
		AddressCIDRs: []string{"192.0.2.40/32"}, Models: []ModelProviderModel{{ID: "model-a", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
	refreshRunnerInputBindings(&input)
	return input
}

// TestRunnerInputRequiresMatchingManagedProvider prevents descriptor loss, legacy state and unsupported native selections.
// TestRunnerInputRequiresMatchingManagedProvider запрещает потерю descriptor, legacy state и неподдержанные native selections.
func TestRunnerInputRequiresMatchingManagedProvider(t *testing.T) {
	if err := modelProviderRunnerFixture().Validate(); err != nil {
		t.Fatal("complete managed provider input rejected", err)
	}
	for name, mutate := range map[string]func(*RunnerInput){
		"missing descriptor": func(i *RunnerInput) { i.ModelProvider = nil },
		"wrong model":        func(i *RunnerInput) { i.Model = "unconfigured-model" },
		"wrong engine":       func(i *RunnerInput) { i.Provider = "openai" },
		"native session":     func(i *RunnerInput) { i.CodexSessionID = "00000000-0000-4000-8000-000000000001" },
		"native selection": func(i *RunnerInput) {
			i.EnvironmentTools = []RuntimeEnvironmentTool{{Name: "shell", Command: "bash", Description: "Synthetic selected tool"}}
		},
		"native overlay": func(i *RunnerInput) { i.ConfigOverlay = "personality = \"friendly\"\n" },
	} {
		t.Run(name, func(t *testing.T) {
			input := modelProviderRunnerFixture()
			mutate(&input)
			if input.Validate() == nil {
				t.Fatal("incompatible managed provider input accepted")
			}
		})
	}
}
