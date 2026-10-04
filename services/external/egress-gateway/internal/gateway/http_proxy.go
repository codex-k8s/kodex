package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"strings"
	"sync/atomic"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

var errProxyHeaderTooLarge = errors.New("runtime proxy request header is too large")

const runtimeProxyProviderDiscoveryLog = "Runtime proxy provider discovery: route=%s event=%s outcome=%s status_class=%s failure=%s"
const runtimeProxyProviderDiscoveryShapeLog = "Runtime proxy provider discovery: route=ACCOUNTS_CHECK event=RESPONSE_SHAPE content_encoding=%s content_type=%s schema=%s"
const maximumDiscoveryBodyBytes = 1 << 20

type discoveryBodyCapture struct{ body []byte }

func (capture *discoveryBodyCapture) Write(body []byte) (int, error) {
	remaining := maximumDiscoveryBodyBytes + 1 - len(capture.body)
	if remaining > 0 {
		capture.body = append(capture.body, body[:min(len(body), remaining)]...)
	}
	return len(body), nil
}

func (capture *discoveryBodyCapture) clear() {
	clear(capture.body)
	capture.body = nil
}

func providerDiscoveryEncoding(response *http.Response) string {
	if response.Uncompressed {
		return "IDENTITY"
	}
	if len(response.Header.Values("Content-Encoding")) > 1 {
		return "UNKNOWN"
	}
	switch strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding"))) {
	case "", "identity":
		return "IDENTITY"
	case "gzip":
		return "GZIP"
	case "br":
		return "BR"
	case "zstd":
		return "ZSTD"
	case "deflate":
		return "DEFLATE"
	default:
		return "UNKNOWN"
	}
}

func providerDiscoveryContentType(response *http.Response) string {
	if len(response.Header.Values("Content-Type")) != 1 {
		return "UNKNOWN"
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return "UNKNOWN"
	}
	switch {
	case mediaType == "application/json" || strings.HasSuffix(mediaType, "+json"):
		return "JSON"
	case mediaType == "text/html":
		return "HTML"
	case mediaType == "text/plain":
		return "TEXT"
	default:
		return "UNKNOWN"
	}
}

// Только известные provider paths преобразуются в закрытую диагностику.
// Host, URL/query, headers, body и identity запроса не записываются.
func providerDiscoveryRoute(request *http.Request, target connect.Target, access runtimecontract.RuntimeProxyAccess) string {
	if !access.ProviderAccess || target.Hostname != "chatgpt.com" || target.Port != 443 || request == nil || request.URL == nil || request.URL.RawPath != "" || request.Method != http.MethodGet {
		return ""
	}
	switch request.URL.Path {
	case "/backend-api/wham/accounts/check":
		return "ACCOUNTS_CHECK"
	case "/backend-api/wham/config/bundle":
		return "CONFIG_BUNDLE"
	default:
		return ""
	}
}

type proxyUpstreamFailure string

const (
	proxyUpstreamDNSFailure  proxyUpstreamFailure = "DNS"
	proxyUpstreamDialFailure proxyUpstreamFailure = "DIAL"
)

func (failure proxyUpstreamFailure) Error() string { return "runtime proxy upstream connection failed" }

func providerDiscoveryFailure(err error, tlsFailed bool) string {
	var upstreamFailure proxyUpstreamFailure
	if errors.As(err, &upstreamFailure) {
		switch upstreamFailure {
		case proxyUpstreamDNSFailure, proxyUpstreamDialFailure:
			return string(upstreamFailure)
		}
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELLED"
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return "TIMEOUT"
	}
	if tlsFailed {
		return "TLS"
	}
	return "UNKNOWN"
}

func providerDiscoveryStatusClass(status int) string {
	switch {
	case status >= 100 && status < 200:
		return "1XX"
	case status >= 200 && status < 300:
		return "2XX"
	case status >= 300 && status < 400:
		return "3XX"
	case status >= 400 && status < 500:
		return "4XX"
	case status >= 500 && status < 600:
		return "5XX"
	default:
		return "UNKNOWN"
	}
}

