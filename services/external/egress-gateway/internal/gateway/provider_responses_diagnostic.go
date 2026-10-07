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

const runtimeProxyProviderResponsesLog = "Runtime proxy provider responses: route=RESPONSES mode=%s event=%s outcome=%s status_class=%s http_status=%s failure=%s"
const runtimeProxyProviderResponsesHandshakeLog = runtimeProxyProviderResponsesLog + " http_version=%s connection_close=%s header_lines=%s"
const runtimeProxyProviderResponsesPumpLog = runtimeProxyProviderResponsesLog + " client_data=%s upstream_data=%s"

// Закреплённый tungstenite fork Codex 0.160.0 допускает 124 header lines.
const providerResponsesWebSocketMaxHeaders = 124

// Считает только LF записанного 101 без body, не сохраняет header bytes.
// Две строки принадлежат status и пустому завершению; счётчик насыщается
// сразу после превышения SDK bound и не влияет на результат Writer.
type providerWebSocketHandshakeWriter struct {
	io.Writer
	lineBreaks int
}

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

type providerResponsesDiagnostic struct{ mode string }

// Диагностика не назначает authority и не сохраняет URL, headers либо body.
// Только два точных provider paths получают закрытое имя RESPONSES.
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

func (diagnostic providerResponsesDiagnostic) enabled() bool {
	return diagnostic.mode == "HTTP" || diagnostic.mode == "WSS"
}

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

// Повторяет только классификацию отказа существующего proxyRequestAllowed;
// этот результат не участвует в authorization и не меняет policy predicate.
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
