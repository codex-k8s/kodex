package runtimecontract

import (
	"errors"
	"net"
	"net/url"
)

const CallbackProfileTrustedCluster = "trusted-cluster"

// ValidateCallbackTransport проверяет server-owned locator. Private Pod IP и
// единственный callback port не разрешают произвольный внешний plaintext URL.
// Ticket, execution digests и NetworkPolicy проверяются отдельными границами.
func (input RunnerInput) ValidateCallbackTransport() error {
	if input.CallbackTLS.Profile == "" {
		if input.CallbackTLS.validate() == nil && validCallbackURL(input.CallbackURL, input.CallbackTLS.ServerName) {
			return nil
		}
		return errors.New("protected runtime callback transport is invalid")
	}
	if input.CallbackTLS != (RuntimeTLSBinding{Profile: CallbackProfileTrustedCluster}) {
		return errors.New("runtime callback profile is invalid")
	}
	parsed, err := url.Parse(input.CallbackURL)
	if err != nil || len(input.CallbackURL) > 512 || parsed.Scheme != "http" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Port() != "8444" {
		return errors.New("trusted runtime callback endpoint is invalid")
	}
	address := net.ParseIP(parsed.Hostname())
	if address == nil || address.To4() == nil || !address.IsPrivate() || address.IsLoopback() || address.IsUnspecified() ||
		parsed.Host != net.JoinHostPort(address.String(), "8444") {
		return errors.New("trusted runtime callback Pod address is invalid")
	}
	return nil
}
