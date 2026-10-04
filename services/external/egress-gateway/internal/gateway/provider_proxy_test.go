package gateway

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/runtimepolicy"
)

func providerProxyFixture(t *testing.T, host string, access runtimecontract.RuntimeWebAccess, upstreamRoots *x509.CertPool) (*Server, securedReader, *fakeResolver, *fakeDialer, <-chan struct{}) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "deploy/k8s/base/egress-gateway/policy.json")
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := policy.LoadFile(path, "2026-09-05.1", digest)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("synthetic", 8))
	active, err := runtimepolicy.New(base, key)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := runtimecontract.SignRuntimeWebAccessGrant(key, "synthetic-execution", strings.Repeat("a", 64), access)
	if err != nil {
		t.Fatal(err)
	}
	proxyCA, roots := proxyAuthorityFixture(t)
	resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute)}}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, err := NewAuthenticated(t.Context(), "unused", active, resolver, dialer, readyStub(true), newTestMetrics(t), proxyCA)
	if err != nil {
		t.Fatal(err)
	}
	server.upstreamRoots = upstreamRoots
	serverSide, clientSide := net.Pipe()
	t.Cleanup(func() { _ = clientSide.Close(); server.cancel() })
	_ = clientSide.SetDeadline(time.Now().Add(5 * time.Second))
	done := make(chan struct{})
	go func() { defer close(done); defer serverSide.Close(); server.handle(serverSide) }()
	credential := base64.StdEncoding.EncodeToString([]byte("kodex:" + grant))
	_, err = io.WriteString(clientSide, "CONNECT "+host+":443 HTTP/1.1\r\nHost: "+host+":443\r\nProxy-Authorization: Basic "+credential+"\r\n\r\n")
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(clientSide), nil)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal("synthetic CONNECT failed")
	}
	secured := tls.Client(clientSide, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: host, NextProtos: []string{"http/1.1"}})
	if err := secured.HandshakeContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return server, securedReader{conn: secured, reader: bufio.NewReader(secured)}, resolver, dialer, done
}

func TestProviderPOSTIsIndependentOfUserReadOnlyAndPreservesSSE(t *testing.T) {
	logs := captureProviderResponsesLogs(t)
	access := runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessAllowlistReadOnly, Rules: []runtimecontract.RuntimeWebAccessRule{{DomainPattern: "example.org", Protocol: "HTTPS", Port: 443, HTTPMethods: []string{"GET"}}}}
	certificate, roots := serverCertificateFixture(t, "api.openai.com")
	_, client, _, dialer, done := providerProxyFixture(t, "api.openai.com", access, roots)
	upstreamResult := make(chan error, 1)
	firstReceived := make(chan struct{})
	go func() {
		peer := <-dialer.peers
		defer peer.Close()
		upstream := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
		request, err := http.ReadRequest(bufio.NewReader(upstream))
		if err != nil {
			upstreamResult <- err
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil || request.Method != "POST" || request.URL.Path != "/v1/responses" || string(body) != "synthetic-payload" || request.Header.Get("Authorization") != "Bearer synthetic" || request.Header.Get("Proxy-Authorization") != "" {
			upstreamResult <- io.ErrUnexpectedEOF
			return
		}
		_, err = io.WriteString(upstream, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nConnection: close\r\n\r\ndata: first\n\n")
		if err != nil {
			upstreamResult <- err
			return
		}
		select {
		case <-firstReceived:
		case <-t.Context().Done():
			upstreamResult <- t.Context().Err()
			return
		case <-time.After(time.Second):
			upstreamResult <- io.ErrNoProgress
			return
		}
		_, err = io.WriteString(upstream, "data: completed\n\n")
		upstreamResult <- err
	}()
	request, _ := http.NewRequest("POST", "https://api.openai.com/v1/responses", strings.NewReader("synthetic-payload"))
	request.Header.Set("Authorization", "Bearer synthetic")
	if err := request.Write(client.conn); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(client.reader, request)
	if err != nil {
		t.Fatal(err)
	}
	first := make([]byte, len("data: first\n\n"))
	if _, err := io.ReadFull(response.Body, first); err != nil || string(first) != "data: first\n\n" {
		t.Fatal("provider SSE was buffered until completion")
	}
	close(firstReceived)
	contents, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 || string(contents) != "data: completed\n\n" {
		t.Fatal("provider streaming response changed")
	}
	if err := <-upstreamResult; err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("provider proxy did not join")
	}
	assertProviderResponsesEvents(t, logs.String(), "HTTP", "event=POLICY outcome=ALLOWED", "event=UPSTREAM outcome=RESPONSE status_class=2XX http_status=200", "event=UPSTREAM_BODY outcome=COMPLETED status_class=2XX http_status=200")
}

