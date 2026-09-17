package integration

import (
	"errors"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
)

// Logical origin утверждён IntegrationDefinition и не зависит от транспорта.
// Только эта exact внутренняя операция получает plaintext dial к Service:443.
// Внешние SMTP/IMAP/POP3 и provider HTTP используют другие TLS adapters.
type trustedEmailTransport struct{ base http.RoundTripper }

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
