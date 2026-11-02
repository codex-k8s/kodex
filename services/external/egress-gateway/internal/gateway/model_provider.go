package gateway

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
)

// modelProviderRouting exposes only operator-approved exact routes, never caller-selected destinations.
// modelProviderRouting раскрывает только operator-approved exact routes без caller-selected destinations.
type modelProviderRouting interface {
	// ModelProviderRoute returns only the active configured route for this host.
	// ModelProviderRoute возвращает только active configured route этого host.
	ModelProviderRoute(string) *runtimecontract.ModelProviderRoute
}

// modelProviderRoute obtains a validated immutable route on the static broker listener only.
// modelProviderRoute получает validated immutable route только на static broker listener.
func (server *Server) modelProviderRoute(host string) *runtimecontract.ModelProviderRoute {
	if server.authenticated {
		return nil
	}
	configured, ok := server.policy.(modelProviderRouting)
	if !ok {
		return nil
	}
	return configured.ModelProviderRoute(host)
}

// dialModelProvider bypasses ambient DNS only for validated /32 or /128 operator pins.
// TLS hostname/CA verification remains at the caller; each attempt shares one bounded request lifetime.
// dialModelProvider не использует ambient DNS только для validated /32 или /128 operator pins.
// TLS hostname/CA verification сохраняется у caller; попытки имеют единый bounded request lifetime.
func (server *Server) dialModelProvider(ctx context.Context, route runtimecontract.ModelProviderRoute, timeout time.Duration) (net.Conn, error) {
	if route.Validate() != nil {
		return nil, errors.New("model provider route is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for _, literal := range route.AddressCIDRs {
		prefix, err := netip.ParsePrefix(literal)
		if err != nil || !route.AllowsAddress(prefix.Addr()) {
			return nil, errors.New("model provider address is invalid")
		}
		connection, err := server.dialer.DialContext(ctx, netip.AddrPortFrom(prefix.Addr(), 443))
		if err == nil {
			if ctx.Err() != nil {
				connection.Close()
				break
			}
			return connection, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, errors.New("model provider literal dial failed")
}
