package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

var errProxyHeaderTooLarge = errors.New("runtime proxy request header is too large")

const runtimeProxyProviderDiscoveryDeniedLog = "Runtime proxy rejected provider discovery route: %s"

// Только известные provider paths преобразуются в закрытую диагностику.
// Host, URL/query, headers, body и identity запроса не записываются.
func deniedProviderDiscoveryRoute(request *http.Request, target connect.Target, access runtimecontract.RuntimeProxyAccess) string {
	if !access.ProviderAccess || target.Hostname != "chatgpt.com" || request == nil || request.URL == nil || request.Method != http.MethodGet {
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
	if !proxyRequestAllowed(request, target, access) {
		if route := deniedProviderDiscoveryRoute(request, target, access); route != "" {
			log.Printf(runtimeProxyProviderDiscoveryDeniedLog, route)
		}
		_ = request.Body.Close()
		writeProxyError(connection, http.StatusForbidden, limits)
		server.metrics.Connection("rejected", "proxy", "policy")
		return
	}
	upstream := upstreamRequest(request, target)
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
		_ = request.Body.Close()
		writeProxyError(connection, http.StatusBadGateway, limits)
		server.metrics.Connection("failed", "proxy", "upstream")
		return
	}
	if response.StatusCode == http.StatusSwitchingProtocols {
		if !websocket || !validWebSocketResponse(response, request) {
			_ = response.Body.Close()
			writeProxyError(connection, http.StatusBadGateway, limits)
			server.metrics.Connection("rejected", "proxy", "policy")
			return
		}
		server.forwardWebSocket(connection, requests, response, limits)
		return
	}
	stripHopByHop(response.Header)
	response.Header.Set("Connection", "close")
	response.Close = true
	if err := connection.SetWriteDeadline(time.Now().Add(duration(limits.WriteTimeoutMilliseconds))); err != nil || response.Write(connection) != nil {
		_ = response.Body.Close()
		server.metrics.Connection("failed", "proxy", "io")
		return
	}
	_ = response.Body.Close()
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
				return nil, errors.New("runtime proxy DNS resolution failed")
			}
			return server.dial(snapshot, target.Port, duration(limits.DialTimeoutMilliseconds))
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
