package integration

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

// trustedEmailRecorder stores the last forwarded request for in-memory transport assertions without contacting an email service.
// trustedEmailRecorder сохраняет последний переданный запрос для проверок транспорта в памяти без обращения к почтовому сервису.
type trustedEmailRecorder struct{ request *http.Request }

// RoundTrip records the forwarded request and returns an empty successful JSON response without performing network I/O.
// RoundTrip записывает переданный запрос и возвращает пустой успешный JSON-ответ без сетевого ввода-вывода.
func (recorder *trustedEmailRecorder) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder.request = request
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
}

// TestTrustedEmailTransportIsExactAndPreservesGrant verifies the exact internal route, overwritten profile headers, preserved fence and unchanged original request, rejecting foreign targets before the recorder; it does not prove cluster network isolation.
// TestTrustedEmailTransportIsExactAndPreservesGrant проверяет точный внутренний маршрут, замену заголовков профиля, сохранение fence и исходного запроса, отклоняет чужие назначения до регистратора; сетевую изоляцию кластера не доказывает.
func TestTrustedEmailTransportIsExactAndPreservesGrant(t *testing.T) {
	recorder := &trustedEmailRecorder{}
	transport := trustedEmailTransport{base: recorder}
	request, err := http.NewRequest(http.MethodPost, emailOrigin+"/v1/mailbox-operations", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer synthetic-fence")
	request.Header.Set("X-Kodex-Trusted-Caller", "untrusted-value")
	response, err := transport.RoundTrip(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if recorder.request.URL.String() != "http://email-bridge.kodex-system.svc.cluster.local:443/v1/mailbox-operations" ||
		recorder.request.Header.Get("X-Kodex-Rpc-Profile") != transportprofile.TrustedCluster ||
		recorder.request.Header.Get("Authorization") != "Bearer synthetic-fence" ||
		recorder.request.Header.Get("X-Kodex-Trusted-Caller") != "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway" ||
		request.URL.Scheme != "https" || request.Header.Get("X-Kodex-Trusted-Caller") != "untrusted-value" {
		t.Fatal("trusted email transport contract mismatch")
	}
	for _, url := range []string{"https://example.invalid/v1/mailbox-operations", emailOrigin + "/other",
		"http://email-bridge.kodex-system.svc.cluster.local/v1/mailbox-operations", emailOrigin + "/v1/mailbox-operations?redirect=1"} {
		candidate, err := http.NewRequest(http.MethodPost, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		recorder.request = nil
		if _, err := transport.RoundTrip(candidate); err == nil || recorder.request != nil {
			t.Fatal("noncanonical trusted destination reached transport")
		}
	}
}
