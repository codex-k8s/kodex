package integration

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/securefile"
)

// localOpenAPIHost identifies the sole internal synthetic provider eligible for the optional trusted-cluster fixture client.
// localOpenAPIHost задаёт единственного внутреннего синтетического провайдера, которому доступен необязательный тестовый клиент trusted-cluster.
const localOpenAPIHost = "integration-synthetic.kodex-system.svc.cluster.local"

// newLocalOpenAPIClient enables only a complete trusted-cluster fixture configuration with the exact HTTPS host and a securely read CA; its direct TLS 1.3 client neither uses a proxy nor follows redirects.
// newLocalOpenAPIClient включает только полную тестовую конфигурацию trusted-cluster с точным HTTPS-узлом и безопасно прочитанным CA; прямой TLS 1.3-клиент не использует прокси и не следует перенаправлениям.
func newLocalOpenAPIClient(config Config) (*url.URL, *http.Client, error) {
	if config.LocalOpenAPIBaseURL == "" && config.LocalOpenAPICAFile == "" {
		return nil, nil, nil
	}
	if config.RPCProfile != transportprofile.TrustedCluster || config.LocalOpenAPIBaseURL == "" || config.LocalOpenAPICAFile == "" {
		return nil, nil, errors.New("local OpenAPI fixture configuration is invalid")
	}
	baseURL, err := url.Parse(config.LocalOpenAPIBaseURL)
	if err != nil || baseURL.Scheme != "https" || baseURL.Host != localOpenAPIHost || baseURL.Path != "" ||
		baseURL.RawQuery != "" || baseURL.Fragment != "" || baseURL.User != nil {
		return nil, nil, errors.New("local OpenAPI fixture endpoint is invalid")
	}
	ca, err := securefile.Read(config.LocalOpenAPICAFile, 1<<20)
	if err != nil {
		return nil, nil, errors.New("local OpenAPI fixture CA is unavailable")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, nil, errors.New("local OpenAPI fixture CA is invalid")
	}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS13, ServerName: localOpenAPIHost, RootCAs: roots},
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: config.Timeout,
	}
	return baseURL, &http.Client{
		Transport: transport,
		Timeout:   config.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("local OpenAPI fixture redirect is forbidden")
		},
	}, nil
}

// openAPIClient chooses the private fixture client only for an exact configured base URL match and uses the ordinary OpenAPI client for every other origin.
// openAPIClient выбирает закрытый тестовый клиент только при точном совпадении заданного базового URL, для остальных источников использует обычный OpenAPI-клиент.
func (adapter *Adapter) openAPIClient(baseURL string) *http.Client {
	if adapter.localOpenAPIBaseURL != nil && baseURL == adapter.localOpenAPIBaseURL.String() {
		return adapter.localOpenAPIClient
	}
	return adapter.openAPIHTTPClient
}
