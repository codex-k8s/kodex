// Package streamproxy shares post-authorization HTTP streaming mechanics without choosing identities or permissions.
// Пакет streamproxy объединяет передачу HTTP после авторизации, не выбирая идентичности или права.
package streamproxy

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Timeouts carries caller-owned header, active-stream and idle budgets without applying defaults or validation.
// Timeouts переносит заданные вызывающим бюджеты заголовков, активного потока и простоя без умолчаний или проверки.
type Timeouts struct {
	Header, Stream, Idle time.Duration
}

// NewClient limits the whole upstream exchange and refuses redirects; transport remains the standard default.
// Callers select the timeout and a fixed authorized target; this constructor does not enforce destination policy.
// NewClient ограничивает весь обмен с upstream и отклоняет перенаправления; транспорт остаётся стандартным.
// Бюджет и фиксированное разрешённое назначение задаёт вызывающий; конструктор не проверяет политику назначений.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("redirect rejected")
		},
	}
}

// NewMTLSServer configures TLS 1.3, mandatory verified client certificates and caller-owned stream budgets.
// It opens no socket and does not authorize requests; handlers, client CA roots, serving and shutdown belong to callers.
// NewMTLSServer настраивает TLS 1.3, обязательные проверенные клиентские сертификаты и заданные бюджеты потока.
// Он не открывает сокет и не авторизует запросы; обработчики, корни клиентских CA, запуск и остановка принадлежат вызывающему.
func NewMTLSServer(address string, handler http.Handler, pool *x509.CertPool, timeouts Timeouts) *http.Server {
	return &http.Server{
		Addr: address, Handler: handler,
		ReadHeaderTimeout: timeouts.Header,
		ReadTimeout:       timeouts.Stream,
		WriteTimeout:      timeouts.Stream,
		IdleTimeout:       timeouts.Idle,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ClientAuth: tls.RequireAndVerifyClientCert,
			ClientCAs:  pool,
		},
	} //nolint:gosec // Caller-owned client CA roots and TLS 1.3.
}

// Forward copies an already authorized path/query to a caller-owned backend, strips Authorization and streams the response.
// Body may be nil for a read-only proxy. The response body is closed; copy errors are ignored after status is committed,
// preserving the callers' previous behavior. No ownership, destination, method or hop-header policy is inferred here.
// Forward копирует уже разрешённые путь и query в заданный сервер, удаляет Authorization и передаёт ответ потоком.
// Для прокси чтения body может быть nil. Тело ответа закрывается; ошибки копирования после отправки статуса игнорируются,
// сохраняя прежнее поведение вызывающих. Политики владения, назначения, методов и hop-заголовков здесь не выводятся.
func Forward(writer http.ResponseWriter, request *http.Request, backend *url.URL, body io.Reader, client *http.Client, maximumResponseBytes int64) {
	target := *backend
	target.Path, target.RawQuery = request.URL.Path, request.URL.RawQuery
	upstream, err := http.NewRequestWithContext(request.Context(), request.Method, target.String(), body)
	if err != nil {
		http.Error(writer, "upstream request rejected", http.StatusBadGateway)
		return
	}
	upstream.Header = request.Header.Clone()
	upstream.Header.Del("Authorization")
	response, err := client.Do(upstream)
	if err != nil {
		http.Error(writer, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	for key, values := range response.Header {
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, io.LimitReader(response.Body, maximumResponseBytes))
}