func providerDiscoveryBodyFailure(err error) string {
	if errors.Is(err, context.Canceled) {
		return "CANCELLED"
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeout(err) {
		return "TIMEOUT"
	}
	return "IO"
}

func proxyResponseNeedsChunked(response *http.Response, method string) bool {
	return response.ContentLength < 0 && response.Body != nil && response.Body != http.NoBody && method != http.MethodHead &&
		response.StatusCode >= 200 && response.StatusCode != http.StatusNoContent &&
		response.StatusCode != http.StatusResetContent && response.StatusCode != http.StatusNotModified
}

func (server *Server) proxyTLS(client net.Conn, reader *bufio.Reader, target connect.Target, access runtimecontract.RuntimeProxyAccess, limits policy.Limits) {
	if reader.Buffered() != 0 || server.interceptCA == nil {
		server.metrics.Connection("rejected", "proxy", "malformed")
		return
	}
	certificate, err := server.interceptCA.certificateFor(target.Hostname)
	if err != nil {
		server.metrics.Connection("failed", "proxy", "certificate")
		return
	}
	connection := tls.Server(client, &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{certificate},
		NextProtos:   []string{"http/1.1"},
	})
	if err := connection.SetDeadline(time.Now().Add(duration(limits.ClientHelloTimeoutMilliseconds))); err != nil || connection.Handshake() != nil {
		server.metrics.Connection("rejected", "proxy", "tls")
		return
	}
	state := connection.ConnectionState()
	if !strings.EqualFold(strings.TrimSuffix(state.ServerName, "."), target.Hostname) {
		server.metrics.Connection("rejected", "proxy", "sni")
		return
	}
	_ = connection.SetDeadline(time.Time{})
	if !server.markEstablished(client) {
		server.metrics.Connection("rejected", "proxy", "not_ready")
		return
	}
	if server.draining.Load() || connection.SetReadDeadline(time.Now().Add(duration(limits.IdleTimeoutMilliseconds))) != nil {
		return
	}
	requests := bufio.NewReaderSize(&boundedProxyHeaderReader{source: connection, maximum: limits.MaximumHeaderBytes}, limits.MaximumHeaderBytes)
	request, err := http.ReadRequest(requests)
	if err != nil {
		if !errors.Is(err, io.EOF) && !isTimeout(err) {
			server.metrics.Connection("rejected", "proxy", "request")
		}
		return
	}
	route := providerDiscoveryRoute(request, target, access)
	responsesDiagnostic := newProviderResponsesDiagnostic(request, target, access)
	if !proxyRequestAllowed(request, target, access) {
		responsesDiagnostic.policy(false, providerResponsesPolicyFailure(request, target, access))
		if route != "" {
			log.Printf(runtimeProxyProviderDiscoveryLog, route, "POLICY", "DENIED", "NONE", "NONE")
		}
		_ = request.Body.Close()
		writeProxyError(connection, http.StatusForbidden, limits)
		server.metrics.Connection("rejected", "proxy", "policy")
		return
	}
	upstream := upstreamRequest(request, target)
	var tlsFailed atomic.Bool
	if route != "" {
		log.Printf(runtimeProxyProviderDiscoveryLog, route, "POLICY", "ALLOWED", "NONE", "NONE")
	}
	responsesDiagnostic.policy(true, "NONE")
	if route != "" || responsesDiagnostic.enabled() {
		upstream = upstream.WithContext(httptrace.WithClientTrace(upstream.Context(), &httptrace.ClientTrace{
			TLSHandshakeDone: func(_ tls.ConnectionState, err error) { tlsFailed.Store(err != nil) },
		}))
	}
	websocket := hasWebSocketUpgrade(request.Header)
	transport := server.proxyTransport(target, limits)
	// RFC 6455 Upgrade требует HTTP/1.1, а не HTTP/2 extended CONNECT.
	transport.ForceAttemptHTTP2 = !websocket
	defer transport.CloseIdleConnections()
	if websocket {
		upstream.Header.Set("Connection", "Upgrade")
		upstream.Header.Set("Upgrade", "websocket")
	}
	response, roundTripErr := transport.RoundTrip(upstream)
	if roundTripErr != nil {
		responsesDiagnostic.upstream(0, roundTripErr, tlsFailed.Load())
		if route != "" {
			log.Printf(runtimeProxyProviderDiscoveryLog, route, "UPSTREAM", "FAILED", "NONE", providerDiscoveryFailure(roundTripErr, tlsFailed.Load()))
		}
		_ = request.Body.Close()
		writeProxyError(connection, http.StatusBadGateway, limits)
		server.metrics.Connection("failed", "proxy", "upstream")
		return
	}
	responsesDiagnostic.upstream(response.StatusCode, nil, false)
	if route != "" {
		log.Printf(runtimeProxyProviderDiscoveryLog, route, "UPSTREAM", "RESPONSE", providerDiscoveryStatusClass(response.StatusCode), "NONE")
	}
	if response.StatusCode == http.StatusSwitchingProtocols {
		if !websocket || !validWebSocketResponse(response, request) {
			responsesDiagnostic.upgrade("REJECTED", response.StatusCode)
			_ = response.Body.Close()
			writeProxyError(connection, http.StatusBadGateway, limits)
			server.metrics.Connection("rejected", "proxy", "policy")
			return
		}
		server.forwardWebSocket(connection, requests, response, limits, responsesDiagnostic)
		return
	}
	if websocket {
		responsesDiagnostic.upgrade("REJECTED", response.StatusCode)
	}
	stripHopByHop(response.Header)
	// Downstream TLS допускает только HTTP/1.1, независимо от upstream ALPN.
	response.Proto, response.ProtoMajor, response.ProtoMinor = "HTTP/1.1", 1, 1
	if proxyResponseNeedsChunked(response, request.Method) {
		response.TransferEncoding = []string{"chunked"}
	}
	response.Header.Set("Connection", "close")
	response.Close = true
	var capture *discoveryBodyCapture
	if route == "ACCOUNTS_CHECK" {
		capture = &discoveryBodyCapture{body: make([]byte, 0, maximumDiscoveryBodyBytes+1)}
		defer capture.clear()
		response.Body = struct {
			io.Reader
			io.Closer
		}{io.TeeReader(response.Body, capture), response.Body}
	}
	writeErr := connection.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds)))
	if writeErr == nil {
		writeErr = response.Write(connection)
	}
	if writeErr != nil {
		responsesDiagnostic.body(response.StatusCode, writeErr)
		if capture != nil {
			capture.clear()
		}
		if route != "" {
			log.Printf(runtimeProxyProviderDiscoveryLog, route, "UPSTREAM_BODY", "FAILED", providerDiscoveryStatusClass(response.StatusCode), providerDiscoveryBodyFailure(writeErr))
		}
		_ = response.Body.Close()
		server.metrics.Connection("failed", "proxy", "io")
		return
	}
	var encoding, contentType, shape string
	if capture != nil {
		encoding, contentType = providerDiscoveryEncoding(response), providerDiscoveryContentType(response)
		shape = "ENCODED_NOT_CHECKED"
		if len(capture.body) > maximumDiscoveryBodyBytes {
			shape = "BOUND_EXCEEDED"
		} else if encoding == "IDENTITY" {
			shape = classifyAccountsCheckShape(capture.body)
		}
		capture.clear()
	}
	_ = response.Body.Close()
	responsesDiagnostic.body(response.StatusCode, nil)
	if route != "" {
		log.Printf(runtimeProxyProviderDiscoveryLog, route, "UPSTREAM_BODY", "COMPLETED", providerDiscoveryStatusClass(response.StatusCode), "NONE")
	}
	if capture != nil {
		log.Printf(runtimeProxyProviderDiscoveryShapeLog, encoding, contentType, shape)
	}
	server.metrics.Connection("completed", "proxy", "none")
}

