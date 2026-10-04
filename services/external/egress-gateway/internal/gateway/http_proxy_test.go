package gateway

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
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
	if got := providerDiscoveryRoute(request, target, access); got != "CONFIG_BUNDLE" {
		t.Fatalf("closed discovery route=%q", got)
	}
	request.URL.Path = "/private-account-path"
	if got := providerDiscoveryRoute(request, target, access); got != "" {
		t.Fatalf("unknown path was exposed: %q", got)
	}
	request.URL.Path = "/backend-api/wham/accounts/check"
	access.ProviderAccess = false
	if got := providerDiscoveryRoute(request, target, access); got != "" {
		t.Fatalf("unverified provider route classified: %q", got)
	}
	access.ProviderAccess = true
	target.Port = 8443
	if got := providerDiscoveryRoute(request, target, access); got != "" {
		t.Fatalf("unverified provider port classified: %q", got)
	}
	target.Port = 443
	request.URL.RawPath = "/backend-api/wham/accounts/%63heck"
	if got := providerDiscoveryRoute(request, target, access); got != "" {
		t.Fatalf("encoded provider path classified: %q", got)
	}
}

func TestProviderDiscoveryProxyDiagnosticsAreClosed(t *testing.T) {
	for _, test := range []struct {
		name, path, failure, statusClass string
		status                           int
		resolverFailure, tlsFailure      bool
	}{
		{name: "upstream forbidden", status: http.StatusForbidden, statusClass: "4XX"},
		{name: "upstream unavailable", status: http.StatusServiceUnavailable, statusClass: "5XX"},
		{name: "DNS failure", resolverFailure: true, failure: "DNS"},
		{name: "TLS failure", tlsFailure: true, failure: "TLS"},
		{name: "known route denied", path: "/backend-api/wham/config/bundle", status: http.StatusForbidden},
		{name: "unknown route omitted", path: "/private-account-path", status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			previousWriter, previousFlags, previousPrefix := log.Writer(), log.Flags(), log.Prefix()
			log.SetOutput(&output)
			log.SetFlags(0)
			log.SetPrefix("")
			t.Cleanup(func() { log.SetOutput(previousWriter); log.SetFlags(previousFlags); log.SetPrefix(previousPrefix) })
			certificate, roots := serverCertificateFixture(t, "chatgpt.com")
			if test.tlsFailure {
				_, _, roots = certificateAuthorityFixture(t, "untrusted-fixture")
			}
			_, client, resolver, dialer, done := providerProxyFixture(t, "chatgpt.com", runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessNone}, roots)
			if test.resolverFailure {
				resolver.err = errors.New("private-dns-error-sentinel")
			}
			upstreamResult := make(chan error, 1)
			if test.path == "" && !test.resolverFailure {
				go func() {
					peer := <-dialer.peers
					defer peer.Close()
					_ = peer.SetDeadline(time.Now().Add(2 * time.Second))
					upstream := tls.Server(peer, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{certificate}})
					if test.tlsFailure {
						if err := upstream.Handshake(); err == nil {
							upstreamResult <- errors.New("untrusted TLS fixture accepted")
						} else {
							upstreamResult <- nil
						}
						return
					}
					request, err := http.ReadRequest(bufio.NewReader(upstream))
					if err != nil {
						upstreamResult <- err
						return
					}
					_ = request.Body.Close()
					body := "private-upstream-body-sentinel"
					response := &http.Response{StatusCode: test.status, ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{"X-Private": []string{"private-response-header-sentinel"}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Close: true}
					upstreamResult <- response.Write(upstream)
				}()
			}
			path := test.path
			if path == "" {
				path = "/backend-api/wham/accounts/check"
			}
			request, err := http.NewRequest(http.MethodGet, "https://chatgpt.com"+path+"?private-query-sentinel=private-identity-sentinel", nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer private-request-header-sentinel")
			if err := request.Write(client.conn); err != nil {
				t.Fatal(err)
			}
			response, err := http.ReadResponse(client.reader, request)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			wantStatus := test.status
			if test.failure != "" {
				wantStatus = http.StatusBadGateway
			}
			if response.StatusCode != wantStatus {
				t.Fatalf("proxy status=%d, want %d", response.StatusCode, wantStatus)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("diagnostic proxy handler did not join")
			}
			if test.path == "" && !test.resolverFailure {
				if err := <-upstreamResult; err != nil {
					t.Fatal(err)
				}
			} else if len(dialer.targets) != 0 {
				t.Fatal("failed DNS or denied route crossed dial boundary")
			}
			want := ""
			switch {
			case test.path == "/backend-api/wham/config/bundle":
				want = fmt.Sprintf(runtimeProxyProviderDiscoveryLog, "CONFIG_BUNDLE", "POLICY", "DENIED", "NONE", "NONE") + "\n"
			case test.path == "":
				want = fmt.Sprintf(runtimeProxyProviderDiscoveryLog, "ACCOUNTS_CHECK", "POLICY", "ALLOWED", "NONE", "NONE") + "\n"
				if test.failure != "" {
					want += fmt.Sprintf(runtimeProxyProviderDiscoveryLog, "ACCOUNTS_CHECK", "UPSTREAM", "FAILED", "NONE", test.failure) + "\n"
				} else {
					want += fmt.Sprintf(runtimeProxyProviderDiscoveryLog, "ACCOUNTS_CHECK", "UPSTREAM", "RESPONSE", test.statusClass, "NONE") + "\n"
				}
			}
			if output.String() != want {
				t.Fatal("provider diagnostic differs from closed event sequence or exposes private fixture data")
			}
		})
	}
}

func TestProviderDiscoveryFailureAndStatusClassesRemainClosed(t *testing.T) {
	for _, test := range []struct {
		err       error
		tlsFailed bool
		want      string
	}{
		{proxyUpstreamDNSFailure, false, "DNS"},
		{fmt.Errorf("private-wrapped-sentinel: %w", proxyUpstreamDialFailure), false, "DIAL"},
		{fmt.Errorf("private-cancel-sentinel: %w", context.Canceled), true, "CANCELLED"},
		{context.DeadlineExceeded, true, "TIMEOUT"},
		{errors.New("private-tls-error-sentinel"), true, "TLS"},
		{errors.New("TLS DNS private-error-sentinel"), false, "UNKNOWN"},
		{proxyUpstreamFailure("private-unknown-kind-sentinel"), false, "UNKNOWN"},
	} {
		if got := providerDiscoveryFailure(test.err, test.tlsFailed); got != test.want {
			t.Fatalf("failure class=%q, want %q", got, test.want)
		}
	}
	for _, test := range []struct {
		status int
		want   string
	}{{99, "UNKNOWN"}, {100, "1XX"}, {199, "1XX"}, {200, "2XX"}, {299, "2XX"}, {300, "3XX"}, {399, "3XX"}, {400, "4XX"}, {499, "4XX"}, {500, "5XX"}, {599, "5XX"}, {600, "UNKNOWN"}} {
		if got := providerDiscoveryStatusClass(test.status); got != test.want {
			t.Fatalf("status class=%q, want %q", got, test.want)
		}
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
