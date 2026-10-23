package runtimecontract

import (
	"errors"
	"net"
	"net/url"
)

// CallbackProfileTrustedCluster identifies the explicit private-Pod callback transport profile; it is not an implicit fallback from protected TLS.
// CallbackProfileTrustedCluster обозначает явный профиль callback-транспорта к частному адресу Pod, а не неявный запасной путь при отказе защищённого TLS.
const CallbackProfileTrustedCluster = "trusted-cluster"

// ValidateCallbackTransport accepts a protected TLS locator or the exact explicit private-IPv4 profile on port 8444; tickets, execution digests and network policy remain separate checks.
// ValidateCallbackTransport принимает защищённый TLS-адрес либо точный явный профиль частного IPv4 на порту 8444; ticket, дайджесты исполнения и сетевая политика проверяются отдельно.
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
