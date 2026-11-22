package kubernetestarget

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/kubernetestransport"
)

const (
	// invalidRequest rejects attempted transport use beyond the pinned API authority without URL leakage.
	// invalidRequest отклоняет обращение за пределами закреплённого API authority без раскрытия URL.
	invalidRequest = "managed Kubernetes target request is forbidden"
	// connectionFailed hides network endpoint details while leaving cancellation independently observable.
	// connectionFailed скрывает сетевые параметры; отмена остаётся отдельно наблюдаемой.
	connectionFailed = "managed Kubernetes target connection failed"
)

// credentialTransport adds credentials only to exact-target requests produced by the broker executor.
// credentialTransport добавляет реквизиты только к exact-target requests исполнителя broker.
type credentialTransport struct {
	base       http.RoundTripper
	origin     string
	token      string
	validUntil time.Time
}

// RoundTrip rejects changed authority, userinfo and caller-supplied authentication before transport.
// It clones the request to keep private Authorization out of caller-owned state and never follows redirects.
// RoundTrip отклоняет иной authority, userinfo и caller authentication до transport.
// Клонирует request, чтобы приватный Authorization не изменял caller state; redirects запрещены.
func (transport *credentialTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || request.URL.Scheme != "https" ||
		request.URL.Host != transport.origin || request.URL.User != nil || request.URL.Opaque != "" ||
		request.URL.Fragment != "" || (request.Host != "" && request.Host != transport.origin) {
		return nil, errors.New(invalidRequest)
	}
	if !transport.validUntil.IsZero() && !time.Now().Before(transport.validUntil) {
		return nil, errors.New(privilegedTransportUnavailable)
	}
	for key := range request.Header {
		lower := strings.ToLower(key)
		if lower == "authorization" || lower == "proxy-authorization" || strings.HasPrefix(lower, "impersonate-") {
			return nil, errors.New(invalidRequest)
		}
	}
	privateRequest := request.Clone(request.Context())
	var cancel context.CancelFunc
	if !transport.validUntil.IsZero() {
		ctx, stop := context.WithDeadline(request.Context(), transport.validUntil)
		cancel = stop
		privateRequest = privateRequest.WithContext(ctx)
	}
	if privateRequest.Header == nil {
		privateRequest.Header = make(http.Header)
	}
	if transport.token != "" {
		privateRequest.Header.Set("Authorization", "Bearer "+transport.token)
	}
	response, err := transport.base.RoundTrip(privateRequest)
	if cancel != nil {
		if err != nil || response == nil || response.Body == nil {
			cancel()
		} else {
			response.Body = &transportLifetimeBody{ReadCloser: response.Body, ctx: privateRequest.Context(), cancel: cancel}
		}
	}
	return response, err
}

// transportLifetimeBody releases a per-request pin-expiry timer after body completion or close.
// transportLifetimeBody освобождает per-request pin-expiry timer после body completion либо close.
type transportLifetimeBody struct {
	io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
}

// Read preserves partial bytes inside the broker but never promotes an expired response's EOF to success.
// The lifetime is checked before cancelling its timer, including an elapsed deadline whose callback is delayed.
// Read сохраняет partial bytes внутри broker, но не превращает EOF истёкшего response в success.
// Lifetime проверяется до отмены timer, включая истёкший deadline с задержанным callback.
func (body *transportLifetimeBody) Read(buffer []byte) (int, error) {
	if err := body.lifetimeError(); err != nil {
		body.cancel()
		return 0, err
	}
	n, err := body.ReadCloser.Read(buffer)
	if lifetimeErr := body.lifetimeError(); lifetimeErr != nil {
		err = lifetimeErr
	}
	if err != nil {
		body.cancel()
	}
	return n, err
}

// lifetimeError refuses expired pin credit even before the context timer goroutine has run.
// lifetimeError отклоняет истёкший pin credit даже до выполнения context timer goroutine.
func (body *transportLifetimeBody) lifetimeError() error {
	if deadline, ok := body.ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return body.ctx.Err()
}

// Close cancels the pinned request before joining its underlying response body.
// Close отменяет pinned request до join его underlying response body.
func (body *transportLifetimeBody) Close() error {
	body.cancel()
	return body.ReadCloser.Close()
}

// NewClient builds a broker-only HTTPS client from already-validated credential material.
// The client connects exclusively to operator-pinned IPs, disables ambient proxies and redirects,
// and does not expose kubeconfig or an SDK config. The owner must close idle connections after join.
// NewClient создаёт broker-only HTTPS client из ранее проверенных реквизитов.
// Соединяется только с operator-pinned IPs, запрещает proxy окружения и redirects,
// не выдаёт kubeconfig или SDK config. Владелец закрывает idle connections после join.
func (credential Credential) NewClient(timeout time.Duration) (*http.Client, func(), error) {
	if validateTarget(credential.target) != nil || len(credential.ca) == 0 ||
		(credential.token == "" && len(credential.cert.Certificate) == 0) || timeout <= 0 || timeout > 10*time.Minute {
		return nil, nil, errors.New(invalidCredential)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(credential.ca) {
		return nil, nil, errors.New(invalidCredential)
	}
	client, err := kubernetestransport.New(roots, credential.target.TLSServerName, timeout, invalidRequest)
	if err != nil {
		return nil, nil, errors.New(invalidCredential)
	}
	base, ok := client.Transport.(*http.Transport)
	if !ok {
		return nil, nil, errors.New(invalidCredential)
	}
	if len(credential.cert.Certificate) > 0 {
		base.TLSClientConfig.Certificates = []tls.Certificate{credential.cert}
	}
	u, err := url.Parse(credential.target.ServerURL)
	if err != nil {
		return nil, nil, errors.New(invalidCredential)
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	expectedAddress := net.JoinHostPort(u.Hostname(), port)
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" || address != expectedAddress {
			return nil, errors.New(invalidRequest)
		}
		for _, pin := range credential.target.AddressPins {
			dialer := net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
			connection, dialErr := dialer.DialContext(ctx, "tcp", net.JoinHostPort(pin, port))
			if dialErr == nil {
				return connection, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		return nil, errors.New(connectionFailed)
	}
	base.MaxIdleConns = 8
	base.MaxIdleConnsPerHost = 4
	base.MaxConnsPerHost = 8
	base.ResponseHeaderTimeout = 15 * time.Second
	client.Transport = &credentialTransport{base: base, origin: u.Host, token: credential.token}
	return client, base.CloseIdleConnections, nil
}
