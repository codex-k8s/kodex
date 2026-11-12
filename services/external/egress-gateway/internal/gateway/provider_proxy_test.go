package gateway

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
	"github.com/codex-k8s/kodex/services/external/egress-gateway/internal/runtimepolicy"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/dnsresolver"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// providerProxyFixture establishes an authenticated CONNECT tunnel with synthetic downstream and upstream TLS roots.
// providerProxyFixture создаёт аутентифицированный CONNECT-туннель с тестовым TLS-доверием клиента и upstream.
func providerProxyFixture(t *testing.T, host string, access runtimecontract.RuntimeWebAccess, upstreamRoots *x509.CertPool) (*Server, securedReader, *fakeResolver, *fakeDialer, <-chan struct{}) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "..", "deploy/k8s/base/egress-gateway/policy.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc policy.Document
	if json.Unmarshal(raw, &doc) != nil {
		t.Fatal("read synthetic policy")
	}
	config := runtimecontract.ModelProviderConfiguration{Schema: runtimecontract.ModelProviderConfigurationSchema, ManagedBy: "git", SourceRevision: strings.Repeat("a", 40), Engine: "opencode", EngineVersion: runtimecontract.OpenCodeVersion, ToolMode: "managed_mcp", ProviderID: "synthetic", API: runtimecontract.ModelAPIResponses, BaseURL: "https://" + host + "/v1", AddressCIDRs: []string{"93.184.216.34/32"}, Models: []runtimecontract.ModelProviderModel{{ID: "synthetic", ContextWindowTokens: 32768, MaximumOutputTokens: 1024}}}
	doc.Spec.ModelProviderCatalog = &runtimecontract.ModelProviderCatalog{Schema: runtimecontract.ModelProviderCatalogSchema, ManagedBy: "git", SourceRevision: config.SourceRevision, DefaultProvider: config.DefinitionKey(), DefaultModel: config.Models[0].ID, Providers: []runtimecontract.ModelProviderConfiguration{config}}
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(t.TempDir(), "policy.json")
	if os.WriteFile(path, raw, 0600) != nil {
		t.Fatal("write synthetic policy")
	}
	digest, err := policy.DigestFile(path)
	if err != nil {
		t.Fatal(err)
	}
	base, err := policy.LoadFile(path, doc.Metadata.Revision, digest)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("synthetic", 8))
	active, err := runtimepolicy.New(base, key)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := runtimecontract.SignRuntimeProxyAccessGrant(key, "synthetic-execution", strings.Repeat("a", 64), access, &config)
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

// TestProviderPOSTIsIndependentOfUserReadOnlyAndPreservesSSE keeps provider POST available and SSE unbuffered.
// TestProviderPOSTIsIndependentOfUserReadOnlyAndPreservesSSE сохраняет POST провайдера и потоковую передачу SSE.
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
