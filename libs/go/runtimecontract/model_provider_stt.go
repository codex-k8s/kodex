package runtimecontract

// OpenAITranscriptionDefinitionKey pins the one credential identity consumed by the fixed OpenAI speech adapter.
// OpenAITranscriptionDefinitionKey закрепляет единственную identity ключа для фиксированного адаптера речи OpenAI.
const OpenAITranscriptionDefinitionKey = "opencode-openai"

// SupportsOpenAITranscription permits direct official OpenAI credentials only; OpenAI-compatible or proxy routes do not prove key portability.
// SupportsOpenAITranscription допускает только ключ прямого официального OpenAI; совместимый API или proxy не доказывают переносимость ключа.
func (config ModelProviderConfiguration) SupportsOpenAITranscription() bool {
	return config.Validate() == nil && config.DefinitionKey() == OpenAITranscriptionDefinitionKey &&
		config.BaseURL == "https://api.openai.com/v1" && (config.RouteKind == "" || config.RouteKind == "DIRECT") && config.API != ModelAPIMessages
}
