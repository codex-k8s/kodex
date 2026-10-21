package gateway

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/connect"
)

// encodedWebAccess grants read-only HTTPS access to one exact host for path tests.
// encodedWebAccess даёт доступ на чтение по HTTPS к одному точному узлу для проверок пути.
func encodedWebAccess(host string) runtimecontract.RuntimeProxyAccess {
	return runtimecontract.RuntimeProxyAccess{WebAccess: runtimecontract.RuntimeWebAccess{
		Mode:  runtimecontract.RuntimeWebAccessAllowlistReadOnly,
		Rules: []runtimecontract.RuntimeWebAccessRule{{DomainPattern: host, Protocol: "HTTPS", Port: 443, HTTPMethods: []string{"GET", "HEAD"}}},
	}}
}

// TestProxyWebAccessAllowsValidatedEncodedPaths preserves approved escaping on the upstream request.
// TestProxyWebAccessAllowsValidatedEncodedPaths сохраняет допустимое кодирование в upstream-запросе.
func TestProxyWebAccessAllowsValidatedEncodedPaths(t *testing.T) {
	for _, host := range []string{"registry.npmjs.org", "packages.example.com"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, path := range []string{
				"/@asyncapi%2fparser", "/@asyncapi%2Fparser", "/@types%2fnode", "/@vue-flow%2fcore",
				"/@babel%2Fruntime/7.0.0", "/space%20name", "/literal%25", "/unicode/%D0%BF%D1%83%D1%82%D1%8C",
			} {
				t.Run(host+"/"+method+path, func(t *testing.T) {
					request, err := http.ReadRequest(bufio.NewReader(strings.NewReader(method + " " + path + " HTTP/1.1\r\nHost: " + host + "\r\n\r\n")))
					if err != nil {
						t.Fatal(err)
					}
					defer request.Body.Close()
					if !proxyRequestAllowed(request, connect.Target{Hostname: host, Port: 443}, encodedWebAccess(host)) {
						t.Fatal("authorized encoded web path was rejected")
					}
					upstream := upstreamRequest(request, connect.Target{Hostname: host, Port: 443})
					if upstream.URL.EscapedPath() != path || upstream.URL.RawPath != request.URL.RawPath || upstream.RequestURI != "" {
						t.Fatal("upstream changed the original escaped path")
					}
				})
			}
		}
	}
}

// TestProxyWebAccessRejectsUnsafeEncodedPaths rejects ambiguous escaping before proxying.
// TestProxyWebAccessRejectsUnsafeEncodedPaths отклоняет неоднозначное кодирование до проксирования.
func TestProxyWebAccessRejectsUnsafeEncodedPaths(t *testing.T) {
	for _, path := range []string{
		"/@scope%252fpackage", "/nested%25252Fpath", "/%250A", "/%255c", "/%00", "/%0A", "/%0d", "/%7F", "/%C2%85",
		"/%5Cpath", "/%5cpath", "/a/%2e%2e/private", "/a/%2E/private", "/a/../private", "/%2fother.example/path", "/%FF",
	} {
		t.Run(path, func(t *testing.T) {
			parsed, err := url.ParseRequestURI(path)
			if err != nil {
				t.Fatal(err)
			}
			request := &http.Request{Method: http.MethodGet, Host: "registry.npmjs.org", URL: parsed, RequestURI: path, Header: make(http.Header)}
			if proxyRequestAllowed(request, connect.Target{Hostname: request.Host, Port: 443}, encodedWebAccess(request.Host)) {
				t.Fatal("unsafe encoded path crossed the web policy boundary")
			}
		})
	}
	for _, raw := range []string{"/@scope%2gpackage", "/@scope%2", "/@scope%", "/@scope%2fother", "/@scope%2fpackage?query", "/@scope%2fpackage#fragment"} {
		t.Run("inconsistent/"+raw, func(t *testing.T) {
			request := &http.Request{Method: http.MethodGet, Host: "registry.npmjs.org", URL: &url.URL{Path: "/@scope/package", RawPath: raw}, RequestURI: raw, Header: make(http.Header)}
			if proxyRequestAllowed(request, connect.Target{Hostname: request.Host, Port: 443}, encodedWebAccess(request.Host)) {
				t.Fatal("malformed or inconsistent RawPath crossed the web policy boundary")
			}
		})
	}
}

