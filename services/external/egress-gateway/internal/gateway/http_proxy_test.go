package gateway

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"io"
	"math/big"
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
	internalpolicy "github.com/codex-k8s/kodex/services/external/egress-gateway/internal/policy"
)

func TestProviderAccountDiscoveryKeepsExactProxyBoundary(t *testing.T) {
	const discovery = "/backend-api/wham/accounts/check"
	for _, test := range []struct {
		name, host, path, method string
		provider, allowed        bool
	}{
		{name: "exact provider GET", host: "chatgpt.com", path: discovery, method: http.MethodGet, provider: true, allowed: true},
		{name: "no provider authority", host: "chatgpt.com", path: discovery, method: http.MethodGet},
		{name: "write rejected", host: "chatgpt.com", path: discovery, method: http.MethodPost, provider: true},
		{name: "other path rejected", host: "chatgpt.com", path: discovery + "/other", method: http.MethodGet, provider: true},
		{name: "other host rejected", host: "api.openai.com", path: discovery, method: http.MethodGet, provider: true},
		{name: "encoded path rejected", host: "chatgpt.com", path: "/backend-api/wham/accounts/%63heck", method: http.MethodGet, provider: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := url.Parse(test.path)
			if err != nil {
				t.Fatal(err)
			}
			request := &http.Request{Method: test.method, Host: test.host, URL: parsed, RequestURI: test.path, Header: make(http.Header)}
			access := runtimecontract.RuntimeProxyAccess{ProviderAccess: test.provider, WebAccess: runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessNone}}
			if got := proxyRequestAllowed(request, connect.Target{Hostname: test.host, Port: 443}, access); got != test.allowed {
				t.Fatalf("account discovery proxy eligibility=%t, want %t", got, test.allowed)
			}
			request.Header.Set("Connection", "Upgrade")
			request.Header.Set("Upgrade", "websocket")
			if proxyRequestAllowed(request, connect.Target{Hostname: test.host, Port: 443}, access) {
				t.Fatal("account discovery acquired WebSocket authority")
			}
		})
	}
}

func TestProviderDiscoveryDiagnosticContainsOnlyClosedRoute(t *testing.T) {
	access := runtimecontract.RuntimeProxyAccess{ProviderAccess: true}
	target := connect.Target{Hostname: "chatgpt.com", Port: 443}
	request := &http.Request{Method: http.MethodGet, URL: &url.URL{Path: "/backend-api/wham/config/bundle", RawQuery: "private=must-not-log"}}
	if got := deniedProviderDiscoveryRoute(request, target, access); got != "CONFIG_BUNDLE" {
		t.Fatalf("closed discovery route=%q", got)
	}
	request.URL.Path = "/private-account-path"
	if got := deniedProviderDiscoveryRoute(request, target, access); got != "" {
		t.Fatalf("unknown path was exposed: %q", got)
	}
	request.URL.Path = "/backend-api/wham/accounts/check"
	access.ProviderAccess = false
	if got := deniedProviderDiscoveryRoute(request, target, access); got != "" {
		t.Fatalf("unverified provider route classified: %q", got)
	}
}

func TestAuthenticatedProxyTerminatesTLSAndForwardsAllowedRequest(t *testing.T) {
	proxyCA, proxyRoots := proxyAuthorityFixture(t)
	upstreamCertificate, upstreamRoots := serverCertificateFixture(t, "example.com")
	access := runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessAllowlistFull, Rules: []runtimecontract.RuntimeWebAccessRule{{
		DomainPattern: "example.com", Protocol: runtimecontract.RuntimeWebProtocolHTTPS, Port: 443,
		HTTPMethods: []string{"DELETE", "GET", "HEAD", "OPTIONS", "PATCH", "POST", "PUT"},
	}}}
	resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute)}}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, err := NewAuthenticated(t.Context(), "unused", authenticatedPolicyStub{access: access}, resolver, dialer, readyStub(true), newTestMetrics(t), proxyCA)
	if err != nil {
		t.Fatal(err)
	}
	server.upstreamRoots = upstreamRoots
	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()
	done := make(chan struct{})
	go func() { defer close(done); defer serverSide.Close(); server.handle(serverSide) }()
	reader := authenticatedConnect(t, clientSide, proxyRoots)

	upstreamResult := make(chan error, 1)
	go func() {
		peer := <-dialer.peers
		defer peer.Close()
		secured := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{upstreamCertificate}})
		request, err := http.ReadRequest(bufio.NewReader(secured))
		if err != nil {
			upstreamResult <- err
			return
		}
		body, err := io.ReadAll(request.Body)
		if err != nil || request.Method != http.MethodPost || request.Host != "example.com" || request.Header.Get("Authorization") != "Bearer fixture" || request.Header.Get("X-Trace") != "kept" || string(body) != "payload" || request.Header.Get("Proxy-Authorization") != "" {
			upstreamResult <- io.ErrUnexpectedEOF
			return
		}
		response := &http.Response{StatusCode: http.StatusCreated, Status: "201 Created", ProtoMajor: 1, ProtoMinor: 1,
			Header: http.Header{"X-Upstream": []string{"ok"}}, Body: io.NopCloser(strings.NewReader("accepted")), ContentLength: 8, Close: true}
		upstreamResult <- response.Write(secured)
	}()

	request, _ := http.NewRequest(http.MethodPost, "https://example.com/resource", strings.NewReader("payload"))
	request.Host = "example.com"
	request.Header.Set("Authorization", "Bearer fixture")
	request.Header.Set("X-Trace", "kept")
	request.Close = true
	if err := request.Write(reader.conn); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(reader.reader, request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusCreated || response.Header.Get("X-Upstream") != "ok" || string(body) != "accepted" {
		t.Fatalf("unexpected proxied response: %d %q", response.StatusCode, body)
	}
	if err := <-upstreamResult; err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("proxy handler did not join")
	}
}

