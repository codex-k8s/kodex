package runtimecontract

import "regexp"

const (
	// ModelAPIChatCompletions selects the upstream OpenAI-compatible chat adapter, without assuming Responses support.
	// ModelAPIChatCompletions выбирает upstream OpenAI-compatible chat adapter без предположения о Responses.
	ModelAPIChatCompletions = "CHAT_COMPLETIONS"
	// ModelAPIResponses selects the bundled upstream OpenAI Responses adapter; OpenAI models remain supported.
	// ModelAPIResponses выбирает bundled upstream OpenAI Responses adapter; модели OpenAI сохраняются.
	ModelAPIResponses = "RESPONSES"
	// ModelAPIMessages selects the bundled upstream Anthropic Messages adapter and its native authentication.
	// ModelAPIMessages выбирает bundled upstream Anthropic Messages adapter и его native authentication.
	ModelAPIMessages = "MESSAGES"
)

// modelRouteVersionPattern bounds a nonsecret operator-declared route version, not remotely observed compatibility.
// modelRouteVersionPattern ограничивает несекретную declared версию маршрута, не подтверждая remote compatibility.
var modelRouteVersionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:[._+-][A-Za-z0-9._+-]+)?$`)

// validModelProviderAPI closes the supported protocols; an omitted API preserves the documented Chat default.
// validModelProviderAPI закрывает набор протоколов; отсутствие API сохраняет документированный Chat default.
func validModelProviderAPI(api string) bool {
	return api == "" || api == ModelAPIChatCompletions || api == ModelAPIResponses || api == ModelAPIMessages
}

// validModelRouteKind separates proxy-only controls from direct providers and requires a version for intermediaries.
// validModelRouteKind отделяет proxy-only controls от direct providers и требует версию посредника.
func validModelRouteKind(kind, version string) bool {
	if kind == "" || kind == "DIRECT" {
		return version == "" || len(version) <= 64 && modelRouteVersionPattern.MatchString(version)
	}
	return (kind == "LITELLM" || kind == "VLLM") && len(version) <= 64 && modelRouteVersionPattern.MatchString(version)
}

// modelProviderAPIPath maps a validated protocol to its only inference endpoint, excluding arbitrary HTTP tools.
// modelProviderAPIPath сопоставляет validated protocol единственному inference endpoint, исключая произвольные HTTP tools.
func modelProviderAPIPath(api string) string {
	switch api {
	case ModelAPIResponses:
		return "/responses"
	case ModelAPIMessages:
		return "/messages"
	default:
		return "/chat/completions"
	}
}

// AdapterPackage selects only a bundled, pinned upstream SDK; configuration cannot name a package or executable.
// AdapterPackage выбирает лишь bundled pinned upstream SDK; конфигурация не может назвать package или executable.
func (config ModelProviderConfiguration) AdapterPackage() string {
	switch config.API {
	case ModelAPIResponses:
		return "@ai-sdk/openai"
	case ModelAPIMessages:
		return "@ai-sdk/anthropic"
	default:
		return "@ai-sdk/openai-compatible"
	}
}
