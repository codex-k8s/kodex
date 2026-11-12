package providercredential

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	// providerEgressProxyURL is the fixed managed proxy for API catalog and isolated process egress; callers
	// cannot choose an arbitrary proxy or bypass target through this adapter.
	// providerEgressProxyURL задаёт фиксированный управляемый proxy для API каталога и выхода изолированного
	// процесса; вызывающий не выбирает произвольный proxy или обходную цель через этот адаптер.
	providerEgressProxyURL = "http://egress-gateway.kodex-system.svc.cluster.local:8080"
	// providerModelsURL pins the single HTTPS account-model-list endpoint used by API-key observation;
	// redirects are rejected by the production client.
	// providerModelsURL закрепляет единственный HTTPS endpoint списка моделей account для API-key
	// наблюдения; production клиент отклоняет redirects.
	providerModelsURL = "https://api.openai.com/v1/models"
	// maximumAPIModelRecords caps a complete provider model-list response at 4096 records before
	// intersection with the smaller verified capability catalog.
	// maximumAPIModelRecords ограничивает полный provider model-list ответ 4096 записями до пересечения с
	// меньшим проверенным capability каталогом.
	maximumAPIModelRecords = 4096
)

// modelCatalogHTTPClient narrows API observation to one injectable HTTP Do operation, allowing synthetic
// response tests without exposing transport selection to callers.
// modelCatalogHTTPClient ограничивает API наблюдение одной подменяемой HTTP Do операцией для тестов с
// синтетическими ответами без передачи вызывающему выбора транспорта.
type modelCatalogHTTPClient interface {
	// Do executes the supplied context-bound request and returns a response whose body is closed by the
	// catalog reader; production proxy, TLS and redirect policy belong to the configured client.
	// Do выполняет переданный context-bound запрос и возвращает ответ, чей body закрывает catalog reader;
	// production proxy, TLS и redirect политика принадлежат настроенному клиенту.
	Do(*http.Request) (*http.Response, error)
}

// newModelCatalogHTTPClient builds a TLS-1.3-only client through the fixed proxy with bounded
// dial/handshake/header/total timeouts and no connection reuse. Its CONNECT hook verifies proxy, target
// and status; redirects always fail rather than forwarding credentials elsewhere.
// newModelCatalogHTTPClient строит TLS-1.3-only клиент через фиксированный proxy с ограниченными
// dial/handshake/header/total таймаутами без повторного использования соединений. CONNECT hook сверяет
// proxy, target и status; redirects всегда отклоняются без пересылки credentials другой цели.
func newModelCatalogHTTPClient() *http.Client {
	proxy, _ := url.Parse(providerEgressProxyURL)
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyURL(proxy),
			DialContext:           (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13},
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 5 * time.Second,
			DisableKeepAlives:     true,
			OnProxyConnectResponse: func(_ context.Context, actual *url.URL, request *http.Request, response *http.Response) error {
				if actual == nil || actual.String() != providerEgressProxyURL || request == nil || request.Method != http.MethodConnect || request.Host != "api.openai.com:443" || response == nil || response.StatusCode != http.StatusOK {
					return errors.New("provider catalog CONNECT boundary is invalid")
				}
				return nil
			},
		},
		Timeout: modelCatalogTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("provider catalog redirect is rejected")
		},
	}
}
