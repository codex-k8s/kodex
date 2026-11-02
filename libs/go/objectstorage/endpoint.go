package objectstorage

import (
	"net/url"
	"regexp"
)

// EndpointPolicy supplies path-style selection and the caller-owned exact plaintext exception; HTTPS prefixes require path-style selection.
// RU: EndpointPolicy передаёт выбор path-style и точное исключение HTTP от caller; HTTPS-префикс требует path-style.
// Both hostname and explicit port must be nonempty; deployment environment and network authorization remain caller responsibilities.
// RU: Имя узла и явный порт должны быть непустыми; проверка среды развёртывания и сетевые полномочия остаются ответственностью вызывающей стороны.
type EndpointPolicy struct {
	UsePathStyle      bool
	AllowPlaintext    bool
	PlaintextHostname string
	PlaintextPort     string
}

// ValidEndpoint accepts an HTTPS storage origin or the exact HTTP origin explicitly allowed by policy, without performing I/O.
// RU: ValidEndpoint допускает адрес хранилища HTTPS либо точный адрес HTTP, явно разрешённый политикой, без операций ввода-вывода.
// A canonical HTTPS mount prefix requires explicit path-style mode; queries, userinfo and fragments remain rejected.
// RU: Канонический HTTPS-префикс требует явного path-style режима; userinfo, непустые query и fragment по-прежнему запрещены.
// This syntax check neither resolves DNS nor grants network access, validates TLS trust or selects a deployment-specific exception.
// RU: Эта синтаксическая проверка не разрешает DNS, не выдаёт сетевой доступ, не проверяет доверие TLS и не выбирает исключение конкретной среды.
func ValidEndpoint(raw string, policy EndpointPolicy) bool {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint == nil || endpoint.Host == "" || endpoint.User != nil ||
		endpoint.RawQuery != "" || endpoint.Fragment != "" || !ValidEndpointPath(endpoint, policy.UsePathStyle) {
		return false
	}
	return endpoint.Scheme == "https" || policy.AllowPlaintext && policy.PlaintextHostname != "" &&
		policy.PlaintextPort != "" && endpoint.Scheme == "http" &&
		endpoint.Hostname() == policy.PlaintextHostname && endpoint.Port() == policy.PlaintextPort
}

// endpointPrefixPattern accepts only unescaped operator-owned mount segments, not object keys or arbitrary URI syntax.
// RU: endpointPrefixPattern допускает только неэкранированные сегменты mount оператора, не object keys или произвольный URI.
var endpointPrefixPattern = regexp.MustCompile(`^/[A-Za-z0-9][A-Za-z0-9_-]*(/[A-Za-z0-9][A-Za-z0-9_-]*)*$`)

// ValidEndpointPath preserves root endpoints and accepts a bounded canonical mount prefix only for HTTPS path-style clients.
// It neither selects a destination nor grants object access; callers still validate URL authority and credentials.
// RU: ValidEndpointPath сохраняет корневые endpoints и допускает ограниченный канонический mount только для HTTPS path-style.
// Адрес и права на объекты не назначаются; caller отдельно проверяет authority и credentials.
func ValidEndpointPath(endpoint *url.URL, usePathStyle bool) bool {
	if endpoint == nil {
		return false
	}
	if endpoint.Path == "" || endpoint.Path == "/" {
		return endpoint.RawPath == ""
	}
	return endpoint.Scheme == "https" && usePathStyle && endpoint.RawPath == "" && len(endpoint.Path) <= 256 && endpointPrefixPattern.MatchString(endpoint.Path)
}
