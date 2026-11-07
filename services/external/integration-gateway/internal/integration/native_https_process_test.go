package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// nativeHTTPSCertificate creates a short-lived synthetic CA/leaf for a fixed test DNS name.
// No system trust, live credential or corporate endpoint is used by the qualification.
// nativeHTTPSCertificate создаёт краткоживущие synthetic CA/leaf для фиксированного test DNS.
// Проверка не использует system trust, живые credentials либо корпоративные endpoints.
func nativeHTTPSCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Synthetic Native Test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"gitlab.example.invalid"}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: private}))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})) {
		t.Fatal("fixture CA rejected")
	}
	return certificate, roots
}

// TestNativeHTTPSProcessKeepsTypedAdapterCredentialsAndTLS exercises actual Execute→HTTPS
// client→CONNECT gateway→literal dial→TLS API in separate binaries. CA/SNI and redirect failures
// remain closed; the fixture key is never sent to an untrusted TLS peer or alternate transport.
// TestNativeHTTPSProcessKeepsTypedAdapterCredentialsAndTLS проверяет настоящий Execute→HTTPS
// client→CONNECT gateway→literal dial→TLS API в отдельных binaries. Отказ CA/SNI и redirect
// остаётся закрытым; fixture key не уходит недоверенному TLS peer либо через другой transport.
func TestNativeHTTPSProcessKeepsTypedAdapterCredentialsAndTLS(t *testing.T) {
	binary := os.Getenv("KODEX_TEST_NATIVE_GATEWAY_BINARY")
	if binary == "" {
		t.Skip("run make test-native-integration-egress for the cross-process qualification")
	}
	info, err := os.Lstat(binary)
	if err != nil || !info.Mode().IsRegular() || !filepath.IsAbs(binary) {
		t.Fatal("exact native test binary required")
	}
	certificate, roots := nativeHTTPSCertificate(t)
	var calls atomic.Int32
	var redirect atomic.Bool
	api := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Host != "gitlab.example.invalid" || r.TLS == nil || r.TLS.ServerName != "gitlab.example.invalid" || r.URL.EscapedPath() != "/api/v4/projects/group%2Fproject" {
			t.Error("typed provider request escaped expected TLS/auth/resource binding")
			w.WriteHeader(403)
			return
		}
		if r.Header.Get("Authorization") != "Bearer owner-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if redirect.Load() {
			w.Header().Set("Location", "https://other.example.invalid/stolen")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":42,"path_with_namespace":"group/project","default_branch":"main","visibility":"private","archived":false}`)
	}))
	api.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}
	api.Config.ErrorLog = log.New(io.Discard, "", 0)
	api.StartTLS()
	defer api.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	command := exec.CommandContext(ctx, binary, "-test.run=^TestNativeGatewayProcessFixture$", "-test.timeout=25s")
	command.Dir = "../../../egress-gateway/internal/gateway"
	command.Env = []string{"KODEX_TEST_NATIVE_GATEWAY_PROCESS=synthetic-only"}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer cancel()
		_ = stdin.Close()
		if err := command.Wait(); err != nil {
			t.Errorf("native gateway fixture failed: %v %s", err, diagnostics.String())
		}
	})
	if err := json.NewEncoder(stdin).Encode(map[string]string{"upstream": api.Listener.Addr().String()}); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	if err != nil {
		t.Fatal("native fixture did not publish listener", err)
	}
	var listener struct{ Listen, Digest string }
	if json.Unmarshal(line, &listener) != nil || len(listener.Digest) != 64 {
		t.Fatal("invalid native fixture readback")
	}
	address, err := net.ResolveTCPAddr("tcp", listener.Listen)
	if err != nil || !address.IP.IsLoopback() {
		t.Fatal("native fixture is not local")
	}
	adapter := testAdapter(t)
	credential := testCredential(t, adapter, "owner-token")
	base := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, TLSHandshakeTimeout: 2 * time.Second}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "egress-gateway-native.kodex-system.svc.cluster.local:8085" {
			t.Error("native HTTPS escaped fixed proxy")
		}
		return (&net.Dialer{}).DialContext(ctx, network, listener.Listen)
	}
	adapter.nativeHTTPClient, err = newNativeHTTPClient(nativeProxyURL, base, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.nativeHTTPClient.CloseIdleConnections()
	invocation := invocationRequest(t, adapter.definitions["gitlab"], "gitlab.project.metadata.read", map[string]any{}, credential)
	invocation.Configuration["base_url"] = "https://gitlab.example.invalid"
	capability, _ := adapter.definitions["gitlab"].Capability(invocation.CapabilityKey)
	invocation.ResourceScope, err = capability.ResourceScopeValues(map[string]string{"base_url": "https://gitlab.example.invalid", "project_path": "group/project"})
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := json.Marshal(invocation.ResourceScope)
	scopeDigest := sha256.Sum256(scope)
	invocation.ResourceScopeDigest = hex.EncodeToString(scopeDigest[:])
	result, err := adapter.Execute(ctx, invocation)
	if err != nil || !strings.Contains(result.Summary, `"id":42`) || calls.Load() != 1 {
		t.Fatalf("typed native HTTPS request failed: %v %s", err, result.Summary)
	}
	// A separately verified user key cannot turn an untrusted certificate into a valid API peer.
	// Отдельно проверенный ключ пользователя не превращает недоверенный сертификат в допустимый API peer.
	untrusted := base.Clone()
	untrusted.TLSClientConfig.RootCAs = x509.NewCertPool()
	adapter.nativeHTTPClient, err = newNativeHTTPClient(nativeProxyURL, untrusted, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Execute(ctx, invocation); err == nil || calls.Load() != 1 {
		t.Fatal("untrusted TLS reached credential-bearing API")
	}
	adapter.nativeHTTPClient.CloseIdleConnections()
	adapter.nativeHTTPClient, err = newNativeHTTPClient(nativeProxyURL, base, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	redirect.Store(true)
	before := calls.Load()
	if _, err := adapter.Execute(ctx, invocation); err == nil || calls.Load() <= before {
		t.Fatal("provider redirect was followed or not exercised")
	}
	adapter.nativeHTTPClient.CloseIdleConnections()
	redirect.Store(false)
	invocation.Credential = testCredential(t, adapter, "synthetic-revoked-key")
	before = calls.Load()
	if _, err := adapter.Execute(ctx, invocation); err == nil || calls.Load() != before+1 {
		t.Fatal("rejected native key retried or silently fell back")
	}
	adapter.nativeHTTPClient.CloseIdleConnections()
}