func TestAuthenticatedProxyRejectsWriteBeforeDNSAndDial(t *testing.T) {
	proxyCA, proxyRoots := proxyAuthorityFixture(t)
	access := runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessAllowlistReadOnly, Rules: []runtimecontract.RuntimeWebAccessRule{{
		DomainPattern: "example.com", Protocol: runtimecontract.RuntimeWebProtocolHTTPS, Port: 443,
		HTTPMethods: []string{"GET", "HEAD", "OPTIONS"},
	}}}
	resolver := &fakeResolver{snapshot: dnsresolver.Snapshot{Addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}, ExpiresAt: time.Now().Add(time.Minute)}}
	dialer := &fakeDialer{peers: make(chan net.Conn, 1)}
	server, err := NewAuthenticated(t.Context(), "unused", authenticatedPolicyStub{access: access}, resolver, dialer, readyStub(true), newTestMetrics(t), proxyCA)
	if err != nil {
		t.Fatal(err)
	}
	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()
	done := make(chan struct{})
	go func() { defer close(done); defer serverSide.Close(); server.handle(serverSide) }()
	reader := authenticatedConnect(t, clientSide, proxyRoots)
	request, _ := http.NewRequest(http.MethodPost, "https://example.com/resource", strings.NewReader("denied"))
	request.Host = "example.com"
	if err := request.Write(reader.conn); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(reader.reader, request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusForbidden || resolver.calls != 0 || len(dialer.targets) != 0 {
		t.Fatalf("write crossed proxy policy boundary: status=%d resolver=%d targets=%v", response.StatusCode, resolver.calls, dialer.targets)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("rejected proxy handler did not join")
	}
}

type authenticatedPolicyStub struct {
	access runtimecontract.RuntimeWebAccess
}

func (stub authenticatedPolicyStub) Allows(string, int) bool { return false }
func (stub authenticatedPolicyStub) Limits() internalpolicy.Limits {
	return fakePolicy{}.Limits()
}
func (stub authenticatedPolicyStub) AuthorizeAuthenticated(host string, port int, credential string) (runtimecontract.RuntimeProxyAccess, bool) {
	return runtimecontract.RuntimeProxyAccess{WebAccess: stub.access}, host == "example.com" && port == 443 && credential == "fixture"
}

type securedReader struct {
	conn   *tls.Conn
	reader *bufio.Reader
}

func authenticatedConnect(t *testing.T, connection net.Conn, roots *x509.CertPool) securedReader {
	t.Helper()
	credential := base64.StdEncoding.EncodeToString([]byte("kodex:fixture"))
	_, _ = io.WriteString(connection, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic "+credential+"\r\n\r\n")
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, nil)
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT failed: %v", err)
	}
	secured := tls.Client(connection, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "example.com", NextProtos: []string{"http/1.1"}})
	if err := secured.HandshakeContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	return securedReader{conn: secured, reader: bufio.NewReader(secured)}
}

func proxyAuthorityFixture(t *testing.T) (*TLSInterceptAuthority, *x509.CertPool) {
	t.Helper()
	certificate, key, roots := certificateAuthorityFixture(t, "proxy")
	block, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewTLSInterceptAuthority(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: block}))
	if err != nil {
		t.Fatal(err)
	}
	return authority, roots
}

func serverCertificateFixture(t *testing.T, hostname string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	ca, caKey, roots := certificateAuthorityFixture(t, "upstream")
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: hostname}, DNSNames: []string{hostname}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	raw, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{raw, ca.Raw}, PrivateKey: key}, roots
}

func certificateAuthorityFixture(t *testing.T, name string) (*x509.Certificate, *ecdsa.PrivateKey, *x509.CertPool) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(2 * time.Hour), IsCA: true, KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, _ := x509.ParseCertificate(raw)
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return certificate, key, roots
}
