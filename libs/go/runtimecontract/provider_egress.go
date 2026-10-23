package runtimecontract

// RuntimeProxyAccess combines user web policy with a separate provider-transport flag and is constructed by the consumer after signed execution-grant validation.
// RuntimeProxyAccess объединяет пользовательскую веб-политику с отдельным флагом транспорта провайдера и создаётся потребителем после проверки подписанного права исполнения.
type RuntimeProxyAccess struct {
	WebAccess      RuntimeWebAccess
	ProviderAccess bool
}

// RuntimeProviderAllowsHost matches only the three canonical provider hostnames; it performs no DNS resolution and grants no user web access.
// RuntimeProviderAllowsHost сопоставляет только три канонических имени провайдера, не выполняет DNS-разрешение и не выдаёт пользовательский веб-доступ.
func RuntimeProviderAllowsHost(host string) bool {
	return host == "api.openai.com" || host == "chatgpt.com" || host == "auth.openai.com"
}

// RuntimeProviderAllowsRequest matches a closed exact host, path and method registry without normalizing untrusted route variants or extending user web policy.
// RuntimeProviderAllowsRequest сопоставляет закрытый реестр точных узла, пути и метода без нормализации недоверенных вариантов маршрута и расширения пользовательской веб-политики.
// Закрытый реестр соответствует transport закреплённого Codex 0.160.0:
// codex-api endpoints responses/models/alpha/search, login oauth refresh, backend usage
// и bootstrap workspace через GET accounts/check (backend-client/src/client.rs,
// get_accounts_check, официальный tag rust-v0.160.0).
// Standalone search: codex-api/src/endpoint/search.rs (POST alpha/search),
// model-provider-info/src/lib.rs (default base URL ChatGPT и API key).
func RuntimeProviderAllowsRequest(host, path, method string) bool {
	switch host {
	case "api.openai.com":
		return path == "/v1/responses" && method == "POST" ||
			path == "/v1/alpha/search" && method == "POST" ||
			path == "/v1/models" && method == "GET"
	case "chatgpt.com":
		return path == "/backend-api/codex/responses" && method == "POST" ||
			path == "/backend-api/codex/alpha/search" && method == "POST" ||
			path == "/backend-api/codex/models" && method == "GET" ||
			path == "/backend-api/wham/accounts/check" && method == "GET" ||
			path == "/backend-api/wham/usage" && method == "GET"
	case "auth.openai.com":
		return path == "/oauth/token" && method == "POST"
	default:
		return false
	}
}

// RuntimeProviderAllowsWebSocket accepts only GET upgrades for the two exact responses endpoints; HTTP-only metadata and search paths remain excluded.
// RuntimeProviderAllowsWebSocket принимает только GET-переходы для двух точных адресов responses; метаданные и поиск, доступные лишь по HTTP, остаются исключёнными.
func RuntimeProviderAllowsWebSocket(host, path, method string) bool {
	return method == "GET" && (host == "api.openai.com" && path == "/v1/responses" ||
		host == "chatgpt.com" && path == "/backend-api/codex/responses")
}
