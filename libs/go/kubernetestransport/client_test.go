package kubernetestransport

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewPreservesTransportBoundary checks independent clients, exact TLS settings, timeout,
// proxy absence and both caller-selected redirect errors without contacting Kubernetes.
// TestNewPreservesTransportBoundary проверяет независимые clients, точные TLS settings, timeout,
// отсутствие proxy и обе ошибки redirect вызывающих без обращения к Kubernetes.
func TestNewPreservesTransportBoundary(t *testing.T) {
	roots := x509.NewCertPool()
	first, err := New(roots, "kubernetes.default.svc", 3*time.Second, "kubernetes API redirect is forbidden")
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(roots, "kubernetes.default.svc", 5*time.Second, "Kubernetes API redirect is forbidden")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first.Transport == second.Transport {
		t.Fatal("clients unexpectedly share transport")
	}
	for _, tc := range []struct {
		client  *http.Client
		timeout time.Duration
		message string
	}{{first, 3 * time.Second, "kubernetes API redirect is forbidden"}, {second, 5 * time.Second, "Kubernetes API redirect is forbidden"}} {
		transport, ok := tc.client.Transport.(*http.Transport)
		if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs != roots ||
			transport.TLSClientConfig.ServerName != "kubernetes.default.svc" ||
			transport.TLSClientConfig.MinVersion != tls.VersionTLS13 || transport.TLSClientConfig.InsecureSkipVerify ||
			!transport.ForceAttemptHTTP2 || transport.Proxy != nil || tc.client.Timeout != tc.timeout {
			t.Fatal("Kubernetes transport settings changed")
		}
		if err := tc.client.CheckRedirect(nil, nil); err == nil || err.Error() != tc.message {
			t.Fatal("redirect error changed")
		}
	}
}

// TestNewRejectsWrongTLSNameAndRedirect uses a disposable local TLS server to verify the supplied
// CA and name are enforced and a redirect target is not reached. No cluster or credentials are used.
// TestNewRejectsWrongTLSNameAndRedirect использует локальный TLS server для проверки переданных CA
// и имени и запрета перехода по redirect. Кластер и credentials не используются.
func TestNewRejectsWrongTLSNameAndRedirect(t *testing.T) {
	var targetHits atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		targetHits.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, err := New(roots, "127.0.0.1", 3*time.Second, "kubernetes API redirect is forbidden")
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL + "/redirect")
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "kubernetes API redirect is forbidden") || targetHits.Load() != 0 {
		t.Fatal("redirect was followed or its error was lost")
	}
	wrongName, err := New(roots, "wrong.invalid", 3*time.Second, "kubernetes API redirect is forbidden")
	if err != nil {
		t.Fatal(err)
	}
	defer wrongName.CloseIdleConnections()
	if response, err := wrongName.Get(server.URL + "/target"); err == nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		t.Fatal("wrong TLS server name accepted")
	}
	untrustedCA, err := New(x509.NewCertPool(), "127.0.0.1", 3*time.Second, "kubernetes API redirect is forbidden")
	if err != nil {
		t.Fatal(err)
	}
	defer untrustedCA.CloseIdleConnections()
	if response, err := untrustedCA.Get(server.URL + "/target"); err == nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		t.Fatal("untrusted TLS certificate accepted")
	}
	if targetHits.Load() != 0 {
		t.Fatal("unverified TLS request reached handler")
	}
}

// TestNewRejectsIncompleteConfiguration prevents fallback to system trust or unbounded transport.
// TestNewRejectsIncompleteConfiguration исключает запасное системное доверие и безлимитный transport.
func TestNewRejectsIncompleteConfiguration(t *testing.T) {
	roots := x509.NewCertPool()
	for _, tc := range []struct {
		roots   *x509.CertPool
		name    string
		timeout time.Duration
		message string
	}{
		{nil, "server", time.Second, "redirect"},
		{roots, "", time.Second, "redirect"},
		{roots, "server", 0, "redirect"},
		{roots, "server", -time.Second, "redirect"},
		{roots, "server", time.Second, ""},
	} {
		client, err := New(tc.roots, tc.name, tc.timeout, tc.message)
		if err == nil || client != nil {
			t.Fatal("incomplete transport accepted")
		}
	}
}
