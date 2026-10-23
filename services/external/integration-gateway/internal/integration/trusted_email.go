package integration

import (
	"errors"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

// trustedEmailTransport implements the explicit trusted-cluster profile's plaintext hop to one internal Service on port 443; the logical origin remains fixed HTTPS, and external mail protocols use separate TLS adapters.
// trustedEmailTransport реализует явный профиль trusted-cluster с незашифрованным переходом к одному внутреннему Service на порту 443; логический источник остаётся закреплённым HTTPS, внешние почтовые протоколы используют отдельные TLS-адаптеры.
type trustedEmailTransport struct{ base http.RoundTripper }

// RoundTrip rejects every destination except the exact mailbox POST, clones the request, changes only the internal transport address and profile identity headers, and preserves the execution fence without mutating the original.
// RoundTrip отклоняет все назначения, кроме точного почтового POST, клонирует запрос, меняет адрес внутреннего транспорта и заголовки профиля, сохраняя fence выполнения и исходный запрос.
func (transport trustedEmailTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || request.Method != http.MethodPost ||
		request.URL.Scheme != "https" || request.URL.Host != "email-bridge.kodex-system.svc.cluster.local" ||
		request.URL.Path != "/v1/mailbox-operations" || request.URL.RawPath != "" || request.URL.User != nil ||
		request.URL.RawQuery != "" || request.URL.Fragment != "" || transport.base == nil {
		return nil, errors.New("trusted email destination rejected")
	}
	forwarded := request.Clone(request.Context())
	forwarded.URL.Scheme = "http"
	forwarded.URL.Host = "email-bridge.kodex-system.svc.cluster.local:443"
	forwarded.Host = forwarded.URL.Host
	forwarded.Header.Set("X-Kodex-Rpc-Profile", transportprofile.TrustedCluster)
	forwarded.Header.Set("X-Kodex-Trusted-Caller", "spiffe://kodex.local/ns/kodex-system/sa/integration-gateway")
	return transport.base.RoundTrip(forwarded)
}
