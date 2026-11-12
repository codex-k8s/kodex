package runtimecontract

import "testing"

// TestNativePreferencesBindProtocolAndOwnerEffort checks only qualified SDK preferences, exact model and resolved catalog effort; it makes no inference request.
// TestNativePreferencesBindProtocolAndOwnerEffort проверяет только настройки проверенных SDK, точную модель и разрешённую каталогом степень; модель не вызывается.
func TestNativePreferencesBindProtocolAndOwnerEffort(t *testing.T) {
	input := modelProviderRunnerFixture()
	for _, api := range []string{ModelAPIChatCompletions, ModelAPIResponses} {
		input.ModelProvider.API = api
		for _, effort := range []string{"low", "medium", "high"} {
			for _, overlay := range []string{"", "model_reasoning_effort = \"" + effort + "\""} {
				if input.ModelProvider.ValidateRuntimePreferences(input.Model, overlay, effort, ReasoningSupported) != nil {
					t.Fatal("qualified preference rejected")
				}
			}
		}
	}
	for _, api := range []string{ModelAPIChatCompletions, ModelAPIResponses, ModelAPIMessages} {
		input.ModelProvider.API = api
		if input.ModelProvider.ValidateRuntimePreferences(input.Model, "", "", ReasoningUnsupported) != nil {
			t.Fatal("route without reasoning controls rejected")
		}
	}
	input.ModelProvider.API = ModelAPIResponses
	for name, values := range map[string][4]string{
		"different model":           {"unconfigured", "", "high", ReasoningSupported},
		"unknown effort":            {input.Model, "", "adaptive", ReasoningSupported},
		"inconsistent owner effort": {input.Model, `model_reasoning_effort = "low"`, "high", ReasoningSupported},
		"unsupported model":         {input.Model, "", "high", ReasoningUnsupported},
		"empty supported effort":    {input.Model, "", "", ReasoningSupported},
		"retired search":            {input.Model, `web_search = "live"`, "high", ReasoningSupported},
		"retired persistence":       {input.Model, "[history]\npersistence = \"none\"", "high", ReasoningSupported},
		"retired personality":       {input.Model, `personality = "friendly"`, "high", ReasoningSupported},
		"retired shell":             {input.Model, "allow_login_shell = false", "high", ReasoningSupported},
	} {
		t.Run(name, func(t *testing.T) {
			if input.ModelProvider.ValidateRuntimePreferences(values[0], values[1], values[2], values[3]) == nil {
				t.Fatal("unqualified native preference accepted")
			}
		})
	}
	input.ModelProvider.API = ModelAPIMessages
	if input.ModelProvider.ValidateRuntimePreferences(input.Model, "", "high", ReasoningSupported) == nil {
		t.Fatal("OpenAI effort silently translated to Anthropic thinking")
	}
}

// TestOpenAITranscriptionRejectsForeignRoutes separates the fixed speech transport from arbitrary OpenAI-compatible inference endpoints.
// TestOpenAITranscriptionRejectsForeignRoutes отделяет фиксированный речевой транспорт от произвольных совместимых модельных endpoint.
func TestOpenAITranscriptionRejectsForeignRoutes(t *testing.T) {
	input := modelProviderRunnerFixture()
	config := *input.ModelProvider
	config.ProviderID = "openai"
	config.BaseURL = "https://api.openai.com/v1"
	config.API = ModelAPIResponses
	config.RouteKind = "DIRECT"
	config.RouteVersion = ""
	if !config.SupportsOpenAITranscription() {
		t.Fatal("official direct profile rejected")
	}
	for _, mutate := range []func(*ModelProviderConfiguration){
		func(c *ModelProviderConfiguration) { c.BaseURL = "https://model.example.invalid/v1" },
		func(c *ModelProviderConfiguration) { c.ProviderID = "foreign" },
		func(c *ModelProviderConfiguration) { c.API = ModelAPIMessages },
		func(c *ModelProviderConfiguration) { c.RouteKind = "LITELLM"; c.RouteVersion = "1.104.2" },
		func(c *ModelProviderConfiguration) { c.EngineVersion = "unknown" },
	} {
		candidate := config
		mutate(&candidate)
		if candidate.SupportsOpenAITranscription() {
			t.Fatal("foreign or invalid speech credential route accepted")
		}
	}
}
