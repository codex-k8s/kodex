package httptransport

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/codex-k8s/kodex/libs/go/emailbridgeapi"
	"github.com/codex-k8s/kodex/services/internal/email-bridge/internal/domain/service/mail"
)

func TestUnavailableSnapshotDoesNotUsePreviousService(t *testing.T) {
	peer, _ := url.Parse(CallerSPIFFE)
	certificate := &x509.Certificate{URIs: []*url.URL{peer}}
	ref := "inv_fixture01"
	binding := &api.ExecutionBinding{InvocationRef: &ref, Lease: api.ExecutionLease{Ref: "lease_fixture01", Fence: "fixture-fence", Generation: 1, ExpiresAt: time.Now().Add(time.Minute)}}
	header, err := api.ExecutionHeaderValue(binding)
	if err != nil {
		t.Fatal(err)
	}
	var current atomic.Pointer[mail.Service]
	previous := &mail.Service{}
	handler := Handler{Service: previous, Current: current.Load}
	for _, active := range []bool{true, false, true} {
		if active {
			current.Store(previous)
		} else {
			current.Store(nil)
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/mailbox-operations", strings.NewReader(`{"operation":"health","mailbox_id":"mailbox"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+binding.Lease.Fence)
		request.Header.Set(api.ExecutionHeader, header)
		request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{certificate}, VerifiedChains: [][]*x509.Certificate{{certificate}}}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		expected := http.StatusServiceUnavailable
		if active {
			expected = http.StatusNotFound
		}
		if response.Code != expected {
			t.Fatalf("snapshot availability status=%d, expected=%d", response.Code, expected)
		}
	}
}
