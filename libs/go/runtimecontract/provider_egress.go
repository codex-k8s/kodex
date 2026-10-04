package runtimecontract

// RuntimeProxyAccess создаётся consumer-ом только после проверки подписанного
// execution grant. Provider route не меняет пользовательскую network policy.
type RuntimeProxyAccess struct {
	WebAccess      RuntimeWebAccess
	ProviderAccess bool
}

func RuntimeProviderAllowsHost(host string) bool {
	return host == "api.openai.com" || host == "chatgpt.com" || host == "auth.openai.com"
}

// Закрытый реестр соответствует transport закреплённого Codex 0.160.0:
// codex-api endpoints responses/models, login oauth refresh и backend usage.
func RuntimeProviderAllowsRequest(host, path, method string) bool {
	switch host {
	case "api.openai.com":
		return path == "/v1/responses" && method == "POST" || path == "/v1/models" && method == "GET"
	case "chatgpt.com":
		return path == "/backend-api/codex/responses" && method == "POST" ||
			path == "/backend-api/codex/models" && method == "GET" ||
			path == "/backend-api/wham/usage" && method == "GET"
	case "auth.openai.com":
		return path == "/oauth/token" && method == "POST"
	default:
		return false
	}
}

func RuntimeProviderAllowsWebSocket(host, path, method string) bool {
	return method == "GET" && (host == "api.openai.com" && path == "/v1/responses" ||
		host == "chatgpt.com" && path == "/backend-api/codex/responses")
}
