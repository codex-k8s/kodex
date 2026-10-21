package gateway

import (
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
)

// runtimeProxyProviderResponsesLog defines the closed common fields for provider-response diagnostics.
// runtimeProxyProviderResponsesLog задаёт закрытые общие поля диагностики ответов провайдера.
const runtimeProxyProviderResponsesLog = "Runtime proxy provider responses: route=RESPONSES mode=%s event=%s outcome=%s status_class=%s http_status=%s failure=%s"

// runtimeProxyProviderResponsesHandshakeLog adds only bounded handshake metadata to the common event.
// runtimeProxyProviderResponsesHandshakeLog добавляет к общему событию лишь ограниченные метаданные рукопожатия.
const runtimeProxyProviderResponsesHandshakeLog = runtimeProxyProviderResponsesLog + " http_version=%s connection_close=%s header_lines=%s"

// runtimeProxyProviderResponsesPumpLog adds two presence flags without logging WebSocket frame bytes.
// runtimeProxyProviderResponsesPumpLog добавляет два признака наличия данных без записи байтов кадров.
const runtimeProxyProviderResponsesPumpLog = runtimeProxyProviderResponsesLog + " client_data=%s upstream_data=%s"

// providerResponsesWebSocketMaxHeaders pins the tungstenite fork's 124-line response limit.
// providerResponsesWebSocketMaxHeaders закрепляет предел ответа в 124 строки для fork tungstenite.
const providerResponsesWebSocketMaxHeaders = 124

// providerWebSocketHandshakeWriter counts only written line breaks, never retaining handshake header bytes.
// providerWebSocketHandshakeWriter считает только записанные переводы строк без хранения байтов заголовков.
// The saturated count includes status and final blank lines and does not alter the wrapped write result.
// Счётчик с насыщением включает строку статуса и пустое завершение, не меняя результат записи.
type providerWebSocketHandshakeWriter struct {
	io.Writer
	lineBreaks int
}

// Write forwards the bytes unchanged and counts only the portion actually accepted by the destination.
// Write передаёт байты без изменений и считает лишь фактически принятый назначением участок.
func (writer *providerWebSocketHandshakeWriter) Write(data []byte) (int, error) {
	n, err := writer.Writer.Write(data)
	for _, value := range data[:n] {
		if writer.lineBreaks == providerResponsesWebSocketMaxHeaders+3 {
			break
		}
		if value == '\n' {
			writer.lineBreaks++
		}
	}
	return n, err
}

// providerResponsesDiagnostic holds a closed HTTP or WSS label only for exact provider response routes.
// providerResponsesDiagnostic хранит закрытую метку HTTP или WSS лишь для точных маршрутов провайдера.
type providerResponsesDiagnostic struct{ mode string }

// newProviderResponsesDiagnostic enables diagnostics only for two exact, authorized provider paths.
// newProviderResponsesDiagnostic включает диагностику только для двух точных разрешённых путей провайдера.
// It grants no authority and retains no URL, header, or body data.
// Она не выдаёт полномочия и не сохраняет URL, заголовки или тело.
func newProviderResponsesDiagnostic(request *http.Request, target connect.Target, access runtimecontract.RuntimeProxyAccess) providerResponsesDiagnostic {
	if !access.ProviderAccess || target.Port != 443 || request == nil || request.URL == nil || request.URL.RawPath != "" {
		return providerResponsesDiagnostic{}
	}
	if !(target.Hostname == "api.openai.com" && request.URL.Path == "/v1/responses" || target.Hostname == "chatgpt.com" && request.URL.Path == "/backend-api/codex/responses") {
		return providerResponsesDiagnostic{}
	}
	if hasWebSocketUpgrade(request.Header) {
		return providerResponsesDiagnostic{mode: "WSS"}
	}
	return providerResponsesDiagnostic{mode: "HTTP"}
}

