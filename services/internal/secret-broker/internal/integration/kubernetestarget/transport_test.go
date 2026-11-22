package kubernetestarget

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// transportFixture starts an isolated TLS API with a synthetic CA and pinned loopback address.
// It never uses kubeconfig defaults, external credentials, DNS or an actual Kubernetes cluster.
// transportFixture запускает изолированный TLS API с synthetic CA и закреплённым loopback.
// Не использует kubeconfig defaults, внешние credentials, DNS или настоящий Kubernetes-кластер.
func transportFixture(t *testing.T, certificateIdentity bool) (Credential, *atomic.Int64) {
	t.Helper()
	return transportFixtureWithHandler(t, certificateIdentity, nil)
}

// transportFixtureWithHandler keeps the same real private TLS boundary for protocol-specific fixtures.
// transportFixtureWithHandler сохраняет ту же настоящую private TLS границу для protocol fixtures.
func transportFixtureWithHandler(t *testing.T, certificateIdentity bool, handler http.Handler) (Credential, *atomic.Int64) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("generate synthetic transport key")
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "synthetic API"},
		DNSNames: []string{"api.synthetic.invalid"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatal("create synthetic transport certificate")
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal("encode synthetic transport key")
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	pair, err := tls.X509KeyPair(ca, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}))
	if err != nil {
		t.Fatal("parse synthetic transport pair")
	}
	calls := &atomic.Int64{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.TLS == nil || request.TLS.Version != tls.VersionTLS13 {
			t.Error("transport accepted an unexpected TLS version")
		}
		if certificateIdentity {
			if len(request.TLS.PeerCertificates) != 1 || request.Header.Get("Authorization") != "" {
				t.Error("transport did not preserve the selected certificate identity")
			}
		} else if request.Header.Get("Authorization") != "Bearer synthetic-broker-token" {
			t.Error("transport did not attach its private token")
		}
		if handler != nil {
			handler.ServeHTTP(writer, request)
			return
		}
		if request.URL.Path == "/redirect" {
			writer.Header().Set("Location", "https://foreign.synthetic.invalid/stolen")
			writer.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(writer, "synthetic Kubernetes response")
	}))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}}
	if certificateIdentity {
		server.TLS.ClientAuth = tls.RequireAnyClientCert
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	endpoint, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal("parse synthetic API endpoint")
	}
	hash := sha256.Sum256(ca)
	credential := Credential{target: Target{
		ID: "synthetic-target", Revision: 1, Context: "synthetic", ServerURL: "https://api.synthetic.invalid:" + endpoint.Port(),
		ClusterUID:              "629c395b-32aa-425b-814d-5cd9a9a42da6",
		KubeconfigSourceOrigins: []string{"https://api.synthetic.invalid:" + endpoint.Port()},
		TLSServerName:           "api.synthetic.invalid", CertificateAuthorityHash: hex.EncodeToString(hash[:]), AddressPins: []string{"127.0.0.1"},
	}, ca: ca, token: "synthetic-broker-token"}
	if certificateIdentity {
		credential.token, credential.cert = "", pair
	}
	return credential, calls
}

// TestNewClientPinsTLSIdentityAndNetwork proves real HTTPS for both selected identities without DNS or proxy fallback.
// TestNewClientPinsTLSIdentityAndNetwork проверяет настоящий HTTPS для обеих identity без DNS и proxy fallback.
func TestNewClientPinsTLSIdentityAndNetwork(t *testing.T) {
	for _, certificateIdentity := range []bool{false, true} {
		name := "token"
		if certificateIdentity {
			name = "certificate"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
			t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
			credential, calls := transportFixture(t, certificateIdentity)
			client, closeIdle, err := credential.NewClient(3 * time.Second)
			if err != nil {
				t.Fatal("build isolated target transport")
			}
			defer closeIdle()
			request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, credential.target.ServerURL+"/api", nil)
			if err != nil {
				t.Fatal("build isolated request")
			}
			request.Header = nil
			response, err := client.Do(request)
			if err != nil {
				t.Fatal("pinned HTTPS request failed")
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || string(body) != "synthetic Kubernetes response" || calls.Load() != 1 {
				t.Fatal("unexpected synthetic transport response")
			}
			if request.Header.Get("Authorization") != "" {
				t.Fatal("private Authorization escaped into the caller request")
			}
		})
	}
}

// TestNewClientRejectsRedirectAndAuthorityChanges prevents private identity from following another origin or caller headers.
// TestNewClientRejectsRedirectAndAuthorityChanges запрещает передачу private identity другому origin или caller headers.
func TestNewClientRejectsRedirectAndAuthorityChanges(t *testing.T) {
	credential, calls := transportFixture(t, false)
	client, closeIdle, err := credential.NewClient(3 * time.Second)
	if err != nil {
		t.Fatal("build isolated target transport")
	}
	defer closeIdle()
	response, err := client.Get(credential.target.ServerURL + "/redirect")
	if err == nil || !strings.Contains(err.Error(), invalidRequest) || calls.Load() != 1 {
		t.Fatal("redirect was accepted or lost its closed error")
	}
	if response != nil {
		_ = response.Body.Close()
	}
	for _, mutate := range []func(*http.Request){
		func(request *http.Request) { request.URL.Host = "foreign.synthetic.invalid" },
		func(request *http.Request) { request.URL.Scheme = "http" },
		func(request *http.Request) { request.URL.User = url.User("foreign") },
		func(request *http.Request) { request.Host = "foreign.synthetic.invalid" },
		func(request *http.Request) { request.Header.Set("Authorization", "caller identity") },
		func(request *http.Request) { request.Header.Set("Proxy-Authorization", "caller identity") },
		func(request *http.Request) { request.Header.Set("Impersonate-User", "privileged identity") },
	} {
		request, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, credential.target.ServerURL+"/api", nil)
		if requestErr != nil {
			t.Fatal("build isolated request")
		}
		mutate(request)
		if _, requestErr = client.Do(request); requestErr == nil || !strings.Contains(requestErr.Error(), invalidRequest) {
			t.Fatal("untrusted target request was accepted")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("rejected request reached the target API")
	}
}

// TestNewClientHonorsCancellation keeps a canceled owner lease from reaching the target API.
// TestNewClientHonorsCancellation запрещает обращение к target API после отмены owner lease.
func TestNewClientHonorsCancellation(t *testing.T) {
	credential, calls := transportFixture(t, false)
	client, closeIdle, err := credential.NewClient(3 * time.Second)
	if err != nil {
		t.Fatal("build isolated target transport")
	}
	defer closeIdle()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, credential.target.ServerURL+"/api", nil)
	if err != nil {
		t.Fatal("build canceled request")
	}
	if _, err := client.Do(request); !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatal("canceled owner lease reached the target API")
	}
}