// TestProxyEncodedPathsPreserveAuthorityBoundaries ensures escaped paths do not widen host or method grants.
// TestProxyEncodedPathsPreserveAuthorityBoundaries исключает расширение прав на узел или метод через кодированный путь.
func TestProxyEncodedPathsPreserveAuthorityBoundaries(t *testing.T) {
	for _, name := range []string{"no_grant", "foreign_host", "foreign_port", "POST", "PUT", "DELETE", "CONNECT", "websocket", "absolute", "opaque", "url_host", "userinfo", "fragment", "provider_only", "provider_websocket"} {
		t.Run(name, func(t *testing.T) {
			parsed, _ := url.ParseRequestURI("/@asyncapi%2fparser")
			request := &http.Request{Method: http.MethodGet, Host: "registry.npmjs.org", URL: parsed, RequestURI: parsed.RawPath, Header: make(http.Header)}
			target := connect.Target{Hostname: request.Host, Port: 443}
			access := encodedWebAccess(request.Host)
			switch name {
			case "no_grant":
				access = runtimecontract.RuntimeProxyAccess{}
			case "foreign_host":
				request.Host = "other.example"
			case "foreign_port":
				request.Host += ":8443"
			case "POST", "PUT", "DELETE", "CONNECT":
				request.Method = name
			case "websocket", "provider_websocket":
				request.Header.Set("Connection", "Upgrade")
				request.Header.Set("Upgrade", "websocket")
			case "absolute":
				request.URL.Scheme = "https"
			case "opaque":
				request.URL.Opaque = "//other.example/path"
			case "url_host":
				request.URL.Host = "other.example"
			case "userinfo":
				request.URL.User = url.User("synthetic")
			case "fragment":
				request.URL.Fragment = "synthetic"
			}
			if name == "provider_only" || name == "provider_websocket" {
				request.Host, target.Hostname = "api.openai.com", "api.openai.com"
				request.URL, _ = url.ParseRequestURI("/v1/%72esponses")
				request.RequestURI = request.URL.RawPath
				access = runtimecontract.RuntimeProxyAccess{ProviderAccess: true}
				if name == "provider_only" {
					request.Method = http.MethodPost
				}
			}
			if proxyRequestAllowed(request, target, access) {
				t.Fatal("encoded path expanded request authority")
			}
		})
	}
}