func TestProviderProxyRejectsForeignTrustAndInvalidUpstreamUpgrade(t *testing.T) {
	for _, boundary := range []string{"foreignCA", "foreignAccept"} {
		t.Run(boundary, func(t *testing.T) {
			logs := captureProviderResponsesLogs(t)
			certificate, roots := serverCertificateFixture(t, "api.openai.com")
			if boundary == "foreignCA" {
				_, roots = serverCertificateFixture(t, "other.example")
			}
			_, client, _, dialer, done := providerProxyFixture(t, "api.openai.com", runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessNone, Rules: []runtimecontract.RuntimeWebAccessRule{}}, roots)
			upstreamDone := make(chan struct{})
			go func() {
				defer close(upstreamDone)
				peer := <-dialer.peers
				defer peer.Close()
				upstream := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}, NextProtos: []string{"h2", "http/1.1"}})
				if _, err := http.ReadRequest(bufio.NewReader(upstream)); err != nil {
					return
				}
				_, _ = io.WriteString(upstream, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: foreign\r\n\r\n")
			}()
			request := syntheticUpgradeRequest(t, "api.openai.com")
			if err := request.Write(client.conn); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(client.reader, request)
			if err != nil || response.StatusCode != 502 {
				t.Fatal("invalid provider trust or upgrade was accepted")
			}
			_, _ = io.ReadAll(response.Body)
			_ = response.Body.Close()
			for _, completed := range []<-chan struct{}{done, upstreamDone} {
				select {
				case <-completed:
				case <-time.After(time.Second):
					t.Fatal("rejected upstream did not join")
				}
			}
			if boundary == "foreignCA" {
				assertProviderResponsesEvents(t, logs.String(), "WSS", "event=UPSTREAM outcome=FAILED status_class=NONE http_status=NONE failure=TLS")
			} else {
				assertProviderResponsesEvents(t, logs.String(), "WSS", "event=UPGRADE outcome=REJECTED status_class=1XX http_status=101")
			}
		})
	}
}

func TestProviderWebSocketPreservesExactHandshakeAndBidirectionalFrames(t *testing.T) {
	for _, host := range []string{"api.openai.com", "chatgpt.com"} {
		t.Run(host, func(t *testing.T) {
			logs := captureProviderResponsesLogs(t)
			certificate, roots := serverCertificateFixture(t, host)
			_, client, _, dialer, done := providerProxyFixture(t, host, runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessNone, Rules: []runtimecontract.RuntimeWebAccessRule{}}, roots)
			path := "/v1/responses"
			if host == "chatgpt.com" {
				path = "/backend-api/codex/responses"
			}
			upstreamResult := make(chan error, 1)
			go func() {
				peer := <-dialer.peers
				defer peer.Close()
				upstream := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
				reader := bufio.NewReader(upstream)
				request, err := http.ReadRequest(reader)
				// HTTP/1.1 допустим и без ALPN. Предложенный сервером h2 не
				// должен выбираться для RFC6455 Upgrade.
				if err != nil || upstream.ConnectionState().NegotiatedProtocol == "h2" || request.Proto != "HTTP/1.1" || request.Method != "GET" || request.URL.Path != path || request.Header.Get("Upgrade") != "websocket" || request.Header.Get("Connection") != "Upgrade" || request.Header.Get("Authorization") != "Bearer synthetic" || request.Header.Get("Proxy-Authorization") != "" {
					upstreamResult <- fmt.Errorf("synthetic upstream request failed: protocol=%s error=%v", upstream.ConnectionState().NegotiatedProtocol, err)
					return
				}
				_, err = io.WriteString(upstream, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\n\r\n")
				if err != nil {
					upstreamResult <- err
					return
				}
				frame := make([]byte, 8)
				_, err = io.ReadFull(reader, frame)
				if err != nil || string(frame) != string([]byte{0x81, 0x82, 1, 2, 3, 4, 'o' ^ 1, 'k' ^ 2}) {
					upstreamResult <- io.ErrUnexpectedEOF
					return
				}
				_, err = upstream.Write([]byte{0x81, 2, 'o', 'k'})
				upstreamResult <- err
			}()
			request, _ := http.NewRequest("GET", "https://"+host+path, nil)
			request.Header.Set("Authorization", "Bearer synthetic")
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			request.Header.Set("Sec-WebSocket-Version", "13")
			request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
			if err := request.Write(client.conn); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(client.reader, request)
			if err != nil || response.StatusCode != 101 {
				select {
				case upstreamErr := <-upstreamResult:
					t.Fatalf("provider upgrade was not preserved: %v", upstreamErr)
				default:
					t.Fatal("provider upgrade was not preserved")
				}
			}
			if _, err := client.conn.Write([]byte{0x81, 0x82, 1, 2, 3, 4, 'o' ^ 1, 'k' ^ 2}); err != nil {
				t.Fatal(err)
			}
			frame := make([]byte, 4)
			if _, err := io.ReadFull(client.reader, frame); err != nil || string(frame) != string([]byte{0x81, 2, 'o', 'k'}) {
				t.Fatal("provider frames changed")
			}
			if err := <-upstreamResult; err != nil {
				t.Fatal(err)
			}
			_ = client.conn.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("websocket pump did not join")
			}
			assertProviderResponsesEvents(t, logs.String(), "WSS", "event=POLICY outcome=ALLOWED", "event=UPSTREAM outcome=RESPONSE status_class=1XX http_status=101", "event=UPGRADE outcome=ACCEPTED status_class=1XX http_status=101", "event=PUMP outcome=CLOSED")
		})
	}
}

