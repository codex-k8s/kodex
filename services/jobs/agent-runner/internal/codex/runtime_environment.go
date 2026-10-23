package codex

import (
	"errors"
	"net/url"
	"os"
	"regexp"
)

// runtimeTrustBundle pins the platform-projected egress CA path; it does not itself verify file bytes.
// runtimeTrustBundle закрепляет путь платформенного CA для egress, но сам не проверяет байты файла.
const runtimeTrustBundle = "/var/run/config/kodex/runtime/egress-trust/ca-bundle.crt"

// runtimeProxyCredentialPattern checks the two-part URL credential's syntax only; authentication and
// signature verification belong to the gateway, not this environment-shape filter.
// runtimeProxyCredentialPattern проверяет только двухчастную форму URL credential; аутентификация
// и проверка подписи принадлежат gateway, а не этому фильтру структуры окружения.
var runtimeProxyCredentialPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)

// runtimeTransportEnvironmentNames is the closed transport allowlist; its first three entries are
// proxy/bypass settings and the remaining entries must all reference the exact trust bundle.
// runtimeTransportEnvironmentNames задаёт закрытый список транспорта: первые три записи — proxy/bypass,
// остальные обязаны ссылаться на один точный путь доверенного CA.
var runtimeTransportEnvironmentNames = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE", "CURL_CA_BUNDLE",
	"REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO",
}

// runtimeTransportEnvironment returns only present nonempty allowlisted values with the exact gateway,
// matching proxies, loopback-only bypass, credential shape and fixed trust paths. It neither inherits
// unrelated variables nor writes credentials to TOML; network reachability and gateway auth are separate.
// runtimeTransportEnvironment возвращает только непустые разрешённые значения с точным gateway,
// одинаковыми proxy, обходом только для loopback, формой credential и закреплёнными путями CA. Она не
// наследует лишние переменные и не пишет реквизиты в TOML; доступность сети и auth gateway проверяются отдельно.
func runtimeTransportEnvironment() (map[string]string, error) {
	values := make(map[string]string, len(runtimeTransportEnvironmentNames))
	for _, name := range runtimeTransportEnvironmentNames {
		value, ok := os.LookupEnv(name)
		if !ok || value == "" {
			return nil, errors.New("runtime transport environment is unavailable")
		}
		values[name] = value
	}
	proxy, err := url.Parse(values["HTTPS_PROXY"])
	if err != nil || proxy.Scheme != "http" || proxy.Host != "egress-gateway.kodex-system.svc:8084" ||
		proxy.Path != "" || proxy.RawPath != "" || proxy.RawQuery != "" || proxy.ForceQuery || proxy.Fragment != "" || proxy.Opaque != "" || proxy.User == nil ||
		proxy.User.Username() != "kodex" || values["HTTP_PROXY"] != values["HTTPS_PROXY"] ||
		values["NO_PROXY"] != "127.0.0.1,localhost" {
		return nil, errors.New("runtime transport environment is invalid")
	}
	credential, ok := proxy.User.Password()
	if !ok || !runtimeProxyCredentialPattern.MatchString(credential) {
		return nil, errors.New("runtime transport credential is invalid")
	}
	for _, name := range runtimeTransportEnvironmentNames[3:] {
		if values[name] != runtimeTrustBundle {
			return nil, errors.New("runtime transport trust path is invalid")
		}
	}
	return values, nil
}