type boundedProxyHeaderReader struct {
	source   io.Reader
	maximum  int
	consumed int
	matched  int
}

func (reader *boundedProxyHeaderReader) Read(buffer []byte) (int, error) {
	read, err := reader.source.Read(buffer)
	for index, value := range buffer[:read] {
		reader.consumed++
		switch reader.matched {
		case 0, 2:
			if value == '\r' {
				reader.matched++
			} else {
				reader.matched = 0
			}
		case 1, 3:
			if value == '\n' {
				reader.matched++
			} else {
				reader.matched = 0
			}
		}
		if reader.matched == 4 {
			return read, err
		}
		if reader.consumed > reader.maximum {
			return index + 1, errProxyHeaderTooLarge
		}
	}
	return read, err
}

func proxyRequestAllowed(request *http.Request, target connect.Target, access runtimecontract.RuntimeProxyAccess) bool {
	if request == nil || request.Method == http.MethodConnect || request.URL == nil || request.URL.IsAbs() || request.RequestURI == "" {
		return false
	}
	host := request.Host
	if host == "" {
		host = request.URL.Host
	}
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil {
		if parsedPort != "443" {
			return false
		}
		host = parsedHost
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host != target.Hostname || request.URL.RawPath != "" {
		return false
	}
	if hasWebSocketUpgrade(request.Header) {
		return access.ProviderAccess && runtimecontract.RuntimeProviderAllowsWebSocket(host, request.URL.Path, request.Method) && validWebSocketRequest(request)
	}
	return access.ProviderAccess && runtimecontract.RuntimeProviderAllowsRequest(host, request.URL.Path, request.Method) ||
		runtimecontract.RuntimeWebAccessAllowsRequest(access.WebAccess, host, request.Method)
}

func upstreamRequest(request *http.Request, target connect.Target) *http.Request {
	result := request.Clone(request.Context())
	result.RequestURI = ""
	result.URL.Scheme = "https"
	result.URL.Host = target.Hostname
	result.Host = target.Hostname
	result.Header = request.Header.Clone()
	stripHopByHop(result.Header)
	result.Header.Del("Proxy-Authorization")
	result.Header.Del("Proxy-Connection")
	return result
}

func stripHopByHop(header http.Header) {
	for _, value := range header.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			header.Del(textproto.CanonicalMIMEHeaderKey(strings.TrimSpace(name)))
		}
	}
	for _, name := range []string{"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Proxy-Connection", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		header.Del(name)
	}
}

