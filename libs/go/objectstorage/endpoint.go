package objectstorage

import "net/url"

// EndpointPolicy supplies the caller-owned opt-in and exact origin for a plaintext endpoint exception.
// RU: EndpointPolicy передаёт принадлежащие вызывающей стороне явное разрешение и точный адрес исключения для открытого транспорта.
// Both hostname and explicit port must be nonempty; deployment environment and network authorization remain caller responsibilities.
// RU: Имя узла и явный порт должны быть непустыми; проверка среды развёртывания и сетевые полномочия остаются ответственностью вызывающей стороны.
type EndpointPolicy struct {
	AllowPlaintext    bool
	PlaintextHostname string
	PlaintextPort     string
}

// ValidEndpoint accepts an HTTPS storage origin or the exact HTTP origin explicitly allowed by policy, without performing I/O.
// RU: ValidEndpoint допускает адрес хранилища HTTPS либо точный адрес HTTP, явно разрешённый политикой, без операций ввода-вывода.
// Userinfo, non-root decoded paths, nonempty queries and fragments are rejected; an empty query delimiter remains valid.
// RU: Реквизиты в URL, непустые пути кроме декодированного корня, непустые запросы и фрагменты отклоняются; пустой разделитель запроса остаётся допустимым.
// This syntax check neither resolves DNS nor grants network access, validates TLS trust or selects a deployment-specific exception.
// RU: Эта синтаксическая проверка не разрешает DNS, не выдаёт сетевой доступ, не проверяет доверие TLS и не выбирает исключение конкретной среды.
func ValidEndpoint(raw string, policy EndpointPolicy) bool {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint == nil || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.Path != "" && endpoint.Path != "/" {
		return false
	}
	return endpoint.Scheme == "https" || policy.AllowPlaintext && policy.PlaintextHostname != "" &&
		policy.PlaintextPort != "" && endpoint.Scheme == "http" &&
		endpoint.Hostname() == policy.PlaintextHostname && endpoint.Port() == policy.PlaintextPort
}
