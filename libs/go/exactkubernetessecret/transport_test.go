package exactkubernetessecret

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestKubernetesHTTPClientPinsProtectedTransport checks exact TLS name, roots, protocol floor, timeouts, connection limits and redirect rejection without accessing Kubernetes or credentials.
// TestKubernetesHTTPClientPinsProtectedTransport проверяет точные TLS-имя, корни, минимальный протокол, таймауты, лимиты соединений и запрет перенаправлений без доступа к Kubernetes и реквизитам.
func TestKubernetesHTTPClientPinsProtectedTransport(t *testing.T) {
	t.Parallel()
	roots := x509.NewCertPool()
	client := kubernetesHTTPClientForRoots(roots, 3*time.Second)
	defer client.CloseIdleConnections()
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil {
		t.Fatal("exact API transport is absent")
	}
	settings := transport.TLSClientConfig
	if settings.MinVersion != tls.VersionTLS13 || settings.ServerName != KubernetesServerName || settings.RootCAs != roots || settings.InsecureSkipVerify {
		t.Fatal("exact API TLS binding changed")
	}
	if transport.Proxy != nil || transport.DialTLSContext != nil || !transport.ForceAttemptHTTP2 ||
		transport.MaxIdleConns != 4 || transport.MaxIdleConnsPerHost != 2 || transport.IdleConnTimeout != 30*time.Second || client.Timeout != 3*time.Second {
		t.Fatal("exact API transport limits or proxy policy changed")
	}
	if client.CheckRedirect == nil {
		t.Fatal("exact API redirects are not checked")
	}
	if err := client.CheckRedirect(&http.Request{}, nil); err == nil || err.Error() != "exact Kubernetes API redirect is forbidden" {
		t.Fatal("exact API redirect was not rejected")
	}
}

// TestKubernetesHTTPClientsKeepSeparateTransportState prevents the shared factory from introducing a singleton client, mutable TLS configuration or timeout coupling.
// TestKubernetesHTTPClientsKeepSeparateTransportState запрещает общей фабрике создавать singleton-клиент, общие изменяемые TLS-настройки и связанность таймаутов.
func TestKubernetesHTTPClientsKeepSeparateTransportState(t *testing.T) {
	t.Parallel()
	first := kubernetesHTTPClientForRoots(x509.NewCertPool(), time.Second)
	second := kubernetesHTTPClientForRoots(x509.NewCertPool(), 10*time.Second)
	defer first.CloseIdleConnections()
	defer second.CloseIdleConnections()
	firstTransport := first.Transport.(*http.Transport)
	secondTransport := second.Transport.(*http.Transport)
	if first == second || firstTransport == secondTransport || firstTransport.TLSClientConfig == secondTransport.TLSClientConfig {
		t.Fatal("exact API clients share mutable transport state")
	}
	firstTransport.TLSClientConfig.ServerName = "changed.invalid"
	first.Timeout = 2 * time.Second
	if secondTransport.TLSClientConfig.ServerName != KubernetesServerName || second.Timeout != 10*time.Second {
		t.Fatal("exact API client mutation affected another instance")
	}
}

// TestExactClientConstructorsRejectInvalidConfigurationBeforeTrustRead requires both constructors to return configuration errors before trying to load mounted CA material.
// TestExactClientConstructorsRejectInvalidConfigurationBeforeTrustRead требует от обоих конструкторов ошибки конфигурации до попытки чтения смонтированного CA.
func TestExactClientConstructorsRejectInvalidConfigurationBeforeTrustRead(t *testing.T) {
	t.Parallel()
	if _, err := New(Config{ResourceName: "../foreign", DataKey: "state", Timeout: time.Second}); err == nil || err.Error() != "exact Kubernetes Secret configuration is invalid" {
		t.Fatal("single-key constructor crossed configuration boundary")
	}
	for _, config := range []MapConfig{
		{ResourceName: "../foreign", AllowedDataKeys: []string{"state"}, Timeout: time.Second},
		{ResourceName: "registered", AllowedDataKeys: []string{"state", "state"}, Timeout: time.Second},
		{ResourceName: "registered", AllowedDataKeys: []string{"state"}, Timeout: 11 * time.Second},
	} {
		if _, err := NewMap(config); err == nil || err.Error() != "exact Kubernetes Secret map configuration is invalid" {
			t.Fatal("map constructor crossed configuration boundary")
		}
	}
	if _, err := NewMap(MapConfig{ResourceName: "registered", AllowedDataKeys: []string{generationDataKey}, Timeout: time.Second}); err == nil || err.Error() != "exact Kubernetes Secret map key is invalid" {
		t.Fatal("caller selected internal generation key")
	}
}

// TestKubernetesSecretRequestPreservesExactEndpointAndContext checks the fixed URL, method, synthetic bearer header, body and cancellation without sending any network request.
// TestKubernetesSecretRequestPreservesExactEndpointAndContext проверяет фиксированный URL, метод, синтетический bearer-заголовок, тело и отмену без отправки сетевого запроса.
func TestKubernetesSecretRequestPreservesExactEndpointAndContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	request, err := newKubernetesSecretRequest(ctx, http.MethodPut, "registered.snapshot", "synthetic-token", bytes.NewReader([]byte("fixture-body")))
	if err != nil {
		t.Fatal("exact request construction failed")
	}
	defer request.Body.Close()
	if request.URL.String() != KubernetesAPIAddress+"/api/v1/namespaces/kodex-system/secrets/registered.snapshot" || request.Method != http.MethodPut ||
		request.Header.Get("Authorization") != "Bearer synthetic-token" || request.Header.Get("Accept") != "application/json" || request.Context() != ctx {
		t.Fatal("exact request binding changed")
	}
	body, err := io.ReadAll(request.Body)
	if err != nil || string(body) != "fixture-body" {
		t.Fatal("exact request body changed")
	}
	cancel()
	if request.Context().Err() != context.Canceled {
		t.Fatal("exact request lost cancellation")
	}
	if _, err := newKubernetesSecretRequest(t.Context(), "invalid method", "registered", "synthetic-token", nil); err == nil {
		t.Fatal("invalid HTTP method accepted")
	}
}