func (server *Server) proxyTransport(target connect.Target, limits policy.Limits) *http.Transport {
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			snapshot, err := server.resolver.Resolve(ctx, target.Hostname)
			if err != nil || dnsresolver.ValidateAddresses(snapshot.Addresses) != nil || !time.Now().Before(snapshot.ExpiresAt) {
				return nil, proxyUpstreamDNSFailure
			}
			connection, err := server.dial(snapshot, target.Port, duration(limits.DialTimeoutMilliseconds))
			if err != nil {
				return nil, proxyUpstreamDialFailure
			}
			return connection, nil
		},
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12, ServerName: target.Hostname, RootCAs: server.upstreamRoots},
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: duration(limits.HeaderTimeoutMilliseconds),
		IdleConnTimeout:       duration(limits.IdleTimeoutMilliseconds),
		MaxIdleConns:          2,
		MaxIdleConnsPerHost:   2,
	}
}

func writeProxyError(connection net.Conn, status int, limits policy.Limits) {
	_ = connection.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds)))
	response := &http.Response{StatusCode: status, Status: http.StatusText(status), ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}, "Cache-Control": []string{"no-store"}, "Connection": []string{"close"}},
		Body:   io.NopCloser(strings.NewReader(http.StatusText(status) + "\n")), Close: true}
	response.ContentLength = int64(len(http.StatusText(status)) + 1)
	_ = response.Write(connection)
}

func isTimeout(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Timeout()
}
