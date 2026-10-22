package callback

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// MCPTransport creates the default TLS 1.3 callback transport with the MCP header budget.
// The caller still supplies the execution bearer and verifies the endpoint/profile; this constructor does not authorize a request.
// MCPTransport создаёт стандартный callback-транспорт TLS 1.3 с бюджетом заголовков MCP.
// Вызывающий по-прежнему передаёт билет выполнения и проверяет endpoint и профиль; конструктор не авторизует запрос.
func MCPTransport(binding model.TLSBinding) (*http.Transport, error) {
	return newExactTransport(binding, time.Duration(runtimecontract.MaximumSynchronousMCPToolTimeoutSeconds+5)*time.Second, "MCP")
}

// newExactTransport loads an isolated CA pool and client keypair, pins TLS 1.3 and SNI, and preserves shared pool/header/handshake settings.
// CA size is checked after os.ReadFile; this is not a no-follow credential reader or endpoint validator.
// Only fixed local wrappers supply the timeout and safe diagnostic prefix; no environment proxy or system-root fallback is enabled.
// newExactTransport загружает отдельный пул CA и клиентскую пару ключа и сертификата, закрепляет TLS 1.3 и SNI, сохраняет общие настройки пула, заголовков и рукопожатия.
// Размер CA проверяется после os.ReadFile; это не reader учётных данных без ссылок и не валидатор endpoint.
// Таймаут и безопасный префикс диагностики задают только фиксированные локальные обёртки; прокси из окружения и обход через системные корни не включаются.
func newExactTransport(binding model.TLSBinding, headerTimeout time.Duration, diagnosticPrefix string) (*http.Transport, error) {
	ca, err := os.ReadFile(binding.CAFile)
	if err != nil || len(ca) == 0 || len(ca) > 1<<20 {
		return nil, errors.New("read " + diagnosticPrefix + " CA")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("parse " + diagnosticPrefix + " CA")
	}
	certificate, err := tls.LoadX509KeyPair(binding.CertificateFile, binding.PrivateKeyFile)
	if err != nil {
		return nil, errors.New("load " + diagnosticPrefix + " client identity")
	}
	return &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, ServerName: binding.ServerName, RootCAs: roots, Certificates: []tls.Certificate{certificate}}, DisableCompression: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: headerTimeout, MaxResponseHeaderBytes: 16 << 10}, nil
}
