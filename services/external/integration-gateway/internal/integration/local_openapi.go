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

const localOpenAPIHost = "integration-synthetic.kodex-system.svc.cluster.local"

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

func (adapter *Adapter) openAPIClient(baseURL string) *http.Client {
	if adapter.localOpenAPIBaseURL != nil && baseURL == adapter.localOpenAPIBaseURL.String() {
		return adapter.localOpenAPIClient
	}
	return adapter.openAPIHTTPClient
}
