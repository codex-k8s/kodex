package component

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// httpsFixture starts a loopback handler requiring fixture-signed client TLS
// and returns a client trusting that same CA; cleanup closes idle clients before the server.
// httpsFixture запускает локальный обработчик с обязательным клиентским TLS стенда
// и возвращает клиент с тем же CA; очистка закрывает простаивающий клиент раньше сервера.
func httpsFixture(t *testing.T, f *providerFixture, handler http.Handler) (*httptest.Server, *http.Client) {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	ca := x509.NewCertPool()
	ca.AppendCertsFromPEM(f.ca)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{f.cert}, ClientCAs: ca, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: ca, Certificates: []tls.Certificate{f.cert}, MinVersion: tls.VersionTLS12}}}
	t.Cleanup(client.CloseIdleConnections)
	return server, client
}

// TestHTTPSFixtureRequiresTrustedPeers verifies the extracted fixture accepts
// its client but never reaches the handler with missing/foreign client certificates or a foreign CA.
// TestHTTPSFixtureRequiresTrustedPeers проверяет вход клиента выделенного стенда
// и недоступность обработчика без клиентского сертификата, с чужим сертификатом или чужим CA.
func TestHTTPSFixtureRequiresTrustedPeers(t *testing.T) {
	f := &providerFixture{}
	f.cert, f.ca = certificate(t)
	var calls atomic.Int32
	server, client := httpsFixture(t, f, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNoContent || calls.Load() != 1 {
		t.Fatal("trusted fixture client did not reach handler")
	}
	foreign, foreignCA := certificate(t)
	for _, kind := range []string{"missing-client", "foreign-client", "foreign-ca"} {
		t.Run(kind, func(t *testing.T) {
			config := client.Transport.(*http.Transport).TLSClientConfig.Clone()
			switch kind {
			case "missing-client":
				config.Certificates = nil
			case "foreign-client":
				config.Certificates = []tls.Certificate{foreign}
			case "foreign-ca":
				config.RootCAs = x509.NewCertPool()
				config.RootCAs.AppendCertsFromPEM(foreignCA)
			}
			other := &http.Client{Transport: &http.Transport{TLSClientConfig: config}}
			t.Cleanup(other.CloseIdleConnections)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := other.Do(request)
			if response != nil {
				response.Body.Close()
			}
			if err == nil || calls.Load() != 1 {
				t.Fatal("untrusted fixture peer reached handler")
			}
		})
	}
}
