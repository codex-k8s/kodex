package integration

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"
)

const (
	// context7Origin fixes the HTTPS provider origin accepted by connection validation; model input cannot select another host.
	// context7Origin закрепляет HTTPS-источник провайдера для проверки соединения; вход модели не выбирает другой узел.
	context7Origin = "https://mcp.context7.com"
	// context7Endpoint adds the sole permitted MCP path to the fixed origin and is compared as a complete URL by the transport.
	// context7Endpoint задаёт единственный разрешённый MCP-путь закреплённого источника; транспорт сравнивает полный URL.
	context7Endpoint = context7Origin + "/mcp"
	// context7Proxy identifies the exact internal egress proxy required for the Context7 client, rather than accepting an arbitrary proxy from configuration.
	// context7Proxy задаёт точный внутренний выходной прокси клиента Context7 вместо произвольного прокси из конфигурации.
	context7Proxy = "http://egress-gateway-openapi.kodex-system.svc.cluster.local:8083"
	// context7RequestBytes caps the encoded request body before forwarding and parsing its protocol method.
	// context7RequestBytes ограничивает размер закодированного тела запроса до передачи и разбора протокольного метода.
	context7RequestBytes = 16 << 10
	// context7ResponseBytes bounds both the response-body reader and the SDK's single SSE event buffer.
	// context7ResponseBytes ограничивает чтение тела ответа и буфер отдельного SSE-события SDK.
	context7ResponseBytes = 512 << 10
	// context7TextBytes limits the combined projected tool text, including separators inserted between content blocks.
	// context7TextBytes ограничивает объединённую текстовую проекцию инструмента вместе с разделителями между блоками.
	context7TextBytes = 32 << 10
	// context7Deadline caps execution and HTTP response waits even when the configured timeout is longer.
	// context7Deadline ограничивает выполнение и ожидание HTTP-ответа, даже если заданный таймаут больше.
	context7Deadline = 30 * time.Second
)

// newContext7HTTPClient requires the exact egress proxy and a bounded timeout, then constructs a TLS 1.3 client with fixed SNI, bounded connections and headers, and no redirects.
// newContext7HTTPClient требует точный выходной прокси и ограниченный таймаут, затем создаёт TLS 1.3-клиент с закреплённым SNI, ограничениями соединений и заголовков, без перенаправлений.
func newContext7HTTPClient(config Config) (*http.Client, error) {
	if config.OpenAPIProxyURL != context7Proxy || config.Timeout < time.Second || config.Timeout > 2*time.Minute {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	proxy, _ := url.Parse(context7Proxy)
	return &http.Client{
		Transport: &http.Transport{
			Proxy:               http.ProxyURL(proxy),
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS13, ServerName: "mcp.context7.com"},
			TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: min(config.Timeout, context7Deadline),
			MaxResponseHeaderBytes: 16 << 10, MaxIdleConns: 2, MaxIdleConnsPerHost: 2,
			MaxConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
		},
		Timeout:       min(config.Timeout, context7Deadline),
		CheckRedirect: func(*http.Request, []*http.Request) error { return &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"} },
	}, nil
}

// context7Transport wraps the configured HTTP transport with an exact provider boundary and an adapter-owned credential, never accepting an agent-selected address or credential.
// context7Transport оборачивает заданный HTTP-транспорт точной границей провайдера и учётными данными адаптера, не принимая адрес или учётные данные от агента.
type context7Transport struct {
	base       http.RoundTripper
	credential string
}

// RoundTrip permits only POST or bodyless DELETE at the exact MCP endpoint, replaces authentication headers, filters protocol methods, and bounds status, content type and body before returning the response.
// RoundTrip разрешает только POST либо DELETE без тела на точной MCP-конечной точке, заменяет заголовки авторизации, фильтрует протокольные методы и проверяет статус, тип и размер тела ответа.
func (transport context7Transport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL == nil || request.URL.String() != context7Endpoint || request.Host != "" && request.Host != "mcp.context7.com" ||
		(request.Method != http.MethodPost && request.Method != http.MethodDelete) {
		return nil, &SafeError{Code: "INTEGRATION_CONFIGURATION_INVALID"}
	}
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Del("Authorization")
	clone.Header.Del("Cookie")
	clone.Header.Set("CONTEXT7_API_KEY", transport.credential)
	method := ""
	if request.Method == http.MethodPost && request.Body == nil || request.Method == http.MethodDelete && request.Body != nil {
		return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	if request.Body != nil {
		body, err := io.ReadAll(io.LimitReader(request.Body, context7RequestBytes+1))
		_ = request.Body.Close()
		if err != nil || len(body) > context7RequestBytes {
			return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		var envelope struct {
			Method string `json:"method"`
		}
		if json.Unmarshal(body, &envelope) != nil || !context7MethodAllowed(envelope.Method) {
			return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
		}
		method = envelope.Method
		clone.Body = io.NopCloser(bytes.NewReader(body))
		clone.ContentLength = int64(len(body))
	}
	response, err := transport.base.RoundTrip(clone)
	if err != nil {
		return nil, &SafeError{Code: "INTEGRATION_UNAVAILABLE", Transient: true}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_ = response.Body.Close()
		return nil, statusError(response.StatusCode)
	}
	if request.Method == http.MethodPost {
		notification := method == "notifications/initialized" || method == "notifications/cancelled"
		mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
		if notification && response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusNoContent ||
			!notification && (response.StatusCode != http.StatusOK || mediaErr != nil || mediaType != "application/json" && mediaType != "text/event-stream") {
			_ = response.Body.Close()
			return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
	}
	if response.ContentLength > context7ResponseBytes {
		_ = response.Body.Close()
		return nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	response.Body = &context7BoundedBody{ReadCloser: response.Body, remaining: context7ResponseBytes}
	return response, nil
}

// context7MethodAllowed admits only initialization, its cancellation notifications, tool listing and tool calls; all other JSON-RPC method names are rejected.
// context7MethodAllowed допускает только инициализацию, её уведомления об отмене, список и вызов инструментов; остальные имена методов JSON-RPC отклоняются.
func context7MethodAllowed(method string) bool {
	switch method {
	case "initialize", "notifications/initialized", "notifications/cancelled", "tools/list", "tools/call":
		return true
	default:
		return false
	}
}

// context7BoundedBody retains the upstream closer while tracking the remaining response bytes; exhausting the budget requires an extra-byte EOF check.
// context7BoundedBody сохраняет закрытие внешнего потока и оставшийся бюджет ответа; исчерпание бюджета требует проверки конца потока чтением дополнительного байта.
type context7BoundedBody struct {
	io.ReadCloser
	remaining int64
}

// Read returns at most the remaining budget and probes one extra byte at exhaustion to distinguish an exact-sized response from overflow, leaving closure to the embedded reader.
// Read возвращает не больше оставшегося бюджета и при его исчерпании проверяет дополнительный байт, отличая точный размер от переполнения; закрытие выполняет встроенный поток.
func (body *context7BoundedBody) Read(buffer []byte) (int, error) {
	if len(buffer) == 0 {
		return 0, nil
	}
	if body.remaining == 0 {
		var extra [1]byte
		count, err := body.ReadCloser.Read(extra[:])
		if count > 0 {
			return 0, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
		}
		return 0, err
	}
	if int64(len(buffer)) > body.remaining {
		buffer = buffer[:body.remaining]
	}
	count, err := body.ReadCloser.Read(buffer)
	body.remaining -= int64(count)
	return count, err
}
