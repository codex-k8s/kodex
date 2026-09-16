package callback

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/jobs/agent-runner/internal/model"
)

// TransportForInput используется одинаково callback client и MCP readiness.
// Plaintext разрешён только явно выбранному exact Pod endpoint, без proxy.
func TransportForInput(input model.Input) (*http.Transport, error) {
	if input.CallbackTLS.Profile == "" {
		return exactTransport(input.CallbackTLS)
	}
	if input.CallbackTLS.Profile != runtimecontract.CallbackProfileTrustedCluster || input.ValidateCallbackTransport() != nil {
		return nil, errors.New("trusted callback transport configuration rejected")
	}
	endpoint, _ := url.Parse(input.CallbackURL)
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != endpoint.Host {
				return nil, errors.New("trusted callback destination rejected")
			}
			return dialer.DialContext(ctx, network, address)
		},
		DisableCompression: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2,
		ResponseHeaderTimeout: 30 * time.Second, MaxResponseHeaderBytes: 16 << 10,
	}, nil
}
