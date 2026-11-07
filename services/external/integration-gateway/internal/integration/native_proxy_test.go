package integration

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestNativeProxyIsOptionalExactAndHasNoDirectFallback checks disabled compatibility and
// verifies that a failed exact proxy dial cannot dispatch a credential-bearing provider request.
// TestNativeProxyIsOptionalExactAndHasNoDirectFallback проверяет совместимость default и
// отсутствие credential-bearing запроса провайдера после отказа exact proxy dial.
func TestNativeProxyIsOptionalExactAndHasNoDirectFallback(t *testing.T) {
	base := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
	client, err := newNativeHTTPClient("", base, time.Second)
	if err != nil || client != nil {
		t.Fatal("disabled native profile changed default")
	}
	for _, bad := range []string{"http://other.example.invalid:8085", nativeProxyURL + "/", nativeProxyURL + "#fragment", nativeProxyURL + "?query", strings.Replace(nativeProxyURL, "http:", "https:", 1)} {
		if _, err := newNativeHTTPClient(bad, base, time.Second); err == nil {
			t.Fatal("arbitrary native proxy accepted")
		}
	}
	var calls atomic.Int32
	base.DialContext = func(_ context.Context, _, address string) (net.Conn, error) {
		calls.Add(1)
		if address != "egress-gateway-native.kodex-system.svc.cluster.local:8085" {
			t.Error("native proxy fell back to a different peer")
		}
		return nil, errors.New("synthetic proxy unavailable")
	}
	client, err = newNativeHTTPClient(nativeProxyURL, base, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://gitlab.example.invalid/api/v4/user", nil)
	request.Header.Set("Authorization", "Bearer synthetic-only-test-key")
	if response, err := client.Do(request); err == nil || response != nil || calls.Load() != 1 {
		t.Fatal("native proxy failure did not stay closed")
	}
	if base.Proxy != nil || base.TLSClientConfig.ServerName != "" || base.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("native constructor changed shared TLS or transport")
	}
}
