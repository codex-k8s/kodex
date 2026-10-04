package codex

import (
	"errors"
	"net/url"
	"os"
	"regexp"
)

const runtimeTrustBundle = "/var/run/config/kodex/runtime/egress-trust/ca-bundle.crt"

var runtimeProxyCredentialPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)

var runtimeTransportEnvironmentNames = []string{
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "SSL_CERT_FILE", "CURL_CA_BUNDLE",
	"REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS", "GIT_SSL_CAINFO",
}

// Проверяется только точный серверный transport env, без наследования остальных
// переменных процесса и без записи credentials в config.toml.
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