// enabled reports whether this request was assigned one of the two closed diagnostic modes.
// enabled сообщает, назначен ли запросу один из двух закрытых режимов диагностики.
func (diagnostic providerResponsesDiagnostic) enabled() bool {
	return diagnostic.mode == "HTTP" || diagnostic.mode == "WSS"
}

// providerResponsesStatus maps only known HTTP statuses to exact labels and hides all other values.
// providerResponsesStatus отображает известные HTTP-статусы в точные метки, скрывая остальные значения.
func providerResponsesStatus(status int) string {
	switch status {
	case 101, 200, 201, 204, 400, 401, 403, 404, 408, 409, 413, 415, 422, 426, 429, 500, 502, 503, 504:
		return strconv.Itoa(status)
	case 0:
		return "NONE"
	default:
		return "UNKNOWN"
	}
}

// providerResponsesPolicyFailure classifies a denied request without participating in authorization.
// providerResponsesPolicyFailure классифицирует отклонённый запрос, не участвуя в авторизации.
// It mirrors proxyRequestAllowed reasons but never changes its predicate.
// Функция повторяет причины proxyRequestAllowed, не меняя его предикат.
func providerResponsesPolicyFailure(request *http.Request, target connect.Target, access runtimecontract.RuntimeProxyAccess) string {
	if request == nil || request.Method == http.MethodConnect || request.URL == nil || request.URL.IsAbs() || request.RequestURI == "" {
		return "REQUEST_ENVELOPE"
	}
	host := request.Host
	if host == "" {
		host = request.URL.Host
	}
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		if parsedPort != "443" {
			return "HOST"
		}
		host = parsedHost
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host != target.Hostname || request.URL.RawPath != "" {
		return "HOST"
	}
	if hasWebSocketUpgrade(request.Header) {
		if !access.ProviderAccess || !runtimecontract.RuntimeProviderAllowsWebSocket(host, request.URL.Path, request.Method) {
			return "METHOD_PATH"
		}
		if !validWebSocketRequest(request) {
			switch {
			case len(request.Header.Values("Sec-WebSocket-Protocol")) != 0:
				return "WS_SUBPROTOCOL"
			case !validWebSocketExtensionOffer(request.Header):
				return "WS_EXTENSIONS"
			default:
				return "WS_HANDSHAKE"
			}
		}
	}
	return "METHOD_PATH"
}

// policy logs an allow or deny decision with only closed outcome and reason labels.
// policy записывает допуск или отказ только с закрытыми метками исхода и причины.
func (diagnostic providerResponsesDiagnostic) policy(allowed bool, reason string) {
	if !diagnostic.enabled() {
		return
	}
	outcome := "DENIED"
	if allowed {
		outcome = "ALLOWED"
		reason = "NONE"
	} else {
		switch reason {
		case "REQUEST_ENVELOPE", "HOST", "METHOD_PATH", "WS_HANDSHAKE", "WS_SUBPROTOCOL", "WS_EXTENSIONS":
		default:
			reason = "UNKNOWN"
		}
	}
	log.Printf(runtimeProxyProviderResponsesLog, diagnostic.mode, "POLICY", outcome, "NONE", "NONE", reason)
}

// upstream logs a bounded response status or classified transport failure without response content.
// upstream записывает ограниченный статус ответа или класс транспортного сбоя без содержимого ответа.
func (diagnostic providerResponsesDiagnostic) upstream(status int, err error, tlsFailed bool) {
	if !diagnostic.enabled() {
		return
	}
	if err != nil {
		log.Printf(runtimeProxyProviderResponsesLog, diagnostic.mode, "UPSTREAM", "FAILED", "NONE", "NONE", providerDiscoveryFailure(err, tlsFailed))
		return
	}
	log.Printf(runtimeProxyProviderResponsesLog, diagnostic.mode, "UPSTREAM", "RESPONSE", providerDiscoveryStatusClass(status), providerResponsesStatus(status), "NONE")
}

