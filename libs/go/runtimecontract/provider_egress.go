package runtimecontract

// RuntimeProxyAccess combines user web policy with a separate provider-transport flag and is constructed by the consumer after signed execution-grant validation.
// RuntimeProxyAccess объединяет пользовательскую веб-политику с отдельным флагом транспорта провайдера и создаётся потребителем после проверки подписанного права исполнения.
type RuntimeProxyAccess struct {
	WebAccess      RuntimeWebAccess
	ProviderAccess bool
	ModelProvider  *ModelProviderRoute
}

// ProviderAllowsRequest applies the configured gateway route or the unchanged built-in provider registry.
// ProviderAllowsRequest применяет configured gateway route либо прежний встроенный реестр провайдера.
func (access RuntimeProxyAccess) ProviderAllowsRequest(host, path, method string) bool {
	if !access.ProviderAccess {
		return false
	}
	if access.ModelProvider != nil {
		return access.ModelProvider.AllowsRequest(host, path, method)
	}
	return RuntimeProviderAllowsRequest(host, path, method)
}

// RuntimeProviderAllowsHost rejects the retired implicit host registry; native execution requires an explicit signed model route.
// RuntimeProviderAllowsHost отклоняет снятый implicit host registry; native execution требует explicit signed model route.
func RuntimeProviderAllowsHost(string) bool { return false }

// RuntimeProviderAllowsRequest rejects unconfigured provider traffic without changing explicit user web grants.
// RuntimeProviderAllowsRequest отклоняет unconfigured provider traffic без изменения explicit user web grants.
func RuntimeProviderAllowsRequest(string, string, string) bool { return false }

// RuntimeProviderAllowsWebSocket closes the removed CLI websocket transport; the qualified native adapters use HTTP SSE.
// RuntimeProviderAllowsWebSocket закрывает удалённый CLI websocket transport; qualified native adapters используют HTTP SSE.
func RuntimeProviderAllowsWebSocket(string, string, string) bool { return false }
