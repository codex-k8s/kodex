package exactkubernetessecret

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// newKubernetesHTTPClient loads only the mounted Kubernetes CA and creates the same protected transport for both exact-resource clients.
// newKubernetesHTTPClient читает только смонтированный Kubernetes CA и создаёт одинаковый защищённый транспорт для обоих клиентов точечного ресурса.
func newKubernetesHTTPClient(timeout time.Duration) (*http.Client, error) {
	caRaw, err := os.ReadFile(KubernetesCAFile)
	if err != nil || len(caRaw) == 0 || len(caRaw) > 1<<20 {
		return nil, errors.New("read exact Kubernetes API CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caRaw) {
		return nil, errors.New("parse exact Kubernetes API CA")
	}
	return kubernetesHTTPClientForRoots(roots, timeout), nil
}

// kubernetesHTTPClientForRoots fixes TLS 1.3, exact server name, supplied trust roots and redirect rejection without inheriting environment proxy configuration.
// kubernetesHTTPClientForRoots закрепляет TLS 1.3, точное имя сервера, переданные корни доверия и запрет перенаправлений без наследования прокси из окружения.
func kubernetesHTTPClientForRoots(roots *x509.CertPool, timeout time.Duration) *http.Client {
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, ServerName: KubernetesServerName, RootCAs: roots,
	}, ForceAttemptHTTP2: true, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second}
	return &http.Client{
		Transport: transport, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("exact Kubernetes API redirect is forbidden")
		},
	}
}

// readKubernetesAPIToken rereads the mounted rotating token for every request and rejects empty, oversized or multiline bearer material without exposing its contents.
// readKubernetesAPIToken заново читает смонтированный сменяемый токен для каждого запроса и отклоняет пустое, чрезмерное или многострочное значение без раскрытия содержимого.
func readKubernetesAPIToken() (string, error) {
	tokenRaw, err := os.ReadFile(KubernetesTokenFile)
	if err != nil {
		return "", errors.New("read exact Kubernetes API token")
	}
	token := strings.TrimSpace(string(tokenRaw))
	if token == "" || len(token) > 16<<10 || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("exact Kubernetes API token is invalid")
	}
	return token, nil
}

// newKubernetesSecretRequest builds only the fixed API namespace and escaped prevalidated Secret name, attaches the already validated bearer token and preserves caller context and body.
// newKubernetesSecretRequest строит только фиксированное пространство API и экранированное проверенное имя Secret, добавляет уже проверенный bearer-токен и сохраняет контекст и тело вызывающего.
func newKubernetesSecretRequest(ctx context.Context, method, resourceName, token string, body io.Reader) (*http.Request, error) {
	endpoint := KubernetesAPIAddress + "/api/v1/namespaces/" + url.PathEscape(KubernetesNamespace) +
		"/secrets/" + url.PathEscape(resourceName)
	request, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	return request, nil
}