// body records completion or a closed read-failure class after streaming the upstream body.
// body фиксирует завершение либо закрытый класс ошибки чтения после передачи тела upstream.
func (diagnostic providerResponsesDiagnostic) body(status int, err error) {
	if !diagnostic.enabled() {
		return
	}
	outcome, failure := "COMPLETED", "NONE"
	if err != nil {
		outcome, failure = "FAILED", providerDiscoveryBodyFailure(err)
	}
	log.Printf(runtimeProxyProviderResponsesLog, diagnostic.mode, "UPSTREAM_BODY", outcome, providerDiscoveryStatusClass(status), providerResponsesStatus(status), failure)
}

// upgrade logs only an approved WebSocket upgrade outcome and bounded HTTP status.
// upgrade записывает только допустимый исход WebSocket upgrade и ограниченный HTTP-статус.
func (diagnostic providerResponsesDiagnostic) upgrade(outcome string, status int) {
	if diagnostic.mode != "WSS" {
		return
	}
	switch outcome {
	case "ACCEPTED", "REJECTED", "WRITE_FAILED":
	default:
		outcome = "UNKNOWN"
	}
	log.Printf(runtimeProxyProviderResponsesLog, diagnostic.mode, "UPGRADE", outcome, providerDiscoveryStatusClass(status), providerResponsesStatus(status), "NONE")
}

// handshake reports protocol and header-line classes after the bounded 101 write, never header values.
// handshake сообщает классы протокола и числа строк после ограниченной записи 101 без значений заголовков.
func (diagnostic providerResponsesDiagnostic) handshake(response *http.Response, writer *providerWebSocketHandshakeWriter) {
	if diagnostic.mode != "WSS" || response == nil || writer == nil {
		return
	}
	version, closeConnection, headerLines := "OTHER", "FALSE", "WITHIN_124"
	if response.ProtoMajor == 1 && response.ProtoMinor == 1 {
		version = "HTTP11"
	}
	if response.Close {
		closeConnection = "TRUE"
	}
	if writer.lineBreaks > providerResponsesWebSocketMaxHeaders+2 {
		headerLines = "OVER_124"
	}
	log.Printf(runtimeProxyProviderResponsesHandshakeLog, diagnostic.mode, "HANDSHAKE", "OBSERVED", "1XX", "101", "NONE", version, closeConnection, headerLines)
}

// pump reports the terminal WebSocket reason and per-direction data presence after both pumps join.
// pump сообщает конечную причину WebSocket и наличие данных по направлениям после завершения обеих pump.
func (diagnostic providerResponsesDiagnostic) pump(reason string, clientData, upstreamData bool) {
	if diagnostic.mode != "WSS" {
		return
	}
	switch reason {
	case "CLIENT_EOF", "UPSTREAM_EOF", "CLIENT_IO", "UPSTREAM_IO", "WRITE_FAILED", "IDLE", "SHUTDOWN":
	default:
		reason = "UNKNOWN"
	}
	clientPresence, upstreamPresence := "ABSENT", "ABSENT"
	if clientData {
		clientPresence = "PRESENT"
	}
	if upstreamData {
		upstreamPresence = "PRESENT"
	}
	log.Printf(runtimeProxyProviderResponsesPumpLog, diagnostic.mode, "PUMP", "CLOSED", "1XX", "101", reason, clientPresence, upstreamPresence)
}

// providerWebSocketReadFailure distinguishes EOF from I/O errors on each side without exposing error text.
// providerWebSocketReadFailure различает EOF и ошибки ввода-вывода сторон без раскрытия текста ошибки.
func providerWebSocketReadFailure(err error, client bool) string {
	if errors.Is(err, io.EOF) {
		if client {
			return "CLIENT_EOF"
		}
		return "UPSTREAM_EOF"
	}
	if client {
		return "CLIENT_IO"
	}
	return "UPSTREAM_IO"
}
