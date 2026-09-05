package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEmailExactTLSAndRedirectBoundary(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, DNSNames: []string{"email-bridge.kodex-system.svc.cluster.local"}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	dir := t.TempDir()
	config := Config{Timeout: time.Second, EmailCAFile: filepath.Join(dir, "ca.pem"), EmailCertificateFile: filepath.Join(dir, "cert.pem"), EmailPrivateKeyFile: filepath.Join(dir, "key.pem")}
	for path, value := range map[string][]byte{config.EmailCAFile: certPEM, config.EmailCertificateFile: certPEM, config.EmailPrivateKeyFile: keyPEM} {
		if err := os.WriteFile(path, value, 0400); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil || r.TLS.ServerName != certificate.DNSNames[0] || len(r.TLS.VerifiedChains) == 0 {
			t.Error("exact mTLS lost")
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://other.example.test", http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}
	server.StartTLS()
	t.Cleanup(server.Close)
	for _, scenario := range []string{"exact", "wrong-name", "wrong-ca", "no-client", "redirect"} {
		t.Run(scenario, func(t *testing.T) {
			client, err := newEmailClient(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(client.CloseIdleConnections)
			transport := client.Transport.(*http.Transport)
			transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}
			path := "/"
			switch scenario {
			case "wrong-name":
				transport.TLSClientConfig.ServerName = "other.example.test"
			case "wrong-ca":
				transport.TLSClientConfig.RootCAs = x509.NewCertPool()
			case "no-client":
				transport.TLSClientConfig.Certificates = nil
			case "redirect":
				path = "/redirect"
			}
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, emailOrigin+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(request)
			if response != nil {
				response.Body.Close()
			}
			if (err == nil) != (scenario == "exact") {
				t.Fatalf("TLS boundary outcome: %v", err)
			}
		})
	}
}