func TestProviderProxyRejectsForeignPathsAndMalformedUpgradesBeforeDial(t *testing.T) {
	for _, mutation := range []string{"foreignpath", "wrongmethod", "protocol", "extensions", "badkey", "foreignupgrade"} {
		t.Run(mutation, func(t *testing.T) {
			logs := captureProviderResponsesLogs(t)
			_, client, resolver, dialer, done := providerProxyFixture(t, "api.openai.com", runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessNone, Rules: []runtimecontract.RuntimeWebAccessRule{}}, nil)
			request, _ := http.NewRequest("GET", "https://api.openai.com/v1/responses", nil)
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			request.Header.Set("Sec-WebSocket-Version", "13")
			request.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
			switch mutation {
			case "foreignpath":
				request.URL.Path = "/v1/other"
			case "wrongmethod":
				request.Method = "DELETE"
			case "protocol":
				request.Header.Set("Sec-WebSocket-Protocol", "foreign")
			case "extensions":
				request.Header.Set("Sec-WebSocket-Extensions", "permessage-deflate")
			case "badkey":
				request.Header.Set("Sec-WebSocket-Key", "invalid")
			case "foreignupgrade":
				request.Header.Set("Upgrade", "h2c")
			}
			if err := request.Write(client.conn); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(client.reader, request)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != 403 || resolver.calls != 0 || len(dialer.targets) != 0 {
				t.Fatal("invalid provider route crossed the DNS boundary")
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("rejected provider proxy did not join")
			}
			if mutation == "foreignpath" {
				if logs.Len() != 0 {
					t.Fatal("unknown model route acquired diagnostics")
				}
			} else {
				reason := map[string]string{"wrongmethod": "METHOD_PATH", "protocol": "WS_SUBPROTOCOL", "extensions": "WS_EXTENSIONS", "badkey": "WS_HANDSHAKE", "foreignupgrade": "WS_HANDSHAKE"}[mutation]
				assertProviderResponsesEvents(t, logs.String(), "WSS", "event=POLICY outcome=DENIED status_class=NONE http_status=NONE failure="+reason)
			}
		})
	}
}

func TestProviderResponsesRejectedUpgradePreservesHTTPResponse(t *testing.T) {
	logs := captureProviderResponsesLogs(t)
	certificate, roots := serverCertificateFixture(t, "api.openai.com")
	_, client, _, dialer, done := providerProxyFixture(t, "api.openai.com", runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessNone, Rules: []runtimecontract.RuntimeWebAccessRule{}}, roots)
	upstreamDone := make(chan struct{})
	go func() {
		defer close(upstreamDone)
		peer := <-dialer.peers
		defer peer.Close()
		upstream := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
		if _, err := http.ReadRequest(bufio.NewReader(upstream)); err != nil {
			return
		}
		_, _ = io.WriteString(upstream, "HTTP/1.1 401 Unauthorized\r\nContent-Length: 9\r\nX-Private-Fixture: synthetic\r\n\r\nsynthetic")
	}()
	request := syntheticUpgradeRequest(t, "api.openai.com")
	if err := request.Write(client.conn); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(client.reader, request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 401 || string(body) != "synthetic" || response.Header.Get("X-Private-Fixture") != "synthetic" {
		t.Fatal("rejected Upgrade changed upstream HTTP response")
	}
	for _, completed := range []<-chan struct{}{done, upstreamDone} {
		select {
		case <-completed:
		case <-time.After(time.Second):
			t.Fatal("rejected Upgrade did not join")
		}
	}
	assertProviderResponsesEvents(t, logs.String(), "WSS", "event=UPSTREAM outcome=RESPONSE status_class=4XX http_status=401", "event=UPGRADE outcome=REJECTED status_class=4XX http_status=401", "event=UPSTREAM_BODY outcome=COMPLETED status_class=4XX http_status=401")
}
