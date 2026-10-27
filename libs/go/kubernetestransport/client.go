// Package kubernetestransport constructs the shared in-cluster Kubernetes HTTP transport for authority
// adapters. Callers validate endpoint, CA and authorization separately before using it.
// Пакет kubernetestransport создаёт общий HTTP transport Kubernetes для authority adapters. Проверки
// endpoint, CA и authorization остаются у вызывающих до использования клиента.
package kubernetestransport

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"time"
)

// New constructs a fresh HTTP client with the caller's CA roots, server name, timeout and exact
// redirect error. It rejects missing roots, server name, timeout or redirect error before
// creating TLS 1.3 transport with HTTP/2 attempts and a nil proxy. It does not validate the
// endpoint or CA contents, load credentials, contact Kubernetes or own caller lifecycle.
// New создаёт новый HTTP client с переданными CA roots, именем сервера, timeout и точной ошибкой
// redirect. Отклоняет отсутствующие roots, имя, timeout или redirect error до создания TLS 1.3
// transport с попытками HTTP/2 и nil proxy. Не проверяет endpoint или содержимое CA, не загружает
// credentials, не обращается к Kubernetes и не владеет lifecycle вызывающего.
func New(roots *x509.CertPool, serverName string, timeout time.Duration, redirectError string) (*http.Client, error) {
	if roots == nil || serverName == "" || timeout <= 0 || redirectError == "" {
		return nil, errors.New("invalid Kubernetes transport configuration")
	}
	transport := &http.Transport{
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: serverName},
		ForceAttemptHTTP2: true,
	}
	return &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return errors.New(redirectError) },
	}, nil
}
