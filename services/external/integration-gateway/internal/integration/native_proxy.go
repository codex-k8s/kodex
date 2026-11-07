package integration

import (
	"errors"
	"net/http"
	"time"
)

// nativeProxyURL is a fixed in-cluster peer, never an arbitrary Connection or caller URL.
// nativeProxyURL — фиксированный peer внутри кластера, а не произвольный URL Connection либо caller.
const nativeProxyURL = "http://egress-gateway-native.kodex-system.svc.cluster.local:8085"

// newNativeHTTPClient keeps the default nil when disabled and clones verified TLS settings
// only for the exact optional native peer. Proxy failure cannot fall back to core or direct HTTP.
// newNativeHTTPClient сохраняет default nil при отключении и копирует проверенные TLS settings
// лишь для точного optional native peer. Ошибка proxy не вызывает fallback к core либо direct HTTP.
func newNativeHTTPClient(raw string, base *http.Transport, timeout time.Duration) (*http.Client, error) {
	if raw == "" {
		return nil, nil
	}
	if raw != nativeProxyURL || base == nil {
		return nil, errors.New("integration adapter native proxy is invalid")
	}
	proxy := mustURL(nativeProxyURL)
	transport := base.Clone()
	transport.Proxy = http.ProxyURL(proxy)
	return &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("native integration redirect is forbidden")
	}}, nil
}