// TestAuthenticatedProxyPreservesEncodedPathOnUpstreamWire checks the exact escaped path after authorization.
// TestAuthenticatedProxyPreservesEncodedPathOnUpstreamWire проверяет точный кодированный путь после авторизации.
func TestAuthenticatedProxyPreservesEncodedPathOnUpstreamWire(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			_, active, _, _, grant := authenticatedChallengeFixture(t)
			proxyCA, proxyRoots := proxyAuthorityFixture(t)
			upstreamCertificate, upstreamRoots := serverCertificateFixture(t, "example.com")
			resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute)}}
			dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
			server, err := NewAuthenticated(t.Context(), "unused", active, resolver, dialer, readyStub(true), newTestMetrics(t), proxyCA)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(server.cancel)
			server.upstreamRoots = upstreamRoots
			serverSide, clientSide := net.Pipe()
			defer clientSide.Close()
			done := make(chan struct{})
			go func() { defer close(done); defer serverSide.Close(); server.handle(serverSide) }()
			reader := encodedPathAuthenticatedConnect(t, clientSide, proxyRoots, grant)
			const path = "/@asyncapi%2fparser?version=3.6.3"
			upstreamResult := make(chan error, 1)
			go func() {
				var peer net.Conn
				select {
				case peer = <-dialer.peers:
				case <-t.Context().Done():
					return
				}
				defer peer.Close()
				secured := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{upstreamCertificate}})
				request, err := http.ReadRequest(bufio.NewReader(secured))
				if err != nil {
					upstreamResult <- err
					return
				}
				defer request.Body.Close()
				if request.RequestURI != path || request.URL.RawPath != "/@asyncapi%2fparser" || request.URL.Path != "/@asyncapi/parser" || request.Method != method || request.Host != "example.com" || request.Header.Get("Proxy-Authorization") != "" {
					upstreamResult <- fmt.Errorf("upstream encoded request was changed")
					return
				}
				response := &http.Response{StatusCode: http.StatusOK, ProtoMajor: 1, ProtoMinor: 1, Header: make(http.Header), Body: http.NoBody, ContentLength: 0, Close: true}
				upstreamResult <- response.Write(secured)
			}()
			request, err := http.NewRequest(method, "https://example.com"+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Close = true
			if err := request.Write(reader.conn); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(reader.reader, request)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("encoded proxy request failed: status=%d", response.StatusCode)
			}
			if err := <-upstreamResult; err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("encoded proxy handler did not join")
			}
		})
	}
}

// TestAuthenticatedProxyRejectsUnsafeEncodedPathBeforeDNSAndDial enforces rejection before any external effect.
// TestAuthenticatedProxyRejectsUnsafeEncodedPathBeforeDNSAndDial требует отказа до любых внешних действий.
func TestAuthenticatedProxyRejectsUnsafeEncodedPathBeforeDNSAndDial(t *testing.T) {
	for _, test := range []struct{ method, path string }{
		{http.MethodGet, "/@scope%252fpackage"},
		{http.MethodHead, "/%0A"},
		{http.MethodGet, "/%5Cpath"},
		{http.MethodGet, "/a/%2e%2e/private"},
		{http.MethodGet, "/%2Fother.example/path"},
		{http.MethodPost, "/@asyncapi%2fparser"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			server, _, resolver, dialer, grant := authenticatedChallengeFixture(t)
			proxyCA, proxyRoots := proxyAuthorityFixture(t)
			server.interceptCA = proxyCA
			serverSide, clientSide := net.Pipe()
			defer clientSide.Close()
			done := make(chan struct{})
			go func() { defer close(done); defer serverSide.Close(); server.handle(serverSide) }()
			reader := encodedPathAuthenticatedConnect(t, clientSide, proxyRoots, grant)
			request, err := http.NewRequest(test.method, "https://example.com"+test.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := request.Write(reader.conn); err != nil {
				t.Fatal(err)
			}
			// Читаем весь существующий proxy error envelope, включая body при HEAD.
			response, err := http.ReadResponse(reader.reader, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.ReadAll(response.Body)
			_ = response.Body.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("rejected encoded proxy handler did not join")
			}
			if response.StatusCode != http.StatusForbidden || resolver.calls != 0 || len(dialer.targets) != 0 {
				t.Fatal("rejected encoded request crossed the DNS or dial boundary")
			}
		})
	}
}

// encodedPathAuthenticatedConnect establishes the signed test tunnel with pinned certificate roots.
// encodedPathAuthenticatedConnect создаёт подписанный тестовый туннель с закреплёнными корневыми сертификатами.
func encodedPathAuthenticatedConnect(t *testing.T, connection net.Conn, roots *x509.CertPool, grant string) securedReader {
	t.Helper()
	if _, err := io.WriteString(connection, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n"+proxyAuthenticationHeader(grant)+"\r\n"); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("signed CONNECT failed: %v", err)
	}
	secured := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "example.com", NextProtos: []string{"http/1.1"}})
	if err := secured.HandshakeContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return securedReader{conn: secured, reader: bufio.NewReader(secured)}
}
